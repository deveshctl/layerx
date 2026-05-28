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
	Example: `  # Run with default thresholds (lowest-efficiency: 0.9)
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
	Args:          cobra.ExactArgs(1),
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

func runCICmd(cmd *cobra.Command, args []string) error {
	imageRef := args[0]

	if err := validateCLIThresholdFlags(cmd); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	analysis, ciErr := executeCICheck(imageRef, cfg, cmd, noCacheRequested())
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
			jsonErr = runJSONExport(imageRef, flagJSON, noCacheRequested())
		}
		return combineCIAndJSONErr(ciErr, jsonErr, os.Stderr)
	}
	return ciErr
}

func executeCICheck(imageRef string, cfg *config.Config, cmd *cobra.Command, noCache bool) (*image.Analysis, error) {
	analysis, err := runCICheckInner(imageRef, cfg, cmd, noCache)
	// ciCmd has SilenceErrors=true and root.go silences errors when CI=true,
	// so cobra will not print anything for us. Surface non-CIFailed errors
	// (e.g. Docker daemon down) to stderr ourselves; the CIFailed sentinel
	// stays silent because executeCICheck has already printed the report.
	if err != nil {
		if _, ok := errors.AsType[*ErrCIFailed](err); !ok {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
	}
	return analysis, err
}

func runCICheckInner(imageRef string, cfg *config.Config, cmd *cobra.Command, noCache bool) (*image.Analysis, error) {
	resolver, err := selectResolver(imageRef)
	if err != nil {
		return nil, err
	}

	analysis, err := image.AnalyzeWithOptions(context.Background(), resolver, imageRef,
		image.AnalyzeOptions{NoCache: noCache})
	if err != nil {
		return nil, err
	}

	efficiency := image.Efficiency(analysis.Layers)
	rules := buildRules(cfg, cmd)
	report := ci.Evaluate(efficiency, analysis.TotalSize, rules)
	report.Print(os.Stdout)

	if report.ExitCode() != 0 {
		return analysis, &ErrCIFailed{}
	}
	return analysis, nil
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
