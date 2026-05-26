package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/deveshctl/layerx/config"
	"github.com/deveshctl/layerx/image"
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
	Use:   "layerx [flags] IMAGE",
	Short: "Inspect Docker image layers",
	Long: `Inspect a Docker image's layers, filesystem changes, and wasted bytes.

By default launches an interactive TUI for browsing layers, viewing file
contents, and surfacing duplicated or removed files. Requires a running
Docker daemon. Use --json to skip the TUI and export the analysis, or run
"layerx ci" for non-interactive efficiency checks.

Cache:
  Analysis results are cached on disk under the user cache directory
  (keyed by image digest) so repeat runs skip re-parsing layer tarballs.
  Use --no-cache to bypass the cache for a single run; the run still
  refreshes the cache on success.

Environment:
  CI=true   When set, "layerx IMAGE" runs the ci subcommand instead of
            launching the TUI. Useful for pipelines that invoke layerx
            without a subcommand.`,
	Example: `  # Inspect an image interactively
  layerx nginx:latest

  # Force a fresh analysis, ignoring any cached result
  layerx --no-cache nginx:latest

  # Export analysis as JSON (skips the TUI)
  layerx --json out.json nginx:latest

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

func Execute() error {
	return rootCmd.Execute()
}

func runInspect(cmd *cobra.Command, args []string) error {
	imageRef := args[0]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	noCache := noCacheRequested()

	if os.Getenv("CI") == "true" {
		// The CI report is already printed by executeCICheck; suppress
		// cobra's default error/usage output so an ErrCIFailed return
		// doesn't tack a redundant "Error: ..." line and usage block
		// onto the report.
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		ciErr := executeCICheck(imageRef, cfg, ciCmd, noCache)
		if flagJSON != "" {
			jsonErr := runJSONExport(imageRef, flagJSON, noCache)
			return combineCIAndJSONErr(ciErr, jsonErr, os.Stderr)
		}
		return ciErr
	}

	if flagJSON != "" {
		return runJSONExport(imageRef, flagJSON, noCache)
	}

	resolver, err := image.NewDockerResolver()
	if err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
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
