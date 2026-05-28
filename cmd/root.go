package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/deveshctl/layerx/config"
	"github.com/deveshctl/layerx/tui"
	"github.com/spf13/cobra"
)

var (
	flagJSON      string
	flagNoCacheFl bool
	flagRefresh   bool
)

// noCacheRequested returns true when the user passed --no-cache or its
// hidden alias --refresh. The two flags have separate underlying vars so
// command-line ordering can't reverse the bypass intent (e.g.
// `--refresh --no-cache=false` would otherwise leave the bypass off).
func noCacheRequested() bool {
	return flagNoCacheFl || flagRefresh
}

var rootCmd = &cobra.Command{
	Use:   "layerx [flags] IMAGE_OR_ARCHIVE",
	Short: "Inspect Docker image layers",
	Long: `Inspect a container image's layers, filesystem changes, and wasted bytes.

By default launches an interactive TUI for browsing layers, viewing file
contents, and surfacing duplicated or removed files.

Inputs:
  IMAGE_OR_ARCHIVE may be either a Docker image reference (e.g.
  "nginx:latest" or "myregistry.io/team/app:1.2") or a path to a local
  image archive produced by "docker save" or an OCI layout tarball. The
  argument is auto-detected: an existing regular file is read directly
  without contacting any container runtime; anything else is resolved via
  the Docker daemon.

  Archive mode requires no Docker daemon, no network, and no running
  containers — useful in CI runners and air-gapped environments where the
  image is already on disk.

Use --json to skip the TUI and export the analysis, or run "layerx ci" for
non-interactive efficiency checks. Both work with image refs and archive
paths.

Cache:
  Analysis results are cached on disk so repeat runs against an unchanged
  image are near-instant. Use --no-cache to bypass the cache for a single
  run; the run still refreshes the cache on success.

Environment:
  CI=true   When set, "layerx IMAGE_OR_ARCHIVE" runs the ci subcommand with
            default (or config-file) thresholds instead of launching the
            TUI. Useful for pipelines that invoke layerx without a
            subcommand. To override thresholds on the command line, invoke
            the ci subcommand directly:
            "layerx ci --lowest-efficiency 0.95 IMG".`,
	Example: `  # Inspect an image interactively (Docker daemon required)
  layerx nginx:latest

  # Inspect a local archive produced by "docker save" (no daemon required)
  layerx ./build/app.tar

  # Inspect an OCI layout tarball
  layerx /tmp/myimage-oci.tar

  # Force a fresh analysis, ignoring any cached result
  layerx --no-cache nginx:latest

  # Export analysis as JSON (skips the TUI; works with archives too)
  layerx --json out.json ./build/app.tar

  # Run efficiency checks (also triggered by CI=true)
  layerx ci nginx:latest`,
	Args: cobra.ExactArgs(1),
	RunE: runInspect,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagJSON, "json", "", "write analysis to PATH as JSON (skips TUI; composes with the ci subcommand)")
	rootCmd.PersistentFlags().BoolVar(&flagNoCacheFl, "no-cache", false, "bypass the analysis cache for this run; the run still writes the cache on success")
	rootCmd.PersistentFlags().BoolVar(&flagRefresh, "refresh", false, "alias for --no-cache")
	_ = rootCmd.PersistentFlags().MarkHidden("refresh")
}

func SetVersionInfo(v, c, d string) {
	c = sanitizeVersionField(c)
	d = sanitizeVersionField(d)
	if c == "" || c == "none" {
		rootCmd.Version = v
		return
	}
	if d == "" || d == "unknown" {
		rootCmd.Version = fmt.Sprintf("%s (commit %s)", v, c)
		return
	}
	rootCmd.Version = fmt.Sprintf("%s (commit %s, built %s)", v, c, d)
}

// sanitizeVersionField trims surrounding whitespace, strips control characters
// (CR/LF/tab) that would smash a single-line --version output, and bounds
// the length so a malformed build-time inject of multi-KB junk can't blow up
// the help block. The return is intended for direct interpolation into the
// version string by SetVersionInfo.
func sanitizeVersionField(s string) string {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("\r", "", "\n", " ", "\t", " ").Replace(s)
	const maxLen = 64
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

func Execute() error {
	return rootCmd.Execute()
}

// ExecuteContext runs the root command with ctx wired through to every
// subcommand's RunE via cmd.Context(). main.go installs a signal-cancelled
// context here so Ctrl+C reaches into long-running analyze paths
// (image pulls, exports) instead of waiting for them to return.
func ExecuteContext(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}

func runInspect(cmd *cobra.Command, args []string) error {
	imageRef := args[0]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	noCache := noCacheRequested()

	if os.Getenv("CI") == "true" {
		if err := validateCLIThresholdFlags(ciCmd); err != nil {
			return err
		}
		// The CI report is already printed by executeCICheck; suppress
		// cobra's default error/usage output so an ErrCIFailed return
		// doesn't tack a redundant "Error: ..." line and usage block
		// onto the report.
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		// Forward the root cobra command's signal-cancellable context to
		// runCICheckInner so image.AnalyzeWithOptions sees Ctrl+C. We pass
		// the context explicitly rather than mutating ciCmd.SetContext —
		// ciCmd is package-level state, and a leaked context outlives the
		// invocation.
		analysis, ciErr := executeCICheck(cmd.Context(), imageRef, cfg, ciCmd, noCache)
		if flagJSON != "" {
			var jsonErr error
			switch {
			case analysis != nil:
				jsonErr = runJSONExportFromAnalysis(analysis, flagJSON)
			case ciErr != nil:
				jsonErr = nil
			default:
				jsonErr = runJSONExport(cmd.Context(), imageRef, flagJSON, noCache)
			}
			return combineCIAndJSONErr(ciErr, jsonErr, os.Stderr)
		}
		return ciErr
	}

	if flagJSON != "" {
		return runJSONExport(cmd.Context(), imageRef, flagJSON, noCache)
	}

	resolver, err := selectResolver(imageRef)
	if err != nil {
		return err
	}

	return tui.Run(tui.Config{
		ImageRef: imageRef,
		Resolver: resolver,
		NoCache:  noCache,
	})
}

// combineCIAndJSONErr decides which error wins when CI=true is paired with
// --json. The CI exit code wins on CI failure (a failing CI rule must stay
// the user-visible result), but a JSON write failure on a CI-pass run must
// not be silent — it becomes the returned error and is also surfaced as a
// warning on stderr regardless of which error wins.
func combineCIAndJSONErr(ciErr, jsonErr error, stderr io.Writer) error {
	if jsonErr != nil {
		fmt.Fprintf(stderr, "warning: JSON export failed: %v\n", jsonErr)
		if ciErr == nil {
			return jsonErr
		}
	}
	return ciErr
}
