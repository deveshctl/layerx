package cmd

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/deveshctl/layerx/image"
)

// progressBufferSize is the buffered slot count of the channel returned by
// stderrProgress. Sized to absorb a healthy resolve's burst rate without
// blocking, but small enough that a stalled writer cannot accumulate an
// unbounded backlog.
const progressBufferSize = 16

// progressHeartbeat is the throttle interval for byte/layer updates while
// in PhasePulling or PhaseExporting. Chosen to be visible in a CI log
// (so 'silent hang on large images' doesn't recur) without being noisy.
const progressHeartbeat = 2 * time.Second

// stderrProgress returns a buffered channel suitable for use as
// AnalyzeOptions.Progress and a stop function. A goroutine drains the
// channel: phase transitions and PhaseCacheWarn print immediately; bytes
// and layer-count updates within PhasePulling/PhaseExporting are throttled
// to one line per progressHeartbeat. The goroutine exits when the channel
// is closed or ctx is done. stop() closes the channel and waits for the
// goroutine — call it via defer so it runs even on panic.
//
// The returned channel is bidirectional; passing it as
// AnalyzeOptions.Progress (chan<- ProgressEvent) is a standard implicit
// narrowing.
func stderrProgress(ctx context.Context, w io.Writer) (chan image.ProgressEvent, func()) {
	ch := make(chan image.ProgressEvent, progressBufferSize)
	ticker := time.NewTicker(progressHeartbeat)
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		defer ticker.Stop()
		runProgressLoop(ctx, w, ch, ticker.C)
	}()

	// sync.Once guards against a double-close panic if a future caller (or
	// test) accidentally invokes stop twice. Callers should still defer it
	// exactly once; this is belt-and-braces for refactor safety.
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			close(ch)
			wg.Wait()
		})
	}
	return ch, stop
}

// progressKey captures the fields that writeHeartbeat uses to format output.
// Two events with the same key produce the same displayed line; deduplicating
// by key rather than the full ProgressEvent struct prevents duplicate lines
// when fields like LayersDone change without affecting the displayed output.
type progressKey struct {
	BytesCurr   int64
	BytesTotal  int64
	LayersDone  int
	LayersTotal int
}

func progressKeyOf(ev image.ProgressEvent) progressKey {
	return progressKey{
		BytesCurr:   ev.BytesCurr,
		BytesTotal:  ev.BytesTotal,
		LayersDone:  ev.LayersDone,
		LayersTotal: ev.LayersTotal,
	}
}

// runProgressLoop is the body of the goroutine started by stderrProgress.
// Split out so tests can drive it with a hand-managed tick channel and
// without spinning up a real timer.
func runProgressLoop(ctx context.Context, w io.Writer, ch <-chan image.ProgressEvent, tick <-chan time.Time) {
	var (
		curPhase    = image.PhaseUnknown
		buffered    image.ProgressEvent // most-recent in-flight event for the current phase
		hasBuffered bool
		// lastKey tracks the display key of the last line written by
		// writeHeartbeat. Two events with the same key render to the same
		// output line; deduplicating by key (not the full struct) prevents
		// duplicate lines when LayersDone changes but the displayed bytes
		// are identical, while still allowing a new line if BytesTotal grows.
		lastKey progressKey
		hasLast bool
	)

	flush := func() {
		if !hasBuffered {
			return
		}
		k := progressKeyOf(buffered)
		if !hasLast || k != lastKey {
			writeHeartbeat(w, buffered)
			lastKey = k
			hasLast = true
		}
		hasBuffered = false
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			flush()
		case ev, ok := <-ch:
			if !ok {
				flush()
				return
			}
			if ev.Phase != curPhase {
				flush()
				hasLast = false
				writePhase(w, ev)
				curPhase = ev.Phase
				continue
			}
			if ev.Phase == image.PhaseCacheWarn {
				// CacheWarn always prints immediately; it carries a Message.
				writePhase(w, ev)
				continue
			}
			if ev.Phase == image.PhasePulling || ev.Phase == image.PhaseExporting {
				buffered = ev
				hasBuffered = true
				// Flush immediately when bytes hit 100% so the completion line
				// prints promptly without waiting for the next tick.
				if ev.Phase == image.PhasePulling && ev.BytesTotal > 0 && ev.BytesCurr >= ev.BytesTotal {
					flush()
				}
			}
		}
	}
}

func writePhase(w io.Writer, ev image.ProgressEvent) {
	switch ev.Phase {
	case image.PhasePulling:
		fmt.Fprintln(w, "layerx: pulling")
	case image.PhaseExporting:
		fmt.Fprintln(w, "layerx: exporting layers")
	case image.PhaseParsing:
		if ev.LayersTotal > 0 {
			fmt.Fprintf(w, "layerx: parsing %d layers\n", ev.LayersTotal)
		} else {
			fmt.Fprintln(w, "layerx: parsing layers")
		}
	case image.PhaseCacheLoad:
		fmt.Fprintln(w, "layerx: loaded from cache")
	case image.PhaseCacheWarn:
		if ev.Message != "" {
			fmt.Fprintf(w, "warning: %s\n", ev.Message)
		} else {
			fmt.Fprintln(w, "warning: cache problem")
		}
	}
}

func writeHeartbeat(w io.Writer, ev image.ProgressEvent) {
	switch ev.Phase {
	case image.PhasePulling:
		if ev.BytesTotal > 0 {
			pct := float64(ev.BytesCurr) * 100 / float64(ev.BytesTotal)
			fmt.Fprintf(w, "layerx:   pulled %s / %s (%.0f%%)\n",
				humanBytes(ev.BytesCurr), humanBytes(ev.BytesTotal), pct)
		} else if ev.LayersTotal > 0 && (ev.LayersDone > 0 || ev.LayersTotal > 1) {
			// Only show the layer-count line when there is genuine progress
			// to report — suppress the initial "pulled 0 / 1 layers" that
			// fires before any byte data arrives on single-layer images.
			fmt.Fprintf(w, "layerx:   pulled %d / %d layers\n", ev.LayersDone, ev.LayersTotal)
		}
	case image.PhaseExporting:
		if ev.LayersTotal > 0 {
			fmt.Fprintf(w, "layerx:   exported %d / %d layers\n", ev.LayersDone, ev.LayersTotal)
		}
	}
}

// humanBytes formats a byte count as KB/MB/GB to one decimal place. We
// don't pull in a third-party library for one helper; the rounding is
// deliberately coarse — this output goes into a CI log, not a UI.
func humanBytes(n int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
