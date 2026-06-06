package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/deveshctl/layerx/theme"
	"github.com/spf13/cobra"
)

var flagThemesJSON bool

var themesCmd = &cobra.Command{
	Use:   "themes",
	Short: "List available color themes",
	Long: `List the color themes layerx ships with.

The active theme is marked with "*". Resolution precedence is:
  --theme flag > $LAYERX_THEME > theme: in .layerx.yaml > default

Override per-invocation with --theme on any layerx command:
  layerx --theme nord nginx:latest

Use --json to emit a machine-readable array of theme names suitable
for shell scripts and CI pipelines.`,
	Example: `  # List with descriptions
  layerx themes

  # Machine-readable
  layerx themes --json

  # See which theme an env var would select
  LAYERX_THEME=latte layerx themes`,
	Args: cobra.NoArgs,
	RunE: runThemes,
}

func init() {
	themesCmd.Flags().BoolVar(&flagThemesJSON, "json", false,
		"emit theme names as a JSON array")
	rootCmd.AddCommand(themesCmd)
}

func runThemes(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true

	// Resolve the active theme for the "*" marker. Failures here MUST
	// NOT crash discovery — a typo in $LAYERX_THEME or .layerx.yaml is
	// exactly the kind of thing the user runs `layerx themes` to debug.
	// Fall back to "default" silently.
	cfg, _ := loadConfig(cmd)
	yamlTheme := ""
	if cfg != nil {
		yamlTheme = cfg.Theme
	}
	active := resolveThemeName(flagTheme, os.Getenv("LAYERX_THEME"), yamlTheme)
	if _, err := theme.Get(active); err != nil {
		active = "default"
	}

	if flagThemesJSON {
		names := make([]string, 0, len(theme.All()))
		for _, t := range theme.All() {
			names = append(names, string(t.Name))
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(names)
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	for _, t := range theme.All() {
		marker := " "
		if string(t.Name) == active {
			marker = "*"
		}
		fmt.Fprintf(w, "%s%s\t%s\n", t.Name, marker, t.Description)
	}
	return w.Flush()
}
