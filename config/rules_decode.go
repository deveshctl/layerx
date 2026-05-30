package config

import (
	"fmt"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// decodeRules interprets the raw AST node for the top-level "rules" key.
// A nil node (rules: absent, or rules: null after the document-level guard)
// returns defaults. Non-mapping shapes (scalars, sequences) error so the
// loader does not silently coerce malformed input into zero values.
//
// `rules: null` is rejected upstream by rejectNullSections in config.go —
// goccy maps a YAML null scalar into an ast.Node field as Go nil rather
// than a *ast.NullNode, so the type-assertion path that used to live here
// could never fire. The pre-decode AST walk catches it before this function
// runs.
func decodeRules(node ast.Node, defaults RulesConfig) (RulesConfig, error) {
	if node == nil {
		return defaults, nil
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
