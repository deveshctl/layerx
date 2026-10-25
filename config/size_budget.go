package config

import (
	"fmt"
	"strings"

	"github.com/deveshctl/layerx/internal/sizebudget"
	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

func decodeSizeBudget(node ast.Node) (sizebudget.Config, error) {
	var out sizebudget.Config
	if node == nil {
		return out, nil
	}
	switch node.(type) {
	case *ast.MappingNode, *ast.MappingValueNode:
	default:
		return out, fmt.Errorf("must be a mapping; got %T", node)
	}
	// Decode presence separately: the string decoder otherwise turns null
	// limits into empty strings, indistinguishable from omitted/disabled keys.
	var fields map[string]any
	if err := yaml.NodeToValue(node, &fields); err != nil {
		return out, err
	}
	for _, name := range []string{"max", "max-growth"} {
		value, present := fields[name]
		if !present {
			continue
		}
		if value == nil {
			return out, fmt.Errorf("%s must have a value, not null; omit the key to disable it", name)
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
			return out, fmt.Errorf("%s must not be blank; omit the key to disable it", name)
		}
	}
	err := yaml.NodeToValue(node, &out, yaml.Strict())
	return out, err
}
