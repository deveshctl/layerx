package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	assert.Equal(t, 0.9, cfg.Rules.LowestEfficiency)
	assert.Equal(t, int64(0), cfg.Rules.HighestWastedBytes)
	assert.Equal(t, 0.1, cfg.Rules.HighestUserWastedPercent)
	assert.Equal(t, "", cfg.Keybindings.Quit)
}

func TestLoadFrom_MissingFile(t *testing.T) {
	cfg, err := LoadFrom("/nonexistent/.layerx.yaml")
	require.NoError(t, err)
	assert.Equal(t, Default(), cfg)
}

func TestLoadFrom_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	content := []byte(`rules:
  lowest-efficiency: 0.95
  highest-wasted-bytes: 5000000
  highest-user-wasted-percent: 0.05
keybindings:
  quit: "q"
  filter: "f"
`)
	require.NoError(t, os.WriteFile(path, content, 0644))

	cfg, err := LoadFrom(path)
	require.NoError(t, err)
	assert.Equal(t, 0.95, cfg.Rules.LowestEfficiency)
	assert.Equal(t, int64(5000000), cfg.Rules.HighestWastedBytes)
	assert.Equal(t, 0.05, cfg.Rules.HighestUserWastedPercent)
	assert.Equal(t, "q", cfg.Keybindings.Quit)
	assert.Equal(t, "f", cfg.Keybindings.Filter)
	assert.Equal(t, "", cfg.Keybindings.Up)
}

func TestLoadFrom_PartialYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	content := []byte(`rules:
  lowest-efficiency: 0.99
`)
	require.NoError(t, os.WriteFile(path, content, 0644))

	cfg, err := LoadFrom(path)
	require.NoError(t, err)
	assert.Equal(t, 0.99, cfg.Rules.LowestEfficiency)
	assert.Equal(t, int64(0), cfg.Rules.HighestWastedBytes)
	assert.Equal(t, 0.1, cfg.Rules.HighestUserWastedPercent)
}

func TestLoadFrom_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	content := []byte(`rules: [[[invalid`)
	require.NoError(t, os.WriteFile(path, content, 0644))

	_, err := LoadFrom(path)
	assert.Error(t, err)
}
