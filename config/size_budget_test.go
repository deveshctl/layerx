package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSizeBudgetConfiguration(t *testing.T) {
	for _, tc := range []struct {
		yaml  string
		valid bool
	}{
		{"size-budget:\n  max: 500MB\n  max-growth: 5%\n", true},
		{"size-budget: {}\n", true},
		{"size-budget:\n  max: 0B\n  max-growth: 0%\n", true},
		{"size-budget: null\n", false},
		{"size-budget:\n  max: -1MB\n", false},
		{"size-budget:\n  max-growth: NaN%\n", false},
		{"size-budget:\n  unknown: 1MB\n", false},
	} {
		t.Run(tc.yaml, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.yaml), 0600))
			cfg, err := LoadFrom(path)
			if tc.valid {
				require.NoError(t, err)
				require.NotNil(t, cfg)
			} else {
				require.Error(t, err)
			}
		})
	}
}
