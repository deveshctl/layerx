package ci

import (
	"testing"

	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
)

// evalOne runs a rule and asserts it returned exactly one result, then
// returns that result. Every existing rule emits a single result; using
// this helper keeps these tests as readable as before the migration.
func evalOne(t *testing.T, r Rule, ctx EvalContext) RuleResult {
	t.Helper()
	results := r.Evaluate(ctx)
	if len(results) != 1 {
		t.Fatalf("rule %s emitted %d results, want 1", r.Name(), len(results))
	}
	return results[0]
}

func TestLowestEfficiency_Pass(t *testing.T) {
	r := LowestEfficiency{Threshold: 0.9}
	result := evalOne(t, r, EvalContext{Efficiency: &image.EfficiencyResult{Score: 0.95}})
	assert.True(t, result.Passed)
	assert.Equal(t, "efficiency", result.Name)
}

func TestLowestEfficiency_Fail(t *testing.T) {
	r := LowestEfficiency{Threshold: 0.9}
	result := evalOne(t, r, EvalContext{Efficiency: &image.EfficiencyResult{Score: 0.85}})
	assert.False(t, result.Passed)
}

func TestLowestEfficiency_Exact(t *testing.T) {
	r := LowestEfficiency{Threshold: 0.9}
	result := evalOne(t, r, EvalContext{Efficiency: &image.EfficiencyResult{Score: 0.9}})
	assert.True(t, result.Passed)
}

func TestHighestWastedBytes_Pass(t *testing.T) {
	r := HighestWastedBytes{Threshold: 1000}
	result := evalOne(t, r, EvalContext{Efficiency: &image.EfficiencyResult{WastedBytes: 500}})
	assert.True(t, result.Passed)
}

func TestHighestWastedBytes_Fail(t *testing.T) {
	r := HighestWastedBytes{Threshold: 1000}
	result := evalOne(t, r, EvalContext{Efficiency: &image.EfficiencyResult{WastedBytes: 1500}})
	assert.False(t, result.Passed)
}

func TestHighestWastedBytes_DisabledWhenZero(t *testing.T) {
	r := HighestWastedBytes{Threshold: 0}
	result := evalOne(t, r, EvalContext{Efficiency: &image.EfficiencyResult{WastedBytes: 999999}})
	assert.True(t, result.Passed)
}

func TestHighestUserWastedPercent_Pass(t *testing.T) {
	r := HighestUserWastedPercent{Threshold: 0.1}
	// Score = 0.95 → waste fraction = 5%, under 10% threshold.
	result := evalOne(t, r, EvalContext{Efficiency: &image.EfficiencyResult{Score: 0.95}})
	assert.True(t, result.Passed)
}

func TestHighestUserWastedPercent_Fail(t *testing.T) {
	r := HighestUserWastedPercent{Threshold: 0.1}
	// Score = 0.80 → waste fraction = 20%, over 10% threshold.
	result := evalOne(t, r, EvalContext{Efficiency: &image.EfficiencyResult{Score: 0.80}})
	assert.False(t, result.Passed)
}

func TestHighestUserWastedPercent_ZeroWaste(t *testing.T) {
	r := HighestUserWastedPercent{Threshold: 0.1}
	// Score = 1.0 → waste fraction = 0%, always passes.
	result := evalOne(t, r, EvalContext{Efficiency: &image.EfficiencyResult{Score: 1.0}})
	assert.True(t, result.Passed)
}

func TestHighestUserWastedPercent_DisabledWhenZero(t *testing.T) {
	r := HighestUserWastedPercent{Threshold: 0}
	result := evalOne(t, r, EvalContext{Efficiency: &image.EfficiencyResult{Score: 0.0}})
	assert.True(t, result.Passed)
}

// Actual / Threshold render as percentages so operators reading
// "wasted %: 0.10 (threshold: 0.10)" don't misread 10% as 0.10%.
func TestHighestUserWastedPercent_ActualAndThresholdRenderedAsPercent(t *testing.T) {
	r := HighestUserWastedPercent{Threshold: 0.1}
	// Score = 0.90 → waste fraction = 10%.
	result := evalOne(t, r, EvalContext{Efficiency: &image.EfficiencyResult{Score: 0.90}})
	assert.Equal(t, "10.0%", result.Actual)
	assert.Equal(t, "10.0%", result.Threshold)
}

// TestHighestUserWastedPercent_BoundaryExact verifies that a waste fraction
// exactly equal to the threshold passes. Uses realistic byte counts so the
// efficiency score is derived from integer arithmetic rather than a hand-
// crafted float, exposing any floating-point boundary failures.
//
// Fixture: 95 live bytes + 5 wasted bytes → score = 95/100 = 0.95
// Threshold = 0.05 (5%). The waste fraction is exactly at the limit → PASS.
func TestHighestUserWastedPercent_BoundaryExact(t *testing.T) {
	// Build two layers: layer0 writes file A (5 bytes), layer1 overwrites it.
	// Result: 5 wasted bytes, 5 live bytes (the overwrite) — but we want
	// exactly 5% waste, so we need: wastedBytes=5, liveBytes=95.
	// Simplest: layer0 writes A (5 bytes) + B (95 bytes); layer1 overwrites A.
	tree0 := image.NewFileTree()
	tree0.Root.AddChild(&image.FileNode{Name: "a", Path: "/a", Size: 5, DiffType: image.Added})
	tree0.Root.AddChild(&image.FileNode{Name: "b", Path: "/b", Size: 95, DiffType: image.Added})
	tree1 := image.NewFileTree()
	tree1.Root.AddChild(&image.FileNode{Name: "a", Path: "/a", Size: 5, DiffType: image.Modified})

	layers := []image.Layer{
		{Index: 0, Tree: tree0},
		{Index: 1, Tree: tree1},
	}
	eff := image.Efficiency(layers)
	// Sanity: score should be 0.95 (5 wasted out of 100 total uncompressed).
	assert.InDelta(t, 0.95, eff.Score, 0.001, "fixture score mismatch")

	r := HighestUserWastedPercent{Threshold: 0.05}
	result := evalOne(t, r, EvalContext{Efficiency: eff})
	assert.True(t, result.Passed, "exactly-at-threshold must pass (got actual=%s)", result.Actual)
}

// TestHighestUserWastedPercent_BoundaryBelow verifies a value just below the
// threshold passes.
func TestHighestUserWastedPercent_BoundaryBelow(t *testing.T) {
	// 4 wasted, 96 live → score = 96/100 = 0.96, waste fraction = 4%.
	tree0 := image.NewFileTree()
	tree0.Root.AddChild(&image.FileNode{Name: "a", Path: "/a", Size: 4, DiffType: image.Added})
	tree0.Root.AddChild(&image.FileNode{Name: "b", Path: "/b", Size: 96, DiffType: image.Added})
	tree1 := image.NewFileTree()
	tree1.Root.AddChild(&image.FileNode{Name: "a", Path: "/a", Size: 4, DiffType: image.Modified})
	layers := []image.Layer{{Index: 0, Tree: tree0}, {Index: 1, Tree: tree1}}
	eff := image.Efficiency(layers)

	r := HighestUserWastedPercent{Threshold: 0.05}
	result := evalOne(t, r, EvalContext{Efficiency: eff})
	assert.True(t, result.Passed, "below threshold must pass (got actual=%s)", result.Actual)
}

// TestHighestUserWastedPercent_BoundaryAbove verifies a value just above the
// threshold fails.
func TestHighestUserWastedPercent_BoundaryAbove(t *testing.T) {
	// 6 wasted, 94 live → score = 94/100 = 0.94, waste fraction = 6%.
	tree0 := image.NewFileTree()
	tree0.Root.AddChild(&image.FileNode{Name: "a", Path: "/a", Size: 6, DiffType: image.Added})
	tree0.Root.AddChild(&image.FileNode{Name: "b", Path: "/b", Size: 94, DiffType: image.Added})
	tree1 := image.NewFileTree()
	tree1.Root.AddChild(&image.FileNode{Name: "a", Path: "/a", Size: 6, DiffType: image.Modified})
	layers := []image.Layer{{Index: 0, Tree: tree0}, {Index: 1, Tree: tree1}}
	eff := image.Efficiency(layers)

	r := HighestUserWastedPercent{Threshold: 0.05}
	result := evalOne(t, r, EvalContext{Efficiency: eff})
	assert.False(t, result.Passed, "above threshold must fail (got actual=%s)", result.Actual)
}
