package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deveshctl/layerx/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// chdirTemp creates a temp dir, chdirs to it, and registers a cleanup that
// chdirs back. Returns the temp dir path.
func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(prev) })
	return dir
}

// TestInit_FlavourFlag: --flavour node writes the node config; output
// matches the embed byte-for-byte.
func TestInit_FlavourFlag(t *testing.T) {
	dir := chdirTemp(t)

	flavour, err := resolveFlavour("node", &bytes.Buffer{}, &bytes.Buffer{})
	require.NoError(t, err)
	data, ok := StarterConfig(flavour)
	require.True(t, ok)
	require.NoError(t, writeStarterConfig(filepath.Join(dir, ".layerx.yaml"), data, false))

	got, err := os.ReadFile(filepath.Join(dir, ".layerx.yaml"))
	require.NoError(t, err)
	want, _ := StarterConfig("node")
	assert.Equal(t, want, got, "written file must match the embedded node config byte-for-byte")
}

// TestInit_DefaultGeneric_NonTTY: stdin is not a TTY, no flavour flag →
// resolveFlavour returns "generic" and emits a stderr note.
func TestInit_DefaultGeneric_NonTTY(t *testing.T) {
	var stderr bytes.Buffer
	flavour, err := resolveFlavour("", &bytes.Buffer{}, &stderr)
	require.NoError(t, err)
	assert.Equal(t, "generic", flavour)
	assert.Contains(t, stderr.String(), "generic")
	assert.Contains(t, stderr.String(), "not a terminal")
}

// TestInit_RefusesOverwrite: an existing .layerx.yaml is not overwritten
// without --force.
func TestInit_RefusesOverwrite(t *testing.T) {
	dir := chdirTemp(t)
	target := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(target, []byte("# pre-existing\n"), 0644))

	data, _ := StarterConfig("generic")
	err := writeStarterConfig(target, data, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to overwrite")

	got, _ := os.ReadFile(target)
	assert.Equal(t, "# pre-existing\n", string(got), "file must be unchanged")
}

// TestInit_Force: --force overwrites.
func TestInit_Force(t *testing.T) {
	dir := chdirTemp(t)
	target := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(target, []byte("# pre-existing\n"), 0644))

	data, _ := StarterConfig("generic")
	require.NoError(t, writeStarterConfig(target, data, true))

	got, _ := os.ReadFile(target)
	assert.Equal(t, data, got)
}

// TestInit_UnknownFlavour: --flavour klingon errors with the valid choices
// listed.
func TestInit_UnknownFlavour(t *testing.T) {
	_, err := resolveFlavour("klingon", &bytes.Buffer{}, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "klingon")
	for _, f := range validFlavours {
		assert.Contains(t, err.Error(), f)
	}
}

// TestInit_PromptFlavour_Default: empty input on the prompt picks generic.
func TestInit_PromptFlavour_Default(t *testing.T) {
	in := strings.NewReader("\n")
	var errw bytes.Buffer
	got, err := promptFlavour(in, &errw)
	require.NoError(t, err)
	assert.Equal(t, "generic", got)
}

// TestInit_PromptFlavour_NumericChoice: input "1" picks the first flavour.
func TestInit_PromptFlavour_NumericChoice(t *testing.T) {
	in := strings.NewReader("1\n")
	var errw bytes.Buffer
	got, err := promptFlavour(in, &errw)
	require.NoError(t, err)
	assert.Equal(t, validFlavours[0], got)
}

// TestInit_PromptFlavour_BadChoice: garbage input errors.
func TestInit_PromptFlavour_BadChoice(t *testing.T) {
	in := strings.NewReader("99\n")
	var errw bytes.Buffer
	_, err := promptFlavour(in, &errw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "99")
}

// TestInit_StarterConfigs_AllParse: every embedded starter must round-trip
// through config.LoadFrom without error and produce at least one rule.
// Pins spec §8.7.
func TestInit_StarterConfigs_AllParse(t *testing.T) {
	for _, flavour := range validFlavours {
		t.Run(flavour, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".layerx.yaml")
			data, ok := StarterConfig(flavour)
			require.True(t, ok)
			require.NoError(t, os.WriteFile(path, data, 0644))

			cfg, err := config.LoadFrom(path)
			require.NoError(t, err, "starter %s must load without error", flavour)
			assert.Equal(t, 1, cfg.Version)
			assert.NotEmpty(t, cfg.PathRules, "starter %s must define at least one path rule", flavour)
		})
	}
}
