package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/deveshctl/layerx/image"
	"github.com/spf13/cobra"
)

// ErrCompareRegression signals that `layerx compare` detected a regression
// (efficiency dropped or wasted bytes increased). main.go maps this sentinel
// to exit code 1; operational errors (resolver failure, daemon down) flow
// through the normal cobra path and exit 2.
type ErrCompareRegression struct{}

func (e *ErrCompareRegression) Error() string {
	return "compare detected regression"
}

const (
	compareModeCompact = "compact"
	compareModeFull    = "full"
	compareModeSummary = "summary"

	compareTopMin     = 1
	compareTopMax     = 1000
	compareTopDefault = 10

	compareCommandWidth = 32
	comparePathWidth    = 40
	compareReasonWidth  = 15
)

var (
	flagCompareMode string
	flagCompareTop  int
)

var compareCmd = &cobra.Command{
	Use:   "compare [flags] OLD_IMAGE NEW_IMAGE",
	Short: "Compare two images and surface size/efficiency deltas",
	Long: `Compare two images and report size, efficiency, layer, file, and waste
deltas in a deterministic, CI-friendly text report.

Both arguments accept the same inputs as "layerx" itself: a Docker image
reference (e.g. "nginx:1.25") or a path to a local image archive produced
by "docker save" or an OCI layout tarball. The two sides may mix freely
(an archive on the old side, a registry ref on the new side, etc.).

Output ends with a single machine-parseable verdict line:
  verdict: ok
  verdict: regression reason=<comma-separated reasons>
  verdict: noop digest=<sha256...>

Exit codes:
  0  no regression detected (or noop)
  1  regression detected (efficiency dropped, wasted bytes increased, or both)
  2  operational error (resolver failure, daemon down, archive missing, etc.)`,
	Example: `  # Compare a release tag against the previous one
  layerx compare myapp:1.4.0 myapp:1.5.0

  # Compare the previous build artifact against the freshly-built archive
  layerx compare ./build/prev.tar ./build/new.tar

  # Show every diff entry (no top-N truncation)
  layerx compare --mode full myapp:old myapp:new

  # CI gate: fails non-zero on regression
  layerx compare myapp:prev myapp:next || echo "image regressed"`,
	Args:          cobra.ExactArgs(2),
	RunE:          runCompareCmd,
	SilenceErrors: true,
	SilenceUsage:  true,
}

func init() {
	compareCmd.Flags().StringVar(&flagCompareMode, "mode", compareModeCompact,
		"output mode: compact (top-N tables, default), full (every entry), summary (header + verdict only)")
	compareCmd.Flags().IntVar(&flagCompareTop, "top", compareTopDefault,
		"in compact mode, show this many rows per section before truncating; ignored otherwise")

	rootCmd.AddCommand(compareCmd)
}

func runCompareCmd(cmd *cobra.Command, args []string) error {
	err := runCompareCmdInner(cmd, args)
	// compareCmd has SilenceErrors=true so cobra will not print operational
	// errors; surface them ourselves to stderr. The ErrCompareRegression
	// sentinel is silent because the report is already on stdout.
	if err != nil {
		if _, ok := errors.AsType[*ErrCompareRegression](err); !ok {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
	}
	return err
}

func runCompareCmdInner(cmd *cobra.Command, args []string) error {
	oldRef, newRef := args[0], args[1]

	if flagJSON != "" {
		return errors.New("--json is not supported by `layerx compare`; the report is text-only")
	}

	if err := validateCompareFlags(flagCompareMode, flagCompareTop); err != nil {
		return err
	}

	ctx := context.Background()
	noCache := noCacheRequested()

	out := cmd.OutOrStdout()

	// Fast path: try ImageID for both refs without analyzing. For archives
	// and already-pulled Docker images this is cheap; if either side requires
	// a pull (Docker ref not local), ImageID returns an error and we fall
	// through to the full analyze pipeline. Either way, the no-op shortcut
	// is honored when both digests are known and equal.
	oldResolver, err := selectResolver(oldRef)
	if err != nil {
		return fmt.Errorf("preparing old image %q: %w", oldRef, err)
	}
	newResolver, err := selectResolver(newRef)
	if err != nil {
		return fmt.Errorf("preparing new image %q: %w", newRef, err)
	}
	if d1, e1 := oldResolver.ImageID(ctx, oldRef); e1 == nil && d1 != "" {
		if d2, e2 := newResolver.ImageID(ctx, newRef); e2 == nil && d2 != "" && d1 == d2 {
			fmt.Fprintf(out, "no-op compare: both images resolve to %s; nothing to diff\n", d1)
			fmt.Fprintf(out, "verdict: noop digest=%s\n", d1)
			return nil
		}
	}

	oldDigest, oldAnalysis, err := analyzeForCompare(ctx, oldResolver, oldRef, noCache)
	if err != nil {
		return fmt.Errorf("analyzing old image %q: %w", oldRef, err)
	}
	newDigest, newAnalysis, err := analyzeForCompare(ctx, newResolver, newRef, noCache)
	if err != nil {
		return fmt.Errorf("analyzing new image %q: %w", newRef, err)
	}

	// Re-check the no-op shortcut after analyze: the pull may have made the
	// digests observable when they weren't before.
	if oldDigest != "" && oldDigest == newDigest {
		fmt.Fprintf(out, "no-op compare: both images resolve to %s; nothing to diff\n", oldDigest)
		fmt.Fprintf(out, "verdict: noop digest=%s\n", oldDigest)
		return nil
	}

	result := image.CompareAnalysis(oldAnalysis, newAnalysis)
	renderCompareReport(out, result, flagCompareMode, flagCompareTop)

	if result.IsRegression() {
		return &ErrCompareRegression{}
	}
	return nil
}

func validateCompareFlags(mode string, top int) error {
	switch mode {
	case compareModeCompact, compareModeFull, compareModeSummary:
	default:
		return fmt.Errorf("--mode must be one of compact|full|summary; got %q", mode)
	}
	if top < compareTopMin || top > compareTopMax {
		return fmt.Errorf("--top must be in [%d, %d]; got %d", compareTopMin, compareTopMax, top)
	}
	return nil
}

// analyzeForCompare runs the analyze pipeline for ref and returns the post-
// resolve image digest used for no-op detection. Post-analyze ImageID errors
// are surfaced as warnings on stderr (the analysis succeeded; the digest is
// only used for the no-op shortcut, so a missing digest is not fatal).
func analyzeForCompare(ctx context.Context, resolver image.Resolver, ref string, noCache bool) (string, *image.Analysis, error) {
	analysis, err := image.AnalyzeWithOptions(ctx, resolver, ref, image.AnalyzeOptions{NoCache: noCache})
	if err != nil {
		return "", nil, err
	}
	digest, idErr := resolver.ImageID(ctx, ref)
	if idErr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not resolve digest for %q after analyze: %v\n", ref, idErr)
	}
	return digest, analysis, nil
}

// renderCompareReport writes a deterministic text report. The verdict line
// is always the last line, prefixed `verdict: ` for grep/CI extraction.
func renderCompareReport(w io.Writer, r *image.CompareResult, mode string, topN int) {
	if r == nil {
		fmt.Fprintln(w, "verdict: ok")
		return
	}

	writeHeader(w, r)

	if mode != compareModeSummary {
		writeLayerTable(w, r, mode, topN)
		writeFileTable(w, r, mode, topN)
		writeWasteTable(w, r, mode, topN)
	}
	writeWarnings(w, r)
	writeVerdict(w, r)
}

func writeHeader(w io.Writer, r *image.CompareResult) {
	fmt.Fprintf(w, "old:  %s  %d layers  %s  eff %.2f\n",
		fallback(r.Before.ImageRef, "(none)"),
		r.Before.LayerCount,
		image.FormatBytes(r.Before.TotalSize),
		r.BeforeEfficiency.Score)

	deltaPart := ""
	// Use the same epsilon IsRegression uses, so the header arrow doesn't
	// flag sub-epsilon float drift the verdict treats as no change.
	if absFloat(r.AfterEfficiency.ScoreDelta) > scoreHeaderEpsilon {
		arrow := "^"
		if r.AfterEfficiency.ScoreDelta < 0 {
			arrow = "v"
		}
		deltaPart = fmt.Sprintf(" (delta %+.2f %s)", r.AfterEfficiency.ScoreDelta, arrow)
	}
	fmt.Fprintf(w, "new:  %s  %d layers  %s  eff %.2f%s\n\n",
		fallback(r.After.ImageRef, "(none)"),
		r.After.LayerCount,
		image.FormatBytes(r.After.TotalSize),
		r.AfterEfficiency.Score,
		deltaPart)
}

// scoreHeaderEpsilon mirrors image.scoreEpsilon (1e-9). Defined locally so
// cmd/ does not depend on an unexported image symbol.
const scoreHeaderEpsilon = 1e-9

func writeLayerTable(w io.Writer, r *image.CompareResult, mode string, topN int) {
	if len(r.LayerDiffs) == 0 {
		return
	}
	fmt.Fprintln(w, "LAYERS                                old size    new size    delta")
	rows := r.LayerDiffs
	if mode == compareModeCompact {
		ranked := rankLayerDiffsByDelta(rows)
		limit := min(len(ranked), topN)
		for i := range limit {
			writeLayerRow(w, ranked[i])
		}
		if len(ranked) > topN {
			fmt.Fprintf(w, "  ... and %d more layers (delta %s)\n",
				len(ranked)-topN,
				image.FormatSignedBytes(sumLayerHidden(ranked[topN:])))
		}
	} else {
		for _, d := range rows {
			writeLayerRow(w, d)
		}
	}
	fmt.Fprintln(w)
}

func sumLayerHidden(rows []image.LayerDiff) int64 {
	var s int64
	for _, d := range rows {
		s += d.SizeDelta
	}
	return s
}

func writeLayerRow(w io.Writer, d image.LayerDiff) {
	fmt.Fprintf(w, "  %2d  %s  %10s  %10s  %s\n",
		d.Index,
		padRight(truncate(fallback(d.AfterCommand, d.BeforeCommand), compareCommandWidth), compareCommandWidth),
		image.FormatBytes(d.BeforeSize),
		image.FormatBytes(d.AfterSize),
		image.FormatSignedBytes(d.SizeDelta))
}

func writeFileTable(w io.Writer, r *image.CompareResult, mode string, topN int) {
	if len(r.FileDiffs) == 0 {
		return
	}
	netDelta := r.FileSummary.BytesAdded - r.FileSummary.BytesRemoved
	fmt.Fprintf(w, "FILE CHANGES                              +%d  ~%d  -%d  delta %s\n",
		r.FileSummary.AddedCount, r.FileSummary.ModifiedCount, r.FileSummary.RemovedCount,
		image.FormatSignedBytes(netDelta))

	rows := r.FileDiffs
	if mode == compareModeCompact {
		ranked := rankFileDiffsByDelta(rows)
		limit := min(len(ranked), topN)
		for i := range limit {
			writeFileRow(w, ranked[i])
		}
		if len(ranked) > topN {
			fmt.Fprintf(w, "  ... and %d more files (delta %s)\n",
				len(ranked)-topN,
				image.FormatSignedBytes(sumFileHidden(ranked[topN:])))
		}
	} else {
		for _, d := range rows {
			writeFileRow(w, d)
		}
	}
	fmt.Fprintln(w)
}

func sumFileHidden(rows []image.FileDiff) int64 {
	var s int64
	for _, d := range rows {
		s += d.SizeDelta
	}
	return s
}

func writeFileRow(w io.Writer, d image.FileDiff) {
	sym := "?"
	switch d.DiffType {
	case image.Added:
		sym = "+"
	case image.Modified:
		sym = "~"
	case image.Removed:
		sym = "-"
	}
	fmt.Fprintf(w, "  %s %s  %s  %s\n",
		sym,
		padRight(truncate(d.Path, comparePathWidth), comparePathWidth),
		padRight(truncate(d.ChangeReason, compareReasonWidth), compareReasonWidth),
		image.FormatSignedBytes(d.SizeDelta))
}

func writeWasteTable(w io.Writer, r *image.CompareResult, mode string, topN int) {
	if len(r.WasteDiffs) == 0 {
		return
	}
	var totalDelta int64
	for _, wd := range r.WasteDiffs {
		totalDelta += wd.WastedDelta
	}
	fmt.Fprintf(w, "WASTE CHANGES                                              delta %s\n",
		image.FormatSignedBytes(totalDelta))

	rows := r.WasteDiffs
	limit := len(rows)
	if mode == compareModeCompact && limit > topN {
		limit = topN
	}
	for i := range limit {
		writeWasteRow(w, rows[i])
	}
	if mode == compareModeCompact && len(rows) > topN {
		var hiddenDelta int64
		for _, wd := range rows[topN:] {
			hiddenDelta += wd.WastedDelta
		}
		fmt.Fprintf(w, "  ... and %d more (delta %s)\n",
			len(rows)-topN, image.FormatSignedBytes(hiddenDelta))
	}
	fmt.Fprintln(w)
}

func writeWasteRow(w io.Writer, wd image.WasteDiff) {
	fmt.Fprintf(w, "  %s  %10s -> %-10s  %s\n",
		padRight(truncate(wd.Path, comparePathWidth), comparePathWidth),
		image.FormatBytes(wd.BeforeWasted),
		image.FormatBytes(wd.AfterWasted),
		image.FormatSignedBytes(wd.WastedDelta))
}

func writeWarnings(w io.Writer, r *image.CompareResult) {
	if len(r.Warnings) == 0 {
		return
	}
	fmt.Fprintln(w, "WARNINGS")
	for _, msg := range r.Warnings {
		fmt.Fprintf(w, "  - %s\n", msg)
	}
	fmt.Fprintln(w)
}

func writeVerdict(w io.Writer, r *image.CompareResult) {
	reasons := r.RegressionReasons()
	if len(reasons) == 0 {
		fmt.Fprintln(w, "verdict: ok")
		return
	}
	fmt.Fprintf(w, "verdict: regression reason=%s\n", strings.Join(reasons, ","))
}

// rankLayerDiffsByDelta returns layer diffs sorted by |SizeDelta| desc, then
// by Index asc for stable ordering within ties.
func rankLayerDiffsByDelta(in []image.LayerDiff) []image.LayerDiff {
	out := make([]image.LayerDiff, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := absInt64(out[i].SizeDelta), absInt64(out[j].SizeDelta)
		if ai != aj {
			return ai > aj
		}
		return out[i].Index < out[j].Index
	})
	return out
}

func rankFileDiffsByDelta(in []image.FileDiff) []image.FileDiff {
	out := make([]image.FileDiff, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := absInt64(out[i].SizeDelta), absInt64(out[j].SizeDelta)
		if ai != aj {
			return ai > aj
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func absInt64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func absFloat(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// truncate shortens s so it visually occupies at most n columns. It cuts on
// rune boundaries (never mid-UTF-8) and reserves one column for the trailing
// ellipsis. The return is at most n runes wide.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	if n == 1 {
		return "."
	}
	// Take the first n-1 runes, then append a trailing ".." marker as a
	// single-byte ASCII so column widths reported by padRight stay
	// consistent across terminals.
	runes := []rune(s)
	return string(runes[:n-2]) + ".."
}

// padRight pads s on the right with spaces so its rune count is exactly n.
// If s is already n or wider in runes, returns s unchanged.
func padRight(s string, n int) string {
	w := utf8.RuneCountInString(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
