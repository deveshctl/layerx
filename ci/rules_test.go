package ci

import (
	"testing"

	"github.com/deveshpharswan/layerx/image"
	"github.com/stretchr/testify/assert"
)

func TestLowestEfficiency_Pass(t *testing.T) {
	r := LowestEfficiency{Threshold: 0.9}
	result := r.Evaluate(&image.EfficiencyResult{Score: 0.95}, 0)
	assert.True(t, result.Passed)
	assert.Equal(t, "efficiency", result.Name)
}

func TestLowestEfficiency_Fail(t *testing.T) {
	r := LowestEfficiency{Threshold: 0.9}
	result := r.Evaluate(&image.EfficiencyResult{Score: 0.85}, 0)
	assert.False(t, result.Passed)
}

func TestLowestEfficiency_Exact(t *testing.T) {
	r := LowestEfficiency{Threshold: 0.9}
	result := r.Evaluate(&image.EfficiencyResult{Score: 0.9}, 0)
	assert.True(t, result.Passed)
}

func TestHighestWastedBytes_Pass(t *testing.T) {
	r := HighestWastedBytes{Threshold: 1000}
	result := r.Evaluate(&image.EfficiencyResult{WastedBytes: 500}, 0)
	assert.True(t, result.Passed)
}

func TestHighestWastedBytes_Fail(t *testing.T) {
	r := HighestWastedBytes{Threshold: 1000}
	result := r.Evaluate(&image.EfficiencyResult{WastedBytes: 1500}, 0)
	assert.False(t, result.Passed)
}

func TestHighestWastedBytes_DisabledWhenZero(t *testing.T) {
	r := HighestWastedBytes{Threshold: 0}
	result := r.Evaluate(&image.EfficiencyResult{WastedBytes: 999999}, 0)
	assert.True(t, result.Passed)
}

func TestHighestUserWastedPercent_Pass(t *testing.T) {
	r := HighestUserWastedPercent{Threshold: 0.1}
	result := r.Evaluate(&image.EfficiencyResult{WastedBytes: 50}, 1000)
	assert.True(t, result.Passed)
}

func TestHighestUserWastedPercent_Fail(t *testing.T) {
	r := HighestUserWastedPercent{Threshold: 0.1}
	result := r.Evaluate(&image.EfficiencyResult{WastedBytes: 200}, 1000)
	assert.False(t, result.Passed)
}

func TestHighestUserWastedPercent_ZeroTotal(t *testing.T) {
	r := HighestUserWastedPercent{Threshold: 0.1}
	result := r.Evaluate(&image.EfficiencyResult{WastedBytes: 100}, 0)
	assert.True(t, result.Passed)
}
