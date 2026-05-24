package image

import (
	"context"
	"errors"
	"fmt"
)

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
//  4. After a successful resolve, re-read ImageID and require the result
//     to match the pre-resolve digest before persisting the cache. A
//     mismatch means the tag flipped during the run (concurrent docker
//     pull) — caching either set of layers under either digest would lie.
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
	canCache := digestErr == nil && digest != "" && cacheRoot != ""

	var (
		layers    []Layer
		fromCache bool
	)
	if canCache && !opts.NoCache {
		cached, ok, loadErr := loadCache(cacheRoot, digest)
		if loadErr != nil && !errors.Is(loadErr, errBadDigest) {
			emitCacheWarn(opts.Progress, fmt.Sprintf("cache load failed: %v", loadErr))
		}
		if ok {
			layers = cached
			fromCache = true
			emitProgress(opts.Progress, ProgressEvent{Phase: PhaseCacheLoad})
		}
	}

	if !fromCache {
		fresh, err := resolver.ResolveWithProgress(ctx, imageRef, opts.Progress)
		if err != nil {
			return nil, err
		}
		layers = fresh

		// Re-read ImageID after the resolve. Two reasons:
		//  1. The image may not have been local before; ResolveWithProgress
		//     has now pulled it, so the digest is observable.
		//  2. The tag's underlying digest may have flipped mid-run (a
		//     concurrent `docker pull` retagged it). If post-resolve
		//     digest disagrees with pre-resolve, we don't know which set
		//     of layers we actually exported, so refuse to cache.
		postDigest, postErr := resolver.ImageID(ctx, imageRef)
		if cacheRoot != "" {
			switch {
			case postErr != nil || postDigest == "":
				// Cannot verify; skip cache write. Surface the gap so users
				// know cold cost will repeat.
				emitCacheWarn(opts.Progress, "cache write skipped: post-resolve image digest unavailable")
			case digestErr != nil:
				// Pre-resolve digest was unknown (image was not local); the
				// post-resolve digest is now authoritative.
				digest = postDigest
				if err := saveCache(cacheRoot, digest, layers); err != nil {
					emitCacheWarn(opts.Progress, fmt.Sprintf("cache write failed: %v", err))
				}
			case postDigest != digest:
				// Tag flipped underneath us. Refuse to cache either way.
				emitCacheWarn(opts.Progress,
					"cache write skipped: image digest changed during analysis (concurrent pull?)")
			default:
				if err := saveCache(cacheRoot, digest, layers); err != nil {
					emitCacheWarn(opts.Progress, fmt.Sprintf("cache write failed: %v", err))
				}
			}
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

// emitProgress sends ev on ch, but never blocks. A caller passing an
// unbuffered or full channel just loses this event — the alternative is
// deadlocking the entire analyze pipeline.
func emitProgress(ch chan<- ProgressEvent, ev ProgressEvent) {
	if ch == nil {
		return
	}
	select {
	case ch <- ev:
	default:
	}
}

func emitCacheWarn(ch chan<- ProgressEvent, msg string) {
	emitProgress(ch, ProgressEvent{Phase: PhaseCacheWarn, Message: msg})
}
