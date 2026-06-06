package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

func resetRootCmdFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		rootCmd.SilenceUsage = false
		rootCmd.SilenceErrors = false
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
}

// resetPersistentFlags snapshots and restores the package-level flag vars
// rootCmd.PersistentFlags writes into. Pair with resetRootCmdFlags.
func resetPersistentFlags(t *testing.T) {
	t.Helper()
	prevJSON := flagJSON
	prevNoCache := flagNoCacheFl
	t.Cleanup(func() {
		flagJSON = prevJSON
		flagNoCacheFl = prevNoCache
	})
}

// Stream split (from cobra/command.go ~line 1160-1170):
//   - The "Error: ..." line is written via PrintErrln → ErrOrStderr (stderr)
//   - The Usage block is written via Println          → OutOrStderr (stdout)
//
// In a real terminal both default to os.Stderr so the user sees them
// together, but in a test the streams must be captured separately or one
// half of the output goes missing.
//
// Pinning both halves of the contract:
//   - TestRootCmd_NoArgs_ShowsUsage              — bare `layerx` → usage visible
//   - TestRootCmd_BadConfig_NoUsageDump (below)  — RunE error    → usage silenced
func TestRootCmd_NoArgs_ShowsUsage(t *testing.T) {
	resetRootCmdFlags(t)

	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{})

	err := rootCmd.Execute()
	require.Error(t, err, "missing image argument must produce an error")

	errOut := stderr.String()
	stdOut := stdout.String()
	assert.Contains(t, errOut, "Error:",
		"cobra must print the one-line error to stderr so the user sees what failed")
	assert.Contains(t, errOut, "accepts 1 arg",
		"the arg-count error must reach the user")
	// Cobra writes the Usage block via cmd.Println → OutOrStdout. Pin the
	// stream split strictly so a regression that re-routes Usage to stderr
	// (or drops it entirely) cannot pass under a merged-buffer check.
	assert.Contains(t, stdOut, "Usage:",
		"usage block must accompany an arg-validation error — that IS the help the user needs")
	assert.Contains(t, stdOut, "layerx [flags] IMAGE_OR_ARCHIVE",
		"the usage line must include the rootCmd Use string")
}

// End-to-end: invoke rootCmd with a malformed .layerx.yaml in cwd. loadConfig
// prints the error and a rules-section hint; cobra must not dump the full
// root usage block.
//
// This test never reaches the Docker daemon: config load fails well before
// resolver selection. Safe to run in CI.
func TestRootCmd_BadConfig_NoUsageDump(t *testing.T) {
	// rules: null is rejected deterministically by the pre-decode AST walk
	// in config.LoadFrom (see rejectNullSections). Use it here rather than
	// "not-a-number" scalars — goccy's coercion of those varies and can
	// skip the config-load path this test is meant to exercise.
	writeConfig(t, "rules: null\n")
	resetRootCmdFlags(t)

	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"fake:latest"})

	err := rootCmd.Execute()
	require.Error(t, err, "bad config must produce an error")
	assert.Contains(t, err.Error(), "loading config",
		"error chain must identify the config-load step")

	errOut := stderr.String()
	assert.Contains(t, errOut, "Error:",
		"the user must see what failed")
	assert.Contains(t, errOut, "must be a mapping, not null",
		"rules:null must be rejected with a clear message")
	assert.Contains(t, errOut, "rules — global CI efficiency thresholds",
		"section-specific hint must accompany a rules error")
	assert.NotContains(t, errOut, "Inspect a container image",
		"the root Long description must not be dumped on a config error")

	stdOut := stdout.String()
	assert.NotContains(t, stdOut, "Usage:",
		"usage block must be silenced on RunE errors — the error is the message, not a help nudge")
	assert.NotContains(t, errOut, "Usage:",
		"usage block must not appear on stderr either")
}

func TestRootCmd_UnknownKey_ShowsGeneralHint(t *testing.T) {
	writeConfig(t, "ruels:\n  lowest-efficiency: 0.9\n")
	resetRootCmdFlags(t)

	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"fake:latest"})

	err := rootCmd.Execute()
	require.Error(t, err)

	errOut := stderr.String()
	assert.Contains(t, errOut, "Error:")
	assert.Contains(t, errOut, "docs/configuration.md",
		"unknown-section config errors must still print the general hint")
	assert.NotContains(t, stdout.String(), "Usage:")
	assert.NotContains(t, errOut, "Usage:")
}

// End-to-end coverage for path-rules section hints, mirroring
// TestRootCmd_BadConfig_NoUsageDump but routed through the path-rules
// validation path so SectionPathRules is the tag carried on the LoadError.
// An invalid glob is the cheapest trigger that survives the strict YAML
// decode and reaches normalizePathRules' validateGlobs call.
func TestRootCmd_BadConfig_PathRules_NoUsageDump(t *testing.T) {
	writeConfig(t, "path-rules:\n  block:\n    - \"[invalid\"\n")
	resetRootCmdFlags(t)

	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"fake:latest"})

	err := rootCmd.Execute()
	require.Error(t, err, "bad path-rules config must produce an error")
	assert.Contains(t, err.Error(), "loading config",
		"error chain must identify the config-load step")

	errOut := stderr.String()
	assert.Contains(t, errOut, "Error:",
		"the user must see what failed")
	assert.Contains(t, errOut, "invalid glob",
		"the path-rules-specific failure cause must reach the user")
	assert.Contains(t, errOut, "path-rules — path-scoped CI rules",
		"section-specific hint must accompany a path-rules error")
	assert.NotContains(t, errOut, "rules — global CI efficiency thresholds",
		"the rules-section hint must NOT appear for a path-rules failure")
	assert.NotContains(t, errOut, "Inspect a container image",
		"the root Long description must not be dumped on a config error")

	stdOut := stdout.String()
	assert.NotContains(t, stdOut, "Usage:",
		"usage block must be silenced on RunE errors")
	assert.NotContains(t, errOut, "Usage:",
		"usage block must not appear on stderr either")
}

// CI=true on rootCmd routes through executeCICheck; passing layers (no path
// overlap → score 1.0) clear lowest-efficiency: 0.9.
func TestRunInspect_CIEnvShortcut_RunsCI(t *testing.T) {
	t.Setenv("CI", "true")
	writeConfig(t, "rules:\n  lowest-efficiency: 0.9\n  highest-user-wasted-percent: 0\n")
	resetRootCmdFlags(t)
	resetPersistentFlags(t)
	withFakeResolver(t, okResolver(passingLayers()...))

	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"nginx:latest"})

	err := rootCmd.Execute()
	require.NoError(t, err, "passing CI must not error; stderr=%s", stderr.String())
}

// Failing layers (duplicated /etc/config) drive efficiency below 0.9; runInspect
// must return *ErrCIFailed so main.go exits 1.
func TestRunInspect_CIEnvShortcut_RuleFailureReturnsErrCIFailed(t *testing.T) {
	t.Setenv("CI", "true")
	writeConfig(t, "rules:\n  lowest-efficiency: 0.9\n  highest-user-wasted-percent: 0\n")
	resetRootCmdFlags(t)
	resetPersistentFlags(t)
	withFakeResolver(t, okResolver(failingLayers()...))

	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"nginx:latest"})

	err := rootCmd.Execute()
	require.Error(t, err)
	var ciFailed *ErrCIFailed
	assert.True(t, errors.As(err, &ciFailed), "err must carry *ErrCIFailed; got %v", err)
}

// --json on rootCmd (no CI=true) routes through runJSONExport. The output file
// must round-trip through json.Unmarshal with the expected schema.
func TestRunInspect_JSONFlag_WritesAnalysis(t *testing.T) {
	resetRootCmdFlags(t)
	resetPersistentFlags(t)
	withFakeResolver(t, okResolver(passingLayers()...))

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "out.json")

	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"--json", outPath, "nginx:latest"})

	err := rootCmd.Execute()
	require.NoError(t, err, "json export must succeed; stderr=%s", stderr.String())

	data, readErr := os.ReadFile(outPath)
	require.NoError(t, readErr, "output file must exist")

	var got jsonExport
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, jsonSchemaVersion, got.SchemaVersion)
	assert.Equal(t, "nginx:latest", got.ImageRef)
	assert.Equal(t, 1, got.LayerCount, "synthetic layer must round-trip")
}

// CI=true + --json must produce both the CI report AND the JSON file on disk
// in one Execute (cmd/root.go:173 sub-branch).
func TestRunInspect_CIEnvAndJSON_BothFire(t *testing.T) {
	t.Setenv("CI", "true")
	writeConfig(t, "rules:\n  lowest-efficiency: 0.9\n  highest-user-wasted-percent: 0\n")
	resetRootCmdFlags(t)
	resetPersistentFlags(t)
	withFakeResolver(t, okResolver(passingLayers()...))

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "out.json")

	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"--json", outPath, "nginx:latest"})

	err := rootCmd.Execute()
	require.NoError(t, err, "passing CI + JSON must not error; stderr=%s", stderr.String())
	_, statErr := os.Stat(outPath)
	assert.NoError(t, statErr, "JSON file must be written")
}

// When CI fails, the JSON must STILL be written — the analysis was produced;
// only the rule check failed (combineCIAndJSONErr's contract).
func TestRunInspect_CIEnvAndJSON_CIFailJSONStillWritten(t *testing.T) {
	t.Setenv("CI", "true")
	writeConfig(t, "rules:\n  lowest-efficiency: 0.9\n  highest-user-wasted-percent: 0\n")
	resetRootCmdFlags(t)
	resetPersistentFlags(t)
	withFakeResolver(t, okResolver(failingLayers()...))

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "out.json")

	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"--json", outPath, "nginx:latest"})

	err := rootCmd.Execute()
	require.Error(t, err)
	var ciFailed *ErrCIFailed
	assert.True(t, errors.As(err, &ciFailed), "err must carry *ErrCIFailed; got %v", err)
	_, statErr := os.Stat(outPath)
	assert.NoError(t, statErr, "JSON must be written even when CI fails — analysis was produced")
}

