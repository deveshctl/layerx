package image

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/moby/moby/client"
)

// Option configures a DockerResolver.
type Option func(*DockerResolver)

// WithClient injects a Docker API client (used for testing).
func WithClient(cli client.APIClient) Option {
	return func(r *DockerResolver) { r.cli = cli }
}

// DockerResolver resolves image layers via the Docker daemon.
type DockerResolver struct {
	cli client.APIClient
}

// NewDockerResolver creates a resolver connected to the local Docker daemon.
func NewDockerResolver(opts ...Option) (Resolver, error) {
	r := &DockerResolver{}
	for _, opt := range opts {
		opt(r)
	}
	if r.cli == nil {
		cli, err := client.New(
			client.FromEnv,
			client.WithAPIVersionNegotiation(),
		)
		if err != nil {
			return nil, fmt.Errorf("cannot connect to Docker daemon: %w", err)
		}
		r.cli = cli
	}
	return r, nil
}

// Inspect returns lightweight image metadata without exporting the full tar.
// It does not pull the image — if the image is not local, it returns an error.
func (r *DockerResolver) Inspect(ctx context.Context, imageRef string) (*ImageMeta, error) {
	inspect, err := r.cli.ImageInspect(ctx, imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect image %s: %w", imageRef, err)
	}

	return &ImageMeta{Size: inspect.Size}, nil
}

// ImageID returns the image content digest from the Docker daemon. It does
// not pull; the caller is expected to ensure the image is local first
// (AnalyzeWithProgress does this).
func (r *DockerResolver) ImageID(ctx context.Context, imageRef string) (string, error) {
	inspect, err := r.cli.ImageInspect(ctx, imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to inspect image %s: %w", imageRef, err)
	}
	return inspect.ID, nil
}

// NewExtractor creates an Extractor using this resolver's Docker client.
func (r *DockerResolver) NewExtractor() Extractor {
	return NewDockerExtractor(r.cli)
}

// Resolve fetches the image, exports it as a tar, and parses the layer list.
func (r *DockerResolver) Resolve(ctx context.Context, imageRef string) ([]Layer, error) {
	return r.ResolveWithProgress(ctx, imageRef, nil)
}

// ResolveWithProgress fetches the image with progress reporting via the channel.
func (r *DockerResolver) ResolveWithProgress(ctx context.Context, imageRef string, progress chan<- ProgressEvent) ([]Layer, error) {
	if err := r.ensureImageWithProgress(ctx, imageRef, progress); err != nil {
		return nil, err
	}

	emitProgress(progress, ProgressEvent{Phase: PhaseExporting})

	rc, err := r.cli.ImageSave(ctx, []string{imageRef})
	if err != nil {
		return nil, fmt.Errorf("failed to export image %s: %w", imageRef, err)
	}
	defer rc.Close()

	emitProgress(progress, ProgressEvent{Phase: PhaseParsing})

	return parseLayers(rc)
}

// ensureImageWithProgress checks if the image exists locally; if not, pulls it with progress.
func (r *DockerResolver) ensureImageWithProgress(ctx context.Context, imageRef string, progress chan<- ProgressEvent) error {
	f := make(client.Filters).Add("reference", imageRef)
	result, err := r.cli.ImageList(ctx, client.ImageListOptions{Filters: f})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return &ErrDaemonNotRunning{Cause: err}
	}

	if len(result.Items) > 0 {
		return nil
	}

	emitProgress(progress, ProgressEvent{Phase: PhasePulling})

	rc, err := r.cli.ImagePull(ctx, imageRef, client.ImagePullOptions{})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return &ErrPullFailed{Ref: imageRef, Cause: err}
	}
	defer rc.Close()

	if progress != nil {
		if err := r.streamPullProgress(ctx, rc, progress); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return &ErrPullFailed{Ref: imageRef, Cause: err}
		}
	} else {
		if _, err := io.Copy(io.Discard, rc); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return &ErrPullFailed{Ref: imageRef, Cause: err}
		}
	}
	return nil
}

// streamPullProgress reads JSON pull events and sends progress updates.
// Returns the first stream error encountered, or nil on clean EOF.
func (r *DockerResolver) streamPullProgress(ctx context.Context, rc client.ImagePullResponse, progress chan<- ProgressEvent) error {
	type layerProgress struct {
		current int64
		total   int64
		done    bool
	}
	layers := make(map[string]*layerProgress)

	for msg, err := range rc.JSONMessages(ctx) {
		if err != nil {
			return err
		}
		if msg.ID == "" {
			continue
		}

		lp, exists := layers[msg.ID]
		if !exists {
			lp = &layerProgress{}
			layers[msg.ID] = lp
		}

		switch msg.Status {
		case "Download complete", "Pull complete":
			lp.done = true
			if lp.total > 0 {
				lp.current = lp.total
			}
		case "Downloading":
			if msg.Progress != nil {
				lp.current = msg.Progress.Current
				lp.total = msg.Progress.Total
			}
		}

		var totalBytes, currentBytes int64
		done := 0
		for _, l := range layers {
			currentBytes += l.current
			totalBytes += l.total
			if l.done {
				done++
			}
		}

		emitProgress(progress, ProgressEvent{
			Phase:       PhasePulling,
			LayersDone:  done,
			LayersTotal: len(layers),
			BytesCurr:   currentBytes,
			BytesTotal:  totalBytes,
		})
	}
	return nil
}

// parseLayers reads a Docker image tar archive and returns the layer list
// with ID, Size, Command, and Tree populated.
// Supports both legacy Docker format (config as <sha>.json at root) and
// OCI format (config as blobs/sha256/<digest>).
//
// Memory bound: the input stream is spooled to a temp file, then walked in
// two passes. Pass 1 reads small metadata (manifest.json, config). Pass 2
// streams each layer through decompress + ParseLayerTar, dropping the buffer
// before the next layer. Peak heap is bounded by the largest single layer
// rather than the sum of all blobs in the archive.
func parseLayers(r io.Reader) ([]Layer, error) {
	spool, err := os.CreateTemp("", "layerx-resolve-*.tar")
	if err != nil {
		return nil, fmt.Errorf("creating temp spool: %w", err)
	}
	spoolPath := spool.Name()
	defer os.Remove(spoolPath)
	defer spool.Close()

	if _, err := io.Copy(spool, r); err != nil {
		return nil, fmt.Errorf("spooling image archive: %w", err)
	}

	// Pass 1: collect manifest.json, root *.json (legacy config), and a
	// header map of every entry's declared size. Bodies of layer/blob
	// entries are streamed past without buffering.
	manifestData, rootJSON, headers, err := scanResolveMetadata(spool)
	if err != nil {
		return nil, err
	}

	if manifestData == nil {
		return nil, fmt.Errorf("invalid image archive: manifest.json not found")
	}

	var manifests []dockerManifest
	if err := json.Unmarshal(manifestData, &manifests); err != nil {
		return nil, fmt.Errorf("invalid image archive: cannot parse manifest: %w", err)
	}
	if len(manifests) == 0 {
		return nil, fmt.Errorf("invalid image archive: empty manifest")
	}
	manifest := manifests[0]

	// Resolve config: legacy (<sha>.json at root) or OCI (blobs/sha256/...).
	configData, ok := rootJSON[manifest.Config]
	if !ok {
		configData, err = readEntryFromSpool(spool, manifest.Config)
		if err != nil {
			return nil, fmt.Errorf("invalid image archive: config %s not found: %w", manifest.Config, err)
		}
	}

	var config imageConfig
	if err := json.Unmarshal(configData, &config); err != nil {
		return nil, fmt.Errorf("invalid image archive: cannot parse config: %w", err)
	}

	var commands []string
	for _, entry := range config.History {
		if !entry.EmptyLayer {
			commands = append(commands, entry.CreatedBy)
		}
	}

	layers := make([]Layer, len(manifest.Layers))
	for i, layerPath := range manifest.Layers {
		layers[i] = Layer{
			Index: i,
			ID:    extractShortID(layerPath),
			Size:  headers[layerPath],
		}
		if i < len(commands) {
			layers[i].Command = commands[i]
		}
	}

	// Pass 2: stream each layer one at a time, parse, drop the buffer
	// before reading the next. Peak heap = largest single layer.
	keep := make(map[string]int, len(manifest.Layers))
	for i, p := range manifest.Layers {
		keep[p] = i
	}

	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek spool: %w", err)
	}
	tr := tar.NewReader(spool)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading image archive: %w", err)
		}
		idx, want := keep[hdr.Name]
		if !want {
			continue
		}
		tarData, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", hdr.Name, err)
		}
		if len(tarData) == 0 {
			continue
		}
		dec, err := decompressIfGzip(tarData)
		if err != nil {
			return nil, fmt.Errorf("decompressing layer %s: %w", hdr.Name, err)
		}
		tree, parseErr := ParseLayerTar(dec)
		dec.Close()
		tarData = nil // drop before next iteration
		if parseErr != nil {
			return nil, fmt.Errorf("parsing layer %s: %w", hdr.Name, parseErr)
		}
		layers[idx].Tree = tree
	}

	return layers, nil
}

// scanResolveMetadata walks the spool once, returning manifest.json bytes,
// any root-level *.json blobs (legacy config payloads), and a header map of
// declared sizes for every entry. Layer / blob bodies are streamed past
// without buffering.
func scanResolveMetadata(spool *os.File) ([]byte, map[string][]byte, map[string]int64, error) {
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return nil, nil, nil, fmt.Errorf("seek spool: %w", err)
	}
	tr := tar.NewReader(spool)

	var manifestData []byte
	rootJSON := make(map[string][]byte)
	headers := make(map[string]int64)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, nil, fmt.Errorf("reading image archive: %w", err)
		}
		headers[hdr.Name] = hdr.Size

		switch {
		case hdr.Name == "manifest.json":
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("reading manifest.json: %w", err)
			}
			manifestData = data
		case strings.HasSuffix(hdr.Name, ".json") && !strings.Contains(hdr.Name, "/"):
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("reading %s: %w", hdr.Name, err)
			}
			rootJSON[hdr.Name] = data
		default:
			// Layer/blob body — skip without buffering.
		}
	}
	return manifestData, rootJSON, headers, nil
}

// readEntryFromSpool walks the spool and returns the bytes of a single named
// entry. Used for OCI configs that live under blobs/sha256/.
func readEntryFromSpool(spool *os.File, name string) ([]byte, error) {
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek spool: %w", err)
	}
	tr := tar.NewReader(spool)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("entry not found: %s", name)
		}
		if err != nil {
			return nil, fmt.Errorf("reading image archive: %w", err)
		}
		if hdr.Name == name {
			return io.ReadAll(tr)
		}
	}
}

// decompressIfGzip returns a reader that decompresses gzip data, or wraps raw
// bytes directly. Docker 25+ OCI format stores layer blobs as gzip-compressed tar.
// Callers must Close the returned reader.
func decompressIfGzip(data []byte) (io.ReadCloser, error) {
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		return gzip.NewReader(bytes.NewReader(data))
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

type dockerManifest struct {
	Config string   `json:"Config"`
	Layers []string `json:"Layers"`
}

type imageConfig struct {
	History []configHistoryEntry `json:"history"`
}

type configHistoryEntry struct {
	CreatedBy  string `json:"created_by"`
	EmptyLayer bool   `json:"empty_layer"`
}

// extractShortID derives a 12-char short ID from a layer path.
// Handles both legacy format ("aabbcc.../layer.tar") and
// OCI format ("blobs/sha256/aabbcc...").
func extractShortID(layerPath string) string {
	parts := strings.Split(layerPath, "/")
	var id string
	if len(parts) >= 3 && parts[0] == "blobs" {
		id = parts[2]
	} else {
		id = parts[0]
	}
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

