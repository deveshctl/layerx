package cmd

import (
	"bytes"
	"errors"
	"fmt"
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
