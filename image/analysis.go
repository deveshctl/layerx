package image

import "context"

// Analysis holds the complete result of inspecting an image.
type Analysis struct {
	ImageRef     string
	Layers       []Layer
	StackedTrees []*FileTree
	TotalSize    int64
}

// AnalyzeOptions controls the analyze pipeline. Zero value is valid: caching
// enabled, no progress channel, default cache directory.
type AnalyzeOptions struct {
	// NoCache forces a cold resolve. The cache is still written on success
	// so the next default-mode run is a hit.
	NoCache bool

	// Progress, if non-nil, receives ProgressEvents during the run. Same
	// semantics as AnalyzeWithProgress; sending PhaseCacheLoad on hit.
	Progress chan<- ProgressEvent

	// CacheRoot overrides CacheDir() for this call. Empty = use CacheDir().
	// Tests can also set LAYERX_CACHE_DIR; this field exists for callers
	// that want explicit control without environment.
	CacheRoot string
}

// Analyze resolves an image and computes stacked file trees. Backwards-compatible
// shim around AnalyzeWithOptions; caching is enabled, no progress channel.
func Analyze(ctx context.Context, resolver Resolver, imageRef string) (*Analysis, error) {
	return AnalyzeWithOptions(ctx, resolver, imageRef, AnalyzeOptions{})
}

// AnalyzeWithProgress is preserved for callers that already pass a progress
// channel. Equivalent to AnalyzeWithOptions{Progress: progress}.
func AnalyzeWithProgress(ctx context.Context, resolver Resolver, imageRef string, progress chan<- ProgressEvent) (*Analysis, error) {
	return AnalyzeWithOptions(ctx, resolver, imageRef, AnalyzeOptions{Progress: progress})
}

// AnalyzeWithOptions is the cache-aware entry point.
//
// Flow:
//  1. Try to obtain image content digest via Resolver.ImageID. On error
//     (e.g. image not local yet, daemon hiccup) we skip the cache lookup
//     and proceed to a cold resolve; ResolveWithProgress will pull as needed.
//  2. If !opts.NoCache and the digest is known, try loadCache. On hit we
//     skip ResolveWithProgress entirely; emit PhaseCacheLoad.
//  3. Otherwise call ResolveWithProgress (existing pull/export/parse path).
//  4. If the resolve succeeded and we have a digest, write the cache. Save
//     errors are non-fatal — the user already has the live result.
//  5. Stack + assignNetDeltas + TotalSize, build Analysis, return.
//
// Hit and miss paths converge at step 5: the same Analysis assembly runs
// either way. ImageRef is taken from imageRef (the current arg), never from
// the envelope.
func AnalyzeWithOptions(ctx context.Context, resolver Resolver, imageRef string, opts AnalyzeOptions) (*Analysis, error) {
	cacheRoot := opts.CacheRoot
	if cacheRoot == "" {
		root, err := CacheDir()
		if err == nil {
			cacheRoot = root
		}
		// If CacheDir fails (very unlikely on supported OSes), proceed
		// without caching — never block the run.
	}

	digest, digestErr := resolver.ImageID(ctx, imageRef)
	// digestErr is expected when the image is not local yet. We just skip
	// the cache lookup; ResolveWithProgress will pull and we'll re-Inspect
	// after that to write the cache.

	canCache := digestErr == nil && cacheRoot != ""

	var (
		layers    []Layer
		fromCache bool
	)
	if canCache && !opts.NoCache {
		cached, ok, _ := loadCache(cacheRoot, digest)
		if ok {
			layers = cached
			fromCache = true
			if opts.Progress != nil {
				opts.Progress <- ProgressEvent{Phase: PhaseCacheLoad}
			}
		}
	}

	if !fromCache {
		fresh, err := resolver.ResolveWithProgress(ctx, imageRef, opts.Progress)
		if err != nil {
			return nil, err
		}
		layers = fresh

		// We may not have had a digest before (image was not local). Try once
		// more after the resolve, which has now ensured the image is local.
		if digestErr != nil && cacheRoot != "" {
			if d, err := resolver.ImageID(ctx, imageRef); err == nil {
				digest = d
				canCache = true
			}
		}
		if canCache {
			_ = saveCache(cacheRoot, digest, layers) // non-fatal on failure
		}
	}

	stacked := Stack(layers)
	assignNetDeltas(layers, stacked)

	var totalSize int64
	for _, l := range layers {
		totalSize += l.Size
	}

	return &Analysis{
		ImageRef:     imageRef,
		Layers:       layers,
		StackedTrees: stacked,
		TotalSize:    totalSize,
	}, nil
}
