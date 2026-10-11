package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFrom_PathRules_RequiredPatterns(t *testing.T) {
	for _, ruleType := range []string{"block", "deny-waste"} {
		for name, paths := range map[string]string{
			"missing":    "",
			"null":       "    paths: null\n",
			"empty":      "    paths: []\n",
			"blank":      "    paths: [\"\"]\n",
			"whitespace": "    paths: [\" \\t \"]\n",
			"mixed":      "    paths: [\"/app/**\", \" \"]\n",
		} {
			t.Run(ruleType+"/"+name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "config.yaml")
				content := fmt.Sprintf("path-rules:\n  - id: app-policy\n    type: %s\n%s", ruleType, paths)
				require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
				_, err := LoadFrom(path)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "app-policy")
				assert.Contains(t, err.Error(), "paths")
			})
		}
	}
}

func TestLoadFrom_PathRules_OptionalPatternsRemainValid(t *testing.T) {
	for name, content := range map[string]string{
		"flat_empty":     "path-rules:\n  block: []\n  deny-waste: []\n",
		"flat_null":      "path-rules:\n  block: null\n  deny-waste: null\n",
		"count_only":     "path-rules:\n  - id: cap\n    type: max-layer-count\n    threshold: 3\n",
		"disabled_count": "path-rules:\n  - id: cap\n    type: max-layer-count\n    threshold: 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
			cfg, err := LoadFrom(path)
			require.NoError(t, err)
			if name == "count_only" {
				require.Len(t, cfg.PathRules, 1)
				assert.Equal(t, 3, cfg.PathRules[0].Threshold)
			} else {
				assert.Empty(t, cfg.PathRules)
			}
		})
	}
}
