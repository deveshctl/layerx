package image

import "context"

// ImageMeta holds lightweight metadata obtainable without exporting the full image.
type ImageMeta struct {
	Size int64
}

// ProgressPhase identifies what the resolver is currently doing.
type ProgressPhase int

const (
	PhasePulling  ProgressPhase = iota
	PhaseExporting
	PhaseParsing
)

// ProgressEvent reports loading progress back to the caller.
type ProgressEvent struct {
	Phase       ProgressPhase
	LayersDone  int
	LayersTotal int
	BytesCurr   int64
	BytesTotal  int64
}

// Resolver fetches image layer information from a container runtime.
type Resolver interface {
	Resolve(ctx context.Context, imageRef string) ([]Layer, error)
	ResolveWithProgress(ctx context.Context, imageRef string, progress chan<- ProgressEvent) ([]Layer, error)
	Inspect(ctx context.Context, imageRef string) (*ImageMeta, error)
}

// Layer holds metadata for a single image layer.
type Layer struct {
	Index   int
	ID      string    // short digest (first 12 chars of sha256)
	Size    int64
	Command string    // Dockerfile instruction that created this layer
	Tree    *FileTree // nil until M05 populates it
}
