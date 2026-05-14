package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/deveshpharswan/layerx/ci"
	"github.com/deveshpharswan/layerx/config"
	"github.com/deveshpharswan/layerx/image"
	"github.com/spf13/cobra"
)

var (
	flagLowestEfficiency         float64
	flagHighestWastedBytes       int64
	flagHighestUserWastedPercent float64
)

var ciCmd = &cobra.Command{
	Use:   "ci [image]",
	Short: "Run efficiency checks for CI pipelines",
	Long:  "Evaluates image efficiency against configurable thresholds. Exits 0 on pass, 1 on failure.",
	Args:  cobra.ExactArgs(1),
	RunE:  runCICmd,
}

func init() {
	ciCmd.Flags().Float64Var(&flagLowestEfficiency, "lowest-efficiency", -1, "minimum efficiency score (0.0-1.0)")
	ciCmd.Flags().Int64Var(&flagHighestWastedBytes, "highest-wasted-bytes", -1, "maximum allowed wasted bytes")
	ciCmd.Flags().Float64Var(&flagHighestUserWastedPercent, "highest-user-wasted-percent", -1, "maximum wasted bytes as fraction of total size (0.0-1.0)")

	rootCmd.AddCommand(ciCmd)
}

func runCICmd(cmd *cobra.Command, args []string) error {
	imageRef := args[0]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	return executeCICheck(imageRef, cfg, cmd)
}

func executeCICheck(imageRef string, cfg *config.Config, cmd *cobra.Command) error {
	resolver, err := image.NewDockerResolver()
	if err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}

	analysis, err := image.Analyze(context.Background(), resolver, imageRef)
	if err != nil {
		return err
	}

	efficiency := image.Efficiency(analysis.Layers)
	rules := buildRules(cfg, cmd)
	report := ci.Evaluate(efficiency, analysis.TotalSize, rules)
	report.Print(os.Stdout)

	if report.ExitCode() != 0 {
		os.Exit(1)
	}
	return nil
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

	rules := []ci.Rule{
		ci.LowestEfficiency{Threshold: le},
	}
	if hwb > 0 {
		rules = append(rules, ci.HighestWastedBytes{Threshold: hwb})
	}
	rules = append(rules, ci.HighestUserWastedPercent{Threshold: huwp})

	return rules
}
