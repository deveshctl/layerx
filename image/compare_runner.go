package image

import (
	"context"
	"fmt"
	"sync"
)

// CompareInput owns one resolver. Supply independent resolvers for the two
// sides unless the implementation explicitly supports concurrent use.
type CompareInput struct {
	Ref      string
	Resolver Resolver
}

type CompareImage struct {
	Analysis *Analysis
	Digest   string
}

type CompareRunOptions struct {
	NoCache   bool
	CacheRoot string
	Platform  string
	Progress  *CompareProgress
}

type CompareSideProgress struct {
	Event   ProgressEvent
	Ready   bool
	Warning string
}

// CompareProgress retains only the latest status and warning for each side.
// Its zero value is ready to use. Poll Snapshot rather than sending every
// download event through a UI's message queue. Use one instance per run.
type CompareProgress struct {
	mu    sync.Mutex
	sides [2]CompareSideProgress
}

func (p *CompareProgress) Snapshot() [2]CompareSideProgress {
	if p == nil {
		return [2]CompareSideProgress{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sides
}

func (p *CompareProgress) update(side int, event ProgressEvent, ready bool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if event.Phase == PhaseCacheWarn {
		p.sides[side].Warning = event.Message
	} else if event.Phase != PhaseUnknown {
		p.sides[side].Event = event
	}
	p.sides[side].Ready = ready
}

// AnalyzePair resolves and analyzes both inputs concurrently without writing
// terminal output. A failure cancels its sibling immediately; all workers and
// progress collectors are joined before returning. Cancellation is cooperative:
// resolver implementations must honor ctx and finish their progress sends before
// returning. No partial result is returned on failure.
func AnalyzePair(ctx context.Context, inputs [2]CompareInput, opts CompareRunOptions) ([2]CompareImage, error) {
	var results [2]CompareImage
	if err := ctx.Err(); err != nil {
		return results, err
	}
	for side, input := range inputs {
		if input.Resolver == nil {
			return results, fmt.Errorf("image %c: missing resolver", 'A'+side)
		}
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var workers sync.WaitGroup
	for side, input := range inputs {
		workers.Go(func() {
			result, err := analyzeCompareSide(ctx, input, opts, side)
			if err != nil {
				cancel(fmt.Errorf("analyzing image %c %q: %w", 'A'+side, input.Ref, err))
				return
			}
			results[side] = result
		})
	}
	workers.Wait()
	if err := context.Cause(ctx); err != nil {
		return [2]CompareImage{}, err
	}
	return results, nil
}

func analyzeCompareSide(ctx context.Context, input CompareInput, opts CompareRunOptions, side int) (CompareImage, error) {
	progress := make(chan ProgressEvent, 32)
	var collector sync.WaitGroup
	collector.Go(func() {
		for event := range progress {
			opts.Progress.update(side, event, false)
		}
	})
	analysis, err := func() (*Analysis, error) {
		defer collector.Wait()
		defer close(progress)
		return AnalyzeWithOptions(ctx, input.Resolver, input.Ref, AnalyzeOptions{
			NoCache: opts.NoCache, CacheRoot: opts.CacheRoot,
			Platform: opts.Platform, Progress: progress,
		})
	}()
	if err != nil {
		return CompareImage{}, err
	}
	if err := ctx.Err(); err != nil {
		return CompareImage{}, err
	}
	opts.Progress.update(side, ProgressEvent{}, true)
	return CompareImage{Analysis: analysis, Digest: analysis.Digest}, nil
}
