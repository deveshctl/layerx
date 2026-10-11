package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutomationCLI_PathRules(t *testing.T) {
	archive := automationArchive(t)
	for _, ruleType := range []string{"block", "deny-waste"} {
		for name, paths := range map[string]string{
			"missing":    "",
			"null":       "    paths: null\n",
			"empty":      "    paths: []\n",
			"blank":      "    paths: [\"\"]\n",
			"whitespace": "    paths: [\" \\t \"]\n",
			"matching":   "    paths: [/app]\n",
			"unmatched":  "    paths: [/other]\n",
		} {
			t.Run(ruleType+"/"+name, func(t *testing.T) {
				configPath := filepath.Join(t.TempDir(), "config.yaml")
				content := "rules:\n  lowest-efficiency: 0\n  highest-user-wasted-percent: 0\n  highest-wasted-bytes: 0\n"
				content += fmt.Sprintf("path-rules:\n  - id: app-policy\n    type: %s\n%s", ruleType, paths)
				require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))
				code, stdout, stderr := runAutomationCLI(t, "", "--config", configPath, "ci", archive)
				switch name {
				case "matching":
					assert.Equal(t, 1, code, "%s", stderr)
					assert.Contains(t, stdout, "FAIL")
				case "unmatched":
					assert.Zero(t, code, "%s", stderr)
					assert.Contains(t, stdout, "PASS")
				default:
					assert.Equal(t, 2, code)
					assert.Contains(t, stderr, "app-policy")
					assert.Contains(t, stderr, "paths")
					assert.Equal(t, 1, strings.Count(stderr, "Error:"))
					assert.NotContains(t, stdout, "PASS")
				}
			})
		}
	}
}
