package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/deveshctl/layerx/theme"
	"github.com/stretchr/testify/require"
)

// runThemesCmd executes `layerx themes` (with optional flags) against
// a fresh in-process buffer and returns stdout. Mirrors the helper
// pattern in cmd/cache_test.go (testing_helpers_test.go).
//
// Resets flagTheme and flagThemesJSON so previous-test leftovers do
// not bleed in. Tests that want a clean $LAYERX_THEME call
// t.Setenv("LAYERX_THEME", "") themselves before invoking the helper;
// tests that exercise env precedence call t.Setenv with their value.
// The helper must not unconditionally clear LAYERX_THEME because that
// would defeat the env-override tests.
func runThemesCmd(t *testing.T, args ...string) string {
	t.Helper()
	prevFlag := flagThemesJSON
	t.Cleanup(func() { flagThemesJSON = prevFlag })
	prevTheme := flagTheme
	t.Cleanup(func() { flagTheme = prevTheme })
	flagTheme = ""
	flagThemesJSON = false

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(append([]string{"themes"}, args...))
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	require.NoError(t, rootCmd.Execute())
	return buf.String()
}

// TestThemes_PlainListsAllThemes: every registered theme appears in
// the plain-text output, with descriptions.
func TestThemes_PlainListsAllThemes(t *testing.T) {
	t.Setenv("LAYERX_THEME", "")
	out := runThemesCmd(t)
	for _, th := range theme.All() {
		require.Contains(t, out, string(th.Name), "missing theme name %q", th.Name)
		require.Contains(t, out, th.Description, "missing description for %q", th.Name)
	}
}

// TestThemes_ActiveMarker: exactly one theme has the "*" marker; with
// no flag/env/YAML, it's "default".
func TestThemes_ActiveMarker(t *testing.T) {
	t.Setenv("LAYERX_THEME", "")
	out := runThemesCmd(t)
	starCount := strings.Count(out, "*")
	require.Equal(t, 1, starCount, "expected exactly one '*' marker")
	// First non-empty line should be "default*\t..." since default is
	// registered first and we have no overrides.
	require.Contains(t, out, "default*")
}

// TestThemes_EnvOverridesActive: $LAYERX_THEME shifts the marker.
func TestThemes_EnvOverridesActive(t *testing.T) {
	t.Setenv("LAYERX_THEME", "nord")
	out := runThemesCmd(t)
	require.Contains(t, out, "nord*")
	require.NotContains(t, out, "default*")
}

// TestThemes_BadEnvFallsBack: an invalid $LAYERX_THEME does not crash;
// the marker silently falls back to "default".
func TestThemes_BadEnvFallsBack(t *testing.T) {
	t.Setenv("LAYERX_THEME", "definitely-not-a-theme")
	out := runThemesCmd(t)
	require.Contains(t, out, "default*")
}

// TestThemes_JSON: --json emits a JSON array of every theme name in
// stable order. Insensitive to env state, but a clean baseline keeps
// the test deterministic when run in isolation.
func TestThemes_JSON(t *testing.T) {
	t.Setenv("LAYERX_THEME", "")
	out := runThemesCmd(t, "--json")
	var names []string
	require.NoError(t, json.Unmarshal([]byte(out), &names))
	want := make([]string, 0, len(theme.All()))
	for _, th := range theme.All() {
		want = append(want, string(th.Name))
	}
	require.Equal(t, want, names)
}
