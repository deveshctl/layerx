package image

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/moby/moby/client"
)

const MaxViewSize = 1 << 20 // 1 MB

// FileContent holds the result of extracting a file from an image.
type FileContent struct {
	Path      string
	Data      []byte
	Size      int64 // actual file size (may exceed len(Data) if truncated)
	Truncated bool
	Binary    bool
}

// Extractor retrieves file contents from a container image.
type Extractor interface {
	Extract(ctx context.Context, imageRef string, filePath string) (*FileContent, error)
	ExtractRaw(ctx context.Context, imageRef string, filePath string) ([]byte, error)
	// ExtractFromLayer returns the file as it exists at the given layer.
	// It walks back from layerCursor toward layer 0 to find the most recent
	// layer that physically contains the path. A whiteout encountered during
	// the walk-back stops the search and returns an error.
	ExtractFromLayer(ctx context.Context, imageRef string, filePath string, layerCursor int) (*FileContent, error)
	// ExtractRawFromLayer is the raw-bytes variant of ExtractFromLayer; same
	// walk-back semantics, no truncation, no binary detection.
	ExtractRawFromLayer(ctx context.Context, imageRef string, filePath string, layerCursor int) ([]byte, error)
}

// IsBinary reports whether data appears to be binary content.
// Trusts net/http content detection for text/* (covering UTF-8/16/32 with BOMs)
// and only falls back to null-byte scanning when detection is inconclusive.
func IsBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	ct := http.DetectContentType(data)
	if strings.HasPrefix(ct, "text/") {
		return false
	}
	// DetectContentType returns application/octet-stream when it cannot
	// classify the bytes. Only in that case do we fall through to the
	// null-byte heuristic — trusting non-text classifications (image/*,
	// application/pdf, etc.) directly avoids overriding correct results.
	if ct != "application/octet-stream" {
		return true
	}
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

// processContent applies binary detection and size limits to raw file data.
func processContent(path string, data []byte, totalSize int64) *FileContent {
	fc := &FileContent{
		Path: path,
		Size: totalSize,
	}

	if len(data) == 0 {
		return fc
	}

	sniff := data
	if len(sniff) > 512 {
		sniff = sniff[:512]
	}
	if IsBinary(sniff) {
		fc.Binary = true
		return fc
	}

	if len(data) > MaxViewSize {
		fc.Data = data[:MaxViewSize]
		fc.Truncated = true
	} else {
		fc.Data = data
	}
	return fc
}

// DockerExtractor extracts files from images via the Docker daemon.
type DockerExtractor struct {
	cli client.APIClient
}

// NewDockerExtractor creates an extractor using the provided Docker client.
func NewDockerExtractor(cli client.APIClient) *DockerExtractor {
	return &DockerExtractor{cli: cli}
}

// Extract creates a temporary container, copies the file out, and removes the container.
func (e *DockerExtractor) Extract(ctx context.Context, imageRef string, filePath string) (*FileContent, error) {
	createResult, err := e.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Image: imageRef,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create container for extraction: %w", err)
	}
	containerID := createResult.ID

	defer func() {
		removeCtx := context.WithoutCancel(ctx)
		_, _ = e.cli.ContainerRemove(removeCtx, containerID, client.ContainerRemoveOptions{Force: true})
	}()

	copyResult, err := e.cli.CopyFromContainer(ctx, containerID, client.CopyFromContainerOptions{
		SourcePath: filePath,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to copy %s from container: %w", filePath, err)
	}
	defer copyResult.Content.Close()

	totalSize := copyResult.Stat.Size

	data, err := readFirstFileFromTar(copyResult.Content, totalSize)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", filePath, err)
	}

	return processContent(filePath, data, totalSize), nil
}

// ExtractRaw extracts a file's raw bytes without truncation or binary detection.
func (e *DockerExtractor) ExtractRaw(ctx context.Context, imageRef string, filePath string) ([]byte, error) {
	createResult, err := e.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Image: imageRef,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create container for extraction: %w", err)
	}
	containerID := createResult.ID

	defer func() {
		removeCtx := context.WithoutCancel(ctx)
		_, _ = e.cli.ContainerRemove(removeCtx, containerID, client.ContainerRemoveOptions{Force: true})
	}()

	copyResult, err := e.cli.CopyFromContainer(ctx, containerID, client.CopyFromContainerOptions{
		SourcePath: filePath,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to copy %s from container: %w", filePath, err)
	}
	defer copyResult.Content.Close()

	return readFullFileFromTar(copyResult.Content)
}

// readFirstFileFromTar reads the first regular file from a tar stream.
// Docker's CopyFromContainer wraps the file in a single-entry tar.
func readFirstFileFromTar(r io.Reader, expectedSize int64) ([]byte, error) {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("no file found in tar stream")
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}

		limit := int64(MaxViewSize + 1)
		if expectedSize > 0 && expectedSize < limit {
			limit = expectedSize
		}
		data, err := io.ReadAll(io.LimitReader(tr, limit))
		if err != nil {
			return nil, err
		}
		return data, nil
	}
}

// readFullFileFromTar reads the first regular file from a tar stream without size limits.
func readFullFileFromTar(r io.Reader) ([]byte, error) {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("no file found in tar stream")
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		return io.ReadAll(tr)
	}
}

// errWhiteoutStop signals that the requested path was whiteouted in a layer
// encountered during walk-back. The caller surfaces this as a "removed in
// layer" error rather than continuing the walk.
var errWhiteoutStop = errors.New("path removed by whiteout")

// loadLayerTars exports the image via ImageSave and returns the ordered
// manifest.Layers list along with raw (possibly gzipped) tar bytes for
// only the first maxLayers entries.
//
// Memory bound: the ImageSave stream is spooled to a temp file (cleaned up
// before return). After that, only the requested layers' bytes are held in
// memory, so peak heap is on the order of the largest needed blob — not the
// whole image. This matters for the TUI extraction path where mashing keys
// can fire concurrent calls; without the cap an 8 GB image would peak at
// 8 GB of heap per call.
//
// maxLayers must be > 0 and is clamped to len(manifest.Layers).
func (e *DockerExtractor) loadLayerTars(ctx context.Context, imageRef string, maxLayers int) ([]string, map[string][]byte, error) {
	rc, err := e.cli.ImageSave(ctx, []string{imageRef})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to export image %s: %w", imageRef, err)
	}
	defer rc.Close()

	spool, err := os.CreateTemp("", "layerx-extract-*.tar")
	if err != nil {
		return nil, nil, fmt.Errorf("creating temp spool: %w", err)
	}
	spoolPath := spool.Name()
	defer os.Remove(spoolPath)
	defer spool.Close()

	if _, err := io.Copy(spool, rc); err != nil {
		return nil, nil, fmt.Errorf("spooling image archive: %w", err)
	}

	// Pass 1: parse manifest only. Blob bodies are streamed past via
	// io.Copy(io.Discard, tr) — no allocation per blob.
	manifestData, err := readManifestFromSpool(spool)
	if err != nil {
		return nil, nil, err
	}

	var manifests []dockerManifest
	if err := json.Unmarshal(manifestData, &manifests); err != nil {
		return nil, nil, fmt.Errorf("invalid image archive: cannot parse manifest: %w", err)
	}
	if len(manifests) == 0 {
		return nil, nil, fmt.Errorf("invalid image archive: empty manifest")
	}
	layerPaths := manifests[0].Layers

	if maxLayers <= 0 {
		return nil, nil, fmt.Errorf("loadLayerTars: maxLayers must be > 0, got %d", maxLayers)
	}
	keepCount := min(maxLayers, len(layerPaths))
	keep := make(map[string]struct{}, keepCount)
	for _, p := range layerPaths[:keepCount] {
		keep[p] = struct{}{}
	}

	// Pass 2: read only blobs in the keep-set.
	blobs, err := readBlobsFromSpool(spool, keep)
	if err != nil {
		return nil, nil, err
	}

	return layerPaths, blobs, nil
}

// readManifestFromSpool walks the spooled outer tar from the beginning and
// returns the manifest.json bytes. Non-manifest entries are streamed past
// without buffering.
func readManifestFromSpool(spool *os.File) ([]byte, error) {
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek spool: %w", err)
	}
	tr := tar.NewReader(spool)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("invalid image archive: manifest.json not found")
		}
		if err != nil {
			return nil, fmt.Errorf("reading image archive: %w", err)
		}
		if hdr.Name == "manifest.json" {
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("reading manifest.json: %w", err)
			}
			return data, nil
		}
	}
}

// readBlobsFromSpool walks the spooled outer tar from the beginning and
// returns a map of name -> raw bytes for entries whose name is in keep.
// Entries not in keep are streamed past without buffering.
func readBlobsFromSpool(spool *os.File, keep map[string]struct{}) (map[string][]byte, error) {
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek spool: %w", err)
	}
	blobs := make(map[string][]byte, len(keep))
	tr := tar.NewReader(spool)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading image archive: %w", err)
		}
		if _, want := keep[hdr.Name]; !want {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", hdr.Name, err)
		}
		blobs[hdr.Name] = data
	}
	return blobs, nil
}

// findFileInLayer walks one layer's tar bytes (decompressing if gzipped) and
// looks for filePath as a regular file. Whiteouts are detected and signaled
// separately so the walk-back can stop.
//
// Returns:
//   - (data,  true,  nil)              when the file is found
//   - (nil,   false, nil)              when the file is not in this layer
//   - (nil,   false, errWhiteoutStop)  when the file (or an ancestor) is whiteouted
func findFileInLayer(layerBytes []byte, filePath string) ([]byte, bool, error) {
	r, err := decompressIfGzip(layerBytes)
	if err != nil {
		return nil, false, fmt.Errorf("decompressing layer: %w", err)
	}

	// filePath is already cleaned (no leading slash) by the caller.

	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, fmt.Errorf("reading layer tar: %w", err)
		}

		name := strings.TrimPrefix(hdr.Name, "./")

		// Regular whiteout (.wh.<seg>) on the file itself or on any ancestor
		// directory. A tar entry "<dir>/.wh.<seg>" deletes "<dir>/<seg>" and
		// everything beneath it, per the overlay-fs whiteout convention.
		// Names beginning with ".wh..wh." are reserved control entries
		// (e.g. ".wh..wh..opq") and must not be treated as regular whiteouts.
		if deleted, ok := regularWhiteoutTarget(name); ok {
			if filePath == deleted || strings.HasPrefix(filePath, deleted+"/") {
				return nil, false, errWhiteoutStop
			}
		}

		// Opaque whiteout in any ancestor of the path
		if strings.HasSuffix(name, "/.wh..wh..opq") || name == ".wh..wh..opq" {
			ancestor := strings.TrimSuffix(name, ".wh..wh..opq")
			ancestor = strings.TrimSuffix(ancestor, "/")
			if ancestor == "" || strings.HasPrefix(filePath, ancestor+"/") || filePath == ancestor {
				return nil, false, errWhiteoutStop
			}
		}

		if name != filePath {
			continue
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			// Symlink, hardlink, dir entry — let walk-back continue.
			return nil, false, nil
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, false, fmt.Errorf("reading file from layer: %w", err)
		}
		return data, true, nil
	}
}

// regularWhiteoutTarget returns the path that a regular whiteout tar entry
// deletes. For an entry "<dir>/.wh.<seg>" the deleted path is "<dir>/<seg>";
// for ".wh.<seg>" at the root it is "<seg>". Returns ok=false for entries
// that are not regular whiteouts, including opaque whiteouts and any other
// reserved ".wh..wh.*" control entries.
func regularWhiteoutTarget(name string) (string, bool) {
	if idx := strings.LastIndex(name, "/.wh."); idx >= 0 {
		ancestorDir := name[:idx]
		segment := name[idx+len("/.wh."):]
		if segment == "" || strings.Contains(segment, "/") || strings.HasPrefix(segment, ".wh.") {
			return "", false
		}
		return ancestorDir + "/" + segment, true
	}
	if strings.HasPrefix(name, ".wh.") && !strings.HasPrefix(name, ".wh..wh.") {
		segment := name[len(".wh."):]
		if segment == "" || strings.Contains(segment, "/") {
			return "", false
		}
		return segment, true
	}
	return "", false
}

// ExtractFromLayer extracts a file as it exists at the given layer cursor.
// It walks back from layerCursor toward layer 0 looking for the most recent
// layer whose tar contains the path as a regular file. A whiteout encountered
// during the walk-back stops the search and returns an error.
func (e *DockerExtractor) ExtractFromLayer(ctx context.Context, imageRef string, filePath string, layerCursor int) (*FileContent, error) {
	if layerCursor < 0 {
		return nil, fmt.Errorf("invalid layer index %d", layerCursor)
	}
	layerPaths, blobs, err := e.loadLayerTars(ctx, imageRef, layerCursor+1)
	if err != nil {
		return nil, err
	}
	if layerCursor >= len(layerPaths) {
		return nil, fmt.Errorf("layer index %d out of range (have %d)", layerCursor, len(layerPaths))
	}

	cleanPath := strings.TrimPrefix(filePath, "/")

	for j := layerCursor; j >= 0; j-- {
		blob := blobs[layerPaths[j]]
		if blob == nil {
			continue
		}
		data, found, err := findFileInLayer(blob, cleanPath)
		if errors.Is(err, errWhiteoutStop) {
			return nil, fmt.Errorf("file %s was removed in layer %d", filePath, j)
		}
		if err != nil {
			return nil, err
		}
		if found {
			return processContent(filePath, data, int64(len(data))), nil
		}
	}
	return nil, fmt.Errorf("file %s not found in any layer up to %d", filePath, layerCursor)
}

// ExtractRawFromLayer is the raw-bytes variant of ExtractFromLayer.
// No truncation, no binary detection. Used by save-to-disk.
func (e *DockerExtractor) ExtractRawFromLayer(ctx context.Context, imageRef string, filePath string, layerCursor int) ([]byte, error) {
	if layerCursor < 0 {
		return nil, fmt.Errorf("invalid layer index %d", layerCursor)
	}
	layerPaths, blobs, err := e.loadLayerTars(ctx, imageRef, layerCursor+1)
	if err != nil {
		return nil, err
	}
	if layerCursor >= len(layerPaths) {
		return nil, fmt.Errorf("layer index %d out of range (have %d)", layerCursor, len(layerPaths))
	}

	cleanPath := strings.TrimPrefix(filePath, "/")

	for j := layerCursor; j >= 0; j-- {
		blob := blobs[layerPaths[j]]
		if blob == nil {
			continue
		}
		data, found, err := findFileInLayer(blob, cleanPath)
		if errors.Is(err, errWhiteoutStop) {
			return nil, fmt.Errorf("file %s was removed in layer %d", filePath, j)
		}
		if err != nil {
			return nil, err
		}
		if found {
			return data, nil
		}
	}
	return nil, fmt.Errorf("file %s not found in any layer up to %d", filePath, layerCursor)
}
