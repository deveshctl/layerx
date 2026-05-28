package image

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ArchiveResolver resolves image layers from a local image archive on disk
// (docker save / OCI layout tarball) without contacting any container runtime.
//
// The archive is opened lazily on each method call rather than held open by
// the resolver. This keeps the resolver cheap to construct and lets the
// caller move or delete the file between calls (the active call still
// finishes against the file handle it opened).
type ArchiveResolver struct {
	path string
}

// NewArchiveResolver creates a resolver that reads from the archive at path.
// The path is not validated here; errors surface when methods are called
// (so the constructor cannot fail and Resolve / Inspect / ImageID share a
// single file-open code path with consistent error wrapping).
func NewArchiveResolver(path string) *ArchiveResolver {
	return &ArchiveResolver{path: path}
}

// openArchive opens path read-only, mapping fs errors to typed errors so the
// caller (TUI, CLI) can render a tailored message: ErrArchiveNotFound for
// missing paths, ErrArchivePermission for EACCES, and a wrapped infra error
// for everything else (I/O failures, too many open files, etc).
func openArchive(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		switch {
		case os.IsNotExist(err):
			return nil, &ErrArchiveNotFound{Path: path, Cause: err}
		case os.IsPermission(err):
			return nil, &ErrArchivePermission{Path: path, Cause: err}
		default:
			return nil, &ErrArchiveInfra{Op: fmt.Sprintf("open archive %q", path), Cause: err}
		}
	}
	return f, nil
}

// Resolve reads the archive and returns the parsed layer list.
func (r *ArchiveResolver) Resolve(ctx context.Context, imageRef string) ([]Layer, error) {
	return r.ResolveWithProgress(ctx, imageRef, nil)
}

// ResolveWithProgress reads the archive, emitting Parsing progress. There is
// no Pulling phase (no daemon, no network) and no Exporting phase (no
// ImageSave call) — archive mode goes straight to parse.
func (r *ArchiveResolver) ResolveWithProgress(ctx context.Context, imageRef string, progress chan<- ProgressEvent) ([]Layer, error) {
	f, err := openArchive(r.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	emitProgress(progress, ProgressEvent{Phase: PhaseParsing})

	layers, err := parseLayers(f)
	if err != nil {
		// Infrastructure failures (temp file, disk full) keep their original
		// shape so the user sees the real cause; only content-class errors
		// get wrapped as "not a valid image archive".
		if _, ok := errors.AsType[*ErrArchiveInfra](err); ok {
			return nil, err
		}
		return nil, &ErrInvalidArchive{Path: r.path, Cause: err}
	}
	return layers, nil
}

// Inspect returns lightweight metadata (total declared layer size) by
// scanning the outer tar headers once. Does not read layer bodies.
//
// "Total layer size" is summed strictly over the entries listed in the
// manifest's Layers — manifest.json, the config blob, and any unrelated
// root-level JSON are excluded. This matches what users mean by "image
// size" and what the spinner / status bar displays.
func (r *ArchiveResolver) Inspect(ctx context.Context, imageRef string) (*ImageMeta, error) {
	f, err := openArchive(r.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	manifestData, _, headers, err := scanResolveMetadata(f)
	if err != nil {
		return nil, &ErrInvalidArchive{Path: r.path, Cause: err}
	}
	if manifestData == nil {
		return nil, &ErrInvalidArchive{Path: r.path, Cause: errors.New("manifest.json not found")}
	}

	var manifests []dockerManifest
	if err := json.Unmarshal(manifestData, &manifests); err != nil {
		return nil, &ErrInvalidArchive{Path: r.path, Cause: fmt.Errorf("cannot parse manifest: %w", err)}
	}
	if len(manifests) == 0 {
		return nil, &ErrInvalidArchive{Path: r.path, Cause: errors.New("empty manifest")}
	}

	var total int64
	for _, layerPath := range manifests[0].Layers {
		total += headers[layerPath]
	}
	return &ImageMeta{Size: total}, nil
}

// ImageID returns the image's content digest, derived from the config blob's
// bytes. This is the same identity used by Docker and OCI registries: the
// sha256 of the JSON config file referenced by manifest.json. Two archives
// with identical image content (same config + same layers) share an ImageID
// regardless of where they live on disk or how the tar was rebuilt; two
// archives that differ in any layer or config field do not.
//
// For OCI layouts the config path embeds the digest (blobs/sha256/<hex>) and
// we use it directly. For legacy docker-save layouts the filename stem
// (<hex>.json) is conventionally the digest but is not authoritative — a
// rewritten tar can keep the filename while changing the bytes — so we hash
// the config blob itself.
func (r *ArchiveResolver) ImageID(ctx context.Context, imageRef string) (string, error) {
	f, err := openArchive(r.path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	manifestData, rootJSON, _, err := scanResolveMetadata(f)
	if err != nil {
		return "", &ErrInvalidArchive{Path: r.path, Cause: err}
	}
	if manifestData == nil {
		return "", &ErrInvalidArchive{Path: r.path, Cause: errors.New("manifest.json not found")}
	}

	var manifests []dockerManifest
	if err := json.Unmarshal(manifestData, &manifests); err != nil {
		return "", &ErrInvalidArchive{Path: r.path, Cause: fmt.Errorf("cannot parse manifest: %w", err)}
	}
	if len(manifests) == 0 {
		return "", &ErrInvalidArchive{Path: r.path, Cause: errors.New("empty manifest")}
	}

	cfg := manifests[0].Config
	// OCI layout: blobs/sha256/<hex>. Path *is* the content digest, no need
	// to re-hash.
	if rest, ok := strings.CutPrefix(cfg, "blobs/sha256/"); ok && rest != "" && !strings.Contains(rest, "/") {
		return "sha256:" + rest, nil
	}

	// Legacy docker-save: read the config blob from the tar and hash its
	// bytes. The blob is at the root of the outer tar and was captured by
	// scanResolveMetadata in rootJSON.
	cfgBytes, ok := rootJSON[cfg]
	if !ok {
		return "", &ErrInvalidArchive{Path: r.path, Cause: fmt.Errorf("config blob %q referenced by manifest not found in archive", cfg)}
	}
	sum := sha256.Sum256(cfgBytes)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// NewExtractor returns an ArchiveExtractor bound to this resolver's archive
// path. Implements ExtractorSource so the TUI's file viewer and save-to-disk
// features work in archive mode.
func (r *ArchiveResolver) NewExtractor() Extractor {
	return &ArchiveExtractor{path: r.path}
}

// ArchiveExtractor extracts file content from layers in a local image archive.
// Reuses findFileInLayer (daemon-independent) for the per-layer search.
type ArchiveExtractor struct {
	path string
}

// Extract is provided for interface completeness. The TUI uses
// ExtractFromLayer; full-image extract via container-create has no archive
// equivalent, so we route to the layer-walk over all layers.
func (e *ArchiveExtractor) Extract(ctx context.Context, imageRef string, filePath string) (*FileContent, error) {
	n, err := e.layerCount()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, fmt.Errorf("archive has no layers")
	}
	return e.ExtractFromLayer(ctx, imageRef, filePath, n-1)
}

// ExtractRaw is the raw-bytes variant of Extract, same routing.
func (e *ArchiveExtractor) ExtractRaw(ctx context.Context, imageRef string, filePath string) ([]byte, error) {
	n, err := e.layerCount()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, fmt.Errorf("archive has no layers")
	}
	return e.ExtractRawFromLayer(ctx, imageRef, filePath, n-1)
}

// layerCount opens the archive and reads only the manifest to discover how
// many layers it has. Cheaper than loadLayerTars when the caller just needs
// the count to compute a cursor.
func (e *ArchiveExtractor) layerCount() (int, error) {
	f, err := openArchive(e.path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	manifestData, err := readManifestFromSpool(f)
	if err != nil {
		return 0, &ErrInvalidArchive{Path: e.path, Cause: err}
	}
	var manifests []dockerManifest
	if err := json.Unmarshal(manifestData, &manifests); err != nil {
		return 0, &ErrInvalidArchive{Path: e.path, Cause: fmt.Errorf("cannot parse manifest: %w", err)}
	}
	if len(manifests) == 0 {
		return 0, &ErrInvalidArchive{Path: e.path, Cause: errors.New("empty manifest")}
	}
	return len(manifests[0].Layers), nil
}

// ExtractFromLayer mirrors DockerExtractor.ExtractFromLayer: walk back from
// layerCursor toward layer 0, return the file from the most recent layer
// whose tar contains it as a regular entry, or surface a typed whiteout /
// non-regular error if encountered first.
func (e *ArchiveExtractor) ExtractFromLayer(ctx context.Context, imageRef string, filePath string, layerCursor int) (*FileContent, error) {
	if layerCursor < 0 {
		return nil, fmt.Errorf("invalid layer index %d", layerCursor)
	}
	layerPaths, load, closeFn, err := e.loadLayerSource(layerCursor + 1)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	if layerCursor >= len(layerPaths) {
		return nil, fmt.Errorf("layer index %d out of range (have %d)", layerCursor, len(layerPaths))
	}

	cleanPath := cleanTarPath(filePath)
	if cleanPath == "" {
		return nil, fmt.Errorf("invalid file path: %s", filePath)
	}

	data, err := walkBackForFile(layerPaths, load, cleanPath, filePath, layerCursor)
	if err != nil {
		return nil, err
	}
	return processContent(filePath, data, int64(len(data))), nil
}

// ExtractRawFromLayer is the raw-bytes variant of ExtractFromLayer.
func (e *ArchiveExtractor) ExtractRawFromLayer(ctx context.Context, imageRef string, filePath string, layerCursor int) ([]byte, error) {
	if layerCursor < 0 {
		return nil, fmt.Errorf("invalid layer index %d", layerCursor)
	}
	layerPaths, load, closeFn, err := e.loadLayerSource(layerCursor + 1)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	if layerCursor >= len(layerPaths) {
		return nil, fmt.Errorf("layer index %d out of range (have %d)", layerCursor, len(layerPaths))
	}

	cleanPath := cleanTarPath(filePath)
	if cleanPath == "" {
		return nil, fmt.Errorf("invalid file path: %s", filePath)
	}

	return walkBackForFile(layerPaths, load, cleanPath, filePath, layerCursor)
}

// loadLayerSource opens the archive and returns the manifest's Layers slice
// plus a lazy blob loader scoped to the first maxLayers entries. The caller
// MUST defer closeFn to release the file handle.
//
// Memory bound mirrors DockerExtractor.loadLayerSource: peak heap is one
// blob (the largest one walkBackForFile happens to read), not the sum of
// every blob up to layerCursor. maxLayers must be > 0 — Extract / ExtractRaw
// discover the layer count via layerCount() first so they never need an
// "all layers" sentinel.
func (e *ArchiveExtractor) loadLayerSource(maxLayers int) (layerPaths []string, load blobLoader, closeFn func(), err error) {
	if maxLayers <= 0 {
		return nil, nil, nil, fmt.Errorf("loadLayerSource: maxLayers must be > 0, got %d", maxLayers)
	}
	f, err := openArchive(e.path)
	if err != nil {
		return nil, nil, nil, err
	}
	cleanup := func() { _ = f.Close() }

	manifestData, err := readManifestFromSpool(f)
	if err != nil {
		cleanup()
		return nil, nil, nil, &ErrInvalidArchive{Path: e.path, Cause: err}
	}

	var manifests []dockerManifest
	if err := json.Unmarshal(manifestData, &manifests); err != nil {
		cleanup()
		return nil, nil, nil, &ErrInvalidArchive{Path: e.path, Cause: fmt.Errorf("cannot parse manifest: %w", err)}
	}
	if len(manifests) == 0 {
		cleanup()
		return nil, nil, nil, &ErrInvalidArchive{Path: e.path, Cause: errors.New("empty manifest")}
	}
	layerPaths = manifests[0].Layers

	keepCount := min(maxLayers, len(layerPaths))
	keep := make(map[string]struct{}, keepCount)
	for _, p := range layerPaths[:keepCount] {
		keep[p] = struct{}{}
	}

	idx, err := scanBlobIndex(f)
	if err != nil {
		cleanup()
		return nil, nil, nil, &ErrInvalidArchive{Path: e.path, Cause: err}
	}

	load = func(name string) ([]byte, error) {
		if _, ok := keep[name]; !ok {
			return nil, nil
		}
		size, present := idx[name]
		if !present {
			return nil, nil
		}
		if size > MaxLayerBlobSize {
			return nil, &ErrInvalidArchive{Path: e.path, Cause: fmt.Errorf("layer blob %s too large: %d bytes (limit %d)", name, size, MaxLayerBlobSize)}
		}
		data, err := readSingleBlobFromSpool(f, name)
		if err != nil {
			return nil, &ErrInvalidArchive{Path: e.path, Cause: err}
		}
		return data, nil
	}
	return layerPaths, load, cleanup, nil
}
