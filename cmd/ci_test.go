package cmd

import (
	"testing"

	"github.com/deveshctl/layerx/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestBuildRules_DisablesLowestEfficiencyOnZeroOrNegative(t *testing.T) {
	cfg := &config.Config{Rules: config.RulesConfig{
		LowestEfficiency:         0,
		HighestWastedBytes:       0,
		HighestUserWastedPercent: 0,
	}}
	// Fresh cobra command so cmd.Flags().Changed() returns false for all flags
	// and the cfg values flow through to buildRules.
	cmd := &cobra.Command{}
	var le, huwp float64
	var hwb int64
	cmd.Flags().Float64Var(&le, "lowest-efficiency", 0, "")
	cmd.Flags().Int64Var(&hwb, "highest-wasted-bytes", 0, "")
	cmd.Flags().Float64Var(&huwp, "highest-user-wasted-percent", 0, "")

	rules := buildRules(cfg, cmd)
	assert.Empty(t, rules, "all-zero thresholds must disable all rules")
}
