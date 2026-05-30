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
	result := evalOne(t, r, EvalContext{Efficiency: &image.EfficiencyResult{WastedBytes: 50}, TotalSize: 1000})
	assert.True(t, result.Passed)
}

func TestHighestUserWastedPercent_Fail(t *testing.T) {
	r := HighestUserWastedPercent{Threshold: 0.1}
	result := evalOne(t, r, EvalContext{Efficiency: &image.EfficiencyResult{WastedBytes: 200}, TotalSize: 1000})
	assert.False(t, result.Passed)
}

func TestHighestUserWastedPercent_ZeroTotal(t *testing.T) {
	r := HighestUserWastedPercent{Threshold: 0.1}
	result := evalOne(t, r, EvalContext{Efficiency: &image.EfficiencyResult{WastedBytes: 100}, TotalSize: 0})
	assert.True(t, result.Passed)
}

func TestHighestUserWastedPercent_DisabledWhenZero(t *testing.T) {
	r := HighestUserWastedPercent{Threshold: 0}
	result := evalOne(t, r, EvalContext{Efficiency: &image.EfficiencyResult{WastedBytes: 999999}, TotalSize: 1000})
	assert.True(t, result.Passed)
}

// Actual / Threshold render as percentages so operators reading
// "wasted %: 0.10 (threshold: 0.10)" don't misread 10% as 0.10%.
func TestHighestUserWastedPercent_ActualAndThresholdRenderedAsPercent(t *testing.T) {
	r := HighestUserWastedPercent{Threshold: 0.1}
	result := evalOne(t, r, EvalContext{Efficiency: &image.EfficiencyResult{WastedBytes: 100}, TotalSize: 1000})
	assert.Equal(t, "10.0%", result.Actual)
	assert.Equal(t, "10.0%", result.Threshold)
}
