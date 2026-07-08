package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
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

// ErrCompareUsage is returned when the user invoked `layerx compare` with the
// wrong number of arguments. The wrapper prints a short hint to stderr; the
// sentinel itself carries no message body so cobra doesn't double-print.
type ErrCompareUsage struct{}

func (e *ErrCompareUsage) Error() string { return "usage" }

const (
	compareModeCompact = "compact"
	compareModeFull    = "full"
	compareModeSummary = "summary"

	compareTopMin     = 1
	compareTopMax     = 1000
	compareTopDefault = 10

	// truncation budgets — tabwriter handles alignment, but very long
	// commands and paths still get abbreviated so a single 200-char path
	// can't push the size columns off-screen.
	compareCommandWidth = 32
	comparePathWidth    = 40
	compareReasonWidth  = 15

	// progressCoalesce is the minimum interval between two consecutive
	// pulling-line refreshes. Docker emits progress JSON aggressively (up
	// to dozens of events per second); without coalescing the stderr line
	// is unreadable. 250 ms feels live without flooding.
	progressCoalesce = 250 * time.Millisecond
)

var (
	flagCompareMode string
	flagCompareTop  int
)

var compareCmd = &cobra.Command{
	Use:   "compare [flags] OLD_IMAGE NEW_IMAGE",
	Short: "Compare two images and surface size/efficiency deltas (exit 1 on regression)",
	Long: `Compare two images and report size, efficiency, layer, file, and waste
deltas in a deterministic, CI-friendly text report.

Both arguments accept the same inputs as "layerx" itself: a container image
reference (Docker or Podman — e.g. "nginx:1.25") or a path to a local image
archive produced by "docker save" / "podman save" or an OCI-layout tarball.
The two sides may mix freely (an archive on the old side, a registry ref on
the new side, etc.).

Output ends with a single machine-parseable verdict line:
  verdict: ok
  verdict: regression reason=<comma-separated reasons>
  verdict: noop digest=<sha256...>
  verdict: noop reason=path-equal       (archive paths matched without a digest)

Progress messages (image pulls, exports) are written to stderr so the
report on stdout stays grep-clean for CI gating. Pipe "2>/dev/null" to
silence them.

Exit codes:
  0  no regression detected (or noop)
  1  regression detected (efficiency dropped, wasted bytes increased, or both)
  2  operational error (resolver failure, daemon down, archive missing, etc.)

See "layerx --help" for details on --engine and --no-cache.`,
	Example: `  # Compare a release tag against the previous one
  layerx compare myapp:1.4.0 myapp:1.5.0

  # Compare the previous build artifact against the freshly-built archive
  layerx compare ./build/prev.tar ./build/new.tar

  # Show every diff entry (no top-N truncation)
  layerx compare --mode full myapp:old myapp:new

  # Force a fresh analysis of both sides, ignoring any cached results
  layerx compare --no-cache myapp:prev myapp:next

  # CI gate: fails non-zero on regression
  layerx compare myapp:prev myapp:next || echo "image regressed"

  # Script-friendly: extract the machine-parseable verdict line
  layerx compare myapp:prev myapp:next | grep '^verdict:'  # ok | regression | noop`,
	Args:          compareArgs,
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

// compareArgs is a custom cobra args validator: on zero or wrong count it
// prints a short usage hint to stderr (synopsis + 3 examples) and returns the
// ErrCompareUsage sentinel so main.go exits 2 without cobra adding its own
// "Error: accepts 2 arg(s)" line. Two-arg invocations pass through.
func compareArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 2 {
		return nil
	}
	w := cmd.ErrOrStderr()
	if len(args) == 0 {
		fmt.Fprintln(w, "layerx compare: compare two images and report size/efficiency deltas")
	} else {
		fmt.Fprintf(w, "layerx compare: needs exactly 2 image arguments, got %d\n", len(args))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  layerx compare [flags] OLD_IMAGE NEW_IMAGE")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  layerx compare myapp:1.4.0 myapp:1.5.0")
	fmt.Fprintln(w, "  layerx compare ./build/prev.tar ./build/new.tar")
	fmt.Fprintln(w, "  layerx compare --mode full nginx:1.25 nginx:1.26")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Each argument may be a container image reference (Docker or Podman) or a path")
	fmt.Fprintln(w, "to a tar archive produced by `docker save` / `podman save` (or an OCI-layout tarball).")
	fmt.Fprintln(w, "Run `layerx compare --help` for the full reference.")
	return &ErrCompareUsage{}
}

func runCompareCmd(cmd *cobra.Command, args []string) error {
	err := runCompareCmdInner(cmd, args)
	// compareCmd has SilenceErrors=true so cobra will not print operational
	// errors; surface them ourselves to stderr. ErrCompareRegression is silent
	// because the report is already on stdout. ErrCompareUsage is silent
	// because compareArgs already wrote a hint.
	if err != nil {
		if _, ok := errors.AsType[*ErrCompareRegression](err); ok {
			return err
		}
		if _, ok := errors.AsType[*ErrCompareUsage](err); ok {
			return err
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
	return err
}

func runCompareCmdInner(cmd *cobra.Command, args []string) error {
	oldRef, newRef := args[0], args[1]

	if err := validateCompareFlags(flagCompareMode, flagCompareTop); err != nil {
		return err
	}

	ctx := cmd.Context()
	noCache := noCacheRequested()

	out := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()

	// Path-equality short-circuit: identical archive paths trivially resolve
	// to the same content. The digest-based check below catches this too when
	// the archive's ImageID is observable, but archive resolvers don't always
	// expose a digest (e.g. malformed manifests, OCI variants), and a cheap
	// path comparison gets us out before two full analyses.
	if oldRef == newRef && isRegularFilePath(oldRef) {
		renderNoOp(out, "")
		return nil
	}

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
			renderNoOp(out, d1)
			return nil
		}
	}

	oldDigest, oldAnalysis, err := analyzeForCompare(ctx, oldResolver, oldRef, noCache, "old", stderr)
	if err != nil {
		return fmt.Errorf("analyzing old image %q: %w", oldRef, err)
	}
	newDigest, newAnalysis, err := analyzeForCompare(ctx, newResolver, newRef, noCache, "new", stderr)
	if err != nil {
		return fmt.Errorf("analyzing new image %q: %w", newRef, err)
	}

	// Re-check the no-op shortcut after analyze: the pull may have made the
	// digests observable when they weren't before.
	if oldDigest != "" && oldDigest == newDigest {
		renderNoOp(out, oldDigest)
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
	// --top is only consulted in compact mode; let summary/full pass through
	// any value (including 0) without rejecting them, matching the help text.
	if mode == compareModeCompact && (top < compareTopMin || top > compareTopMax) {
		return fmt.Errorf("--top must be in [%d, %d]; got %d", compareTopMin, compareTopMax, top)
	}
	return nil
}

// analyzeForCompare runs the analyze pipeline for ref with progress events
// drained to stderr (prefixed with side="old"/"new" so the user can tell which
// image is currently resolving). Returns the post-resolve image digest used
// for no-op detection. Post-analyze ImageID errors are surfaced as warnings
// on stderr (the analysis succeeded; the digest is only used for the no-op
// shortcut, so a missing digest is not fatal).
func analyzeForCompare(ctx context.Context, resolver image.Resolver, ref string, noCache bool, side string, stderr io.Writer) (string, *image.Analysis, error) {
	progress := make(chan image.ProgressEvent, 32)
	var wg sync.WaitGroup
	wg.Go(func() {
		drainProgress(stderr, side, ref, progress)
	})
	// LIFO order matters: defer wg.Wait() registers FIRST so it runs LAST,
	// after defer close(progress) has unblocked the drain goroutine. If
	// AnalyzeWithOptions panics or returns through any future early-return
	// path, the goroutine still exits and we don't deadlock.
	defer wg.Wait()
	defer close(progress)

	analysis, err := image.AnalyzeWithOptions(ctx, resolver, ref, image.AnalyzeOptions{
		NoCache:  noCache,
		Progress: progress,
	})

	if err != nil {
		return "", nil, err
	}
	digest, idErr := resolver.ImageID(ctx, ref)
	if idErr != nil {
		fmt.Fprintf(stderr, "warning: could not resolve digest for %q after analyze: %v\n", ref, idErr)
	}
	if digest != "" {
		fmt.Fprintf(stderr, "[%s] resolved %s\n", side, shortDigest(digest))
	}
	return digest, analysis, nil
}

// drainProgress reads events from ch and writes one human-readable status line
// per phase transition to w. Pulling events are coalesced to one line per
// progressCoalesce window to avoid spamming on chatty Docker pulls.
func drainProgress(w io.Writer, side, ref string, ch <-chan image.ProgressEvent) {
	drainProgressWithClock(w, side, ref, ch, progressCoalesce, time.Now)
}

// drainProgressWithClock is the testable seam of drainProgress. The coalesce
// interval and clock are injected so tests don't sleep.
func drainProgressWithClock(w io.Writer, side, ref string, ch <-chan image.ProgressEvent, coalesce time.Duration, now func() time.Time) {
	var (
		lastPhase     = image.PhaseUnknown
		lastPullEmit  time.Time
		announced     bool
	)
	for ev := range ch {
		// Print "resolving REF" exactly once, on the first event we see for
		// this side, so the user knows which image is currently working.
		if !announced {
			fmt.Fprintf(w, "[%s] resolving %s\n", side, ref)
			announced = true
		}
		switch ev.Phase {
		case image.PhasePulling:
			// Coalesce repeated pulling events into at most one line per
			// coalesce window. The first pulling event always prints (so
			// the user sees "pulling" immediately); subsequent ones bunch.
			if lastPhase != image.PhasePulling {
				fmt.Fprintf(w, "[%s] pulling image\n", side)
				lastPhase = image.PhasePulling
				lastPullEmit = now()
				continue
			}
			if !lastPullEmit.IsZero() && now().Sub(lastPullEmit) < coalesce {
				continue
			}
			lastPullEmit = now()
			if ev.LayersTotal > 0 || ev.BytesTotal > 0 {
				fmt.Fprintf(w, "[%s] pulling: %d/%d layers, %s / %s\n",
					side, ev.LayersDone, ev.LayersTotal,
					image.FormatBytes(ev.BytesCurr), image.FormatBytes(ev.BytesTotal))
			} else {
				fmt.Fprintf(w, "[%s] pulling: working...\n", side)
			}
		case image.PhaseExporting:
			if lastPhase != image.PhaseExporting {
				fmt.Fprintf(w, "[%s] exporting image\n", side)
				lastPhase = image.PhaseExporting
			}
		case image.PhaseParsing:
			if lastPhase != image.PhaseParsing {
				fmt.Fprintf(w, "[%s] parsing layers\n", side)
				lastPhase = image.PhaseParsing
			}
		case image.PhaseCacheLoad:
			fmt.Fprintf(w, "[%s] cache hit\n", side)
			lastPhase = image.PhaseCacheLoad
		case image.PhaseCacheWarn:
			if ev.Message != "" {
				fmt.Fprintf(w, "[%s] cache warning: %s\n", side, ev.Message)
			}
		}
	}
}

// shortDigest returns "sha256:abcdef0123…" — the first 19 characters of a
// digest, enough to be visually distinct without occupying half the line.
func shortDigest(d string) string {
	const short = 19
	if utf8.RuneCountInString(d) <= short {
		return d
	}
	runes := []rune(d)
	return string(runes[:short]) + "..."
}

// renderNoOp prints the same-digest message in plain language followed by the
// machine-parseable verdict line. exit code stays 0; callers return nil.
// When digest is empty (e.g. path-equality short-circuit on archives whose
// resolvers can't expose an ImageID), the verdict line uses reason=path-equal
// so parsers don't see an empty digest= value.
func renderNoOp(w io.Writer, digest string) {
	fmt.Fprintln(w, "Both inputs resolve to the same image content - no diff to show.")
	if digest != "" {
		fmt.Fprintf(w, "  digest: %s  (full: %s)\n", shortDigest(digest), digest)
		fmt.Fprintf(w, "verdict: noop digest=%s\n", digest)
		return
	}
	fmt.Fprintln(w, "verdict: noop reason=path-equal")
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
	tw := newCompareTabwriter(w)
	fmt.Fprintf(tw, "old:\t%s\t%d layers\t%s\teff %.2f\n",
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
	fmt.Fprintf(tw, "new:\t%s\t%d layers\t%s\teff %.2f%s\n",
		fallback(r.After.ImageRef, "(none)"),
		r.After.LayerCount,
		image.FormatBytes(r.After.TotalSize),
		r.AfterEfficiency.Score,
		deltaPart)
	_ = tw.Flush()
	fmt.Fprintln(w)
}

// scoreHeaderEpsilon mirrors image.scoreEpsilon (1e-9). Defined locally so
// cmd/ does not depend on an unexported image symbol.
const scoreHeaderEpsilon = 1e-9

// newCompareTabwriter returns a tabwriter tuned for compare tables: 2-space
// minimum cell padding, no left padding, padchar=' '. Output is plain ASCII
// so it survives CI logs and pipes to grep/awk.
func newCompareTabwriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

func writeLayerTable(w io.Writer, r *image.CompareResult, mode string, topN int) {
	if len(r.LayerDiffs) == 0 {
		return
	}
	fmt.Fprintln(w, "LAYERS")
	tw := newCompareTabwriter(w)
	fmt.Fprintln(tw, "  IDX\tCOMMAND\tOLD SIZE\tNEW SIZE\tdelta")
	rows := r.LayerDiffs
	if mode == compareModeCompact {
		ranked := rankLayerDiffsByDelta(rows)
		limit := min(len(ranked), topN)
		for i := range limit {
			writeLayerRow(tw, ranked[i])
		}
		_ = tw.Flush()
		if len(ranked) > topN {
			fmt.Fprintf(w, "  ... and %d more layers (delta %s)\n",
				len(ranked)-topN,
				image.FormatSignedBytes(sumLayerHidden(ranked[topN:])))
		}
	} else {
		for _, d := range rows {
			writeLayerRow(tw, d)
		}
		_ = tw.Flush()
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

func writeLayerRow(tw *tabwriter.Writer, d image.LayerDiff) {
	fmt.Fprintf(tw, "  %2d\t%s\t%s\t%s\t%s\n",
		d.Index,
		truncate(fallback(d.AfterCommand, d.BeforeCommand), compareCommandWidth),
		image.FormatBytes(d.BeforeSize),
		image.FormatBytes(d.AfterSize),
		image.FormatSignedBytes(d.SizeDelta))
}

func writeFileTable(w io.Writer, r *image.CompareResult, mode string, topN int) {
	if len(r.FileDiffs) == 0 {
		return
	}
	netDelta := r.FileSummary.BytesAdded - r.FileSummary.BytesRemoved
	fmt.Fprintf(w, "FILE CHANGES   +%d  ~%d  -%d   delta %s\n",
		r.FileSummary.AddedCount, r.FileSummary.ModifiedCount, r.FileSummary.RemovedCount,
		image.FormatSignedBytes(netDelta))

	tw := newCompareTabwriter(w)
	rows := r.FileDiffs
	if mode == compareModeCompact {
		ranked := rankFileDiffsByDelta(rows)
		limit := min(len(ranked), topN)
		for i := range limit {
			writeFileRow(tw, ranked[i])
		}
		_ = tw.Flush()
		if len(ranked) > topN {
			fmt.Fprintf(w, "  ... and %d more files (delta %s)\n",
				len(ranked)-topN,
				image.FormatSignedBytes(sumFileHidden(ranked[topN:])))
		}
	} else {
		for _, d := range rows {
			writeFileRow(tw, d)
		}
		_ = tw.Flush()
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

func writeFileRow(tw *tabwriter.Writer, d image.FileDiff) {
	sym := "?"
	switch d.DiffType {
	case image.Added:
		sym = "+"
	case image.Modified:
		sym = "~"
	case image.Removed:
		sym = "-"
	}
	fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n",
		sym,
		truncate(d.Path, comparePathWidth),
		truncate(d.ChangeReason, compareReasonWidth),
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
	fmt.Fprintf(w, "WASTE CHANGES   delta %s\n", image.FormatSignedBytes(totalDelta))

	tw := newCompareTabwriter(w)
	fmt.Fprintln(tw, "  PATH\tWAS\tNOW\tdelta")
	rows := r.WasteDiffs
	limit := len(rows)
	if mode == compareModeCompact && limit > topN {
		limit = topN
	}
	for i := range limit {
		writeWasteRow(tw, rows[i])
	}
	_ = tw.Flush()
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

func writeWasteRow(tw *tabwriter.Writer, wd image.WasteDiff) {
	fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n",
		truncate(wd.Path, comparePathWidth),
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
// rune boundaries (never mid-UTF-8) and reserves two columns for the trailing
// ".." marker. The return is at most n runes wide.
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
	runes := []rune(s)
	return string(runes[:n-2]) + ".."
}

func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
