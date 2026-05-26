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
`)
	require.NoError(t, os.WriteFile(path, content, 0644))

	cfg, err := LoadFrom(path)
	require.NoError(t, err)
	assert.Equal(t, 0.95, cfg.Rules.LowestEfficiency)
	assert.Equal(t, int64(5000000), cfg.Rules.HighestWastedBytes)
	assert.Equal(t, 0.05, cfg.Rules.HighestUserWastedPercent)
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

func TestLoadFrom_RejectsLowestEfficiencyOutOfRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`rules:
  lowest-efficiency: 1.5
`), 0644))

	_, err := LoadFrom(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lowest-efficiency")
}

func TestLoadFrom_RejectsNegativeUserWastedPercent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`rules:
  highest-user-wasted-percent: -0.1
`), 0644))

	_, err := LoadFrom(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "highest-user-wasted-percent")
}

func TestLoadFrom_RejectsNegativeWastedBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`rules:
  highest-wasted-bytes: -1
`), 0644))

	_, err := LoadFrom(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "highest-wasted-bytes")
}
