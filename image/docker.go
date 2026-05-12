package image

import (
	"archive/tar"
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
		cli, err := client.NewClientWithOpts(
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

// Resolve fetches the image, exports it as a tar, and parses the layer list.
func (r *DockerResolver) Resolve(ctx context.Context, imageRef string) ([]Layer, error) {
	if err := r.ensureImage(ctx, imageRef); err != nil {
		return nil, err
	}

	rc, err := r.cli.ImageSave(ctx, []string{imageRef})
	if err != nil {
		return nil, fmt.Errorf("failed to export image %s: %w", imageRef, err)
	}
	defer rc.Close()

	return parseLayers(rc)
}

// ensureImage checks if the image exists locally; if not, pulls it.
func (r *DockerResolver) ensureImage(ctx context.Context, imageRef string) error {
	f := make(client.Filters).Add("reference", imageRef)
	result, err := r.cli.ImageList(ctx, client.ImageListOptions{Filters: f})
	if err != nil {
		return fmt.Errorf("cannot connect to Docker daemon: is Docker running? %w", err)
	}

	if len(result.Items) > 0 {
		return nil
	}

	rc, err := r.cli.ImagePull(ctx, imageRef, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %w", imageRef, err)
	}
	defer rc.Close()
	if _, err := io.Copy(io.Discard, rc); err != nil {
		return fmt.Errorf("failed to complete pull of %s: %w", imageRef, err)
	}
	return nil
}

// parseLayers reads a Docker image tar archive and returns the layer list.
func parseLayers(r io.Reader) ([]Layer, error) {
	tr := tar.NewReader(r)
	var manifestData []byte

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading image archive: %w", err)
		}
		if hdr.Name == "manifest.json" {
			manifestData, err = io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("reading manifest.json: %w", err)
			}
			break
		}
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

	layers := make([]Layer, len(manifests[0].Layers))
	for i, layerPath := range manifests[0].Layers {
		layers[i] = Layer{
			Index: i,
			ID:    extractShortID(layerPath),
		}
	}
	return layers, nil
}

type dockerManifest struct {
	Config string   `json:"Config"`
	Layers []string `json:"Layers"`
}

// extractShortID derives a 12-char short ID from a layer tar path.
func extractShortID(layerPath string) string {
	dir := strings.Split(layerPath, "/")[0]
	if len(dir) > 12 {
		return dir[:12]
	}
	return dir
}
