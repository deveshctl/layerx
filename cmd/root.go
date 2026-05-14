package cmd

import (
	"fmt"
	"os"

	"github.com/deveshpharswan/layerx/config"
	"github.com/deveshpharswan/layerx/image"
	"github.com/deveshpharswan/layerx/tui"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "layerx [image]",
	Short: "Inspect Docker image layers",
	Long:  "A terminal-based Docker image layer inspector.",
	Args:  cobra.ExactArgs(1),
	RunE:  runInspect,
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

	if os.Getenv("CI") == "true" {
		return executeCICheck(imageRef, cfg, ciCmd)
	}

	resolver, err := image.NewDockerResolver()
	if err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}

	return tui.Run(tui.Config{
		ImageRef:    imageRef,
		Resolver:    resolver,
		Keybindings: cfg.Keybindings,
	})
}
