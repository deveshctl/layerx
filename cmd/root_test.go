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
