package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"

	"github.com/goccy/go-yaml"
)

const defaultConfigFile = ".layerx.yaml"

// Config holds all user-configurable settings.
type Config struct {
	Rules RulesConfig `yaml:"rules"`
	// Keybindings is a placeholder for the M12 keybinding-override feature
	// documented in CLAUDE.md. Declared here so yaml.Strict() accepts the
	// documented top-level key without rejecting the whole config; semantics
	// are wired up in M12.
	Keybindings map[string]string `yaml:"keybindings,omitempty"`
}

// RulesConfig holds CI rule thresholds.
type RulesConfig struct {
	LowestEfficiency        float64 `yaml:"lowest-efficiency"`
	HighestWastedBytes      int64   `yaml:"highest-wasted-bytes"`
	HighestUserWastedPercent float64 `yaml:"highest-user-wasted-percent"`
}

// Default returns the config with hardcoded default values.
func Default() *Config {
	return &Config{
		Rules: RulesConfig{
			LowestEfficiency:        0.9,
			HighestWastedBytes:      0,
			HighestUserWastedPercent: 0.1,
		},
	}
}

// Load reads .layerx.yaml from the current directory.
// Returns default config if the file does not exist.
func Load() (*Config, error) {
	return LoadFrom(defaultConfigFile)
}

// LoadFrom reads config from the specified path.
// Returns default config if the file does not exist, is empty, or contains
// only whitespace/comments — the M12 contract treats absent and content-less
// configs identically: "use defaults". A bytes.TrimSpace pre-check handles
// this without depending on the YAML decoder's empty-input error shape,
// which has varied across goccy/go-yaml releases.
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Default(), nil
		}
		return nil, err
	}

	// Strip comments and check if anything substantive remains. A YAML
	// document containing only `# comment` lines parses to nothing; we
	// fall back to defaults rather than letting the decoder's behaviour
	// on zero-document input drive the result.
	if !hasYAMLContent(data) {
		return Default(), nil
	}

	cfg := Default()
	dec := yaml.NewDecoder(bytes.NewReader(data), yaml.Strict())
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validating %s: %w", path, err)
	}
	return cfg, nil
}

// hasYAMLContent reports whether data contains anything that would parse to a
// non-empty YAML document. Whitespace and full-line comments (`# …`) are
// stripped; if nothing remains, the file is treated as empty.
func hasYAMLContent(data []byte) bool {
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		if trimmed[0] == '#' {
			continue
		}
		return true
	}
	return false
}

func (c *Config) validate() error {
	le := c.Rules.LowestEfficiency
	if math.IsNaN(le) || math.IsInf(le, 0) || le < 0 || le > 1 {
		return fmt.Errorf("rules.lowest-efficiency must be a finite number in [0, 1]; got %v", le)
	}
	huwp := c.Rules.HighestUserWastedPercent
	if math.IsNaN(huwp) || math.IsInf(huwp, 0) || huwp < 0 || huwp > 1 {
		return fmt.Errorf("rules.highest-user-wasted-percent must be a finite number in [0, 1]; got %v", huwp)
	}
	if c.Rules.HighestWastedBytes < 0 {
		return fmt.Errorf("rules.highest-wasted-bytes must be >= 0; got %d", c.Rules.HighestWastedBytes)
	}
	return nil
}
