package image

import (
	"context"
	"errors"
	"fmt"
)

type Analysis struct {
	ImageRef        string
	Layers          []Layer
	StackedTrees    []*FileTree // per-layer delta view: filesystem changes introduced by each layer only.
	AggregatedTrees []*FileTree // cumulative provenance view: full filesystem state from layer 0 up to each layer.
	TotalSize       int64
}

// AnalyzeOptions controls the analyze pipeline. Zero value is valid: caching
// enabled, no progress channel, default cache directory.
type AnalyzeOptions struct {
	// NoCache forces a cold resolve. The cache is still written on success
	// so the next default-mode run is a hit.
	NoCache bool

	Progress chan<- ProgressEvent

	// CacheRoot overrides CacheDir() for this call. Empty = use CacheDir().
	// Tests can also set LAYERX_CACHE_DIR; this field exists for callers
	// that want explicit control without environment.
	CacheRoot string

	// Platform is the canonical "os/arch[/variant]" string the caller
	// requested via --platform (e.g. "linux/arm64"). When non-empty it is
	// appended to the cache key so that analyses for different platforms of
	// the same image digest never share a cache entry. Empty = no platform
	// pin; the behaviour is identical to before this field was added.
	Platform string
}

func Analyze(ctx context.Context, resolver Resolver, imageRef string) (*Analysis, error) {
	return AnalyzeWithOptions(ctx, resolver, imageRef, AnalyzeOptions{})
}

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
		cached, ok, loadErr := loadCacheWithPlatform(cacheRoot, digest, opts.Platform)
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
				// Pre-resolve digest was unknown (image was not local); use
				// the post-resolve digest as the authoritative cache key.
				if err := saveCacheWithPlatform(cacheRoot, postDigest, opts.Platform, imageRef, layers, opts.Progress); err != nil {
					emitCacheWarn(opts.Progress, fmt.Sprintf("cache write failed: %v", err))
				}
			case postDigest != digest:
				// Tag flipped underneath us. Refuse to cache either way.
				emitCacheWarn(opts.Progress,
					"cache write skipped: image digest changed during analysis (concurrent pull?)")
			default:
				if err := saveCacheWithPlatform(cacheRoot, digest, opts.Platform, imageRef, layers, opts.Progress); err != nil {
					emitCacheWarn(opts.Progress, fmt.Sprintf("cache write failed: %v", err))
				}
			}
		}
	}

	stacked := Stack(layers)
	aggregated := BuildAggregatedTrees(layers)
	assignNetDeltas(layers, stacked)

	var totalSize int64
	for _, l := range layers {
		totalSize += l.Size
	}

	return &Analysis{
		ImageRef:        imageRef,
		Layers:          layers,
		StackedTrees:    stacked,
		AggregatedTrees: aggregated,
		TotalSize:       totalSize,
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
