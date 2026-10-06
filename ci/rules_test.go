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
// Fixture: layer0 writes a(5)+b(90); layer1 overwrites a(5).
// liveBytes=5+90=95, wastedBytes=5, total=100.
// score = 1 - 5/100 = 0.95, pct = 5%. Threshold 5% → exactly passes.
func TestHighestUserWastedPercent_BoundaryExact(t *testing.T) {
	tree0 := image.NewFileTree()
	tree0.Root.AddChild(&image.FileNode{Name: "a", Path: "/a", Size: 5, DiffType: image.Added})
	tree0.Root.AddChild(&image.FileNode{Name: "b", Path: "/b", Size: 90, DiffType: image.Added})
	tree1 := image.NewFileTree()
	tree1.Root.AddChild(&image.FileNode{Name: "a", Path: "/a", Size: 5, DiffType: image.Modified})

	layers := []image.Layer{
		{Index: 0, Tree: tree0},
		{Index: 1, Tree: tree1},
	}
	eff := image.Efficiency(layers)
	assert.InDelta(t, 0.95, eff.Score, 0.001, "fixture score mismatch: want 5/100=0.95")
	assert.Equal(t, int64(5), eff.WastedBytes, "fixture waste mismatch: want 5 bytes wasted")

	r := HighestUserWastedPercent{Threshold: 0.05}
	result := evalOne(t, r, EvalContext{Efficiency: eff})
	assert.True(t, result.Passed, "exactly-at-threshold must pass (got actual=%s)", result.Actual)
}

// TestHighestUserWastedPercent_BoundaryBelow verifies a value just below the
// threshold passes.
//
// Fixture: layer0 writes a(4)+b(92); layer1 overwrites a(4).
// liveBytes=4+92=96, wastedBytes=4, total=100. pct=4% < 5% → passes.
func TestHighestUserWastedPercent_BoundaryBelow(t *testing.T) {
	tree0 := image.NewFileTree()
	tree0.Root.AddChild(&image.FileNode{Name: "a", Path: "/a", Size: 4, DiffType: image.Added})
	tree0.Root.AddChild(&image.FileNode{Name: "b", Path: "/b", Size: 92, DiffType: image.Added})
	tree1 := image.NewFileTree()
	tree1.Root.AddChild(&image.FileNode{Name: "a", Path: "/a", Size: 4, DiffType: image.Modified})
	layers := []image.Layer{{Index: 0, Tree: tree0}, {Index: 1, Tree: tree1}}
	eff := image.Efficiency(layers)
	assert.InDelta(t, 0.96, eff.Score, 0.001, "fixture score mismatch: want 4/100=0.96")
	assert.Equal(t, int64(4), eff.WastedBytes, "fixture waste mismatch: want 4 bytes wasted")

	r := HighestUserWastedPercent{Threshold: 0.05}
	result := evalOne(t, r, EvalContext{Efficiency: eff})
	assert.True(t, result.Passed, "below threshold must pass (got actual=%s)", result.Actual)
}

// TestHighestUserWastedPercent_BoundaryAbove verifies a value just above the
// threshold fails.
//
// Fixture: layer0 writes a(6)+b(88); layer1 overwrites a(6).
// liveBytes=6+88=94, wastedBytes=6, total=100. pct=6% > 5% → fails.
func TestHighestUserWastedPercent_BoundaryAbove(t *testing.T) {
	tree0 := image.NewFileTree()
	tree0.Root.AddChild(&image.FileNode{Name: "a", Path: "/a", Size: 6, DiffType: image.Added})
	tree0.Root.AddChild(&image.FileNode{Name: "b", Path: "/b", Size: 88, DiffType: image.Added})
	tree1 := image.NewFileTree()
	tree1.Root.AddChild(&image.FileNode{Name: "a", Path: "/a", Size: 6, DiffType: image.Modified})
	layers := []image.Layer{{Index: 0, Tree: tree0}, {Index: 1, Tree: tree1}}
	eff := image.Efficiency(layers)
	assert.InDelta(t, 0.94, eff.Score, 0.001, "fixture score mismatch: want 6/100=0.94")
	assert.Equal(t, int64(6), eff.WastedBytes, "fixture waste mismatch: want 6 bytes wasted")

	r := HighestUserWastedPercent{Threshold: 0.05}
	result := evalOne(t, r, EvalContext{Efficiency: eff})
	assert.False(t, result.Passed, "above threshold must fail (got actual=%s)", result.Actual)
}

// TestHighestUserWastedPercent_Boundary30Pct verifies the boundary behaviour
// at a different threshold (30%) to confirm the rule is not hard-coded for 5%.
//
// Fixture: layer0 writes a(30)+b(40); layer1 overwrites a(30).
// liveBytes=30+40=70, wastedBytes=30, total=100.
// score=1-30/100=0.70, pct=30%. Tests at/above threshold.
func TestHighestUserWastedPercent_Boundary30Pct(t *testing.T) {
	buildLayers := func(aSize, bSize int64) *image.EfficiencyResult {
		tree0 := image.NewFileTree()
		tree0.Root.AddChild(&image.FileNode{Name: "a", Path: "/a", Size: aSize, DiffType: image.Added})
		tree0.Root.AddChild(&image.FileNode{Name: "b", Path: "/b", Size: bSize, DiffType: image.Added})
		tree1 := image.NewFileTree()
		tree1.Root.AddChild(&image.FileNode{Name: "a", Path: "/a", Size: aSize, DiffType: image.Modified})
		return image.Efficiency([]image.Layer{{Index: 0, Tree: tree0}, {Index: 1, Tree: tree1}})
	}

	// Exactly 30%: a=30, b=40 → liveBytes=70, wastedBytes=30, total=100.
	eff := buildLayers(30, 40)
	assert.InDelta(t, 0.70, eff.Score, 0.001, "fixture score mismatch: want 30/100=0.70")
	assert.Equal(t, int64(30), eff.WastedBytes)
	r30 := HighestUserWastedPercent{Threshold: 0.30}
	result := evalOne(t, r30, EvalContext{Efficiency: eff})
	assert.True(t, result.Passed, "exactly at 30%% threshold must pass (got actual=%s)", result.Actual)

	// Just above 30%: a=31, b=38 → liveBytes=69, wastedBytes=31, total=100.
	eff2 := buildLayers(31, 38)
	assert.Equal(t, int64(31), eff2.WastedBytes)
	result2 := evalOne(t, r30, EvalContext{Efficiency: eff2})
	assert.False(t, result2.Passed, "31%% must fail the 30%% threshold (got actual=%s)", result2.Actual)
}
