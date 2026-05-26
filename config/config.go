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
	return cfg, nil
}
