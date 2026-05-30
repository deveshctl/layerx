package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

const defaultConfigFile = ".layerx.yaml"

// Config holds all user-configurable settings.
type Config struct {
	// Version is the schema version of this .layerx.yaml. Currently only 1
	// is accepted. Unset (zero) is treated as 1. Future schema breakage
	// will rev this — readers that see a version they don't understand
	// must error rather than guess.
	Version int `yaml:"version,omitempty"`

	Rules RulesConfig `yaml:"rules"`

	// PathRules is populated by the loader's normalize() pass. It is NOT
	// directly unmarshalled from YAML — see normalizePathRules() — because
	// the YAML can be either a mapping (flat form) or a sequence (list
	// form). Tagged "-" so yaml.Strict() doesn't barf on a missing key.
	PathRules []PathRuleSpec `yaml:"-"`

	// Keybindings is a placeholder for the M12 keybinding-override feature
	// documented in CLAUDE.md. Declared here so yaml.Strict() accepts the
	// documented top-level key without rejecting the whole config; semantics
	// are wired up in M12.
	Keybindings map[string]string `yaml:"keybindings,omitempty"`
}

// rawConfig mirrors Config but captures rules and path-rules as raw AST nodes
// so the loader can reject null/malformed shapes before they zero defaults.
// Used only inside LoadFrom — never exposed.
type rawConfig struct {
	Version     int               `yaml:"version,omitempty"`
	Rules       ast.Node          `yaml:"rules,omitempty"`
	PathRules   ast.Node          `yaml:"path-rules,omitempty"`
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
		Version: 1,
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
// only whitespace/comments.
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Default(), nil
		}
		return nil, err
	}

	if !hasYAMLContent(data) {
		return Default(), nil
	}

	// Decode into rawConfig so rules and path-rules stay as AST nodes for
	// shape validation before defaults are applied.
	var raw rawConfig
	dec := yaml.NewDecoder(bytes.NewReader(data), yaml.Strict())
	if err := dec.Decode(&raw); err != nil {
		return nil, newLoadError(path, inferParseSection(err), err)
	}

	rules, err := decodeRules(raw.Rules, Default().Rules)
	if err != nil {
		return nil, newLoadError(path, SectionRules, err)
	}

	specs, err := normalizePathRules(raw.PathRules)
	if err != nil {
		return nil, newLoadError(path, SectionPathRules, err)
	}

	cfg := &Config{
		Version:     raw.Version,
		Rules:       rules,
		PathRules:   specs,
		Keybindings: raw.Keybindings,
	}

	if err := cfg.validate(); err != nil {
		return nil, newLoadError(path, validationSection(err.Error()), err)
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
	if c.Version != 0 && c.Version != 1 {
		return fmt.Errorf("version: only schema version 1 is supported; got %d", c.Version)
	}
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

func validationSection(msg string) string {
	switch {
	case strings.HasPrefix(msg, "version:"):
		return SectionVersion
	case strings.HasPrefix(msg, "rules."):
		return SectionRules
	default:
		return ""
	}
}

// inferParseSection maps strict YAML parse errors to a config section when
// the library surfaces a field path; otherwise returns "".
func inferParseSection(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "path-rules"):
		return SectionPathRules
	case strings.Contains(msg, "keybindings"):
		return SectionKeybindings
	case strings.Contains(msg, "rules"):
		return SectionRules
	case strings.Contains(msg, "version"):
		return SectionVersion
	default:
		return ""
	}
}

// PathRuleType identifies one of the three supported path-rule kinds.
// Unknown values are rejected at load time.
type PathRuleType string

const (
	PathRuleBlock         PathRuleType = "block"
	PathRuleDenyWaste     PathRuleType = "deny-waste"
	PathRuleMaxLayerCount PathRuleType = "max-layer-count"
)

// PathRuleSpec is the canonical, post-normalize representation of one path
// rule. Both YAML forms (flat map and list-of-rules) collapse to a slice of
// these — downstream code never needs to know which form the user wrote.
//
// For Block / DenyWaste: Paths is populated, Threshold is 0.
// For MaxLayerCount:    Threshold is populated, Paths is nil.
//
// ID is the rule identifier used for log-grep and future --rule-id filters.
// In flat form, ID is the rule type name itself ("block", "deny-waste",
// "max-layer-count"). In list form, ID is user-supplied and must be unique.
type PathRuleSpec struct {
	ID        string
	Type      PathRuleType
	Paths     []string
	Threshold int
}
