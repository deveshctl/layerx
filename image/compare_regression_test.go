package image

// Regression tests for confirmed compare bugs.
// See internal-docs/compare-audit.md for full analysis.
//
// All tests use in-process fixtures only — no Docker required.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B1: CompareAnalysis calls Efficiency(before.Layers) instead of
// EfficiencyFromAnalysis, which re-stacks already-stacked trees.
// When StackedTrees diverge from what Stack(layers) would produce,
// the efficiency score and file diffs disagree.
//
// This test builds an Analysis via AnalyzeWithOptions (the real pipeline)
// so StackedTrees are exactly what Stack produced. Both Efficiency(layers)
// and EfficiencyFromAnalysis(a) must then agree — the test pins that the
// CompareResult carries consistent scores.
func TestCompareAnalysis_EfficiencyConsistentWithStackedTrees(t *testing.T) {
	layers := []Layer{
		{Index: 0, Size: 100, Tree: makeTree(makeFile("app", "/app", 100))},
		{Index: 1, Size: 200, Tree: makeTree(makeFile("app", "/app", 200))},
	}
	resolver := &mockResolver{layers: layers}
	a, err := Analyze(context.Background(), resolver, "img")
	require.NoError(t, err)

	r := CompareAnalysis(a, a)
	require.NotNil(t, r)

	// Comparing an image with itself: score delta and waste delta must be zero.
	assert.Equal(t, float64(0), r.AfterEfficiency.ScoreDelta,
		"score delta must be zero when comparing identical analyses")
	assert.Equal(t, int64(0), r.AfterEfficiency.WastedBytesDelta,
		"waste delta must be zero when comparing identical analyses")
	assert.False(t, r.IsRegression(),
		"identical image must not report a regression")

	// The efficiency score must match what EfficiencyFromAnalysis computes
	// directly — confirming CompareAnalysis and the preferred entry point agree.
	directEff := EfficiencyFromAnalysis(a)
	assert.InDelta(t, directEff.Score, r.AfterEfficiency.Score, 1e-9,
		"CompareAnalysis score must agree with EfficiencyFromAnalysis")
	assert.Equal(t, directEff.WastedBytes, r.AfterEfficiency.WastedBytes,
		"CompareAnalysis waste must agree with EfficiencyFromAnalysis")
}

// B1 variant: two distinct multi-layer analyses. Before has wasted bytes;
// after fixes the waste. The score delta must be positive (improvement) and
// waste delta must be negative. IsRegression must be false.
func TestCompareAnalysis_WasteFixed_NotRegression(t *testing.T) {
	// before: /app written twice (100 + 200 bytes — 100 bytes wasted)
	beforeLayers := []Layer{
		{Index: 0, Size: 100, Tree: makeTree(makeFile("app", "/app", 100))},
		{Index: 1, Size: 200, Tree: makeTree(makeFile("app", "/app", 200))},
	}
	// after: /app written once (200 bytes — no waste)
	afterLayers := []Layer{
		{Index: 0, Size: 200, Tree: makeTree(makeFile("app", "/app", 200))},
	}

	beforeAnalysis, err := Analyze(context.Background(), &mockResolver{layers: beforeLayers}, "before")
	require.NoError(t, err)
	afterAnalysis, err := Analyze(context.Background(), &mockResolver{layers: afterLayers}, "after")
	require.NoError(t, err)

	r := CompareAnalysis(beforeAnalysis, afterAnalysis)
	require.NotNil(t, r)

	assert.True(t, r.AfterEfficiency.ScoreDelta > 0,
		"score delta must be positive when after improves; got %v", r.AfterEfficiency.ScoreDelta)
	assert.True(t, r.AfterEfficiency.WastedBytesDelta < 0,
		"waste delta must be negative when after fixes waste; got %v", r.AfterEfficiency.WastedBytesDelta)
	assert.False(t, r.IsRegression(),
		"fixing waste must not be reported as a regression")
}

// B1 variant: after introduces new waste. IsRegression must fire.
func TestCompareAnalysis_WasteIntroduced_IsRegression(t *testing.T) {
	// before: /app written once — no waste
	beforeLayers := []Layer{
		{Index: 0, Size: 200, Tree: makeTree(makeFile("app", "/app", 200))},
	}
	// after: /app written twice — 200 bytes wasted
	afterLayers := []Layer{
		{Index: 0, Size: 200, Tree: makeTree(makeFile("app", "/app", 200))},
		{Index: 1, Size: 200, Tree: makeTree(makeFile("app", "/app", 200))},
	}

	beforeAnalysis, err := Analyze(context.Background(), &mockResolver{layers: beforeLayers}, "before")
	require.NoError(t, err)
	afterAnalysis, err := Analyze(context.Background(), &mockResolver{layers: afterLayers}, "after")
	require.NoError(t, err)

	r := CompareAnalysis(beforeAnalysis, afterAnalysis)
	require.NotNil(t, r)

	assert.True(t, r.AfterEfficiency.WastedBytesDelta > 0,
		"waste delta must be positive when after introduces waste; got %v", r.AfterEfficiency.WastedBytesDelta)
	assert.True(t, r.IsRegression(), "introducing waste must be reported as a regression")
	reasons := r.RegressionReasons()
	assert.Contains(t, reasons, "waste")
}

// B2: RegressionReasons order must match the documented canonical order
// ["efficiency", "waste"] even when both conditions are true simultaneously.
// This locks the verdict line format that CI parsers depend on.
func TestCompareAnalysis_RegressionReasons_CanonicalOrder(t *testing.T) {
	// Both score dropped AND waste increased.
	r := &CompareResult{
		BeforeEfficiency: EfficiencySummary{Score: 0.95, WastedBytes: 50},
		AfterEfficiency: EfficiencySummary{
			Score:            0.80,
			WastedBytes:      200,
			ScoreDelta:       -0.15,
			WastedBytesDelta: 150,
		},
	}
	reasons := r.RegressionReasons()
	require.Len(t, reasons, 2)
	assert.Equal(t, "efficiency", reasons[0], "efficiency must precede waste in canonical order")
	assert.Equal(t, "waste", reasons[1])
}

// B2 corollary: when only waste regresses (score unchanged), the reasons
// slice must contain only "waste" — not "efficiency".
func TestCompareAnalysis_RegressionReasons_WasteOnly(t *testing.T) {
	r := &CompareResult{
		AfterEfficiency: EfficiencySummary{
			ScoreDelta:       0,
			WastedBytesDelta: 100,
		},
	}
	reasons := r.RegressionReasons()
	require.Len(t, reasons, 1)
	assert.Equal(t, "waste", reasons[0])
}

// B2 corollary: when only efficiency regresses (waste unchanged), reasons
// must contain only "efficiency".
func TestCompareAnalysis_RegressionReasons_EfficiencyOnly(t *testing.T) {
	r := &CompareResult{
		AfterEfficiency: EfficiencySummary{
			ScoreDelta:       -0.10,
			WastedBytesDelta: 0,
		},
	}
	reasons := r.RegressionReasons()
	require.Len(t, reasons, 1)
	assert.Equal(t, "efficiency", reasons[0])
}

// B3 (coverage gap): exercising CompareAnalysis with real Stack output for
// a multi-layer before→after where a file is whited out in after.
// collectLiveFiles must treat the whited-out path as absent (no FileDiff).
func TestCompareAnalysis_RealStackedAnalysis_WhiteoutedFileAbsent(t *testing.T) {
	// before: /a and /b both live
	beforeLayers := []Layer{
		{Index: 0, Tree: makeTree(
			makeFile("a", "/a", 10),
			makeFile("b", "/b", 20),
		)},
	}
	// after: /b removed via whiteout in layer 1
	afterLayers := []Layer{
		{Index: 0, Tree: makeTree(
			makeFile("a", "/a", 10),
			makeFile("b", "/b", 20),
		)},
		{Index: 1, Tree: makeTree(
			makeFile(".wh.b", "/.wh.b", 0),
		)},
	}

	beforeA, err := Analyze(context.Background(), &mockResolver{layers: beforeLayers}, "before")
	require.NoError(t, err)
	afterA, err := Analyze(context.Background(), &mockResolver{layers: afterLayers}, "after")
	require.NoError(t, err)

	r := CompareAnalysis(beforeA, afterA)
	require.NotNil(t, r)

	require.Len(t, r.FileDiffs, 1, "only /b should appear in FileDiffs (removed)")
	assert.Equal(t, "/b", r.FileDiffs[0].Path)
	assert.Equal(t, Removed, r.FileDiffs[0].DiffType)
}

// B3 (coverage gap): a file modified across layers surfaces as Modified in FileDiffs.
func TestCompareAnalysis_RealStackedAnalysis_ModifiedFile(t *testing.T) {
	// before: /app = 100
	beforeLayers := []Layer{
		{Index: 0, Tree: makeTree(makeFile("app", "/app", 100))},
	}
	// after: /app = 200 (same path, bigger)
	afterLayers := []Layer{
		{Index: 0, Tree: makeTree(makeFile("app", "/app", 100))},
		{Index: 1, Tree: makeTree(makeFile("app", "/app", 200))},
	}

	beforeA, err := Analyze(context.Background(), &mockResolver{layers: beforeLayers}, "before")
	require.NoError(t, err)
	afterA, err := Analyze(context.Background(), &mockResolver{layers: afterLayers}, "after")
	require.NoError(t, err)

	r := CompareAnalysis(beforeA, afterA)
	require.NotNil(t, r)

	require.Len(t, r.FileDiffs, 1)
	d := r.FileDiffs[0]
	assert.Equal(t, "/app", d.Path)
	assert.Equal(t, Modified, d.DiffType)
	assert.Equal(t, "size", d.ChangeReason)
	assert.Equal(t, int64(100), d.SizeDelta)
}

// Stability: CompareAnalysis with no layers on either side must not panic
// and must return an empty but valid result.
func TestCompareAnalysis_BothEmpty_NoLayers(t *testing.T) {
	empty, err := Analyze(context.Background(), &mockResolver{layers: []Layer{}}, "empty")
	require.NoError(t, err)

	r := CompareAnalysis(empty, empty)
	require.NotNil(t, r)
	assert.Empty(t, r.FileDiffs)
	assert.Empty(t, r.LayerDiffs)
	assert.False(t, r.IsRegression())
}

// Stability: WasteDiffs must not include paths whose delta is zero.
// This guards against a map-iteration regression that could re-include
// unchanged entries.
func TestCompareAnalysis_WasteDiff_ZeroDeltaExcluded(t *testing.T) {
	// /a appears in both before and after with identical waste.
	// /b appears only in before (waste eliminated in after).
	sharedLayers := []Layer{
		{Index: 0, Tree: makeTree(makeFile("a", "/a", 50))},
		{Index: 1, Tree: makeTree(makeFile("a", "/a", 50))},
	}
	beforeLayers := append(sharedLayers, Layer{
		Index: 2, Tree: makeTree(makeFile("b", "/b", 100)),
	})
	// after: same /a waste, /b gone
	afterLayers := sharedLayers

	beforeA, err := Analyze(context.Background(), &mockResolver{layers: beforeLayers}, "before")
	require.NoError(t, err)
	afterA, err := Analyze(context.Background(), &mockResolver{layers: afterLayers}, "after")
	require.NoError(t, err)

	r := CompareAnalysis(beforeA, afterA)
	require.NotNil(t, r)

	for _, wd := range r.WasteDiffs {
		assert.NotEqual(t, int64(0), wd.WastedDelta,
			"WasteDiff with zero delta must not appear; path=%s", wd.Path)
	}
}
