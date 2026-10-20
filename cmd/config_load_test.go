package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/deveshctl/layerx/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintConfigSectionHint_LoadError(t *testing.T) {
	var buf bytes.Buffer
	err := fmt.Errorf("loading config: %w", &config.LoadError{
		Path:    ".layerx.yaml",
		Section: config.SectionRules,
		Err:     errors.New("rules.lowest-efficiency out of range"),
	})
	printConfigSectionHint(&buf, err)
	assert.Contains(t, buf.String(), "lowest-efficiency")
}

func TestPresentConfigLoadFailure_SetsSilenceErrors(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.SilenceErrors = false
	var buf bytes.Buffer
	cmd.SetErr(&buf)

	err := fmtWrapLoadError("bad value")
	presentConfigLoadFailure(cmd, err)

	assert.True(t, cmd.SilenceErrors)
	assert.Contains(t, buf.String(), "Error:")
	assert.Contains(t, buf.String(), "rules — global CI efficiency thresholds")
}

func fmtWrapLoadError(msg string) error {
	return fmt.Errorf("loading config: %w", &config.LoadError{
		Path:    ".layerx.yaml",
		Section: config.SectionRules,
		Err:     errors.New(msg),
	})
}

func TestPrintConfigSectionHint_UnknownSection_StillPrintsGeneral(t *testing.T) {
	var buf bytes.Buffer
	err := fmt.Errorf("loading config: %w", &config.LoadError{
		Path:    ".layerx.yaml",
		Section: "",
		Err:     errors.New("unknown field \"ruels\""),
	})
	printConfigSectionHint(&buf, err)
	assert.Contains(t, buf.String(), "docs/configuration.md")
	assert.Contains(t, buf.String(), "layerx init")
}

func TestPrintConfigSectionHint_NonLoadError_StillPrintsGeneral(t *testing.T) {
	var buf bytes.Buffer
	printConfigSectionHint(&buf, fmt.Errorf("loading config: %w", errors.New("permission denied")))
	assert.Contains(t, buf.String(), "docs/configuration.md")
}

func TestLoadConfig_BadRulesNull(t *testing.T) {
	writeConfig(t, "rules: null\n")

	cmd := &cobra.Command{Use: "test"}
	var buf bytes.Buffer
	cmd.SetErr(&buf)

	_, err := loadConfig(cmd)
	require.Error(t, err)
	assert.Contains(t, buf.String(), "must be a mapping, not null")
	assert.Contains(t, buf.String(), "rules — global CI efficiency thresholds")
}

func TestLoadConfig_ExplicitPathSkipsDiscovery(t *testing.T) {
	writeConfig(t, "rules: null\n")
	oldConfig := flagConfig
	t.Cleanup(func() { flagConfig = oldConfig })
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing=%t", missing), func(t *testing.T) {
			flagConfig = filepath.Join(t.TempDir(), "chosen.yaml")
			want := config.Default()
			if !missing {
				require.NoError(t, os.WriteFile(flagConfig, []byte("rules:\n  lowest-efficiency: 0.97\n"), 0o600))
				want.Rules.LowestEfficiency = 0.97
			}
			cmd := &cobra.Command{}
			var stderr bytes.Buffer
			cmd.SetErr(&stderr)
			got, err := loadConfig(cmd)
			require.NoError(t, err)
			assert.Equal(t, want, got)
			assert.Empty(t, stderr.String())
		})
	}
}
