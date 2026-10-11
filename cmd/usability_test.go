package cmd

import (
	"bytes"
	"errors"
	"os/exec"
	"testing"

	"github.com/deveshctl/layerx/image"
	"github.com/deveshctl/layerx/tui"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCIInvalidThresholdsPrintErrorBeforeAnalysis(t *testing.T) {
	previousLE, previousWB, previousWP := flagLowestEfficiency, flagHighestWastedBytes, flagHighestUserWastedPercent
	t.Cleanup(func() {
		flagLowestEfficiency, flagHighestWastedBytes, flagHighestUserWastedPercent = previousLE, previousWB, previousWP
	})
	previousResolver := selectResolver
	selectResolver = func(string) (image.Resolver, error) {
		t.Fatal("invalid thresholds must not contact an engine")
		return nil, nil
	}
	t.Cleanup(func() { selectResolver = previousResolver })

	for _, args := range [][]string{
		{"--lowest-efficiency", "2"},
		{"--highest-wasted-bytes", "-1"},
		{"--highest-user-wasted-percent", "NaN"},
	} {
		t.Run(args[0], func(t *testing.T) {
			cmd := &cobra.Command{Use: "ci IMAGE", RunE: runCICmd, SilenceErrors: true, SilenceUsage: true}
			cmd.Flags().Float64Var(&flagLowestEfficiency, "lowest-efficiency", -1, "")
			cmd.Flags().Int64Var(&flagHighestWastedBytes, "highest-wasted-bytes", -1, "")
			cmd.Flags().Float64Var(&flagHighestUserWastedPercent, "highest-user-wasted-percent", -1, "")
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs(append(args, "app:local"))
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, stderr.String(), args[0])
			assert.Equal(t, 1, bytes.Count(stderr.Bytes(), []byte("Error:")))
			assert.Empty(t, stdout.String())
		})
	}
}

func TestFlagParseErrorsPrintOnce(t *testing.T) {
	for _, silent := range []bool{false, true} {
		cmd := &cobra.Command{Use: "test", SilenceErrors: silent, Run: func(*cobra.Command, []string) {}}
		cmd.SetFlagErrorFunc(rootCmd.FlagErrorFunc())
		var stdout, stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"--unknown-option"})
		require.Error(t, cmd.Execute())
		assert.Equal(t, 1, bytes.Count(stderr.Bytes(), []byte("Error:")))
		assert.Contains(t, stderr.String(), "unknown-option")
		assert.Empty(t, stdout.String())
	}
}

func TestCacheRejectsUnknownCommand(t *testing.T) {
	err := cacheCmd.Args(cacheCmd, []string{"invalid"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "use list or prune")
	assert.NoError(t, cacheCmd.Args(cacheCmd, nil))
}

func TestBuildOperationalErrorsPrintOnce(t *testing.T) {
	for _, stage := range []string{"engine-start", "resolver", "viewer"} {
		t.Run(stage, func(t *testing.T) {
			prepareBuildRoutingTest(t)
			withFakeResolver(t, okResolver(passingLayers()...))
			failure := errors.New("inspection unavailable")
			runBuildCommand = func(*exec.Cmd) error { return nil }
			runBuildTUI = func(tui.Config) error { return nil }
			switch stage {
			case "engine-start":
				runBuildCommand = func(*exec.Cmd) error { return failure }
			case "resolver":
				selectResolver = func(string) (image.Resolver, error) { return nil, failure }
			case "viewer":
				runBuildTUI = func(tui.Config) error { return failure }
			}
			var stderr bytes.Buffer
			rootCmd.SetErr(&stderr)
			rootCmd.SetArgs([]string{"build", "--engine", "docker", "-t", "app:local", "."})
			require.ErrorIs(t, rootCmd.Execute(), failure)
			assert.Contains(t, stderr.String(), "inspection unavailable")
			assert.Equal(t, 1, bytes.Count(stderr.Bytes(), []byte("Error:")))
		})
	}
}
