package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleResult builds a CompareResult exercising every section so the
// formatter is hit broadly without depending on Docker, the daemon, or any
// real analysis pipeline. Sizes are picked so FormatBytes/FormatSignedBytes
// produce stable, easy-to-assert strings.
func sampleResult() *image.CompareResult {
	return &image.CompareResult{
		Before: image.ImageSummary{ImageRef: "old:1", LayerCount: 2, TotalSize: 2048},
		After:  image.ImageSummary{ImageRef: "new:2", LayerCount: 3, TotalSize: 4096},
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

	assert.Contains(t, out, "old:  old:1  2 layers")
	assert.Contains(t, out, "new:  new:2  3 layers")
	assert.Contains(t, out, "LAYERS")
	assert.Contains(t, out, "FILE CHANGES")
	assert.Contains(t, out, "WASTE CHANGES")
	assert.Contains(t, out, "WARNINGS")
	assert.True(t, strings.HasSuffix(out, "verdict: regression reason=efficiency,waste\n"),
		"verdict line must be the final line; got tail: %q", lastLine(out))
}

func TestRenderCompareReport_Summary_OmitsTables(t *testing.T) {
	var buf bytes.Buffer
	renderCompareReport(&buf, sampleResult(), compareModeSummary, compareTopDefault)
	out := buf.String()

	assert.NotContains(t, out, "LAYERS")
	assert.NotContains(t, out, "FILE CHANGES")
	assert.NotContains(t, out, "WASTE CHANGES")
	assert.Contains(t, out, "WARNINGS")
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
			Path:      fmt.Sprintf("/f%02d", i),
			DiffType:  image.Modified,
			SizeDelta: int64(12 - i),
			ChangeReason: "size",
		})
		r.FileSummary.ModifiedCount++
	}

	var buf bytes.Buffer
	renderCompareReport(&buf, r, compareModeCompact, 5)
	out := buf.String()

	assert.Contains(t, out, "... and 7 more files",
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
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			flagCompareMode = tc.mode
			flagCompareTop = tc.top
			err := validateCompareFlags()
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

func TestTruncate_HandlesShortBudgets(t *testing.T) {
	assert.Equal(t, "abc", truncate("abc", 3))
	assert.Equal(t, "ab…", truncate("abcdef", 3))
	assert.Equal(t, "…", truncate("abcdef", 1))
	assert.Equal(t, "…", truncate("abcdef", 0))
}

// lastLine returns the final newline-terminated line of s for assertion
// messages. Empty trailing content (trailing newline only) is reported as
// "<empty>" to keep failure output readable.
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
