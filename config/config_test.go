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

func TestLoadFrom_Version_Default(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`rules:
  lowest-efficiency: 0.9
`), 0644))

	cfg, err := LoadFrom(path)
	require.NoError(t, err)
	assert.Equal(t, 1, cfg.Version, "absent version: defaults to 1")
}

func TestLoadFrom_Version_Explicit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`version: 1
rules:
  lowest-efficiency: 0.9
`), 0644))

	cfg, err := LoadFrom(path)
	require.NoError(t, err)
	assert.Equal(t, 1, cfg.Version)
}

func TestLoadFrom_Version_Unknown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`version: 2
rules:
  lowest-efficiency: 0.9
`), 0644))

	_, err := LoadFrom(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version")
	assert.Contains(t, err.Error(), "1")
}

func TestLoadFrom_PathRules_FlatForm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`version: 1
rules:
  lowest-efficiency: 0.9
path-rules:
  block:
    - "**/.git/**"
    - /tmp/**
  deny-waste:
    - "**/*.pyc"
  max-layer-count: 3
`), 0644))

	cfg, err := LoadFrom(path)
	require.NoError(t, err)
	require.Len(t, cfg.PathRules, 3)

	assert.Equal(t, "block", cfg.PathRules[0].ID)
	assert.Equal(t, PathRuleBlock, cfg.PathRules[0].Type)
	assert.Equal(t, []string{"**/.git/**", "/tmp/**"}, cfg.PathRules[0].Paths)

	assert.Equal(t, "deny-waste", cfg.PathRules[1].ID)
	assert.Equal(t, PathRuleDenyWaste, cfg.PathRules[1].Type)

	assert.Equal(t, "max-layer-count", cfg.PathRules[2].ID)
	assert.Equal(t, PathRuleMaxLayerCount, cfg.PathRules[2].Type)
	assert.Equal(t, 3, cfg.PathRules[2].Threshold)
}

func TestLoadFrom_PathRules_FlatForm_Partial(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`path-rules:
  block:
    - /tmp/**
`), 0644))

	cfg, err := LoadFrom(path)
	require.NoError(t, err)
	require.Len(t, cfg.PathRules, 1)
	assert.Equal(t, PathRuleBlock, cfg.PathRules[0].Type)
}

func TestLoadFrom_PathRules_ListForm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`path-rules:
  - id: no-apt-cache
    type: block
    paths:
      - /var/lib/apt/lists/**
  - id: no-pyc-waste
    type: deny-waste
    paths: ["**/*.pyc"]
  - id: dedupe-cap
    type: max-layer-count
    threshold: 3
`), 0644))

	cfg, err := LoadFrom(path)
	require.NoError(t, err)
	require.Len(t, cfg.PathRules, 3)
	assert.Equal(t, "no-apt-cache", cfg.PathRules[0].ID)
	assert.Equal(t, PathRuleBlock, cfg.PathRules[0].Type)
	assert.Equal(t, "no-pyc-waste", cfg.PathRules[1].ID)
	assert.Equal(t, "dedupe-cap", cfg.PathRules[2].ID)
	assert.Equal(t, 3, cfg.PathRules[2].Threshold)
}

func TestLoadFrom_PathRules_ListForm_DuplicateID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`path-rules:
  - id: foo
    type: block
    paths: [/a/**]
  - id: foo
    type: deny-waste
    paths: [/b/**]
`), 0644))

	_, err := LoadFrom(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate id")
	assert.Contains(t, err.Error(), "foo")
}

func TestLoadFrom_PathRules_ListForm_MissingType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`path-rules:
  - id: foo
    paths: [/a/**]
`), 0644))

	_, err := LoadFrom(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "type")
	assert.Contains(t, err.Error(), "foo")
}

func TestLoadFrom_PathRules_ListForm_MissingID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`path-rules:
  - type: block
    paths: [/a/**]
`), 0644))

	_, err := LoadFrom(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id")
}

func TestLoadFrom_PathRules_ListForm_UnknownType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`path-rules:
  - id: foo
    type: blokc
    paths: [/a/**]
`), 0644))

	_, err := LoadFrom(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blokc")
	assert.Contains(t, err.Error(), "block")
}

func TestLoadFrom_PathRules_InvalidGlob(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`path-rules:
  block:
    - "[invalid"
`), 0644))

	_, err := LoadFrom(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid glob")
	assert.Contains(t, err.Error(), "[invalid")
}

func TestLoadFrom_PathRules_MaxLayerCount_Range(t *testing.T) {
	cases := []struct {
		name      string
		yaml      string
		wantError string
	}{
		{
			name:      "one_rejected",
			yaml:      "path-rules:\n  max-layer-count: 1\n",
			wantError: ">= 2",
		},
		{
			name:      "negative_rejected",
			yaml:      "path-rules:\n  max-layer-count: -1\n",
			wantError: ">= 0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".layerx.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.yaml), 0644))

			_, err := LoadFrom(path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantError)
		})
	}
}

// max-layer-count: 0 is treated as disabled (rule absent), not an error.
// The flat normalizer drops it via the `flat.MaxLayerCount != 0` check.
func TestLoadFrom_PathRules_MaxLayerCount_ZeroDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`path-rules:
  max-layer-count: 0
`), 0644))

	cfg, err := LoadFrom(path)
	require.NoError(t, err)
	assert.Empty(t, cfg.PathRules, "max-layer-count: 0 must produce no rule (treated as disabled)")
}

// List-form symmetry with the flat-form ZeroDisabled test above. A list-form
// entry of type max-layer-count with threshold:0 must also be dropped, not
// emit a useless spec the evaluator would have to special-case at runtime.
// Other rules in the same list (e.g. a block rule alongside) must still load.
func TestLoadFrom_PathRules_MaxLayerCount_ListFormZeroDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`path-rules:
  - id: dedupe-cap
    type: max-layer-count
    threshold: 0
  - id: secrets
    type: block
    paths: [/etc/shadow]
`), 0644))

	cfg, err := LoadFrom(path)
	require.NoError(t, err)
	require.Len(t, cfg.PathRules, 1, "max-layer-count threshold:0 must be dropped, leaving only the block rule")
	assert.Equal(t, "secrets", cfg.PathRules[0].ID)
	assert.Equal(t, PathRuleBlock, cfg.PathRules[0].Type)
}

// Mixing forms is impossible at the YAML AST level — a node is either a
// mapping or a sequence, not both. This test pins that goccy gives a clean
// error rather than silently picking one.
func TestLoadFrom_PathRules_BothForms_Rejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".layerx.yaml")
	// Sequence syntax with mapping-like content — invalid YAML structure
	// for our schema; goccy will error one way or another.
	require.NoError(t, os.WriteFile(path, []byte(`path-rules:
  block: [a]
  - id: foo
    type: block
    paths: [/a/**]
`), 0644))

	_, err := LoadFrom(path)
	require.Error(t, err)
}
