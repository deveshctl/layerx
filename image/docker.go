package image

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		return &ErrDaemonNotRunning{Cause: err}
	}

	if len(result.Items) > 0 {
		return nil
	}

	emitProgress(progress, ProgressEvent{Phase: PhasePulling})

	rc, err := r.cli.ImagePull(ctx, imageRef, client.ImagePullOptions{})
	if err != nil {
		return &ErrPullFailed{Ref: imageRef, Cause: err}
	}
	defer rc.Close()

	if progress != nil {
		if err := r.streamPullProgress(ctx, rc, progress); err != nil {
			return &ErrPullFailed{Ref: imageRef, Cause: err}
		}
	} else {
		if _, err := io.Copy(io.Discard, rc); err != nil {
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
func parseLayers(r io.Reader) ([]Layer, error) {
	tr := tar.NewReader(r)

	// Buffer all entries: metadata goes to contents, potential layer/blob data
	// goes to blobs. After manifest is parsed, blobs are resolved by reference.
	contents := make(map[string][]byte)
	blobs := make(map[string][]byte)
	headers := make(map[string]int64)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading image archive: %w", err)
		}

		headers[hdr.Name] = hdr.Size

		switch {
		case hdr.Name == "manifest.json":
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", hdr.Name, err)
			}
			contents[hdr.Name] = data
		case strings.HasSuffix(hdr.Name, ".json") && !strings.Contains(hdr.Name, "/"):
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", hdr.Name, err)
			}
			contents[hdr.Name] = data
		case strings.HasPrefix(hdr.Name, "blobs/sha256/"):
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", hdr.Name, err)
			}
			blobs[hdr.Name] = data
		case strings.HasSuffix(hdr.Name, "/layer.tar"):
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", hdr.Name, err)
			}
			blobs[hdr.Name] = data
		}
	}

	manifestData := contents["manifest.json"]
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

	// Resolve config: check both contents (legacy .json) and blobs (OCI).
	configData := contents[manifest.Config]
	if configData == nil {
		configData = blobs[manifest.Config]
	}
	if configData == nil {
		return nil, fmt.Errorf("invalid image archive: config %s not found", manifest.Config)
	}

	var config imageConfig
	if err := json.Unmarshal(configData, &config); err != nil {
		return nil, fmt.Errorf("invalid image archive: cannot parse config: %w", err)
	}

	layerSizes := make(map[string]int64)
	for _, layerPath := range manifest.Layers {
		if size, ok := headers[layerPath]; ok {
			layerSizes[layerPath] = size
		}
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
			Size:  layerSizes[layerPath],
		}
		if i < len(commands) {
			layers[i].Command = commands[i]
		}
		if tarData, ok := blobs[layerPath]; ok && len(tarData) > 0 {
			r, err := decompressIfGzip(tarData)
			if err != nil {
				return nil, fmt.Errorf("decompressing layer %s: %w", layerPath, err)
			}
			tree, err := ParseLayerTar(r)
			r.Close()
			if err != nil {
				return nil, fmt.Errorf("parsing layer %s: %w", layerPath, err)
			}
			layers[i].Tree = tree
		}
	}

	return layers, nil
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

