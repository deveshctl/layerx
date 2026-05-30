package cmd

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetVersionInfo_FullInfo(t *testing.T) {
	t.Cleanup(func() { rootCmd.Version = "" })
	SetVersionInfo("0.13.0", "abc1234", "2026-05-25")
	assert.Equal(t, "0.13.0 (commit abc1234, built 2026-05-25)", rootCmd.Version)
}

func TestSetVersionInfo_DefaultsHidden(t *testing.T) {
	t.Cleanup(func() { rootCmd.Version = "" })
	// main.go's defaults: commit="none", date="unknown" when built without
	// -ldflags. SetVersionInfo must not splice "(commit none, built unknown)"
	// into the user-visible version string.
	SetVersionInfo("dev", "none", "unknown")
	assert.Equal(t, "dev", rootCmd.Version)
}

func TestSetVersionInfo_EmptyCommitHidden(t *testing.T) {
	t.Cleanup(func() { rootCmd.Version = "" })
	SetVersionInfo("0.1.0", "", "2026-05-25")
	assert.Equal(t, "0.1.0", rootCmd.Version)
}

func TestSetVersionInfo_UnknownDateOmitsBuiltSuffix(t *testing.T) {
	// A common dev-CI build pattern injects only commit:
	//   go build -ldflags "-X main.commit=$(git rev-parse HEAD)"
	// leaving date at main.go's "unknown" default. The version string
	// must omit the "built unknown" garbage.
	t.Cleanup(func() { rootCmd.Version = "" })
	SetVersionInfo("v1.0.0", "abc1234", "unknown")
	assert.Equal(t, "v1.0.0 (commit abc1234)", rootCmd.Version)
}

func TestSetVersionInfo_EmptyDateOmitsBuiltSuffix(t *testing.T) {
	t.Cleanup(func() { rootCmd.Version = "" })
	SetVersionInfo("v1.0.0", "abc1234", "")
	assert.Equal(t, "v1.0.0 (commit abc1234)", rootCmd.Version)
}

func TestCombineCIAndJSONErr_BothSucceed(t *testing.T) {
	var buf bytes.Buffer
	err := combineCIAndJSONErr(nil, nil, &buf)
	assert.NoError(t, err)
	assert.Empty(t, buf.String())
}

func TestCombineCIAndJSONErr_JSONFailsCIPasses_JSONErrWins(t *testing.T) {
	var buf bytes.Buffer
	jsonErr := errors.New("permission denied")
	err := combineCIAndJSONErr(nil, jsonErr, &buf)
	assert.ErrorIs(t, err, jsonErr)
	assert.Contains(t, buf.String(), "warning: JSON export failed: permission denied")
}

func TestCombineCIAndJSONErr_CIFailsJSONPasses_CIErrWins(t *testing.T) {
	var buf bytes.Buffer
	ciErr := errors.New("ci failed")
	err := combineCIAndJSONErr(ciErr, nil, &buf)
	assert.ErrorIs(t, err, ciErr)
	assert.Empty(t, buf.String())
}

func TestCombineCIAndJSONErr_BothFail_CIWinsButJSONWarned(t *testing.T) {
	// CI exit code must be the user-visible result when both fail, but the
	// JSON failure can't be silent — surface it on stderr.
	var buf bytes.Buffer
	ciErr := errors.New("ci failed")
	jsonErr := errors.New("disk full")
	err := combineCIAndJSONErr(ciErr, jsonErr, &buf)
	assert.ErrorIs(t, err, ciErr)
	assert.Contains(t, buf.String(), "warning: JSON export failed: disk full")
}

// ExecuteContext must reach RunE through cmd.Context(); a cancelled context
// passed in must arrive cancelled at the subcommand. main.go relies on this so
// Ctrl+C cancels image pulls/exports inside ci/json/compare.
func TestExecuteContext_PropagatesToRunE(t *testing.T) {
	root := &cobra.Command{Use: "test-root"}
	var observed context.Context
	child := &cobra.Command{
		Use: "child",
		RunE: func(c *cobra.Command, args []string) error {
			observed = c.Context()
			return nil
		},
	}
	root.AddCommand(child)
	root.SetArgs([]string{"child"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, root.ExecuteContext(ctx))

	require.NotNil(t, observed)
	select {
	case <-observed.Done():
		// expected — cancelled context arrived intact
	case <-time.After(100 * time.Millisecond):
		t.Fatal("RunE's cmd.Context() was not cancelled")
	}
}

// rootCmd must silence cobra's default usage block on RunE errors. Without
// this, every config-parse error, "image not found" error, or daemon-down
// error gets a 60-line help dump tacked on after the one-line message —
// burying the actual problem.
//
// This is the structural guarantee. The behavioral counterpart below
// (TestRootCmd_BadConfig_NoUsageDump) exercises the same contract end-to-end
// through cobra so a future change that swaps mechanisms (e.g. custom error
// handler) can't quietly regress the user-visible behavior.
func TestRootCmd_SilenceUsageIsSet(t *testing.T) {
	assert.True(t, rootCmd.SilenceUsage,
		"rootCmd.SilenceUsage must be true so config/daemon errors are not buried under the usage block")
}

// End-to-end: invoke rootCmd with a malformed .layerx.yaml in cwd. The error
// chain runs config.Load → returns parse error → cobra prints "Error: ..."
// to stderr. Without SilenceUsage the full usage block follows, drowning the
// error.
//
// This test never reaches the Docker daemon: config.Load fails at line 137 of
// runInspect, well before resolver selection at line 179. Safe to run in CI.
func TestRootCmd_BadConfig_NoUsageDump(t *testing.T) {
	// writeConfig (defined in ci_test.go) chdirs to a temp dir and drops a
	// .layerx.yaml so config.Load() picks it up.
	writeConfig(t, "rules:\n  lowest-efficiency: not-a-number\n")

	// Capture cobra's writer streams. Cobra prints both errors and the
	// usage block through cmd.ErrOrStderr().
	var stderr bytes.Buffer
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"fake:latest"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	err := rootCmd.Execute()
	require.Error(t, err, "bad config must produce an error")
	assert.Contains(t, err.Error(), "loading config",
		"error chain must identify the config-load step")

	out := stderr.String()
	assert.Contains(t, out, "Error:",
		"cobra must still print the one-line error so the user sees what failed")
	assert.NotContains(t, out, "Usage:",
		"usage block must be silenced — the error is the message, not a help nudge")
	assert.NotContains(t, out, "Inspect a container image",
		"the Long description must not be dumped to stderr on a config error")
}
