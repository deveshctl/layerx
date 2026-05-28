package cmd

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/deveshctl/layerx/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// main.go maps a non-nil cobra error to exit 1 only when the error chain
// contains *ErrCIFailed; everything else exits 2. Lock that contract here so
// a future change that wraps or replaces the sentinel doesn't silently
// collapse both exit codes to 2.
func TestErrCIFailed_DetectableThroughWrapping(t *testing.T) {
	sentinel := &ErrCIFailed{}
	wrapped := fmt.Errorf("running ci check: %w", sentinel)

	var got *ErrCIFailed
	assert.True(t, errors.As(sentinel, &got), "bare sentinel must match")
	assert.True(t, errors.As(wrapped, &got), "wrapped sentinel must match")
	assert.False(t, errors.As(errors.New("unrelated"), &got), "plain errors must not match")
	assert.False(t, errors.As(fmt.Errorf("docker daemon down"), &got), "internal errors must not match")
}

// runCICheckInner must reject a config that disables every rule rather than
// silently exit 0 on a bloated image. Without the guard the rules slice is
// empty, ci.Evaluate's Passed defaults to true, and CI greenlights anything.
func TestRunCICheckInner_RejectsAllRulesDisabled(t *testing.T) {
	cfg := &config.Config{Rules: config.RulesConfig{
		LowestEfficiency:         0,
		HighestWastedBytes:       0,
		HighestUserWastedPercent: 0,
	}}
	cmd := &cobra.Command{}
	var le, huwp float64
	var hwb int64
	cmd.Flags().Float64Var(&le, "lowest-efficiency", 0, "")
	cmd.Flags().Int64Var(&hwb, "highest-wasted-bytes", 0, "")
	cmd.Flags().Float64Var(&huwp, "highest-user-wasted-percent", 0, "")

	analysis, err := runCICheckInner(context.Background(), "nginx:latest", cfg, cmd, false, false)
	require.Error(t, err)
	assert.Nil(t, analysis)
	assert.Contains(t, err.Error(), "no CI rules enabled")
	assert.Contains(t, err.Error(), "--lowest-efficiency", "direct `layerx ci` invocation must surface flag names")
	var ciFailed *ErrCIFailed
	assert.False(t, errors.As(err, &ciFailed), "config error must not look like a rule failure")
}

// errNoCIRulesEnabled tailors the message based on how the user reached the
// CI path: the `CI=true layerx IMG` shortcut runs through rootCmd, so naming
// threshold flags is misleading (they belong to the ci subcommand). Lock the
// branch so a future refactor doesn't collapse both messages.
func TestErrNoCIRulesEnabled_MessagesDifferByPath(t *testing.T) {
	direct := errNoCIRulesEnabled(false).Error()
	viaEnv := errNoCIRulesEnabled(true).Error()

	assert.Contains(t, direct, "--lowest-efficiency")
	assert.Contains(t, viaEnv, "layerx ci`")
	assert.NotEqual(t, direct, viaEnv, "messages must differ so the user gets path-appropriate guidance")
}

// --json must live on rootCmd's persistent flag set so it is inherited by
// the ci subcommand (`layerx ci --json out.json IMG`). Earlier it was a
// local flag on rootCmd which made the ci subcommand reject --json with an
// "unknown flag" error.
func TestJSONFlagIsPersistent(t *testing.T) {
	persistent := rootCmd.PersistentFlags().Lookup("json")
	assert.NotNil(t, persistent, "--json must be on rootCmd.PersistentFlags so subcommands inherit it")

	// And the ci subcommand must see it through inherited flags.
	inherited := ciCmd.InheritedFlags().Lookup("json")
	assert.NotNil(t, inherited, "ci subcommand must inherit --json from rootCmd")
}
