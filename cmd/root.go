package cmd

import (
	"fmt"
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
	rootCmd.Flags().StringVar(&flagJSON, "json", "", "write analysis to PATH as JSON and exit (skips TUI)")
	rootCmd.PersistentFlags().BoolVar(&flagNoCacheFl, "no-cache", false, "bypass the analysis cache for this run; the run still writes the cache on success")
	rootCmd.PersistentFlags().BoolVar(&flagRefresh, "refresh", false, "alias for --no-cache")
	_ = rootCmd.PersistentFlags().MarkHidden("refresh")
}

func SetVersionInfo(v, c, d string) {
	rootCmd.Version = v
}

func Execute() error {
	return rootCmd.Execute()
}

func runInspect(cmd *cobra.Command, args []string) error {
	imageRef := args[0]

	if flagJSON != "" {
		return runJSONExport(imageRef, flagJSON, noCacheRequested())
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if os.Getenv("CI") == "true" {
		return executeCICheck(imageRef, cfg, ciCmd, noCacheRequested())
	}

	resolver, err := image.NewDockerResolver()
	if err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}

	return tui.Run(tui.Config{
		ImageRef: imageRef,
		Resolver: resolver,
		NoCache:  noCacheRequested(),
	})
}
