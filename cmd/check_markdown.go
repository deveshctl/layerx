package cmd

import (
	"fmt"
	"html"
	"io"
	"strings"

	"github.com/deveshctl/layerx/internal/sizebudget"
)

func markdownCell(s string) string {
	s = html.EscapeString(cleanCompareText(s))
	return strings.NewReplacer("\\", "\\\\", "|", "\\|", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "~", "\\~").Replace(s)
}

func writeMarkdownCheckReport(w io.Writer, text string, report checkReport) error {
	var out strings.Builder
	status := func(passed bool) string {
		if passed {
			return "PASS"
		}
		return "FAIL"
	}
	fmt.Fprintf(&out, "### LayerX image checks — %s\n\n", status(report.Passed))
	row := func(cells ...string) {
		for _, cell := range cells {
			fmt.Fprintf(&out, "| %s ", markdownCell(cell))
		}
		out.WriteString("|\n")
	}
	row("Input", "Reference", "Platform", "Uncompressed layers")
	out.WriteString("| --- | --- | --- | ---: |\n")
	if report.Before != nil {
		b := report.Before
		row("Baseline", b.Reference, fallback(b.Platform, "unknown"), sizebudget.Format(b.Bytes))
	}
	row("Image", report.After.Reference, fallback(report.After.Platform, "unknown"), sizebudget.Format(report.After.Bytes))
	if report.Comparison != nil {
		out.WriteString("\n")
		row("Metric", "Before", "After", "Change")
		out.WriteString("| --- | ---: | ---: | ---: |\n")
		for _, values := range comparisonSummaryRows(report.Comparison) {
			row(values[:]...)
		}
	}
	if len(report.SizeChecks) > 0 {
		out.WriteString("\n")
		row("Size budget", "Result", "Actual / allowance", "Usage", "Headroom")
		out.WriteString("| --- | --- | ---: | ---: | ---: |\n")
		for _, check := range report.SizeChecks {
			usage, remaining := budgetHeadroom(check)
			row(check.Name+" ("+check.Limit+")", status(check.Passed), sizebudget.Format(check.ActualBytes)+" / "+sizebudget.Format(check.LimitBytes), usage, remaining)
		}
	}
	if report.CI != nil && len(report.CI.Results) > 0 {
		out.WriteString("\n")
		row("CI check", "Result", "Actual", "Threshold", "Detail")
		out.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, check := range report.CI.Results {
			row(check.Name, status(check.Passed), check.Actual, check.Threshold, check.Detail)
		}
	}
	// Keep all selected detail, exact failing bytes and comparison limitations.
	// Use a variable-length fence so image-controlled text stays literal.
	out.WriteString("\n<details>\n<summary>Detailed report</summary>\n\n")
	if err := writeReportFence(&out, text); err != nil {
		return err
	}
	out.WriteString("\n</details>\n")
	_, err := io.WriteString(w, out.String())
	return err
}
