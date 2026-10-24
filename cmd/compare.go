package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/deveshctl/layerx/config"
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
	Short: "Compare image size, efficiency, and file changes",
	Long: `Compare OLD_IMAGE with NEW_IMAGE. Each input can be a Docker or Podman
image reference, a saved-image tar archive, or an OCI layout tarball.

Exit codes:
  0  no regression, or identical inputs
  1  efficiency decreased, wasted bytes increased, or a size budget failed
  2  operational error (invalid input, configuration, or engine connection)

Reports go to stdout; progress and errors go to stderr. The last report line
starts with "verdict:" and contains ok, regression, or noop. In CI, run the
command directly so its exit code determines whether the step passes.

Use --mode full for all changes, or --mode summary for totals and verdict.

Reads size-budget and theme from .layerx.yaml; --config selects another file.
Flags override size budgets. CI efficiency thresholds and path rules apply
only to ci; compare retains its efficiency/waste regression checks.
Size budgets measure uncompressed layer tar bytes. File differences compare
size and metadata, not file contents. Use --format json or markdown for reports.`,
	Example: `  layerx compare app:1.4.0 app:1.5.0
  layerx compare ./build/previous.tar ./build/current.tar
  layerx compare --mode full app:1.4.0 app:1.5.0
  layerx compare --mode summary nginx:1.26 nginx:stable
  layerx compare --max-growth 5% app:main app:pr
  layerx compare --format markdown app:main app:pr`,
	Args:          compareArgs,
	RunE:          runCompareCmd,
	SilenceErrors: true,
	SilenceUsage:  true,
}

func init() {
	compareCmd.Flags().StringVar(&flagCompareMode, "mode", compareModeCompact,
		"report detail: compact, full, or summary")
	compareCmd.Flags().IntVar(&flagCompareTop, "top", compareTopDefault,
		"rows per section in compact mode (1-1000)")

	addSizeBudgetFlags(compareCmd)
	compareCmd.Flags().String("theme", "", "report colour theme (defaults to configuration)")
	_ = compareCmd.RegisterFlagCompletionFunc("theme", completeTheme)
	rootCmd.AddCommand(compareCmd)
}

// compareArgs prints a short input hint and avoids a duplicate Cobra error.
func compareArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 2 {
		return nil
	}
	w := cmd.ErrOrStderr()
	fmt.Fprintf(w, "layerx compare: expected 2 images or archives, got %d\n", len(args))
	fmt.Fprintln(w, "Usage: layerx compare [flags] OLD_IMAGE NEW_IMAGE")
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  layerx compare app:1.4.0 app:1.5.0")
	fmt.Fprintln(w, "Run layerx compare --help for options.")
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
		if _, ok := errors.AsType[*config.LoadError](err); ok {
			presentConfigLoadFailure(cmd, err)
		} else {
			_ = presentCLIError(cmd.ErrOrStderr(), err)
		}
	}
	return err
}

func runCompareCmdInner(cmd *cobra.Command, args []string) error {
	oldRef, newRef := args[0], args[1]

	if err := validateCompareFlags(flagCompareMode, flagCompareTop); err != nil {
		return err
	}

	cfg, err := readConfig()
	if err != nil {
		return err
	}
	budgetConfig, limits, err := effectiveSizeBudget(cmd, cfg)
	if err != nil {
		return err
	}
	themeFlag, _ := cmd.Flags().GetString("theme")
	themeName, err := resolveTheme(cfg.Theme, themeFlag)
	if err != nil {
		return err
	}
	format, _ := cmd.Flags().GetString("format")
	ctx := cmd.Context()
	noCache := noCacheRequested()

	out := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()

	oldResolver, err := selectResolver(oldRef)
	if err != nil {
		return fmt.Errorf("preparing old image %q: %w", oldRef, err)
	}
	newResolver, err := selectResolver(newRef)
	if err != nil {
		return fmt.Errorf("preparing new image %q: %w", newRef, err)
	}

	oldDigest, oldAnalysis, err := analyzeForCompare(ctx, oldResolver, oldRef, noCache, "old", stderr)
	if err != nil {
		return fmt.Errorf("analyzing old image %q: %w", oldRef, err)
	}
	newDigest, newAnalysis, err := analyzeForCompare(ctx, newResolver, newRef, noCache, "new", stderr)
	if err != nil {
		return fmt.Errorf("analyzing new image %q: %w", newRef, err)
	}

	if limits.Growth != nil {
		if err := matchingPlatforms(oldAnalysis, newAnalysis); err != nil {
			return err
		}
	}
	sizeChecks, err := limits.Evaluate(budgetConfig, newAnalysis.UncompressedSize, &oldAnalysis.UncompressedSize)
	if err != nil {
		return err
	}
	result := image.CompareAnalysis(oldAnalysis, newAnalysis)
	if !limits.Enabled() && format == "text" && oldDigest != "" && oldDigest == newDigest && allLayersShared(result) {
		renderNoOp(out, oldDigest)
		return nil
	}

	report := newCheckReport(newAnalysis, newDigest, budgetConfig, sizeChecks)
	before := reportSnapshot(oldAnalysis, oldDigest)
	report.Before = &before
	report.Comparison = result
	report.Passed = report.Passed && !result.IsRegression()
	options := terminalCompareOptions(out, themeName)
	if format != "text" {
		options.Color = false
	}
	var rendered strings.Builder
	renderComparison(&rendered, result, flagCompareMode, flagCompareTop, sizeChecks, options)
	if err := writeCheckReport(out, format, rendered.String(), report); err != nil {
		return err
	}
	if !report.Passed {
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
	// Drain all progress before writing the final digest line. The scoped
	// defers also close and join the goroutine if analysis panics.
	analysis, err := func() (*image.Analysis, error) {
		defer wg.Wait()
		defer close(progress)
		return image.AnalyzeWithOptions(ctx, resolver, ref, image.AnalyzeOptions{
			NoCache: noCache, Progress: progress, Platform: activePlatformDisplay(),
		})
	}()

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
		lastPhase    = image.PhaseUnknown
		lastPullEmit time.Time
		announced    bool
	)
	for ev := range ch {
		if !announced {
			fmt.Fprintf(w, "[%s] resolving %s\n", side, ref)
			announced = true
		}
		switch ev.Phase {
		case image.PhasePulling:
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

// renderCompareReport is the plain deterministic renderer used in tests and logs.
func renderCompareReport(w io.Writer, r *image.CompareResult, mode string, topN int) {
	renderComparison(w, r, mode, topN, nil, compareOptions{Width: 100})
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

func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func allLayersShared(r *image.CompareResult) bool {
	if r.Before.LayerCount != r.After.LayerCount || r.Before.Platform != r.After.Platform {
		return false
	}
	for _, d := range r.LayerDiffs {
		if d.Match != "shared" {
			return false
		}
	}
	return true
}
