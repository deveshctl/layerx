package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetVersionInfo_FullInfo(t *testing.T) {
	t.Cleanup(func() { rootCmd.Version = "" })
	SetVersionInfo("0.13.0", "abc1234", "2026-05-25")
	assert.Equal(t, "0.13.0 (commit abc1234, built 2026-05-25)", rootCmd.Version)
}

func TestSetVersionInfo_DefaultsHidden(t *testing.T) {
	t.Cleanup(func() { rootCmd.Version = "" })
	// main.go's defaults: commit="none", date="unknown" when built without
	// -ldflags. SetVersionInfo must not splice "(commit none, built unknown)"
	// into the user-visible version string.
	SetVersionInfo("dev", "none", "unknown")
	assert.Equal(t, "dev", rootCmd.Version)
}

func TestSetVersionInfo_EmptyCommitHidden(t *testing.T) {
	t.Cleanup(func() { rootCmd.Version = "" })
	SetVersionInfo("0.1.0", "", "2026-05-25")
	assert.Equal(t, "0.1.0", rootCmd.Version)
}
