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
			BytesTotal: 20 * 1024 * 1024,
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
