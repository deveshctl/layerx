package config

import (
	"fmt"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// decodeRules interprets the raw AST node for the top-level "rules" key.
// A missing node keeps defaults. null and non-mapping shapes error so a
// structurally invalid rules block cannot silently zero out thresholds.
func decodeRules(node ast.Node, defaults RulesConfig) (RulesConfig, error) {
	if node == nil {
		return defaults, nil
	}
	if _, ok := node.(*ast.NullNode); ok {
		return RulesConfig{}, fmt.Errorf("must be a mapping, not null")
	}
	switch node.(type) {
	case *ast.MappingNode, *ast.MappingValueNode:
	default:
		return RulesConfig{}, fmt.Errorf("must be a mapping; got %T", node)
	}
	out := defaults
	if err := yaml.NodeToValue(node, &out, yaml.Strict()); err != nil {
		return RulesConfig{}, err
	}
	return out, nil
}
