package ci

import (
	"fmt"

	"github.com/deveshctl/layerx/image"
)

// Rule evaluates one aspect of image efficiency.
type Rule interface {
	Name() string
	Evaluate(result *image.EfficiencyResult, totalSize int64) RuleResult
}

// RuleResult holds the outcome of evaluating a single rule.
type RuleResult struct {
	Passed    bool
	Name      string
	Actual    string
	Threshold string
}

// LowestEfficiency fails if the efficiency score is below the threshold.
type LowestEfficiency struct {
	Threshold float64
}

func (r LowestEfficiency) Name() string { return "efficiency" }

func (r LowestEfficiency) Evaluate(result *image.EfficiencyResult, _ int64) RuleResult {
	return RuleResult{
		Passed:    result.Score >= r.Threshold,
		Name:      r.Name(),
		Actual:    fmt.Sprintf("%.2f", result.Score),
		Threshold: fmt.Sprintf("%.2f", r.Threshold),
	}
}

// HighestWastedBytes fails if wasted bytes exceed the threshold.
// A threshold of 0 disables this rule (always passes).
type HighestWastedBytes struct {
	Threshold int64
}

func (r HighestWastedBytes) Name() string { return "wasted bytes" }

func (r HighestWastedBytes) Evaluate(result *image.EfficiencyResult, _ int64) RuleResult {
	passed := true
	if r.Threshold > 0 {
		passed = result.WastedBytes <= r.Threshold
	}
	return RuleResult{
		Passed:    passed,
		Name:      r.Name(),
		Actual:    image.FormatBytes(result.WastedBytes),
		Threshold: image.FormatBytes(r.Threshold),
	}
}

// HighestUserWastedPercent fails if wasted bytes as a fraction of total size exceed the threshold.
// A threshold of 0 disables this rule (always passes).
type HighestUserWastedPercent struct {
	Threshold float64
}

func (r HighestUserWastedPercent) Name() string { return "wasted %" }

func (r HighestUserWastedPercent) Evaluate(result *image.EfficiencyResult, totalSize int64) RuleResult {
	var pct float64
	if totalSize > 0 {
		pct = float64(result.WastedBytes) / float64(totalSize)
	}
	passed := true
	if r.Threshold > 0 {
		passed = pct <= r.Threshold
	}
	return RuleResult{
		Passed:    passed,
		Name:      r.Name(),
		Actual:    fmt.Sprintf("%.2f", pct),
		Threshold: fmt.Sprintf("%.2f", r.Threshold),
	}
}
