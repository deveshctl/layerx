package ci

import (
	"fmt"
	"math"

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
// A threshold of 0 (or any non-positive value) disables this rule. A NaN
// score (e.g. an empty image with zero total bytes) is treated as a pass —
// the rule cannot meaningfully evaluate a non-finite score.
type LowestEfficiency struct {
	Threshold float64
}

func (r LowestEfficiency) Name() string { return "efficiency" }

func (r LowestEfficiency) Evaluate(result *image.EfficiencyResult, _ int64) RuleResult {
	passed := true
	if r.Threshold > 0 && !math.IsNaN(result.Score) {
		passed = result.Score >= r.Threshold
	}
	return RuleResult{
		Passed:    passed,
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
		Actual:    fmt.Sprintf("%.1f%%", pct*100),
		Threshold: fmt.Sprintf("%.1f%%", r.Threshold*100),
	}
}
