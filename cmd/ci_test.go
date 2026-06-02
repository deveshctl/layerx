package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deveshctl/layerx/ci"
	"github.com/deveshctl/layerx/config"
	"github.com/deveshctl/layerx/image"
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

	analysis, err := runCICheckInner(context.Background(), "nginx:latest", cfg, cmd, false, false, nil)
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

// writeConfig writes a .layerx.yaml into a temp dir and chdirs there so
// config.Load() finds it. Returns a cleanup func; t.Cleanup handles it.
func writeConfig(t *testing.T, content string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".layerx.yaml"), []byte(content), 0644))
	prev, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

// fakeAnalysisLayer builds a Layer with a per-layer tree containing the
// listed Added paths. Used for synthetic CI runs that don't need Docker.
func fakeAnalysisLayer(index int, id string, addedPaths []string) image.Layer {
	tree := image.NewFileTree()
	for _, p := range addedPaths {
		tree.Root.AddChild(&image.FileNode{Name: p, Path: p, DiffType: image.Added})
	}
	return image.Layer{Index: index, ID: id, Tree: tree}
}

// TestCI_FlatPathRules_Block_Fails: synthetic analysis + flat block: config
// → exit 1, report contains the matched path and layer index.
//
// Drives buildRules directly (no Docker), asserts on the printed report.
func TestCI_FlatPathRules_Block_Fails(t *testing.T) {
	writeConfig(t, `version: 1
rules:
  lowest-efficiency: 0.9
path-rules:
  block:
    - /tmp/**
`)
	cfg, err := config.Load()
	require.NoError(t, err)

	rules := buildRules(cfg, ciCmd)
	require.NotEmpty(t, rules)

	layer := fakeAnalysisLayer(0, "abc123", []string{"/tmp/secret"})
	eff := &image.EfficiencyResult{Score: 0.95, WastedBytes: 0}
	report := ci.Evaluate(ci.EvalContext{
		Efficiency: eff,
		TotalSize:  10000,
		Layers:     []image.Layer{layer},
	}, rules)
	assert.Equal(t, 1, report.ExitCode())

	var buf bytes.Buffer
	report.Print(&buf)
	out := buf.String()
	assert.Contains(t, out, "/tmp/secret")
	assert.Contains(t, out, "layer 0")
	assert.Contains(t, out, "Path Rules:")
}

// TestCI_ListPathRules_DenyWaste_Fails: list-form deny-waste config →
// exit 1, report contains the user-supplied rule id.
func TestCI_ListPathRules_DenyWaste_Fails(t *testing.T) {
	writeConfig(t, `version: 1
rules:
  lowest-efficiency: 0.9
path-rules:
  - id: no-pyc-waste
    type: deny-waste
    paths: ["**/*.pyc"]
`)
	cfg, err := config.Load()
	require.NoError(t, err)
	rules := buildRules(cfg, ciCmd)

	eff := &image.EfficiencyResult{
		Score: 0.95,
		WastedFiles: []image.WastedFile{
			{Path: "/usr/lib/python/x.pyc", TotalWasted: 100, LayerCount: 2},
		},
	}
	report := ci.Evaluate(ci.EvalContext{Efficiency: eff, TotalSize: 10000}, rules)
	assert.Equal(t, 1, report.ExitCode())

	// The rule id must appear in at least one result's RuleID.
	hasID := false
	for _, r := range report.Results {
		if strings.Contains(r.RuleID, "no-pyc-waste") {
			hasID = true
		}
	}
	assert.True(t, hasID, "result RuleID must include the user-supplied id 'no-pyc-waste'")
}

// TestCI_NoPathRules_BackwardCompat: pre-existing rules:-only config behaves
// identically to today (regression check).
func TestCI_NoPathRules_BackwardCompat(t *testing.T) {
	writeConfig(t, `rules:
  lowest-efficiency: 0.9
`)
	cfg, err := config.Load()
	require.NoError(t, err)
	rules := buildRules(cfg, ciCmd)

	eff := &image.EfficiencyResult{Score: 0.95, WastedBytes: 0}
	report := ci.Evaluate(ci.EvalContext{Efficiency: eff, TotalSize: 10000}, rules)
	assert.Equal(t, 0, report.ExitCode(), "good image with no path rules must pass")
	assert.Empty(t, cfg.PathRules)
}

// TestCI_PathRulesGroupedInReport: output contains both section headers
// in the documented order. Echoes the ci/evaluator_test.go grouping test
// but exercises it through the cmd-level pipeline.
func TestCI_PathRulesGroupedInReport(t *testing.T) {
	writeConfig(t, `version: 1
rules:
  lowest-efficiency: 0.99
path-rules:
  block:
    - /tmp/**
`)
	cfg, err := config.Load()
	require.NoError(t, err)
	rules := buildRules(cfg, ciCmd)

	layer := fakeAnalysisLayer(0, "abc", []string{"/tmp/x"})
	eff := &image.EfficiencyResult{Score: 0.50}
	report := ci.Evaluate(ci.EvalContext{Efficiency: eff, TotalSize: 10000, Layers: []image.Layer{layer}}, rules)

	var buf bytes.Buffer
	report.Print(&buf)
	out := buf.String()
	gIdx := strings.Index(out, "Global Rules:")
	pIdx := strings.Index(out, "Path Rules:")
	require.Greater(t, gIdx, -1, "Global Rules: header must be present")
	require.Greater(t, pIdx, gIdx, "Path Rules: header must follow Global Rules:")
}

// Bare `layerx ci` (no image arg) and `layerx ci a b` (too many args) must
// print a synopsis + Usage line + at least one example to stderr — not
// cobra's terse "accepts 1 arg(s)" alone. ciCmd has SilenceErrors+SilenceUsage
// set, so without ciArgs the user would see no actionable help. Mirrors the
// rootCmd no-args contract pinned in TestRootCmd_NoArgs_ShowsUsage and the
// compareCmd one in cmd/compare_test.go.
func TestCICmd_NoArgs_ShowsUsage(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		wantSynopsis string // first line on stderr
	}{
		{
			name:        "zero_args",
			args:        []string{"ci"},
			wantSynopsis: "layerx ci: run efficiency checks",
		},
		{
			name:        "too_many_args",
			args:        []string{"ci", "a", "b"},
			wantSynopsis: "needs exactly 1 image argument, got 2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			rootCmd.SetOut(&stdout)
			rootCmd.SetErr(&stderr)
			rootCmd.SetArgs(tc.args)
			t.Cleanup(func() {
				rootCmd.SetOut(nil)
				rootCmd.SetErr(nil)
				rootCmd.SetArgs(nil)
			})

			err := rootCmd.Execute()
			require.Error(t, err, "wrong arg count must produce an error")

			var ciUsage *ErrCIUsage
			require.ErrorAs(t, err, &ciUsage,
				"the returned error must carry the ErrCIUsage sentinel so main.go exits 2 cleanly")

			errOut := stderr.String()
			assert.Contains(t, errOut, tc.wantSynopsis,
				"the user must see a one-line synopsis explaining what went wrong")
			assert.Contains(t, errOut, "Usage:",
				"a Usage: header must accompany the hint")
			assert.Contains(t, errOut, "layerx ci [flags] IMAGE_OR_ARCHIVE",
				"the synopsis line must reflect ciCmd.Use")
			assert.Contains(t, errOut, "layerx ci nginx:latest",
				"at least one concrete example must reach the user")
			// ciCmd has SilenceErrors=true, so cobra's own "Error: ..."
			// prefix must NOT appear — ciArgs is the only output source.
			assert.NotContains(t, errOut, "Error: usage",
				"the ErrCIUsage sentinel body must not be printed verbatim")
		})
	}
}

// TestRunCICheckInner_ContextCancelled is a placeholder. The production
// path uses selectResolver, which is keyed on the imageRef, so injecting
// a context-blocking fake resolver requires either a new test seam in
// cmd/ or building the test against a real resolver. Neither is in
// scope for this change. The cancellation contract for the analyze
// pipeline itself is pinned by image/docker_test.go's
// TestParseLayers_HonoursContextCancel.
func TestRunCICheckInner_ContextCancelled(t *testing.T) {
	t.Skip("resolver-injection seam not yet present in cmd/")
}
