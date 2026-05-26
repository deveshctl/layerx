package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/goccy/go-yaml"
)

const defaultConfigFile = ".layerx.yaml"

// Config holds all user-configurable settings.
type Config struct {
	Rules RulesConfig `yaml:"rules"`
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
// Returns default config if the file does not exist.
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Default(), nil
		}
		return nil, err
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validating %s: %w", path, err)
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.Rules.LowestEfficiency < 0 || c.Rules.LowestEfficiency > 1 {
		return fmt.Errorf("rules.lowest-efficiency must be in [0, 1]; got %v", c.Rules.LowestEfficiency)
	}
	if c.Rules.HighestUserWastedPercent < 0 || c.Rules.HighestUserWastedPercent > 1 {
		return fmt.Errorf("rules.highest-user-wasted-percent must be in [0, 1]; got %v", c.Rules.HighestUserWastedPercent)
	}
	if c.Rules.HighestWastedBytes < 0 {
		return fmt.Errorf("rules.highest-wasted-bytes must be >= 0; got %d", c.Rules.HighestWastedBytes)
	}
	return nil
}
