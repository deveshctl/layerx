package ci

import (
	"fmt"
	"io"

	"github.com/deveshctl/layerx/image"
)

// Report holds the results of all rule evaluations.
type Report struct {
	Passed   bool
	Results  []RuleResult
	TopWaste []image.WastedFile
	Score    float64
}

// Evaluate runs all rules against the given context and returns a report.
// A nil ctx.Efficiency is treated as a zero-value result (score 0, no
// wasted files); rules still run.
//
// One Rule may emit multiple RuleResults — path rules typically emit one
// per matched path. The report's Passed flag is the AND of every result.
func Evaluate(ctx EvalContext, rules []Rule) *Report {
	if ctx.Efficiency == nil {
		ctx.Efficiency = &image.EfficiencyResult{}
	}
	report := &Report{
		Passed: true,
		Score:  ctx.Efficiency.Score,
	}

	for _, rule := range rules {
		for _, r := range rule.Evaluate(ctx) {
			report.Results = append(report.Results, r)
			if !r.Passed {
				report.Passed = false
			}
		}
	}

	limit := min(10, len(ctx.Efficiency.WastedFiles))
	report.TopWaste = append([]image.WastedFile(nil), ctx.Efficiency.WastedFiles[:limit]...)

	return report
}

// ExitCode returns 0 if all rules passed, 1 otherwise.
func (r *Report) ExitCode() int {
	if r.Passed {
		return 0
	}
	return 1
}

// Print writes the human-readable report to w.
func (r *Report) Print(w io.Writer) {
	if r.Passed {
		fmt.Fprintf(w, "  PASS: Image efficiency check passed (score: %.0f%%)\n", r.Score*100)
		return
	}

	fmt.Fprintf(w, "  FAIL: Image efficiency check failed\n\n")
	fmt.Fprintf(w, "  Results:\n")

	for _, result := range r.Results {
		status := "PASS"
		if !result.Passed {
			status = "FAIL"
		}
		fmt.Fprintf(w, "    %-14s %s (threshold: %s)  %s\n",
			result.Name+":", result.Actual, result.Threshold, status)
	}

	if len(r.TopWaste) > 0 {
		fmt.Fprintf(w, "\n  Top wasted files:\n")
		for _, wf := range r.TopWaste {
			fmt.Fprintf(w, "    %-50s %s  (%d layers)\n",
				wf.Path, image.FormatBytes(wf.TotalWasted), wf.LayerCount)
		}
	}

	fmt.Fprintf(w, "\n  Exit code: 1\n")
}
