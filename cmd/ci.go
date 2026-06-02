package cmd

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/deveshctl/layerx/ci"
	"github.com/deveshctl/layerx/config"
	"github.com/deveshctl/layerx/image"
	"github.com/spf13/cobra"
)

// ErrCIFailed signals that one or more CI rules did not pass. main.go
// maps this sentinel to exit code 1; any other non-nil error returned
// from cobra exits 2. The report has already been printed to stdout by
// the time this is returned, so the caller should not print the error
// message itself.
type ErrCIFailed struct{}

func (e *ErrCIFailed) Error() string {
	return "CI check failed"
}

// ErrCIUsage is returned when the user invoked `layerx ci` with the wrong
// number of arguments. ciArgs writes a short hint to stderr; the sentinel
// itself carries no message body so cobra doesn't double-print after it.
// main.go has no special case for this sentinel, so it exits 2 (operational
// error), matching ErrCompareUsage.
type ErrCIUsage struct{}

func (e *ErrCIUsage) Error() string { return "usage" }

var (
	flagLowestEfficiency         float64
	flagHighestWastedBytes       int64
	flagHighestUserWastedPercent float64
)

var ciCmd = &cobra.Command{
	Use:   "ci [flags] IMAGE_OR_ARCHIVE",
	Short: "Run efficiency checks for CI pipelines",
	Long: `Evaluate image efficiency against configurable thresholds.

Accepts either a Docker image reference or a path to a local image archive
(docker save / OCI layout tarball). Archive mode requires no Docker daemon —
useful in CI runners that already produced the artifact and want to avoid
loading it into an engine just to inspect it.

Exits 0 when all rules pass, 1 when any rule fails. Output is plain text
suitable for CI logs.

Exit codes:
  0  all rules passed
  1  one or more rules failed
  2  internal error (Docker daemon down, archive not found, malformed config, etc.)

Thresholds can be set via flags or a .layerx.yaml file in the working
directory. Flags take precedence over config values. A missing config
file is silently ignored and built-in defaults apply.

  rules:
    lowest-efficiency: 0.9           # minimum efficiency score (0.0-1.0)
    highest-wasted-bytes: 0          # max wasted bytes (0 = disabled)
    highest-user-wasted-percent: 0.1 # max waste as fraction of total (0.0-1.0)

Cache:
  Analysis results are cached on disk and reused across runs. Pass
  --no-cache to force a fresh analysis (useful for pipelines that must
  re-parse the image after a rebuild within the same digest).`,
	Example: `  # Run with default thresholds (lowest-efficiency: 0.9, highest-user-wasted-percent: 0.1)
  layerx ci nginx:latest

  # Inspect a local archive in CI (no daemon required)
  layerx ci ./build/app.tar

  # Override a single threshold
  layerx ci --lowest-efficiency 0.95 nginx:latest

  # Combine multiple rules
  layerx ci \
    --lowest-efficiency 0.9 \
    --highest-wasted-bytes 10485760 \
    nginx:latest

  # Force a fresh analysis, ignoring any cached result
  layerx ci --no-cache nginx:latest`,
	Args:          ciArgs,
	RunE:          runCICmd,
	SilenceErrors: true,
	SilenceUsage:  true,
}

func init() {
	ciCmd.Flags().Float64Var(&flagLowestEfficiency, "lowest-efficiency", -1, "minimum acceptable efficiency score, 0.0-1.0 (0 disables the rule; config default: 0.9)")
	ciCmd.Flags().Int64Var(&flagHighestWastedBytes, "highest-wasted-bytes", -1, "maximum allowed wasted bytes (0 disables the rule)")
	ciCmd.Flags().Float64Var(&flagHighestUserWastedPercent, "highest-user-wasted-percent", -1, "maximum wasted bytes as fraction of total size, 0.0-1.0 (0 disables the rule)")

	rootCmd.AddCommand(ciCmd)
}

// ciArgs is a custom cobra args validator: on zero or wrong count it prints a
// short usage hint to stderr (synopsis + 3 examples) and returns the
// ErrCIUsage sentinel so main.go exits 2 without cobra adding its own
// "Error: accepts 1 arg(s)" line. ciCmd has SilenceErrors=true and
// SilenceUsage=true, so without this validator a bare `layerx ci` produced
// only cobra's terse "accepts 1 arg(s), received 0" with no actionable help.
// Mirrors compareArgs in cmd/compare.go.
func ciArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 1 {
		return nil
	}
	w := cmd.ErrOrStderr()
	if len(args) == 0 {
		fmt.Fprintln(w, "layerx ci: run efficiency checks for CI pipelines")
	} else {
		fmt.Fprintf(w, "layerx ci: needs exactly 1 image argument, got %d\n", len(args))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  layerx ci [flags] IMAGE_OR_ARCHIVE")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  layerx ci nginx:latest")
	fmt.Fprintln(w, "  layerx ci ./build/app.tar")
	fmt.Fprintln(w, "  layerx ci --lowest-efficiency 0.95 nginx:latest")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "The argument may be a Docker image reference or a path to a tar archive")
	fmt.Fprintln(w, "produced by `docker save` (or an OCI layout tarball).")
	fmt.Fprintln(w, "Run `layerx ci --help` for the full reference.")
	return &ErrCIUsage{}
}

func runCICmd(cmd *cobra.Command, args []string) error {
	imageRef := args[0]

	if err := validateCLIThresholdFlags(cmd); err != nil {
		return err
	}

	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}

	analysis, ciErr := executeCICheck(cmd.Context(), imageRef, cfg, cmd, noCacheRequested(), false)
	if flagJSON != "" {
		var jsonErr error
		switch {
		case analysis != nil:
			jsonErr = runJSONExportFromAnalysis(analysis, flagJSON)
		case ciErr != nil:
			// CI failed before producing an analysis (e.g. Docker daemon
			// down). Nothing to export.
			jsonErr = nil
		default:
			jsonErr = runJSONExport(cmd.Context(), imageRef, flagJSON, noCacheRequested())
		}
		return combineCIAndJSONErr(ciErr, jsonErr, os.Stderr)
	}
	return ciErr
}

// executeCICheck runs the rules pipeline. viaCIEnv is true when the caller is
// the CI=true shortcut on rootCmd (cmd is the package-level ciCmd, not the
// command the user actually typed); it is false when invoked through `layerx
// ci`. The flag steers the all-rules-disabled error message — the shortcut
// path can't accept threshold flags, so naming them in the error is wrong.
func executeCICheck(ctx context.Context, imageRef string, cfg *config.Config, cmd *cobra.Command, noCache bool, viaCIEnv bool) (*image.Analysis, error) {
	progCh, stop := stderrProgress(ctx, os.Stderr)
	defer stop()
	analysis, err := runCICheckInner(ctx, imageRef, cfg, cmd, noCache, viaCIEnv, progCh)
	// ciCmd has SilenceErrors=true and root.go silences errors when CI=true,
	// so cobra will not print anything for us. Surface non-CIFailed errors
	// (e.g. Docker daemon down) to stderr ourselves; the CIFailed sentinel
	// stays silent because executeCICheck has already printed the report.
	if err != nil {
		if _, ok := errors.AsType[*ErrCIFailed](err); !ok {
			_ = presentCLIError(os.Stderr, err)
		}
	}
	return analysis, err
}

func runCICheckInner(ctx context.Context, imageRef string, cfg *config.Config, cmd *cobra.Command, noCache bool, viaCIEnv bool, progress chan<- image.ProgressEvent) (*image.Analysis, error) {
	rules := buildRules(cfg, cmd)
	if len(rules) == 0 {
		return nil, errNoCIRulesEnabled(viaCIEnv)
	}

	resolver, err := selectResolver(imageRef)
	if err != nil {
		return nil, err
	}

	analysis, err := image.AnalyzeWithOptions(ctx, resolver, imageRef,
		image.AnalyzeOptions{NoCache: noCache, Progress: progress})
	if err != nil {
		return nil, err
	}

	efficiency := image.EfficiencyFromAnalysis(analysis)
	report := ci.Evaluate(ci.EvalContext{
		Efficiency:   efficiency,
		TotalSize:    analysis.TotalSize,
		Layers:       analysis.Layers,
		StackedTrees: analysis.StackedTrees,
	}, rules)
	report.Print(os.Stdout)

	if report.ExitCode() != 0 {
		return analysis, &ErrCIFailed{}
	}
	return analysis, nil
}

// errNoCIRulesEnabled produces a context-aware error when every threshold is
// 0 / disabled. The exact wording depends on how the user reached the CI path:
// invoking `layerx ci` exposes the threshold flags directly, while the
// `CI=true layerx IMG` shortcut runs the same code without those flags being
// part of the surface (the parent command is `layerx`, not `layerx ci`). In
// the shortcut case we steer users to the config file rather than naming
// flags they can't pass on that invocation.
func errNoCIRulesEnabled(viaCIEnv bool) error {
	if viaCIEnv {
		return fmt.Errorf("no CI rules enabled — set at least one threshold > 0 in .layerx.yaml (or invoke `layerx ci` directly to use --lowest-efficiency / --highest-wasted-bytes / --highest-user-wasted-percent)")
	}
	return fmt.Errorf("no CI rules enabled — set at least one threshold > 0 in .layerx.yaml or via --lowest-efficiency / --highest-wasted-bytes / --highest-user-wasted-percent")
}

func buildRules(cfg *config.Config, cmd *cobra.Command) []ci.Rule {
	le := cfg.Rules.LowestEfficiency
	if cmd.Flags().Changed("lowest-efficiency") {
		le = flagLowestEfficiency
	}

	hwb := cfg.Rules.HighestWastedBytes
	if cmd.Flags().Changed("highest-wasted-bytes") {
		hwb = flagHighestWastedBytes
	}

	huwp := cfg.Rules.HighestUserWastedPercent
	if cmd.Flags().Changed("highest-user-wasted-percent") {
		huwp = flagHighestUserWastedPercent
	}

	rules := []ci.Rule{}
	if le > 0 {
		rules = append(rules, ci.LowestEfficiency{Threshold: le})
	}
	if hwb > 0 {
		rules = append(rules, ci.HighestWastedBytes{Threshold: hwb})
	}
	if huwp > 0 {
		rules = append(rules, ci.HighestUserWastedPercent{Threshold: huwp})
	}

	for _, spec := range cfg.PathRules {
		switch spec.Type {
		case config.PathRuleBlock:
			rules = append(rules, ci.BlockPathRule{ID: spec.ID, Patterns: spec.Paths})
		case config.PathRuleDenyWaste:
			rules = append(rules, ci.DenyWastePathRule{ID: spec.ID, Patterns: spec.Paths})
		case config.PathRuleMaxLayerCount:
			rules = append(rules, ci.MaxLayerCountRule{ID: spec.ID, MaxCount: spec.Threshold})
		}
	}

	return rules
}

// validateCLIThresholdFlags rejects out-of-range or non-finite CLI values for
// the three threshold flags. Config-file path is already checked by
// config.validate(); this mirrors that for command-line input so a typo like
// "--lowest-efficiency -0.5" surfaces as an error instead of silently
// disabling the rule.
func validateCLIThresholdFlags(cmd *cobra.Command) error {
	if cmd.Flags().Changed("lowest-efficiency") {
		v := flagLowestEfficiency
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return fmt.Errorf("--lowest-efficiency must be a finite number in [0, 1]; got %v", v)
		}
	}
	if cmd.Flags().Changed("highest-wasted-bytes") {
		if flagHighestWastedBytes < 0 {
			return fmt.Errorf("--highest-wasted-bytes must be >= 0; got %d", flagHighestWastedBytes)
		}
	}
	if cmd.Flags().Changed("highest-user-wasted-percent") {
		v := flagHighestUserWastedPercent
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return fmt.Errorf("--highest-user-wasted-percent must be a finite number in [0, 1]; got %v", v)
		}
	}
	return nil
}
