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
	"slices"
	"strings"

	"github.com/moby/moby/client"
)

const MaxViewSize = 1 << 20 // 1 MB

// MaxSaveSize bounds save-to-disk extraction. The viewer caps reads at
// MaxViewSize, but the save path historically called io.ReadAll with no
// limit — a 10 GB file inside a layer (or a maliciously crafted tar entry
// with an inflated header.Size) would OOM the process. 2 GiB is generous
// enough for legitimate single-file extracts and well below typical RAM.
const MaxSaveSize = 2 << 30 // 2 GiB

// MaxLayerBlobSize bounds a single layer-tar blob loaded from a spooled
// image archive. Distinct from MaxSaveSize because layer blobs aggregate
// many files (apt caches, ML model directories, full base-image userlands)
// and legitimately exceed the per-file cap. 16 GiB is permissive enough for
// production ML images while still rejecting a malformed manifest that
// claims a single blob is petabytes.
const MaxLayerBlobSize = 16 << 30 // 16 GiB

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
	if slices.Contains(data, 0) {
		return true
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
// Non-regular entries (directories, symlinks, hardlinks, devices, fifos) are
// skipped — the contract is "read the first *regular file* in the stream".
func readFirstFileFromTar(r io.Reader, expectedSize int64) ([]byte, error) {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("no file found in tar stream")
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg {
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

// readFullFileFromTar reads the first regular file from a tar stream.
// Bounded by MaxSaveSize to protect against OOM on huge tar entries; the
// header is consulted first as a fast-fail path, and io.LimitReader caps
// the worst case where the header understates the actual stream length.
// Non-regular entries (directories, symlinks, hardlinks, devices, fifos) are
// skipped — the contract is "read the first *regular file* in the stream".
func readFullFileFromTar(r io.Reader) ([]byte, error) {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("no file found in tar stream")
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if hdr.Size > MaxSaveSize {
			return nil, fmt.Errorf("file too large to save: %d bytes (limit %d)", hdr.Size, MaxSaveSize)
		}
		data, err := io.ReadAll(io.LimitReader(tr, MaxSaveSize+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > MaxSaveSize {
			return nil, fmt.Errorf("file too large to save: exceeds %d bytes", MaxSaveSize)
		}
		return data, nil
	}
}

// blobLoader returns the bytes of a single layer blob by name. Implementations
// read on-demand from the underlying archive (spool file or on-disk archive)
// so peak heap is one blob, not the sum of every blob up to the layer cursor.
// A blob that does not exist returns (nil, nil) — walkBackForFile treats
// "missing blob" the same as "blob whose tar contains nothing for the path"
// and continues the walk-back.
type blobLoader func(name string) ([]byte, error)

// walkBackForFile walks layerPaths from layerCursor toward 0 looking for
// cleanPath as a regular file. The first layer that contains it returns
// (data, nil); a whiteout or non-regular entry encountered before any regular
// hit returns a typed error. cleanPath must already be cleaned by the caller
// (no leading slash). Shared by DockerExtractor and ArchiveExtractor — the
// only difference between the two is how they obtain layerPaths and blobs.
//
// load is invoked lazily, once per layer index visited, so the largest blob
// resident in memory at any moment is the single blob currently being
// scanned. This is the load-bearing memory bound for images with many large
// layers (e.g. ML model images): the prior eager-load implementation pinned
// every kept blob until the function returned, which OOMed on multi-GB
// images.
func walkBackForFile(layerPaths []string, load blobLoader, cleanPath, displayPath string, layerCursor int) ([]byte, error) {
	for j := layerCursor; j >= 0; j-- {
		blob, err := load(layerPaths[j])
		if err != nil {
			return nil, err
		}
		if blob == nil {
			continue
		}
		data, found, err := findFileInLayer(blob, cleanPath)
		if errors.Is(err, errWhiteoutStop) {
			return nil, fmt.Errorf("file %s was removed in layer %d", displayPath, j)
		}
		if errors.Is(err, errPathNotRegular) {
			return nil, fmt.Errorf("file %s is not a regular file at layer %d", displayPath, j)
		}
		if err != nil {
			return nil, err
		}
		if found {
			return data, nil
		}
	}
	return nil, fmt.Errorf("file %s not found in any layer up to %d", displayPath, layerCursor)
}

// errWhiteoutStop signals that the requested path was whiteouted in a layer
// encountered during walk-back. The caller surfaces this as a "removed in
// layer" error rather than continuing the walk.
var errWhiteoutStop = errors.New("path removed by whiteout")

// errPathNotRegular signals that the requested path exists in this layer
// but is not a regular file (symlink, hardlink, or directory). The caller
// must surface this as a typed error instead of falling through to older
// layers — at this layer the path's effective state is the symlink/dir,
// not whatever older regular file used to live there.
var errPathNotRegular = errors.New("path exists but is not a regular file")

// loadLayerSource exports the image via ImageSave, spools it to a temp file,
// parses the manifest, and returns:
//   - layerPaths  : the manifest's ordered Layers list
//   - load        : a blobLoader that reads one blob at a time from the spool
//   - closeFn     : releases and removes the spool; the caller MUST defer it
//
// Memory bound: peak heap during a walk-back is one blob — the largest blob
// the walk happens to read — plus the manifest. The prior implementation
// pre-read every blob up to the layer cursor into a map, which OOMed on
// multi-GB ML-model images and on TUI key-mash sequences that fired
// concurrent extracts.
//
// maxLayers must be > 0; it bounds which manifest entries are eligible for
// load. A request for an entry outside that prefix returns (nil, nil) —
// indistinguishable from "manifest entry missing", because both are skipped
// by walkBackForFile.
func (e *DockerExtractor) loadLayerSource(ctx context.Context, imageRef string, maxLayers int) (layerPaths []string, load blobLoader, closeFn func(), err error) {
	if maxLayers <= 0 {
		return nil, nil, nil, fmt.Errorf("loadLayerSource: maxLayers must be > 0, got %d", maxLayers)
	}

	rc, err := e.cli.ImageSave(ctx, []string{imageRef})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to export image %s: %w", imageRef, err)
	}
	defer rc.Close()

	spool, err := os.CreateTemp("", "layerx-extract-*.tar")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("creating temp spool: %w", err)
	}
	spoolPath := spool.Name()

	// closeFn is the unified teardown so partial-failure paths below can call
	// it once and return (nil, nil, nil, err) without leaking.
	cleanup := func() {
		_ = spool.Close()
		_ = os.Remove(spoolPath)
	}

	if _, err := copyCtx(ctx, spool, rc); err != nil {
		cleanup()
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, nil, nil, err
		}
		return nil, nil, nil, fmt.Errorf("spooling image archive: %w", err)
	}

	manifestData, err := readManifestFromSpool(spool)
	if err != nil {
		cleanup()
		return nil, nil, nil, err
	}
	var manifests []dockerManifest
	if err := json.Unmarshal(manifestData, &manifests); err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("invalid image archive: cannot parse manifest: %w", err)
	}
	if len(manifests) == 0 {
		cleanup()
		return nil, nil, nil, fmt.Errorf("invalid image archive: empty manifest")
	}
	layerPaths = manifests[0].Layers

	keepCount := min(maxLayers, len(layerPaths))
	keep := make(map[string]struct{}, keepCount)
	for _, p := range layerPaths[:keepCount] {
		keep[p] = struct{}{}
	}

	idx, err := scanBlobIndex(spool)
	if err != nil {
		cleanup()
		return nil, nil, nil, err
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
			return nil, fmt.Errorf("layer blob %s too large: %d bytes (limit %d)", name, size, MaxLayerBlobSize)
		}
		return readSingleBlobFromSpool(spool, name)
	}
	return layerPaths, load, cleanup, nil
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
		if errors.Is(err, io.EOF) {
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

// readSingleBlobFromSpool walks the spooled outer tar from the beginning and
// returns the bytes of the entry whose name equals wanted, or (nil, nil) if
// the spool does not contain that entry. Other entries are streamed past
// without buffering.
//
// The blob is bounded by MaxLayerBlobSize. A blob whose tar header declares
// a size above the cap fails fast; an io.LimitReader on the body catches
// streams whose declared size understates the actual length. The cap is
// generous (16 GiB) — legitimate ML / dataset layers can exceed 1 GiB —
// while still preventing runaway allocation from a malformed or hostile
// archive.
func readSingleBlobFromSpool(spool *os.File, wanted string) ([]byte, error) {
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek spool: %w", err)
	}
	tr := tar.NewReader(spool)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("reading image archive: %w", err)
		}
		if hdr.Name != wanted {
			continue
		}
		if hdr.Size > MaxLayerBlobSize {
			return nil, fmt.Errorf("layer blob %s too large: %d bytes (limit %d)", wanted, hdr.Size, MaxLayerBlobSize)
		}
		data, err := io.ReadAll(io.LimitReader(tr, MaxLayerBlobSize+1))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", wanted, err)
		}
		if int64(len(data)) > MaxLayerBlobSize {
			return nil, fmt.Errorf("layer blob %s too large: stream exceeds %d bytes", wanted, MaxLayerBlobSize)
		}
		return data, nil
	}
}

// blobIndex maps a blob name to its declared header.Size. The lazy loader
// uses the size to early-reject oversized blobs before opening a tar reader,
// and to short-circuit "blob is in the manifest but missing from the spool"
// vs "blob exists but exceeds the cap" with distinct errors.
//
// Offset tracking is intentionally NOT included: tar.Reader does not expose
// the post-header file position in a stable way across stdlib versions, and
// rebuilding it via spool.Seek(SeekCurrent) after Next() is fragile (the
// reader internally buffers). The index is the cheap-but-sufficient win:
// one full scan to build the map, and from then on each load reseeks and
// scans header-only until it finds the wanted name. Header scan is two
// orders of magnitude cheaper than re-reading bodies, so an N-layer extract
// does N header scans + 1 body read, not N body reads.
type blobIndex map[string]int64

// scanBlobIndex walks the spooled outer tar once, recording every entry's
// declared size. Bodies are streamed past via io.Copy(io.Discard, tr) so
// peak memory during the scan is the tar reader's internal buffer (~tens
// of KB), not the sum of layer bodies.
func scanBlobIndex(spool *os.File) (blobIndex, error) {
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek spool: %w", err)
	}
	idx := make(blobIndex)
	tr := tar.NewReader(spool)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return idx, nil
		}
		if err != nil {
			return nil, fmt.Errorf("indexing image archive: %w", err)
		}
		idx[hdr.Name] = hdr.Size
	}
}

// findFileInLayer walks one layer's tar bytes (decompressing if gzipped) and
// looks for filePath as a regular file. Whiteouts and non-regular-file
// matches are surfaced as typed errors so the walk-back can stop.
//
// Returns:
//   - (data,  true,  nil)                  when the file is found
//   - (nil,   false, nil)                  when the file is not in this layer
//   - (nil,   false, errWhiteoutStop)      when the path (or ancestor) is whiteouted
//   - (nil,   false, errPathNotRegular)    when the path exists at this layer
//     but is a symlink, hardlink, or directory — not the regular file the
//     caller can read. The caller must NOT fall through to older layers; the
//     effective state at this layer is the non-regular entry.
func findFileInLayer(layerBytes []byte, filePath string) ([]byte, bool, error) {
	r, err := decompressIfGzip(layerBytes)
	if err != nil {
		return nil, false, fmt.Errorf("decompressing layer: %w", err)
	}
	defer r.Close()

	// filePath is already cleaned (no leading slash) by the caller.

	// Cap the decompressed byte count walked by the tar reader. Without this
	// a crafted gzip stream (tiny compressed input, huge expanded output) can
	// make tar.Reader consume unbounded bytes when skipping between entries —
	// the per-entry MaxSaveSize check below only bounds reads of the matched
	// file, not the walk itself. MaxLayerBlobSize matches the ceiling used
	// when loading layer blobs from a spooled image archive.
	tr := tar.NewReader(io.LimitReader(r, MaxLayerBlobSize+1))

	// Walk the full layer once. A regular entry for filePath in the same
	// layer wins over an earlier-positioned whiteout for the same path —
	// overlayfs upper-layer semantics permit `.wh.X` followed by `X` in
	// one layer (the readd shadows the deletion). Recording state and
	// resolving at end of walk mirrors the opaque-whiteout handling in
	// Stack.
	var (
		whiteoutHit  bool
		nonRegular   error
		regularData  []byte
		foundRegular bool
	)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, false, fmt.Errorf("reading layer tar: %w", err)
		}

		name := cleanTarPath(hdr.Name)
		if name == "" {
			continue
		}

		// Regular whiteout (.wh.<seg>) on the file itself or on any ancestor
		// directory. A tar entry "<dir>/.wh.<seg>" deletes "<dir>/<seg>" and
		// everything beneath it, per the overlay-fs whiteout convention.
		// Names beginning with ".wh..wh." are reserved control entries
		// (e.g. ".wh..wh..opq") and must not be treated as regular whiteouts.
		if deleted, ok := regularWhiteoutTarget(name); ok {
			if filePath == deleted || strings.HasPrefix(filePath, deleted+"/") {
				whiteoutHit = true
			}
			continue
		}

		// Opaque whiteout in any ancestor of the path. Per overlayfs
		// convention "<dir>/.wh..wh..opq" clears the *contents* of <dir>,
		// not <dir> itself — so a query for <dir> exactly must NOT be
		// reported as removed. The regular-whiteout branch above handles
		// the "delete the directory entry" case via .wh.<seg>.
		if strings.HasSuffix(name, "/.wh..wh..opq") || name == ".wh..wh..opq" {
			ancestor := strings.TrimSuffix(name, ".wh..wh..opq")
			ancestor = strings.TrimSuffix(ancestor, "/")
			if ancestor == "" || strings.HasPrefix(filePath, ancestor+"/") {
				whiteoutHit = true
			}
			continue
		}

		if name != filePath {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			nonRegular = fmt.Errorf("%w: typeflag %d", errPathNotRegular, hdr.Typeflag)
			continue
		}

		// Bound the read so a crafted tar (header.Size inflated, or stream
		// longer than declared) can't OOM the process. Mirrors the cap in
		// readFullFileFromTar — fail-fast on header, then LimitReader as
		// belt-and-braces for streams that exceed their declared size.
		if hdr.Size > MaxSaveSize {
			return nil, false, fmt.Errorf("file too large to extract: %d bytes (limit %d)", hdr.Size, MaxSaveSize)
		}
		data, err := io.ReadAll(io.LimitReader(tr, MaxSaveSize+1))
		if err != nil {
			return nil, false, fmt.Errorf("reading file from layer: %w", err)
		}
		if int64(len(data)) > MaxSaveSize {
			return nil, false, fmt.Errorf("file too large to extract: exceeds %d bytes", MaxSaveSize)
		}
		regularData = data
		foundRegular = true
	}

	if foundRegular {
		return regularData, true, nil
	}
	if nonRegular != nil {
		return nil, false, nonRegular
	}
	if whiteoutHit {
		return nil, false, errWhiteoutStop
	}
	return nil, false, nil
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
	layerPaths, load, closeFn, err := e.loadLayerSource(ctx, imageRef, layerCursor+1)
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
// No truncation, no binary detection. Used by save-to-disk.
func (e *DockerExtractor) ExtractRawFromLayer(ctx context.Context, imageRef string, filePath string, layerCursor int) ([]byte, error) {
	if layerCursor < 0 {
		return nil, fmt.Errorf("invalid layer index %d", layerCursor)
	}
	layerPaths, load, closeFn, err := e.loadLayerSource(ctx, imageRef, layerCursor+1)
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
