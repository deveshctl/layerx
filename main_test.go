package main

import (
	"runtime/debug"
	"testing"

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
