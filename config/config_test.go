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

// keybindings is a documented (M12) top-level key. Strict YAML must accept it
// so users following CLAUDE.md examples don't get their whole config rejected
// when they include a keybindings block alongside rules.
func TestLoadFrom_AcceptsKeybindingsBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`rules:
  lowest-efficiency: 0.95
keybindings:
  quit: q
  filter: /
`), 0644))

	cfg, err := LoadFrom(path)
	require.NoError(t, err)
	assert.Equal(t, 0.95, cfg.Rules.LowestEfficiency)
	assert.Equal(t, "q", cfg.Keybindings["quit"])
	assert.Equal(t, "/", cfg.Keybindings["filter"])
}

// Strict mode must still reject genuinely unknown top-level keys so typos in
// rules/keybindings names surface instead of silently being ignored.
func TestLoadFrom_RejectsUnknownTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`ruels:
  lowest-efficiency: 0.9
`), 0644))

	_, err := LoadFrom(path)
	require.Error(t, err)
}

// M12 contract: a present-but-content-less .layerx.yaml is identical to an
// absent one — fall back to defaults rather than aborting startup. goccy
// returns io.EOF on zero-document input, which without the EOF guard would
// have surfaced as a misleading "parsing" error.
func TestLoadFrom_EmptyFile_UsesDefaults(t *testing.T) {
	cases := map[string]string{
		"empty":             "",
		"whitespace_only":   "   \n\n\t\n",
		"comments_only":     "# just a comment\n# another\n",
		"comments_and_ws":   "\n# placeholder\n\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".layerx.yaml")
			require.NoError(t, os.WriteFile(path, []byte(content), 0644))

			cfg, err := LoadFrom(path)
			require.NoError(t, err, "content-less config must not error")
			assert.Equal(t, Default(), cfg, "content-less config must equal defaults")
		})
	}
}
