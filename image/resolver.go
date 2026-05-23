package image

import "context"

// ImageMeta holds lightweight metadata obtainable without exporting the full image.
type ImageMeta struct {
	Size int64
}

// ProgressPhase identifies what the resolver is currently doing.
type ProgressPhase int

const (
	// PhaseUnknown is the zero value, used by callers to mean "no phase
	// observed yet" so a UI can avoid rendering a stale default phase
	// before the first event arrives.
	PhaseUnknown ProgressPhase = iota
	PhasePulling
	PhaseExporting
	PhaseParsing
	PhaseCacheLoad // emitted on cache hit; no live Docker work follows
	// PhaseCacheWarn is a non-fatal diagnostic: the cache load or save
	// failed with an unexpected I/O error. The run continues with live
	// data; UIs typically render Message in a status bar.
	PhaseCacheWarn
)

// ProgressEvent reports loading progress back to the caller.
type ProgressEvent struct {
	Phase       ProgressPhase
	LayersDone  int
	LayersTotal int
	BytesCurr   int64
	BytesTotal  int64
	// Message carries human-readable diagnostic text for PhaseCacheWarn.
	// Empty for normal phases.
	Message string
}

// Resolver fetches image layer information from a container runtime.
type Resolver interface {
	Resolve(ctx context.Context, imageRef string) ([]Layer, error)
	ResolveWithProgress(ctx context.Context, imageRef string, progress chan<- ProgressEvent) ([]Layer, error)
	Inspect(ctx context.Context, imageRef string) (*ImageMeta, error)
	// ImageID returns the image content digest (e.g. "sha256:abc...") for ref.
	// Implementations may return an error if the image is not local; callers
	// that want pull-on-miss must call ensure-image semantics first.
	ImageID(ctx context.Context, imageRef string) (string, error)
}

// ExtractorSource is implemented by resolvers that can produce an Extractor.
type ExtractorSource interface {
	NewExtractor() Extractor
}

// Layer holds metadata for a single image layer.
type Layer struct {
	Index   int
	ID      string    // short digest (first 12 chars of sha256)
	Size    int64
	Command string    // Dockerfile instruction that created this layer
	Tree    *FileTree // nil until M05 populates it
	// NetDelta is the merged-filesystem change at this step.
	// [0] = live file bytes after the first layer (full size, not 0).
	// [i>0] = live bytes in stacked[i] minus stacked[i-1]. Negative when
	// a layer net-removes more than it adds (cleanup, whiteout deletes).
	NetDelta int64
}
