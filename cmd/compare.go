package cmd

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

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
	oldRef, newRef := args[0], args[1]

	if err := validateCompareFlags(); err != nil {
		return err
	}

	ctx := context.Background()
	noCache := noCacheRequested()

	oldDigest, oldAnalysis, err := analyzeForCompare(ctx, oldRef, noCache)
	if err != nil {
		return fmt.Errorf("failed to analyze old image %q: %w", oldRef, err)
	}
	newDigest, newAnalysis, err := analyzeForCompare(ctx, newRef, noCache)
	if err != nil {
		return fmt.Errorf("failed to analyze new image %q: %w", newRef, err)
	}

	out := cmd.OutOrStdout()

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

func validateCompareFlags() error {
	switch flagCompareMode {
	case compareModeCompact, compareModeFull, compareModeSummary:
	default:
		return fmt.Errorf("--mode must be one of compact|full|summary; got %q", flagCompareMode)
	}
	if flagCompareTop < compareTopMin || flagCompareTop > compareTopMax {
		return fmt.Errorf("--top must be in [%d, %d]; got %d", compareTopMin, compareTopMax, flagCompareTop)
	}
	return nil
}

// analyzeForCompare wraps the existing resolver + analyze pipeline and also
// returns the post-resolve image digest used for no-op detection. A
// digest-unavailable case (rare; archive resolvers may not expose one) is
// not fatal — empty digest disables the no-op shortcut for that side.
func analyzeForCompare(ctx context.Context, ref string, noCache bool) (string, *image.Analysis, error) {
	resolver, err := selectResolver(ref)
	if err != nil {
		return "", nil, err
	}
	analysis, err := image.AnalyzeWithOptions(ctx, resolver, ref, image.AnalyzeOptions{NoCache: noCache})
	if err != nil {
		return "", nil, err
	}
	digest, _ := resolver.ImageID(ctx, ref)
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
	if r.AfterEfficiency.ScoreDelta != 0 {
		arrow := "↑"
		if r.AfterEfficiency.ScoreDelta < 0 {
			arrow = "↓"
		}
		deltaPart = fmt.Sprintf(" (Δ%+.2f %s)", r.AfterEfficiency.ScoreDelta, arrow)
	}
	fmt.Fprintf(w, "new:  %s  %d layers  %s  eff %.2f%s\n\n",
		fallback(r.After.ImageRef, "(none)"),
		r.After.LayerCount,
		image.FormatBytes(r.After.TotalSize),
		r.AfterEfficiency.Score,
		deltaPart)
}

func writeLayerTable(w io.Writer, r *image.CompareResult, mode string, topN int) {
	if len(r.LayerDiffs) == 0 {
		return
	}
	fmt.Fprintln(w, "LAYERS                                old size    new size    Δ")
	rows := r.LayerDiffs
	if mode == compareModeCompact && len(rows) > topN {
		ranked := rankLayerDiffsByDelta(rows)
		for i := range topN {
			writeLayerRow(w, ranked[i])
		}
		fmt.Fprintf(w, "  ... and %d more layers\n", len(rows)-topN)
	} else {
		for _, d := range rows {
			writeLayerRow(w, d)
		}
	}
	fmt.Fprintln(w)
}

func writeLayerRow(w io.Writer, d image.LayerDiff) {
	fmt.Fprintf(w, "  %2d  %-32s  %10s  %10s  %s\n",
		d.Index, truncate(fallback(d.AfterCommand, d.BeforeCommand), 32),
		image.FormatBytes(d.BeforeSize),
		image.FormatBytes(d.AfterSize),
		image.FormatSignedBytes(d.SizeDelta))
}

func writeFileTable(w io.Writer, r *image.CompareResult, mode string, topN int) {
	if len(r.FileDiffs) == 0 {
		return
	}
	netDelta := r.FileSummary.BytesAdded - r.FileSummary.BytesRemoved
	fmt.Fprintf(w, "FILE CHANGES                              +%d  ~%d  −%d  Δ %s\n",
		r.FileSummary.AddedCount, r.FileSummary.ModifiedCount, r.FileSummary.RemovedCount,
		image.FormatSignedBytes(netDelta))

	rows := r.FileDiffs
	if mode == compareModeCompact && len(rows) > topN {
		ranked := rankFileDiffsByDelta(rows)
		for i := range topN {
			writeFileRow(w, ranked[i])
		}
		fmt.Fprintf(w, "  ... and %d more files\n", len(rows)-topN)
	} else {
		for _, d := range rows {
			writeFileRow(w, d)
		}
	}
	fmt.Fprintln(w)
}

func writeFileRow(w io.Writer, d image.FileDiff) {
	sym := "?"
	switch d.DiffType {
	case image.Added:
		sym = "+"
	case image.Modified:
		sym = "~"
	case image.Removed:
		sym = "−"
	}
	fmt.Fprintf(w, "  %s %-40s  %-15s  %s\n",
		sym, truncate(d.Path, 40), d.ChangeReason, image.FormatSignedBytes(d.SizeDelta))
}

func writeWasteTable(w io.Writer, r *image.CompareResult, mode string, topN int) {
	if len(r.WasteDiffs) == 0 {
		return
	}
	var totalDelta int64
	for _, wd := range r.WasteDiffs {
		totalDelta += wd.WastedDelta
	}
	fmt.Fprintf(w, "WASTE CHANGES                                              Δ %s\n",
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
		fmt.Fprintf(w, "  ... and %d more\n", len(rows)-topN)
	}
	fmt.Fprintln(w)
}

func writeWasteRow(w io.Writer, wd image.WasteDiff) {
	fmt.Fprintf(w, "  %-40s  %10s → %-10s  %s\n",
		truncate(wd.Path, 40),
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
	if !r.IsRegression() {
		fmt.Fprintln(w, "verdict: ok")
		return
	}
	var reasons []string
	if r.AfterEfficiency.Score < r.BeforeEfficiency.Score {
		reasons = append(reasons, "efficiency")
	}
	if r.AfterEfficiency.WastedBytes > r.BeforeEfficiency.WastedBytes {
		reasons = append(reasons, "waste")
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return s[:n-1] + "…"
}

func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
