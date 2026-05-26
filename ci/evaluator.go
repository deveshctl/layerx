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

// Evaluate runs all rules against the efficiency result and returns a report.
// A nil efficiency argument is treated as a zero-value result (score 0, no
// wasted files); rules still run, which lets callers surface "no analysis
// available" cleanly without needing to nil-guard at every call site.
func Evaluate(efficiency *image.EfficiencyResult, totalSize int64, rules []Rule) *Report {
	if efficiency == nil {
		efficiency = &image.EfficiencyResult{}
	}
	report := &Report{
		Passed: true,
		Score:  efficiency.Score,
	}

	for _, rule := range rules {
		result := rule.Evaluate(efficiency, totalSize)
		report.Results = append(report.Results, result)
		if !result.Passed {
			report.Passed = false
		}
	}

	limit := min(10, len(efficiency.WastedFiles))
	report.TopWaste = append([]image.WastedFile(nil), efficiency.WastedFiles[:limit]...)

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
