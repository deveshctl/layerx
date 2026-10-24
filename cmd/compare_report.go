package cmd

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/image"
	"github.com/deveshctl/layerx/internal/sizebudget"
	"github.com/deveshctl/layerx/tui"
	"golang.org/x/term"
)

type compareOptions struct {
	Width int
	Color bool
	Theme tui.Theme
}

func terminalCompareOptions(w io.Writer, theme string) compareOptions {
	o := compareOptions{Width: 100, Theme: tui.ThemeByName(theme)}
	if f, ok := w.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		o.Color = os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
		if width, _, err := term.GetSize(int(f.Fd())); err == nil {
			o.Width = width
		}
	}
	return o
}

func cleanCompareText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}

func compareSigned(n int64) string {
	if n > 0 {
		return "+" + sizebudget.Format(n)
	}
	return sizebudget.Format(n)
}

func growthLabel(before, after int64) string {
	delta := after - before
	if before == 0 {
		if after == 0 {
			return "0 B"
		}
		return compareSigned(delta) + " (n/a)"
	}
	return fmt.Sprintf("%s (%+.1f%%)", compareSigned(delta), 100*float64(delta)/float64(before))
}

func percentagePointDelta(delta float64) string {
	pp := 100 * delta
	if pp != 0 && pp > -0.05 && pp < 0.05 {
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%+.8f", pp), "0"), ".") + " pp"
	}
	return fmt.Sprintf("%+.1f pp", pp)
}

func (o compareOptions) heading(s string) string {
	if !o.Color {
		return s
	}
	return lipgloss.NewStyle().Foreground(o.Theme.Accent).Bold(true).Render(s)
}

func renderComparison(w io.Writer, r *image.CompareResult, mode string, top int, checks []sizebudget.Result, o compareOptions) {
	if r == nil {
		fmt.Fprintln(w, "verdict: ok")
		return
	}
	width := max(o.Width, 20)
	reasons := r.RegressionReasons()
	for _, c := range checks {
		if !c.Passed {
			reasons = append(reasons, strings.ReplaceAll(c.Name, " ", "-"))
		}
	}
	status, title := "PASS", "No regression detected"
	if len(reasons) > 0 {
		status, title = "FAIL", "Comparison checks failed"
	}
	statusText := status
	if o.Color {
		color := o.Theme.Added
		if status == "FAIL" {
			color = o.Theme.Removed
		}
		statusText = lipgloss.NewStyle().Bold(true).Foreground(color).Render(status)
	}
	fmt.Fprintf(w, "%s  %s\n\n", statusText, title)
	line := func(label, value string) {
		fmt.Fprintf(w, "%s %s\n", label, ansi.Truncate(cleanCompareText(value), max(width-len(label)-1, 1), "…"))
	}
	line("Before:", r.Before.ImageRef)
	line("After: ", r.After.ImageRef)
	platform := fallback(r.After.Platform, "unknown")
	if r.Before.Platform != r.After.Platform {
		platform = fallback(r.Before.Platform, "unknown") + " → " + platform + " (different platforms)"
	}
	line("Platform:", platform)
	fmt.Fprintln(w)
	rows := [][4]string{
		{"Uncompressed layers", sizebudget.Format(r.Before.UncompressedSize), sizebudget.Format(r.After.UncompressedSize), growthLabel(r.Before.UncompressedSize, r.After.UncompressedSize)},
		{"Layer waste", sizebudget.Format(r.BeforeEfficiency.WastedBytes), sizebudget.Format(r.AfterEfficiency.WastedBytes), compareSigned(r.AfterEfficiency.WastedBytesDelta)},
		{"Layer efficiency", fmt.Sprintf("%.1f%%", 100*r.BeforeEfficiency.Score), fmt.Sprintf("%.1f%%", 100*r.AfterEfficiency.Score), percentagePointDelta(r.AfterEfficiency.ScoreDelta)},
		{"Layers", fmt.Sprint(r.Before.LayerCount), fmt.Sprint(r.After.LayerCount), fmt.Sprintf("%+d", r.After.LayerCount-r.Before.LayerCount)},
	}
	if width >= 76 {
		fmt.Fprintf(w, "  %-21s %12s %12s %23s\n", "", "Before", "After", "Change")
		for _, row := range rows {
			fmt.Fprintf(w, "  %-21s %12s %12s %23s\n", row[0], row[1], row[2], row[3])
		}
	} else {
		for _, row := range rows {
			fmt.Fprintln(w, o.heading(row[0]))
			fmt.Fprintf(w, "  %s → %s\n", row[1], row[2])
			fmt.Fprintf(w, "  Change: %s\n", row[3])
		}
	}
	fmt.Fprintln(w)
	writeSizeChecks(w, checks, width)
	if len(reasons) > 0 {
		labels := map[string]string{"efficiency": "layer efficiency decreased", "waste": "layer waste increased", "image-size": "absolute size budget exceeded", "image-growth": "growth budget exceeded"}
		for _, reason := range reasons {
			writeWrapped(w, "Failed: "+labels[reason], width)
		}
		fmt.Fprintln(w)
	}
	if mode != compareModeSummary {
		writeComparisonLayers(w, r, mode, top, width, o)
		writeComparisonFiles(w, r, mode, top, width, o)
		if len(r.WasteDiffs) > 0 {
			fmt.Fprintln(w, o.heading("Waste changes"))
			limit := len(r.WasteDiffs)
			if mode == compareModeCompact {
				limit = min(limit, top)
			}
			for _, d := range r.WasteDiffs[:limit] {
				writeComparisonEntry(w, compareSigned(d.WastedDelta), d.Path, "", width)
			}
			if limit < len(r.WasteDiffs) {
				fmt.Fprintf(w, "  … %d more waste changes\n", len(r.WasteDiffs)-limit)
			}
			fmt.Fprintln(w)
		}
	}
	if len(r.Warnings) > 0 {
		fmt.Fprintln(w, o.heading("Comparison notes"))
		for _, note := range r.Warnings {
			writeWrapped(w, "  "+cleanCompareText(note), width)
		}
		fmt.Fprintln(w)
	}
	if mode == compareModeCompact {
		writeWrapped(w, "Use --mode full for all changes; --format json for exact byte values.", width)
	}
	if len(reasons) == 0 {
		fmt.Fprintln(w, "verdict: ok")
	} else {
		fmt.Fprintf(w, "verdict: regression reason=%s\n", strings.Join(reasons, ","))
	}
}

func writeComparisonEntry(w io.Writer, delta, path, detail string, width int) {
	path = cleanCompareText(path)
	if width < 60 {
		fmt.Fprintf(w, "  %s\n", fitCompareEnd(path, max(width-2, 1)))
		writeWrapped(w, "    "+delta+" "+detail, width)
		return
	}
	fmt.Fprintf(w, "  %12s  %s\n", delta, fitCompareEnd(path, max(width-18, 1)))
	if detail != "" {
		writeWrapped(w, "                "+cleanCompareText(detail), width)
	}
}

func writeComparisonLayers(w io.Writer, r *image.CompareResult, mode string, top, width int, o compareOptions) {
	var changed []image.LayerDiff
	shared := 0
	for _, d := range r.LayerDiffs {
		if d.Match == "shared" {
			shared++
		} else {
			changed = append(changed, d)
		}
	}
	noun := "layers"
	if shared == 1 {
		noun = "layer"
	}
	fmt.Fprintln(w, o.heading(fmt.Sprintf("Layer changes · %d shared %s", shared, noun)))
	if len(changed) == 0 {
		fmt.Fprintln(w, "  No changed layers.")
		fmt.Fprintln(w)
		return
	}
	if mode == compareModeCompact {
		changed = rankLayerDiffsByDelta(changed)
	}
	limit := len(changed)
	if mode == compareModeCompact {
		limit = min(limit, top)
	}
	for _, d := range changed[:limit] {
		label := fmt.Sprintf("Added · after L%d", d.AfterIndex+1)
		if d.Match == "removed" {
			label = fmt.Sprintf("Removed · before L%d", d.BeforeIndex+1)
		}
		if d.Match == "command" {
			label = fmt.Sprintf("Changed · L%d → L%d · matched by command", d.BeforeIndex+1, d.AfterIndex+1)
		}
		writeComparisonEntry(w, compareSigned(d.SizeDelta), fallback(d.AfterCommand, d.BeforeCommand), label, width)
	}
	if limit < len(changed) {
		fmt.Fprintf(w, "  … %d more changed layers\n", len(changed)-limit)
	}
	fmt.Fprintln(w)
}

func writeComparisonFiles(w io.Writer, r *image.CompareResult, mode string, top, width int, o compareOptions) {
	f := r.FileSummary
	heading := fmt.Sprintf("File changes · %d added · %d modified · %d removed", f.AddedCount, f.ModifiedCount, f.RemovedCount)
	if width < 65 {
		heading = fmt.Sprintf("File changes: +%d ~%d -%d", f.AddedCount, f.ModifiedCount, f.RemovedCount)
	}
	fmt.Fprintln(w, o.heading(heading))
	if len(r.FileDiffs) == 0 {
		fmt.Fprintln(w, "  No size or metadata changes detected.")
		fmt.Fprintln(w)
		return
	}
	rows := r.FileDiffs
	if mode == compareModeCompact {
		rows = rankFileDiffsByDelta(rows)
	}
	limit := len(rows)
	if mode == compareModeCompact {
		limit = min(limit, top)
	}
	for _, d := range rows[:limit] {
		label := "Modified"
		if d.DiffType == image.Added {
			label = "Added"
		}
		if d.DiffType == image.Removed {
			label = "Removed"
		}
		if d.ChangeReason != "" {
			label += " · " + d.ChangeReason
		}
		writeComparisonEntry(w, compareSigned(d.SizeDelta), d.Path, label, width)
	}
	if limit < len(rows) {
		fmt.Fprintf(w, "  … %d more files; use --mode full\n", len(rows)-limit)
	}
	fmt.Fprintln(w)
	if len(rows) > top {
		groups := map[string]int64{}
		for _, d := range rows {
			parts := strings.Split(strings.TrimPrefix(d.Path, "/"), "/")
			if len(parts) < 2 {
				continue
			}
			parts = parts[:min(2, len(parts)-1)]
			path := "/" + strings.Join(parts, "/")
			groups[path] += d.SizeDelta
		}
		type group struct {
			path  string
			delta int64
		}
		var ranked []group
		for path, delta := range groups {
			if delta != 0 {
				ranked = append(ranked, group{path, delta})
			}
		}
		sort.Slice(ranked, func(i, j int) bool {
			a, b := absInt64(ranked[i].delta), absInt64(ranked[j].delta)
			if a == b {
				return ranked[i].path < ranked[j].path
			}
			return a > b
		})
		if len(ranked) > 0 {
			fmt.Fprintln(w, o.heading("Directory changes · logical file bytes"))
			for _, g := range ranked[:min(5, len(ranked))] {
				writeComparisonEntry(w, compareSigned(g.delta), g.path, "", width)
			}
			fmt.Fprintln(w)
		}
	}
}

func fitCompareEnd(s string, width int) string {
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.TruncateLeft(s, ansi.StringWidth(s)-width+1, "…")
}

func writeWrapped(w io.Writer, s string, width int) {
	for ansi.StringWidth(s) > width {
		part := ansi.Truncate(s, width, "")
		if i := strings.LastIndex(part, " "); i > width/2 {
			part = part[:i]
		}
		if part == "" {
			return
		}
		fmt.Fprintln(w, part)
		s = "  " + strings.TrimSpace(strings.TrimPrefix(s, part))
	}
	fmt.Fprintln(w, s)
}
