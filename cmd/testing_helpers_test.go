package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/deveshctl/layerx/image"
)

// fakeResolver implements image.Resolver for cmd/ tests. blockOnResolve, when
// non-nil, makes ResolveWithProgress block on ctx.Done() or the channel.
type fakeResolver struct {
	imageID    string
	imageIDErr error
	layers     []image.Layer
	resolveErr error
	inspect    *image.ImageMeta

	blockOnResolve chan struct{}
}

func (f *fakeResolver) Resolve(ctx context.Context, ref string) ([]image.Layer, error) {
	return f.ResolveWithProgress(ctx, ref, nil)
}

func (f *fakeResolver) ResolveWithProgress(ctx context.Context, _ string, _ chan<- image.ProgressEvent) ([]image.Layer, error) {
	if f.blockOnResolve != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.blockOnResolve:
		}
	}
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	return f.layers, nil
}

func (f *fakeResolver) Inspect(_ context.Context, _ string) (*image.ImageMeta, error) {
	return f.inspect, nil
}

func (f *fakeResolver) ImageID(_ context.Context, _ string) (string, error) {
	if f.imageIDErr != nil {
		return "", f.imageIDErr
	}
	return f.imageID, nil
}

// withFakeResolver swaps cmd's selectResolver to return r and restores it on
// cleanup. Tests using this helper MUST NOT call t.Parallel.
func withFakeResolver(t *testing.T, r image.Resolver) {
	t.Helper()
	prev := selectResolver
	selectResolver = func(_ string) (image.Resolver, error) { return r, nil }
	t.Cleanup(func() { selectResolver = prev })
}

func okResolver(layers ...image.Layer) *fakeResolver {
	return &fakeResolver{layers: layers}
}

func errResolver(err error) *fakeResolver {
	return &fakeResolver{resolveErr: err}
}

func cancelResolver() *fakeResolver {
	return &fakeResolver{blockOnResolve: make(chan struct{})}
}

func synthLayer(index int, path string, size int64) image.Layer {
	// Build a proper directory hierarchy from path so Stack's name-based
	// merging matches image_test.go's makeFile/makeTree pattern. A flat
	// child with Name="/etc/passwd" carries a leading slash that confuses
	// name lookups across stacked layers.
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	leaf := &image.FileNode{Name: parts[len(parts)-1], Path: path, Size: size, DiffType: image.Added}
	cur := leaf
	for i := len(parts) - 2; i >= 0; i-- {
		dirPath := "/" + strings.Join(parts[:i+1], "/")
		cur = &image.FileNode{Name: parts[i], Path: dirPath, IsDir: true, Children: []*image.FileNode{cur}}
	}
	root := &image.FileNode{Name: "/", Path: "/", IsDir: true, Children: []*image.FileNode{cur}}
	return image.Layer{
		Index: index,
		ID:    "synth",
		Size:  size,
		Tree:  &image.FileTree{Root: root},
	}
}

// passingLayers yields a single layer (no overlap possible → score 1.0).
func passingLayers() []image.Layer {
	return []image.Layer{synthLayer(0, "/usr/bin/app", 200)}
}

// failingLayers shares /etc/config across two layers so duplicate bytes drive
// efficiency below 0.9.
func failingLayers() []image.Layer {
	return []image.Layer{
		synthLayer(0, "/etc/config", 100),
		synthLayer(1, "/etc/config", 200),
	}
}
