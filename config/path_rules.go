package config

import (
	"fmt"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// flatPathRules mirrors the flat YAML form for direct unmarshal:
//
//	path-rules:
//	  block: [...]
//	  deny-waste: [...]
//	  max-layer-count: 3
type flatPathRules struct {
	Block         []string `yaml:"block,omitempty"`
	DenyWaste     []string `yaml:"deny-waste,omitempty"`
	MaxLayerCount int      `yaml:"max-layer-count,omitempty"`
}

// listPathRule mirrors one entry of the list YAML form:
//
//	- id: foo
//	  type: block
//	  paths: [...]
//	  threshold: 3
type listPathRule struct {
	ID        string   `yaml:"id"`
	Type      string   `yaml:"type"`
	Paths     []string `yaml:"paths,omitempty"`
	Threshold int      `yaml:"threshold,omitempty"`
}

// normalizePathRules dispatches on the node kind and returns []PathRuleSpec.
// nil node (path-rules: absent) returns nil, nil. Mixing flat and list forms
// is rejected by goccy itself (a node is either Mapping or Sequence, never
// both); unknown shapes (e.g. a scalar) error out here.
func normalizePathRules(node ast.Node) ([]PathRuleSpec, error) {
	if node == nil {
		return nil, nil
	}
	switch node.(type) {
	case *ast.MappingNode, *ast.MappingValueNode:
		return normalizeFlat(node)
	case *ast.SequenceNode:
		return normalizeList(node)
	case *ast.NullNode:
		return nil, nil
	default:
		return nil, fmt.Errorf("path-rules must be a mapping (flat form) or a sequence (list form); got %T", node)
	}
}

// normalizeFlat decodes a mapping into flatPathRules and emits one
// PathRuleSpec per non-empty field.
func normalizeFlat(node ast.Node) ([]PathRuleSpec, error) {
	var flat flatPathRules
	if err := yaml.NodeToValue(node, &flat, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("path-rules (flat form): %w", err)
	}

	var specs []PathRuleSpec
	if len(flat.Block) > 0 {
		if err := validateGlobs("block", flat.Block); err != nil {
			return nil, err
		}
		specs = append(specs, PathRuleSpec{
			ID:    "block",
			Type:  PathRuleBlock,
			Paths: flat.Block,
		})
	}
	if len(flat.DenyWaste) > 0 {
		if err := validateGlobs("deny-waste", flat.DenyWaste); err != nil {
			return nil, err
		}
		specs = append(specs, PathRuleSpec{
			ID:    "deny-waste",
			Type:  PathRuleDenyWaste,
			Paths: flat.DenyWaste,
		})
	}
	if flat.MaxLayerCount != 0 {
		if err := validateMaxLayerCount(flat.MaxLayerCount); err != nil {
			return nil, err
		}
		specs = append(specs, PathRuleSpec{
			ID:        "max-layer-count",
			Type:      PathRuleMaxLayerCount,
			Threshold: flat.MaxLayerCount,
		})
	}
	return specs, nil
}

// normalizeList decodes a sequence into []listPathRule and emits one
// PathRuleSpec per entry. Validates id (required, unique), type (required,
// known), and per-type fields.
func normalizeList(node ast.Node) ([]PathRuleSpec, error) {
	var raw []listPathRule
	if err := yaml.NodeToValue(node, &raw, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("path-rules (list form): %w", err)
	}

	specs := make([]PathRuleSpec, 0, len(raw))
	seenIDs := make(map[string]struct{}, len(raw))
	for i, entry := range raw {
		if entry.ID == "" {
			return nil, fmt.Errorf("path-rules[%d]: missing required field 'id'", i)
		}
		if _, dup := seenIDs[entry.ID]; dup {
			return nil, fmt.Errorf("path-rules: duplicate id %q", entry.ID)
		}
		seenIDs[entry.ID] = struct{}{}

		if entry.Type == "" {
			return nil, fmt.Errorf("path-rules[id=%s]: missing required field 'type'", entry.ID)
		}
		typ := PathRuleType(entry.Type)
		switch typ {
		case PathRuleBlock, PathRuleDenyWaste:
			if err := validateGlobs(entry.ID, entry.Paths); err != nil {
				return nil, err
			}
			specs = append(specs, PathRuleSpec{
				ID:    entry.ID,
				Type:  typ,
				Paths: entry.Paths,
			})
		case PathRuleMaxLayerCount:
			if err := validateMaxLayerCount(entry.Threshold); err != nil {
				return nil, fmt.Errorf("path-rules[id=%s]: %w", entry.ID, err)
			}
			specs = append(specs, PathRuleSpec{
				ID:        entry.ID,
				Type:      typ,
				Threshold: entry.Threshold,
			})
		default:
			return nil, fmt.Errorf("path-rules[id=%s]: unknown type %q (want block, deny-waste, or max-layer-count)", entry.ID, entry.Type)
		}
	}
	return specs, nil
}

// validateGlobs checks every pattern with doublestar's validator. The ruleID
// is used to make error messages findable.
func validateGlobs(ruleID string, patterns []string) error {
	for _, p := range patterns {
		if !doublestar.ValidatePattern(p) {
			return fmt.Errorf("path-rules[id=%s]: invalid glob pattern %q", ruleID, p)
		}
	}
	return nil
}

// validateMaxLayerCount enforces the spec's range (>= 2 if set).
// 0 is treated as disabled (rule absent); the caller drops it before emitting
// a spec, but defensive validation here catches a pathological list-form
// entry like {threshold: 0}.
func validateMaxLayerCount(n int) error {
	if n < 0 {
		return fmt.Errorf("max-layer-count must be >= 0; got %d", n)
	}
	if n == 1 {
		return fmt.Errorf("max-layer-count must be >= 2; %d would flag every file", n)
	}
	return nil
}
