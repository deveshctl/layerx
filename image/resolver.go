package image

import "context"

// Resolver fetches image layer information from a container runtime.
type Resolver interface {
	Resolve(ctx context.Context, imageRef string) ([]Layer, error)
}

// Layer holds metadata for a single image layer.
type Layer struct {
	Index   int
	ID      string    // short digest (first 12 chars of sha256)
	Size    int64
	Command string    // Dockerfile instruction that created this layer
	Tree    *FileTree // nil until M05 populates it
}
