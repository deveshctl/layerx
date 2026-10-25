package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/deveshctl/layerx/ci"
	"github.com/deveshctl/layerx/config"
	"github.com/deveshctl/layerx/image"
	"github.com/deveshctl/layerx/internal/sizebudget"
	"github.com/spf13/cobra"
)

func addSizeBudgetFlags(cmd *cobra.Command) {
	cmd.Flags().String("max-image-size", "", "maximum uncompressed layer size, e.g. 500MB or 512MiB (overrides config)")
	cmd.Flags().String("max-growth", "", "maximum growth, e.g. 20MB or 5% (requires a baseline; overrides config)")
	cmd.Flags().String("format", "text", "check report format: text, json, or markdown")
}

func effectiveSizeBudget(cmd *cobra.Command, cfg *config.Config) (sizebudget.Config, sizebudget.Limits, error) {
	c := cfg.SizeBudget
	if cmd.Flags().Changed("max-image-size") {
		c.Max, _ = cmd.Flags().GetString("max-image-size")
	}
	if cmd.Flags().Changed("max-growth") {
		c.MaxGrowth, _ = cmd.Flags().GetString("max-growth")
	}
	l, err := sizebudget.Parse(c)
	if err != nil {
		return c, l, err
	}
	format, _ := cmd.Flags().GetString("format")
	if cmd.Flags().Lookup("format") == nil {
		format = "text"
	}
	if format != "text" && format != "json" && format != "markdown" {
		return c, l, fmt.Errorf("--format must be text, json, or markdown")
	}
	return c, l, nil
}

func matchingPlatforms(before, after *image.Analysis) error {
	if before.Platform == "" || after.Platform == "" {
		return fmt.Errorf("cannot enforce image growth: image platform metadata is unavailable; use images with OS/architecture metadata")
	}
	if before.Platform != after.Platform {
		return fmt.Errorf("cannot compare growth across platforms: baseline is %s, new image is %s; select the same --platform", before.Platform, after.Platform)
	}
	return nil
}

type reportImage struct {
	Reference string `json:"reference"`
	Identity  string `json:"identity,omitempty"`
	Platform  string `json:"platform,omitempty"`
	Bytes     int64  `json:"bytes"`
}

type checkReport struct {
	SchemaVersion string               `json:"schemaVersion"`
	Metric        string               `json:"metric"`
	Passed        bool                 `json:"passed"`
	Before        *reportImage         `json:"baseline,omitempty"`
	After         reportImage          `json:"image"`
	Budget        sizebudget.Config    `json:"sizeBudget"`
	SizeChecks    []sizebudget.Result  `json:"sizeChecks"`
	CI            *ci.Report           `json:"checks,omitempty"`
	Comparison    *image.CompareResult `json:"comparison,omitempty"`
}

func reportSnapshot(a *image.Analysis, id string) reportImage {
	return reportImage{a.ImageRef, id, a.Platform, a.UncompressedSize}
}

func newCheckReport(a *image.Analysis, id string, c sizebudget.Config, checks []sizebudget.Result) checkReport {
	r := checkReport{SchemaVersion: "1.0.0", Metric: "uncompressed-layer-tar-bytes", Passed: true, After: reportSnapshot(a, id), Budget: c, SizeChecks: checks}
	if r.SizeChecks == nil {
		r.SizeChecks = []sizebudget.Result{}
	}
	for _, check := range checks {
		if !check.Passed {
			r.Passed = false
		}
	}
	return r
}

func writeCheckReport(w io.Writer, format, text string, report checkReport) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	case "markdown":
		return writeMarkdownCheckReport(w, text, report)
	default:
		_, err := io.WriteString(w, text)
		return err
	}
}

func writeReportFence(w io.Writer, text string) error {
	// A fence longer than every run in image-controlled text prevents
	// filenames and commands from breaking out into Markdown markup.
	longest, run := 2, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	_, err := fmt.Fprintf(w, "%stext\n%s\n%s\n", fence, strings.TrimRight(text, "\n"), fence)
	return err
}

// Reductions consume none of a growth allowance. A zero ceiling has no
// meaningful utilization percentage, but its remaining bytes are still exact.
func budgetHeadroom(c sizebudget.Result) (usage, remaining string) {
	actual := max(c.ActualBytes, 0)
	usage = "n/a (zero limit)"
	if c.LimitBytes > 0 {
		usage = fmt.Sprintf("%.1f%% used", 100*float64(actual)/float64(c.LimitBytes))
	}
	delta := c.LimitBytes - actual
	remaining = sizebudget.Format(delta) + " remaining"
	if delta < 0 {
		remaining = sizebudget.Format(-delta) + " over limit"
	}
	return usage, remaining
}

func writeSizeChecks(w io.Writer, checks []sizebudget.Result, width int) {
	if len(checks) == 0 {
		return
	}
	fmt.Fprintln(w, "Size budget")
	for _, c := range checks {
		status := "PASS"
		detail := ""
		if !c.Passed {
			status = "FAIL"
			detail = "; " + sizebudget.Format(c.ActualBytes-c.LimitBytes) + " over limit"
		}
		writeWrapped(w, fmt.Sprintf("  %s  %s: %s / %s (%s)%s", status, c.Name, sizebudget.Format(c.ActualBytes), sizebudget.Format(c.LimitBytes), c.Limit, detail), width)
		usage, remaining := budgetHeadroom(c)
		writeWrapped(w, "    "+usage+" · "+remaining, width)
		if !c.Passed {
			writeWrapped(w, fmt.Sprintf("    Exact: %d B; allowance: %d B", c.ActualBytes, c.LimitBytes), width)
		}
	}
	fmt.Fprintln(w)
}
