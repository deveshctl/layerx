package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:     "version",
	Short:   "Print the layerx version",
	Long:    "Print the layerx version. Identical output to --version.",
	Example: "  layerx version",
	Args:    cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "%s version %s\n", rootCmd.Name(), rootCmd.Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
