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
	flagJSON    string
	flagNoCache bool
)

var rootCmd = &cobra.Command{
	Use:   "layerx [image]",
	Short: "Inspect Docker image layers",
	Long: `A terminal-based Docker image layer inspector.

Launches an interactive TUI by default to browse layers, explore filesystem
changes, view file contents, and surface wasted bytes. Requires a running
Docker daemon. Use --json to skip the TUI and export analysis to a file,
or run "layerx ci" for non-interactive CI checks.`,
	Example: `  # Inspect an image interactively
  layerx nginx:latest

  # Export analysis as JSON (skips the TUI)
  layerx --json out.json nginx:latest

  # Run efficiency checks (also triggered by CI=true)
  layerx ci nginx:latest`,
	Args: cobra.ExactArgs(1),
	RunE: runInspect,
}

func init() {
	rootCmd.Flags().StringVar(&flagJSON, "json", "", "write analysis to PATH as JSON and exit (skips TUI)")
	rootCmd.PersistentFlags().BoolVar(&flagNoCache, "no-cache", false, "bypass the analysis cache for this run; the run still writes the cache on success")
	rootCmd.PersistentFlags().BoolVar(&flagNoCache, "refresh", false, "alias for --no-cache")
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
		return runJSONExport(imageRef, flagJSON, flagNoCache)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if os.Getenv("CI") == "true" {
		return executeCICheck(imageRef, cfg, ciCmd, flagNoCache)
	}

	resolver, err := image.NewDockerResolver()
	if err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}

	return tui.Run(tui.Config{
		ImageRef: imageRef,
		Resolver: resolver,
		NoCache:  flagNoCache,
	})
}
