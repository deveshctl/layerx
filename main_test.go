package main

import (
	"errors"
	"fmt"
	"runtime/debug"
	"testing"

	"github.com/deveshctl/layerx/cmd"
	"github.com/stretchr/testify/assert"
)

// fakeBuildInfo constructs a *debug.BuildInfo populated with the given
// vcs.* settings. Pass empty strings to omit a setting. modified is only
// included when explicitly true or false (use "" to skip the key entirely).
func fakeBuildInfo(revision, vcsTime, modified string) *debug.BuildInfo {
	info := &debug.BuildInfo{}
	if revision != "" {
		info.Settings = append(info.Settings, debug.BuildSetting{Key: "vcs.revision", Value: revision})
	}
	if vcsTime != "" {
		info.Settings = append(info.Settings, debug.BuildSetting{Key: "vcs.time", Value: vcsTime})
	}
	if modified != "" {
		info.Settings = append(info.Settings, debug.BuildSetting{Key: "vcs.modified", Value: modified})
	}
	return info
}

func TestResolveBuildInfo_HappyPath(t *testing.T) {
	info := fakeBuildInfo("2c3fc3cabcdef0123456789abcdef0123456789a", "2026-05-27T10:14:22Z", "false")
	commit, date := resolveBuildInfo(info)
	assert.Equal(t, "2c3fc3c", commit)
	assert.Equal(t, "2026-05-27", date)
}

func TestResolveBuildInfo_Modified(t *testing.T) {
	info := fakeBuildInfo("2c3fc3cabcdef0123456789abcdef0123456789a", "2026-05-27T10:14:22Z", "true")
	commit, date := resolveBuildInfo(info)
	assert.Equal(t, "2c3fc3c-dirty", commit)
	assert.Equal(t, "2026-05-27", date)
}

func TestResolveBuildInfo_NoVCS(t *testing.T) {
	info := fakeBuildInfo("", "", "")
	commit, date := resolveBuildInfo(info)
	assert.Equal(t, "", commit)
	assert.Equal(t, "", date)
}

func TestResolveBuildInfo_BadTime(t *testing.T) {
	info := fakeBuildInfo("2c3fc3cabcdef0123456789abcdef0123456789a", "not-a-date", "false")
	commit, date := resolveBuildInfo(info)
	assert.Equal(t, "2c3fc3c", commit)
	assert.Equal(t, "", date)
}

func TestResolveBuildInfo_ShortRev(t *testing.T) {
	// Revision shorter than 7 chars must not panic from a slice OOB.
	info := fakeBuildInfo("abc", "2026-05-27T10:14:22Z", "false")
	commit, date := resolveBuildInfo(info)
	assert.Equal(t, "abc", commit)
	assert.Equal(t, "2026-05-27", date)
}

func TestResolveBuildInfo_NilInfo(t *testing.T) {
	commit, date := resolveBuildInfo(nil)
	assert.Equal(t, "", commit)
	assert.Equal(t, "", date)
}

func TestResolveBuildInfo_RevisionOnly(t *testing.T) {
	// Caller will accept a populated commit even with no date; that's fine.
	info := fakeBuildInfo("2c3fc3cabcdef0123456789abcdef0123456789a", "", "")
	commit, date := resolveBuildInfo(info)
	assert.Equal(t, "2c3fc3c", commit)
	assert.Equal(t, "", date)
}

func TestResolveBuildInfo_TimeOnly(t *testing.T) {
	// Date is returned even without a commit.
	info := fakeBuildInfo("", "2026-05-27T10:14:22Z", "")
	commit, date := resolveBuildInfo(info)
	assert.Equal(t, "", commit)
	assert.Equal(t, "2026-05-27", date)
}

// --- exitCodeFor ---------------------------------------------------------------

func TestExitCodeFor_NilError(t *testing.T) {
	assert.Equal(t, 0, exitCodeFor(nil))
}

func TestExitCodeFor_ErrCIFailed(t *testing.T) {
	assert.Equal(t, 1, exitCodeFor(&cmd.ErrCIFailed{}))
}

func TestExitCodeFor_ErrCompareRegression(t *testing.T) {
	assert.Equal(t, 1, exitCodeFor(&cmd.ErrCompareRegression{}))
}

func TestExitCodeFor_NegativeExitCode(t *testing.T) {
	// Signal-killed processes return ExitCode -1; must be mapped to 1 so
	// os.Exit never receives a negative argument.
	assert.Equal(t, 1, exitCodeFor(&cmd.ErrBuildFailed{ExitCode: -1}))
}

func TestExitCodeFor_PositiveExitCodes(t *testing.T) {
	// Positive codes from the engine must pass through unchanged so CI scripts
	// can distinguish OOM (137), not-executable (126), and daemon failures (255).
	for _, code := range []int{1, 2, 126, 137, 255} {
		t.Run(fmt.Sprintf("code=%d", code), func(t *testing.T) {
			assert.Equal(t, code, exitCodeFor(&cmd.ErrBuildFailed{ExitCode: code}),
				"positive exit code %d must pass through unchanged", code)
		})
	}
}

func TestExitCodeFor_UnknownError(t *testing.T) {
	// Unrecognised errors map to 2 (usage / internal error), distinct from
	// rule failures (1) and clean runs (0).
	assert.Equal(t, 2, exitCodeFor(errors.New("unexpected failure")))
}
