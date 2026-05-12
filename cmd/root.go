package cmd

import (
	"fmt"
	"io"

	"github.com/deveshpharswan/layerx/image"
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

	resolver, err := image.NewDockerResolver()
	if err != nil {
		return err
	}

	layers, err := resolver.Resolve(cmd.Context(), imageRef)
	if err != nil {
		return err
	}

	printLayerTable(cmd.OutOrStdout(), layers)
	return nil
}

func printLayerTable(w io.Writer, layers []image.Layer) {
	fmt.Fprintf(w, "%-4s %-14s %10s  %s\n", "#", "ID", "SIZE", "COMMAND")
	for _, l := range layers {
		cmd := truncateCommand(l.Command, 72)
		fmt.Fprintf(w, "%-4d %-14s %10s  %s\n", l.Index, l.ID, image.FormatBytes(l.Size), cmd)
	}
}

func truncateCommand(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
