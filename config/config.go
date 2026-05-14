package config

import (
	"errors"
	"io/fs"
	"os"

	"github.com/goccy/go-yaml"
)

const defaultConfigFile = ".layerx.yaml"

// Config holds all user-configurable settings.
type Config struct {
	Rules       RulesConfig       `yaml:"rules"`
	Keybindings KeybindingsConfig `yaml:"keybindings"`
}

// RulesConfig holds CI rule thresholds.
type RulesConfig struct {
	LowestEfficiency        float64 `yaml:"lowest-efficiency"`
	HighestWastedBytes      int64   `yaml:"highest-wasted-bytes"`
	HighestUserWastedPercent float64 `yaml:"highest-user-wasted-percent"`
}

// KeybindingsConfig holds keybinding overrides. Empty string means keep default.
type KeybindingsConfig struct {
	Quit     string `yaml:"quit"`
	Up       string `yaml:"up"`
	Down     string `yaml:"down"`
	Filter   string `yaml:"filter"`
	Sort     string `yaml:"sort"`
	DiffOnly string `yaml:"diff-only"`
	Extract  string `yaml:"extract"`
	Help     string `yaml:"help"`
	Switch   string `yaml:"switch"`
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
		return nil, err
	}
	return cfg, nil
}
