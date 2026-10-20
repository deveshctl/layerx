package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/deveshctl/layerx/ci"
	"github.com/deveshctl/layerx/config"
	"github.com/deveshctl/layerx/image"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigurationThresholdPrecedence(t *testing.T) {
	oldLE, oldHWB, oldHUWP := flagLowestEfficiency, flagHighestWastedBytes, flagHighestUserWastedPercent
	t.Cleanup(func() {
		flagLowestEfficiency, flagHighestWastedBytes, flagHighestUserWastedPercent = oldLE, oldHWB, oldHUWP
	})
	configured := "rules:\n  lowest-efficiency: 0.95\n  highest-wasted-bytes: 5\n  highest-user-wasted-percent: 0.05\n"
	zeros := []string{"--lowest-efficiency=0", "--highest-wasted-bytes=0", "--highest-user-wasted-percent=0"}
	for _, tc := range []struct {
		name       string
		yaml       string
		args       []string
		thresholds map[string]string
		passed     bool
	}{
		{"omitted defaults", "version: 1\n", nil, map[string]string{"efficiency": "90.0%", "wasted %": "10.0%"}, true},
		{"empty rules retain defaults", "rules: {}\n", nil, map[string]string{"efficiency": "90.0%", "wasted %": "10.0%"}, true},
		{"configured thresholds", configured, nil, map[string]string{"efficiency": "95.0%", "wasted bytes": "5 B", "wasted %": "5.0%"}, false},
		{"CLI overrides config", configured, []string{"--lowest-efficiency=0.9", "--highest-wasted-bytes=10", "--highest-user-wasted-percent=0.1"}, map[string]string{"efficiency": "90.0%", "wasted bytes": "10 B", "wasted %": "10.0%"}, true},
		{"CLI zero disables only selected rule", configured, []string{"--lowest-efficiency=0"}, map[string]string{"wasted bytes": "5 B", "wasted %": "5.0%"}, false},
		{"YAML zero disables only selected rule", "rules:\n  lowest-efficiency: 0\n", nil, map[string]string{"wasted %": "10.0%"}, true},
		{"YAML disables all global rules", "rules:\n  lowest-efficiency: 0\n  highest-wasted-bytes: 0\n  highest-user-wasted-percent: 0\n", nil, nil, false},
		{"CLI disables all global rules", configured, zeros, nil, false},
		{"zero global flags retain path policy", configured + "path-rules:\n  block: [\"/tmp/**\"]\n", zeros, map[string]string{"block": "/tmp/**"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.yaml), 0o600))
			cfg, err := config.LoadFrom(path)
			require.NoError(t, err)
			cmd := &cobra.Command{}
			cmd.Flags().Float64Var(&flagLowestEfficiency, "lowest-efficiency", -1, "")
			cmd.Flags().Int64Var(&flagHighestWastedBytes, "highest-wasted-bytes", -1, "")
			cmd.Flags().Float64Var(&flagHighestUserWastedPercent, "highest-user-wasted-percent", -1, "")
			require.NoError(t, cmd.ParseFlags(tc.args))
			require.NoError(t, validateCLIThresholdFlags(cmd))
			if len(tc.thresholds) == 0 {
				analysis, err := runCICheckInner(t.Context(), "unused", cfg, cmd, false, false, nil)
				require.ErrorContains(t, err, "no CI rules enabled")
				assert.Nil(t, analysis)
				return
			}
			report := ci.Evaluate(ci.EvalContext{
				Efficiency: &image.EfficiencyResult{Score: 0.92, WastedBytes: 8},
				Layers:     []image.Layer{fakeAnalysisLayer(0, "test", []string{"/tmp/secret"})},
			}, buildRules(cfg, cmd))
			thresholds := make(map[string]string)
			for _, result := range report.Results {
				thresholds[result.Name] = result.Threshold
			}
			assert.Equal(t, tc.thresholds, thresholds)
			assert.Equal(t, tc.passed, report.Passed)
		})
	}
}

func TestConfiguredPathRulesUseWastedPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`rules:
  lowest-efficiency: 0
  highest-wasted-bytes: 0
  highest-user-wasted-percent: 0
path-rules:
  deny-waste: ["/x"]
  max-layer-count: 2
`), 0o600))
	cfg, err := config.LoadFrom(path)
	require.NoError(t, err)
	for _, tc := range []struct {
		name   string
		sizes  []int64
		passed map[string]bool
	}{
		{"live carryover", []int64{100, 0, 0, 0}, map[string]bool{"deny-waste": true, "max-layer-count": true}},
		{"written once then deleted", []int64{100, -1}, map[string]bool{"deny-waste": false, "max-layer-count": true}},
		{"three payload writes", []int64{100, 200, 300}, map[string]bool{"deny-waste": false, "max-layer-count": false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &image.Analysis{}
			for _, size := range tc.sizes {
				tree := image.NewFileTree()
				if size > 0 {
					tree.Root.AddChild(&image.FileNode{Name: "x", Path: "/x", Size: size})
				} else if size < 0 {
					tree.Root.AddChild(&image.FileNode{Name: ".wh.x", Path: "/.wh.x"})
				}
				a.Layers = append(a.Layers, image.Layer{Tree: tree})
			}
			report := ci.Evaluate(ci.EvalContext{Efficiency: image.EfficiencyFromAnalysis(a)}, buildRules(cfg, &cobra.Command{}))
			passed := make(map[string]bool)
			for _, result := range report.Results {
				passed[result.Name] = result.Passed
			}
			assert.Equal(t, tc.passed, passed)
		})
	}
}
