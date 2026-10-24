package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/deveshctl/layerx/image"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleResult builds a CompareResult exercising every section so the
// formatter is hit broadly without depending on Docker, the daemon, or any
// real analysis pipeline. Sizes are picked so FormatBytes/FormatSignedBytes
// produce stable, easy-to-assert strings.
func sampleResult() *image.CompareResult {
	return &image.CompareResult{
		Before: image.ImageSummary{ImageRef: "old:1", LayerCount: 2, TotalSize: 2048, UncompressedSize: 2048, Platform: "linux/amd64"},
		After:  image.ImageSummary{ImageRef: "new:2", LayerCount: 3, TotalSize: 4096, UncompressedSize: 4096, Platform: "linux/amd64"},
		BeforeEfficiency: image.EfficiencySummary{
			Score: 0.95, WastedBytes: 100,
		},
		AfterEfficiency: image.EfficiencySummary{
			Score: 0.80, WastedBytes: 500,
			ScoreDelta:       -0.15,
			WastedBytesDelta: 400,
		},
		LayerDiffs: []image.LayerDiff{
			{Index: 0, BeforeSize: 1024, AfterSize: 1024, SizeDelta: 0, BeforeCommand: "FROM alpine", AfterCommand: "FROM alpine", CommandsMatch: true},
			{Index: 1, BeforeSize: 1024, AfterSize: 2048, SizeDelta: 1024, BeforeCommand: "RUN x", AfterCommand: "RUN y", CommandsMatch: false},
			{Index: 2, BeforeSize: 0, AfterSize: 1024, SizeDelta: 1024, BeforeCommand: "", AfterCommand: "COPY ./ /", CommandsMatch: false},
		},
		FileDiffs: []image.FileDiff{
			{Path: "/a", DiffType: image.Added, AfterSize: 512, SizeDelta: 512},
			{Path: "/b", DiffType: image.Modified, BeforeSize: 100, AfterSize: 200, SizeDelta: 100, ChangeReason: "size"},
			{Path: "/c", DiffType: image.Removed, BeforeSize: 64, SizeDelta: -64},
		},
		FileSummary: image.FileDiffSummary{AddedCount: 1, ModifiedCount: 1, RemovedCount: 1, BytesAdded: 612, BytesRemoved: 64},
		WasteDiffs: []image.WasteDiff{
			{Path: "/big", BeforeWasted: 0, AfterWasted: 400, WastedDelta: 400},
		},
		Warnings: []string{"layer count differs: before=2, after=3"},
	}
}

func TestRenderCompareReport_Compact_VerdictRegression(t *testing.T) {
	var buf bytes.Buffer
	renderCompareReport(&buf, sampleResult(), compareModeCompact, compareTopDefault)
	out := buf.String()

	assert.Contains(t, out, "Before: old:1")
	assert.Contains(t, out, "After:  new:2")
	assert.Contains(t, out, "Uncompressed layers")
	assert.Contains(t, out, "Layer changes")
	assert.Contains(t, out, "File changes")
	assert.Contains(t, out, "Waste changes")
	assert.Contains(t, out, "Comparison notes")
	assert.Contains(t, out, "95.0%")
	assert.Contains(t, out, "-15.0 pp")

	assert.True(t, strings.HasSuffix(out, "verdict: regression reason=efficiency,waste\n"),
		"verdict line must be the final line; got tail: %q", lastLine(out))
}

func TestRenderCompareReport_Summary_OmitsTables(t *testing.T) {
	var buf bytes.Buffer
	renderCompareReport(&buf, sampleResult(), compareModeSummary, compareTopDefault)
	out := buf.String()

	assert.NotContains(t, out, "Layer changes")
	assert.NotContains(t, out, "File changes")
	assert.NotContains(t, out, "Waste changes")
	assert.Contains(t, out, "Comparison notes")
	assert.True(t, strings.HasSuffix(out, "verdict: regression reason=efficiency,waste\n"))
}

func TestRenderCompareReport_Verdict_OK(t *testing.T) {
	r := &image.CompareResult{
		Before:           image.ImageSummary{ImageRef: "img"},
		After:            image.ImageSummary{ImageRef: "img"},
		BeforeEfficiency: image.EfficiencySummary{Score: 0.9, WastedBytes: 0},
		AfterEfficiency:  image.EfficiencySummary{Score: 0.9, WastedBytes: 0},
	}
	var buf bytes.Buffer
	renderCompareReport(&buf, r, compareModeCompact, compareTopDefault)
	assert.True(t, strings.HasSuffix(buf.String(), "verdict: ok\n"),
		"verdict line must be ok when nothing regressed; got %q", buf.String())
}

func TestRenderCompareReport_Verdict_OnlyEfficiency(t *testing.T) {
	r := &image.CompareResult{
		BeforeEfficiency: image.EfficiencySummary{Score: 0.95, WastedBytes: 50},
		AfterEfficiency:  image.EfficiencySummary{Score: 0.80, WastedBytes: 50, ScoreDelta: -0.15},
	}
	var buf bytes.Buffer
	renderCompareReport(&buf, r, compareModeCompact, compareTopDefault)
	assert.True(t, strings.HasSuffix(buf.String(), "verdict: regression reason=efficiency\n"))
}

func TestRenderCompareReport_Verdict_OnlyWaste(t *testing.T) {
	r := &image.CompareResult{
		BeforeEfficiency: image.EfficiencySummary{Score: 0.9, WastedBytes: 0},
		AfterEfficiency:  image.EfficiencySummary{Score: 0.9, WastedBytes: 100, WastedBytesDelta: 100},
	}
	var buf bytes.Buffer
	renderCompareReport(&buf, r, compareModeCompact, compareTopDefault)
	assert.True(t, strings.HasSuffix(buf.String(), "verdict: regression reason=waste\n"))
}

func TestRenderCompareReport_NilResult_VerdictOK(t *testing.T) {
	var buf bytes.Buffer
	renderCompareReport(&buf, nil, compareModeCompact, compareTopDefault)
	assert.Equal(t, "verdict: ok\n", buf.String())
}

func TestRenderCompareReport_TopN_TruncatesFilesAndAppendsCounter(t *testing.T) {
	r := &image.CompareResult{}
	for i := range 12 {
		// Use varying SizeDelta so the rank is non-trivial; magnitudes
		// 12, 11, ... 1 ensures top-5 picks the largest five.
		r.FileDiffs = append(r.FileDiffs, image.FileDiff{
			Path:         fmt.Sprintf("/f%02d", i),
			DiffType:     image.Modified,
			SizeDelta:    int64(12 - i),
			ChangeReason: "size",
		})
		r.FileSummary.ModifiedCount++
	}

	var buf bytes.Buffer
	renderCompareReport(&buf, r, compareModeCompact, 5)
	out := buf.String()

	assert.Contains(t, out, "… 7 more files",
		"compact mode must announce truncation count")
	assert.Contains(t, out, "/f00", "largest delta must survive truncation")
	assert.NotContains(t, out, "/f11", "smallest delta must be truncated out")
}

func TestRenderCompareReport_FullMode_NoTruncation(t *testing.T) {
	r := &image.CompareResult{}
	for i := range 12 {
		r.FileDiffs = append(r.FileDiffs, image.FileDiff{
			Path:      fmt.Sprintf("/f%02d", i),
			DiffType:  image.Added,
			SizeDelta: 1,
			AfterSize: 1,
		})
		r.FileSummary.AddedCount++
	}

	var buf bytes.Buffer
	renderCompareReport(&buf, r, compareModeFull, 5)
	out := buf.String()

	assert.NotContains(t, out, "... and ", "full mode must not truncate")
	for i := range 12 {
		assert.Contains(t, out, fmt.Sprintf("/f%02d", i))
	}
}

func TestValidateCompareFlags(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		top     int
		wantErr bool
	}{
		{"compact default", compareModeCompact, compareTopDefault, false},
		{"full mode", compareModeFull, compareTopDefault, false},
		{"summary mode", compareModeSummary, compareTopDefault, false},
		{"unknown mode", "verbose", compareTopDefault, true},
		{"top below min", compareModeCompact, 0, true},
		{"top above max", compareModeCompact, compareTopMax + 1, true},
		{"top at min", compareModeCompact, compareTopMin, false},
		{"top at max", compareModeCompact, compareTopMax, false},
		// --top is documented as ignored in summary/full modes; passing an
		// out-of-range value must not surface an error in those modes.
		{"summary mode ignores top=0", compareModeSummary, 0, false},
		{"full mode ignores top=0", compareModeFull, 0, false},
		{"summary mode ignores oversized top", compareModeSummary, compareTopMax + 100, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			flagCompareMode = tc.mode
			flagCompareTop = tc.top
			err := validateCompareFlags(tc.mode, tc.top)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
	flagCompareMode = compareModeCompact
	flagCompareTop = compareTopDefault
}

// main.go maps a non-nil cobra error to exit 1 only when the chain contains
// *ErrCIFailed OR *ErrCompareRegression; other errors exit 2. Lock that
// contract here so a future change can't silently collapse exit codes.
func TestErrCompareRegression_DetectableThroughWrapping(t *testing.T) {
	sentinel := &ErrCompareRegression{}
	wrapped := fmt.Errorf("running compare: %w", sentinel)

	var got *ErrCompareRegression
	assert.True(t, errors.As(sentinel, &got), "bare sentinel must match")
	assert.True(t, errors.As(wrapped, &got), "wrapped sentinel must match")
	assert.False(t, errors.As(errors.New("unrelated"), &got), "plain errors must not match")
	assert.False(t, errors.As(fmt.Errorf("docker daemon down"), &got), "internal errors must not match")
}

func TestRankFileDiffsByDelta_StableAndDescending(t *testing.T) {
	in := []image.FileDiff{
		{Path: "/x", SizeDelta: 50},
		{Path: "/a", SizeDelta: -100},
		{Path: "/b", SizeDelta: 100},
		{Path: "/c", SizeDelta: 0},
	}
	out := rankFileDiffsByDelta(in)
	require.Len(t, out, 4)
	// |delta| desc: 100, 100, 50, 0; tie between /a and /b broken by Path asc.
	assert.Equal(t, "/a", out[0].Path)
	assert.Equal(t, "/b", out[1].Path)
	assert.Equal(t, "/x", out[2].Path)
	assert.Equal(t, "/c", out[3].Path)
}

func TestFitCompareEndPreservesFilename(t *testing.T) {
	assert.Equal(t, "file.txt", fitCompareEnd("file.txt", 20))
	assert.Equal(t, "…file.txt", fitCompareEnd("/long/path/file.txt", 9))
}

func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	if s == "" {
		return "<empty>"
	}
	return s
}

func TestCompareArgs_NoArgs_PrintsUsageHint(t *testing.T) {
	cmd := &cobra.Command{}
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	err := compareArgs(cmd, nil)

	require.Error(t, err)
	var sentinel *ErrCompareUsage
	assert.True(t, errors.As(err, &sentinel), "must return ErrCompareUsage sentinel")

	out := stderr.String()
	assert.Contains(t, out, "expected 2 images or archives")
	assert.Contains(t, out, "OLD_IMAGE NEW_IMAGE")
	assert.Contains(t, out, "Examples:")
	assert.Contains(t, out, "app:1.4.0 app:1.5.0")
	assert.Contains(t, out, "--help")
}

func TestCompareArgs_OneArg_PrintsCountHint(t *testing.T) {
	cmd := &cobra.Command{}
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	err := compareArgs(cmd, []string{"only-one"})

	require.Error(t, err)
	var sentinel *ErrCompareUsage
	assert.True(t, errors.As(err, &sentinel))
	assert.Contains(t, stderr.String(), "expected 2 images or archives, got 1")
}

func TestCompareArgs_TwoArgs_PassesThrough(t *testing.T) {
	cmd := &cobra.Command{}
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	err := compareArgs(cmd, []string{"a", "b"})

	assert.NoError(t, err)
	assert.Empty(t, stderr.String(), "happy path must not print to stderr")
}

func TestErrCompareUsage_DetectableThroughWrapping(t *testing.T) {
	sentinel := &ErrCompareUsage{}
	wrapped := fmt.Errorf("wrap: %w", sentinel)

	var got *ErrCompareUsage
	assert.True(t, errors.As(sentinel, &got))
	assert.True(t, errors.As(wrapped, &got))
	assert.False(t, errors.As(errors.New("unrelated"), &got))
}

func TestRenderNoOp_PlainLanguageAndVerdict(t *testing.T) {
	var buf bytes.Buffer
	renderNoOp(&buf, "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")
	out := buf.String()

	assert.Contains(t, out, "Both inputs resolve to the same image content")
	assert.Contains(t, out, "no diff to show")
	assert.Contains(t, out, "digest:")
	// short digest shown (first 19 chars of input + "...")
	assert.Contains(t, out, "sha256:abcdef012345...")
	// full digest also shown so scripts can match
	assert.Contains(t, out, "(full: sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789)")
	// machine-parseable verdict line is preserved verbatim
	assert.Contains(t, out, "verdict: noop digest=sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")
	assert.True(t, strings.HasSuffix(out, "\n"), "must end with newline")
}

// Empty digest path: archive resolvers don't always expose an ImageID, so the
// path-equality short-circuit calls renderNoOp(out, ""). The verdict line must
// stay parseable (`reason=path-equal`, no empty `digest=`) and the no-op body
// must not print a stray "digest:" line with nothing after it.
func TestRenderNoOp_EmptyDigestUsesPathEqualReason(t *testing.T) {
	var buf bytes.Buffer
	renderNoOp(&buf, "")
	out := buf.String()

	assert.Contains(t, out, "Both inputs resolve to the same image content")
	assert.Contains(t, out, "verdict: noop reason=path-equal")
	assert.NotContains(t, out, "digest=", "empty digest must not produce a digest= verdict")
	assert.NotContains(t, out, "digest:", "empty digest must not print a stray short-digest line")
	assert.True(t, strings.HasSuffix(out, "\n"), "must end with newline")
}

func TestShortDigest(t *testing.T) {
	long := "sha256:abcdef0123456789ffffffffffffffffffff"
	short := shortDigest(long)
	// First 19 runes of the input ("sha256:" + 12 hex chars) plus "...".
	assert.Equal(t, "sha256:abcdef012345...", short)
	// Inputs at or under the budget are returned as-is.
	assert.Equal(t, "short", shortDigest("short"))
}

func TestDrainProgress_RemoteFlow(t *testing.T) {
	var buf bytes.Buffer
	ch := make(chan image.ProgressEvent, 8)
	// Coalesce=0 + frozen clock means every pulling event is emitted.
	now := func() time.Time { return time.Unix(0, 0) }

	go func() {
		ch <- image.ProgressEvent{Phase: image.PhasePulling, LayersDone: 0, LayersTotal: 0}
		ch <- image.ProgressEvent{Phase: image.PhasePulling, LayersDone: 1, LayersTotal: 3,
			BytesCurr: 10 * 1024, BytesTotal: 30 * 1024}
		ch <- image.ProgressEvent{Phase: image.PhaseExporting}
		ch <- image.ProgressEvent{Phase: image.PhaseParsing}
		close(ch)
	}()
	drainProgressWithClock(&buf, "old", "nginx:latest", ch, 0, now)
	out := buf.String()

	assert.Contains(t, out, "[old] resolving nginx:latest", "first event announces side+ref")
	assert.Contains(t, out, "[old] pulling image", "phase entry line printed once")
	assert.Contains(t, out, "[old] pulling: 1/3 layers, 10.0 KB / 30.0 KB")
	assert.Contains(t, out, "[old] exporting image")
	assert.Contains(t, out, "[old] parsing layers")
}

func TestDrainProgress_PullCoalescing(t *testing.T) {
	var buf bytes.Buffer
	ch := make(chan image.ProgressEvent, 8)
	// Frozen clock + non-zero coalesce: pulling events after the first
	// detail line should be dropped because clock never advances.
	now := func() time.Time { return time.Unix(100, 0) }

	go func() {
		ch <- image.ProgressEvent{Phase: image.PhasePulling, LayersTotal: 1, BytesTotal: 100}
		// All these should be coalesced into the single first detail line.
		for i := range 20 {
			ch <- image.ProgressEvent{Phase: image.PhasePulling, LayersTotal: 1,
				BytesCurr: int64(i + 1), BytesTotal: 100}
		}
		ch <- image.ProgressEvent{Phase: image.PhaseParsing}
		close(ch)
	}()
	drainProgressWithClock(&buf, "new", "x", ch, time.Second, now)

	count := strings.Count(buf.String(), "[new] pulling:")
	// One "pulling image" entry line plus exactly one "pulling: ..." detail
	// line (the rest are coalesced by the unmoving clock).
	assert.LessOrEqual(t, count, 1, "coalescing must collapse repeated pulling events; got %d details", count)
	assert.Contains(t, buf.String(), "[new] pulling image")
	assert.Contains(t, buf.String(), "[new] parsing layers")
}

func TestDrainProgress_CacheHit(t *testing.T) {
	var buf bytes.Buffer
	ch := make(chan image.ProgressEvent, 4)
	go func() {
		ch <- image.ProgressEvent{Phase: image.PhaseCacheLoad}
		close(ch)
	}()
	drainProgressWithClock(&buf, "old", "alpine:3.19", ch, 0, time.Now)
	out := buf.String()

	assert.Contains(t, out, "[old] resolving alpine:3.19")
	assert.Contains(t, out, "[old] cache hit")
	assert.NotContains(t, out, "pulling")
	assert.NotContains(t, out, "exporting")
}

func TestDrainProgress_CacheWarn(t *testing.T) {
	var buf bytes.Buffer
	ch := make(chan image.ProgressEvent, 4)
	go func() {
		ch <- image.ProgressEvent{Phase: image.PhaseCacheWarn, Message: "disk full"}
		close(ch)
	}()
	drainProgressWithClock(&buf, "old", "x", ch, 0, time.Now)

	assert.Contains(t, buf.String(), "[old] cache warning: disk full")
}

func TestDrainProgress_NoEvents_NoOutput(t *testing.T) {
	var buf bytes.Buffer
	ch := make(chan image.ProgressEvent)
	close(ch)
	drainProgressWithClock(&buf, "old", "x", ch, 0, time.Now)
	assert.Empty(t, buf.String(), "no events => no announce line, no noise")
}

func TestRenderCompareReport_Alignment_NumericColumnsAlignAcrossRows(t *testing.T) {
	var out bytes.Buffer
	renderCompareReport(&out, sampleResult(), compareModeSummary, 10)
	sizeEnd, wasteEnd := -1, -1
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "  Uncompressed layers") {
			sizeEnd = strings.Index(line, "2.05 KB") + len("2.05 KB")
		}
		if strings.HasPrefix(line, "  Layer waste") {
			wasteEnd = strings.Index(line, "100 B") + len("100 B")
		}
	}
	require.Greater(t, sizeEnd, 0)
	require.Greater(t, wasteEnd, 0)
	assert.Equal(t, sizeEnd, wasteEnd, "Before values must be right-aligned despite different widths")
}

func TestRenderCompareReport_NoOpHelperDirectly(t *testing.T) {
	// renderNoOp is exercised through runCompareCmd; this guards the helper
	// shape so a refactor that changes the verdict format is caught locally.
	var buf bytes.Buffer
	renderNoOp(&buf, "sha256:deadbeefdeadbeefdeadbeefdeadbeef")
	out := buf.String()
	require.True(t, strings.HasSuffix(out, "\n"))
	last := lastLine(out)
	assert.True(t, strings.HasPrefix(last, "verdict: noop digest=sha256:"),
		"final line must be machine-parseable verdict; got %q", last)
}

// compareFakeResolver is a minimal Resolver for testing analyzeForCompare's wiring.
// All methods return defaults; tests substitute hooks via the function fields.
type compareFakeResolver struct {
	resolveFn func(ctx context.Context, ref string, progress chan<- image.ProgressEvent) ([]image.Layer, error)
	imageIDFn func(ctx context.Context, ref string) (string, error)
}

func (f *compareFakeResolver) Resolve(ctx context.Context, ref string) ([]image.Layer, error) {
	return f.ResolveWithProgress(ctx, ref, nil)
}
func (f *compareFakeResolver) ResolveWithProgress(ctx context.Context, ref string, p chan<- image.ProgressEvent) ([]image.Layer, error) {
	if f.resolveFn != nil {
		return f.resolveFn(ctx, ref, p)
	}
	return nil, nil
}
func (f *compareFakeResolver) Inspect(ctx context.Context, ref string) (*image.ImageMeta, error) {
	return &image.ImageMeta{}, nil
}
func (f *compareFakeResolver) ImageID(ctx context.Context, ref string) (string, error) {
	if f.imageIDFn != nil {
		return f.imageIDFn(ctx, ref)
	}
	return "", nil
}

// analyzeForCompare must close(progress) on every exit path so the drain
// goroutine isn't leaked. Use a panicking resolver to exercise the
// defer-close behavior.
func TestAnalyzeForCompare_PanicInResolverUnwindsCleanly(t *testing.T) {
	res := &compareFakeResolver{
		resolveFn: func(ctx context.Context, ref string, p chan<- image.ProgressEvent) ([]image.Layer, error) {
			panic("simulated resolver crash")
		},
	}
	var stderr bytes.Buffer
	done := make(chan struct{})
	var panicked bool
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
			close(done)
		}()
		_, _, _ = analyzeForCompare(context.Background(), res, "x", false, "old", &stderr)
	}()
	select {
	case <-done:
		// expected — defer wg.Wait() / defer close(progress) unwound the goroutine.
	case <-time.After(2 * time.Second):
		t.Fatal("analyzeForCompare did not unwind within 2s — drain goroutine likely deadlocked")
	}
	assert.True(t, panicked, "panic must propagate out of analyzeForCompare")
}
