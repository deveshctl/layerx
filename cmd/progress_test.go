package cmd

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// driveProgress runs runProgressLoop with a hand-managed tick channel so
// timing is deterministic in tests.
func driveProgress(t *testing.T, ctx context.Context, events []image.ProgressEvent, tick chan time.Time, closeCh bool) string {
	t.Helper()
	var buf bytes.Buffer
	ch := make(chan image.ProgressEvent, len(events)+1)
	for _, ev := range events {
		ch <- ev
	}
	if closeCh {
		close(ch)
	}

	done := make(chan struct{})
	go func() {
		runProgressLoop(ctx, &buf, ch, tick)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runProgressLoop did not exit within 1s")
	}
	return buf.String()
}

func TestStderrProgress_PrintsPhaseTransitions(t *testing.T) {
	out := driveProgress(t, context.Background(), []image.ProgressEvent{
		{Phase: image.PhasePulling},
		{Phase: image.PhaseExporting},
		{Phase: image.PhaseParsing, LayersTotal: 7},
	}, make(chan time.Time), true)

	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "pulling")
	assert.Contains(t, lines[1], "exporting")
	assert.Contains(t, lines[2], "parsing 7 layers")
}

func TestStderrProgress_ThrottlesHeartbeat(t *testing.T) {
	events := []image.ProgressEvent{{Phase: image.PhasePulling}}
	for i := 0; i < 20; i++ {
		events = append(events, image.ProgressEvent{
			Phase:      image.PhasePulling,
			BytesCurr:  int64(i * 1024 * 1024),
			BytesTotal: 21 * 1024 * 1024, // never reaches 100% so saturation doesn't fire
		})
	}
	tick := make(chan time.Time, 1)
	tick <- time.Now() // exactly one tick fires
	out := driveProgress(t, context.Background(), events, tick, true)

	lines := strings.Split(strings.TrimSpace(out), "\n")
	// 1 phase line + at most 2 heartbeat lines (one from the pre-loaded
	// tick and one from the close-driven flush; Go's select-on-ready is
	// uniformly random, so both can fire even though the events stream
	// past in <2s wall time). An unthrottled implementation would print
	// ~21 lines, so 3 cleanly distinguishes throttled from not.
	assert.LessOrEqual(t, len(lines), 3, "got %d lines: %q", len(lines), out)
	assert.Contains(t, out, "pulling")
}

func TestStderrProgress_FlushesOnPhaseChange(t *testing.T) {
	out := driveProgress(t, context.Background(), []image.ProgressEvent{
		{Phase: image.PhasePulling},
		{Phase: image.PhasePulling, BytesCurr: 5_000_000, BytesTotal: 10_000_000},
		{Phase: image.PhaseExporting},
	}, make(chan time.Time), true)

	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 3, "got: %q", out)
	assert.Contains(t, lines[0], "pulling")
	assert.Contains(t, lines[1], "pulled")     // flushed before phase change
	assert.Contains(t, lines[2], "exporting")
}

func TestStderrProgress_CacheLoadAndWarn(t *testing.T) {
	out := driveProgress(t, context.Background(), []image.ProgressEvent{
		{Phase: image.PhaseCacheLoad},
		{Phase: image.PhaseCacheWarn, Message: "cache write failed: disk full"},
	}, make(chan time.Time), true)

	assert.Contains(t, out, "loaded from cache")
	assert.Contains(t, out, "warning: cache write failed: disk full")
}

func TestStderrProgress_DrainsOnClose(t *testing.T) {
	var buf bytes.Buffer
	ch := make(chan image.ProgressEvent, 1)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runProgressLoop(context.Background(), &buf, ch, make(chan time.Time))
	}()

	close(ch)
	doneCh := make(chan struct{})
	go func() { wg.Wait(); close(doneCh) }()
	select {
	case <-doneCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("loop did not exit within 100ms of channel close")
	}
}

func TestStderrProgress_ExitsOnCtxCancel(t *testing.T) {
	var buf bytes.Buffer
	ch := make(chan image.ProgressEvent) // never closed
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runProgressLoop(ctx, &buf, ch, make(chan time.Time))
	}()

	cancel()
	doneCh := make(chan struct{})
	go func() { wg.Wait(); close(doneCh) }()
	select {
	case <-doneCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("loop did not exit within 100ms of ctx cancel")
	}
}

func TestStderrProgress_StopIsIdempotent(t *testing.T) {
	// Calling stop twice must not panic on close-of-closed-channel; a
	// future refactor or test that double-defers would otherwise crash.
	_, stop := stderrProgress(context.Background(), &bytes.Buffer{})
	stop()
	stop()
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{2048, "2.0 KB"},
		{5 * 1024 * 1024, "5.0 MB"},
		{3 * 1024 * 1024 * 1024, "3.0 GB"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, humanBytes(tc.in))
	}
}

// TestStderrProgress_SuppressZeroLayerCount verifies that the "pulled 0 / 1
// layers" line is not printed when the first event has no byte data yet.
// Previously this appeared on every single-layer pull before bytes arrived.
func TestStderrProgress_SuppressZeroLayerCount(t *testing.T) {
	events := []image.ProgressEvent{
		{Phase: image.PhasePulling},
		// First event from Docker: layer registered, no bytes yet.
		{Phase: image.PhasePulling, LayersDone: 0, LayersTotal: 1, BytesCurr: 0, BytesTotal: 0},
	}
	tick := make(chan time.Time, 1)
	tick <- time.Now()
	out := driveProgress(t, context.Background(), events, tick, true)

	assert.NotContains(t, out, "pulled 0 / 1 layers", "zero-progress layer count line must be suppressed")
}

// TestStderrProgress_100PercentPrintsOnce verifies that hitting 100% causes
// exactly one "pulled X / X (100%)" line even when Docker continues to emit
// "Pull complete" / "Download complete" events afterwards.
func TestStderrProgress_100PercentPrintsOnce(t *testing.T) {
	const total = 10 * 1024 * 1024
	events := []image.ProgressEvent{
		{Phase: image.PhasePulling},
		{Phase: image.PhasePulling, BytesCurr: total / 2, BytesTotal: total},
		// Reaches 100%.
		{Phase: image.PhasePulling, BytesCurr: int64(total), BytesTotal: total},
		// Docker trailing events after completion — should all be dropped.
		{Phase: image.PhasePulling, BytesCurr: int64(total), BytesTotal: total},
		{Phase: image.PhasePulling, BytesCurr: int64(total), BytesTotal: total},
		{Phase: image.PhasePulling, BytesCurr: int64(total), BytesTotal: total},
	}
	tick := make(chan time.Time, 1)
	tick <- time.Now()
	out := driveProgress(t, context.Background(), events, tick, true)

	count := strings.Count(out, "100%")
	assert.Equal(t, 1, count, "100%% must appear exactly once, got output: %q", out)
}

// TestStderrProgress_PullCompleteOnPhaseTransition verifies that transitioning
// away from PhasePulling after saturation emits "pull complete" instead of
// reprinting the 100% line.
func TestStderrProgress_PullCompleteOnPhaseTransition(t *testing.T) {
	const total = 5 * 1024 * 1024
	events := []image.ProgressEvent{
		{Phase: image.PhasePulling},
		{Phase: image.PhasePulling, BytesCurr: int64(total), BytesTotal: total},
		// Phase transition — saturation path should print "pull complete".
		{Phase: image.PhaseExporting},
	}
	out := driveProgress(t, context.Background(), events, make(chan time.Time), true)

	assert.Contains(t, out, "pull complete", "saturated phase transition must print 'pull complete'")
	assert.Equal(t, 1, strings.Count(out, "100%"), "100%% must appear exactly once")
}
