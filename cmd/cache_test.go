package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOlderThan_Happy(t *testing.T) {
	cases := map[string]time.Duration{
		"30d": 30 * 24 * time.Hour,
		"12h": 12 * time.Hour,
		"2w":  2 * 7 * 24 * time.Hour,
		"45s": 45 * time.Second,
		"90m": 90 * time.Minute,
		"1d":  24 * time.Hour,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			got, err := parseOlderThan(in)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestParseOlderThan_Rejects(t *testing.T) {
	cases := []struct {
		in       string
		wantSubs string
	}{
		{"", "empty"},
		{"0d", "must be positive"},
		{"-3d", "must be positive"},
		{"5", "missing unit"},
		{"1mo", "unsupported unit"},
		{"1y", "unsupported unit"},
		{"30dh", "unsupported unit"},
		{"abc", "missing leading number"},
		{"d", "missing leading number"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			_, err := parseOlderThan(tc.in)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantSubs)
		})
	}
}

func TestParseOlderThan_Overflow(t *testing.T) {
	// 9223372036854775807 ns ≈ 292 years. 1000 years in weeks overflows.
	_, err := parseOlderThan("100000000w")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "overflow")
}

func TestRenderListTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	renderListTable(&buf, "/tmp/cache", nil)
	out := buf.String()
	assert.Contains(t, out, "Cache directory: /tmp/cache")
	assert.Contains(t, out, "Total: 0 entries, 0 B")
	assert.NotContains(t, out, "DIGEST")
}

func TestRenderListTable_Populated(t *testing.T) {
	freezeRelativeTime(t)

	entries := []image.CacheEntry{
		{Digest: strings.Repeat("a", 64), Size: 12 * 1024 * 1024, CachedAt: nowFn().Add(-2 * time.Hour)},
		{Digest: strings.Repeat("b", 64), Size: 148 * 1024 * 1024, CachedAt: nowFn().Add(-4 * 24 * time.Hour)},
		{Digest: strings.Repeat("c", 64), Size: 3 * 1024 * 1024, CachedAt: nowFn().Add(-21 * 24 * time.Hour)},
	}
	var buf bytes.Buffer
	renderListTable(&buf, "/cache", entries)
	out := buf.String()
	assert.Contains(t, out, "DIGEST")
	assert.Contains(t, out, "SIZE")
	assert.Contains(t, out, "CACHED")
	assert.Contains(t, out, "aaaaaaaaaaaa…")
	assert.Contains(t, out, "2 hours ago")
	assert.Contains(t, out, "4 days ago")
	assert.Contains(t, out, "3 weeks ago")
	assert.Contains(t, out, "Total: 3 entries,")
}

func TestRenderPruneResult_RealRun(t *testing.T) {
	res := image.PruneResult{
		Removed: []image.CacheEntry{
			{Digest: strings.Repeat("a", 64), Size: 12 * 1024 * 1024},
			{Digest: strings.Repeat("b", 64), Size: 3 * 1024 * 1024},
		},
	}
	var buf bytes.Buffer
	renderPruneResult(&buf, res, false)
	out := buf.String()
	assert.Contains(t, out, "Removed aaaaaaaaaaaa…")
	assert.Contains(t, out, "Removed bbbbbbbbbbbb…")
	assert.Contains(t, out, "Removed 2 entries, freed")
	assert.NotContains(t, out, "Would")
}

func TestRenderPruneResult_DryRun(t *testing.T) {
	res := image.PruneResult{
		Removed: []image.CacheEntry{
			{Digest: strings.Repeat("a", 64), Size: 12 * 1024 * 1024},
		},
	}
	var buf bytes.Buffer
	renderPruneResult(&buf, res, true)
	out := buf.String()
	assert.Contains(t, out, "Would remove aaaaaaaaaaaa…")
	assert.Contains(t, out, "Would remove 1 entries, freeing")
	assert.NotContains(t, out, "Removed 1 entries")
}

func TestRenderPruneResult_Empty(t *testing.T) {
	var buf bytes.Buffer
	renderPruneResult(&buf, image.PruneResult{}, false)
	out := buf.String()
	assert.Equal(t, "Nothing to prune.\n", out)
}

func TestRelativeTime(t *testing.T) {
	freezeRelativeTime(t)
	now := nowFn()

	cases := []struct {
		name string
		t    time.Time
		want string
	}{
		{"just now", now.Add(-30 * time.Second), "just now"},
		{"5 minutes", now.Add(-5 * time.Minute), "5 minutes ago"},
		{"1 minute (singular)", now.Add(-1 * time.Minute), "1 minute ago"},
		{"3 hours", now.Add(-3 * time.Hour), "3 hours ago"},
		{"2 days", now.Add(-2 * 24 * time.Hour), "2 days ago"},
		{"1 week (singular)", now.Add(-8 * 24 * time.Hour), "1 week ago"},
		{"5 months", now.Add(-150 * 24 * time.Hour), "5 months ago"},
		{"2 years", now.Add(-2 * 365 * 24 * time.Hour), "2 years ago"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, relativeTime(tc.t))
		})
	}
}

func TestCachePrune_FlagsMutuallyExclusive(t *testing.T) {
	// Reset flags between cobra tests; package-level flag vars are global.
	resetCacheFlags(t)

	rootCmd.SetArgs([]string{"cache", "prune", "--older-than", "7d", "--all"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	err := rootCmd.Execute()
	require.Error(t, err)
	// cobra's MarkFlagsMutuallyExclusive emits this exact phrasing.
	assert.Contains(t, err.Error(), "none of the others can be")
}

func TestCacheList_EndToEnd(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LAYERX_CACHE_DIR", root)
	resetCacheFlags(t)

	// Seed two digests via saveCache (the public route image-package
	// callers take). We can't call image.saveCache (unexported), so seed
	// via image.PruneCache's input shape: write the gob ourselves with
	// the same writeFakeCache pattern.
	seedFakeCache(t, root, strings.Repeat("a", 64), 1024, time.Now().Add(-1*time.Hour))
	seedFakeCache(t, root, strings.Repeat("b", 64), 2048, time.Now().Add(-2*time.Hour))

	rootCmd.SetArgs([]string{"cache", "list"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	err := rootCmd.Execute()
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "Cache directory: "+root)
	assert.Contains(t, out, "DIGEST")
	assert.Contains(t, out, "Total: 2 entries,")
}

func TestCachePrune_AllDryRun_DoesNotTouchDisk(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LAYERX_CACHE_DIR", root)
	resetCacheFlags(t)

	digestA := strings.Repeat("a", 64)
	digestB := strings.Repeat("b", 64)
	seedFakeCache(t, root, digestA, 1024, time.Now().Add(-1*time.Hour))
	seedFakeCache(t, root, digestB, 2048, time.Now().Add(-2*time.Hour))

	rootCmd.SetArgs([]string{"cache", "prune"}) // bare prune = dry run
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	err := rootCmd.Execute()
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "Would remove")
	assert.Contains(t, out, "freeing")
	// Bare prune is purely a dry run; the hint must point at the commands
	// that actually remove entries so users don't think prune ran.
	assert.Contains(t, out, "--all to remove every entry")
	assert.Contains(t, out, "--older-than DURATION")

	// Disk untouched.
	for _, d := range []string{digestA, digestB} {
		_, statErr := os.Stat(filepath.Join(root, d, "layers.gob"))
		assert.NoError(t, statErr, "dry run must not remove %s", d)
	}
}

func TestCachePrune_ExplicitDryRun_NoHint(t *testing.T) {
	// `--all --dry-run` is an explicit scope — the user already knows
	// what they want. The "re-run with --all" hint is only useful for
	// the bare `prune` case.
	root := t.TempDir()
	t.Setenv("LAYERX_CACHE_DIR", root)
	resetCacheFlags(t)

	seedFakeCache(t, root, strings.Repeat("a", 64), 1024, time.Now().Add(-1*time.Hour))

	rootCmd.SetArgs([]string{"cache", "prune", "--all", "--dry-run"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	err := rootCmd.Execute()
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "Would remove")
	assert.NotContains(t, out, "Re-run with --all")
	assert.NotContains(t, out, "--older-than DURATION")
}

// freezeRelativeTime overrides cmd's nowFn for the test and restores it
// on cleanup. Tests using it MUST NOT call t.Parallel.
func freezeRelativeTime(t *testing.T) {
	t.Helper()
	frozen := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	prev := nowFn
	nowFn = func() time.Time { return frozen }
	t.Cleanup(func() { nowFn = prev })
}

// resetCacheFlags clears the package-level prune flag vars AND cobra's
// internal `Changed` state on each prune flag between cobra-based test
// invocations. Without the Changed reset, a prior test that passed
// --older-than/--all leaves those flags marked as set, and subsequent
// tests trip the mutual-exclusion check on a bare `cache prune`.
func resetCacheFlags(t *testing.T) {
	t.Helper()
	prevOlder := flagCacheOlderThan
	prevAll := flagCacheAll
	prevDry := flagCacheDryRun
	flagCacheOlderThan = ""
	flagCacheAll = false
	flagCacheDryRun = false
	for _, name := range []string{"older-than", "all", "dry-run"} {
		if f := cachePruneCmd.Flags().Lookup(name); f != nil {
			f.Changed = false
		}
	}
	t.Cleanup(func() {
		flagCacheOlderThan = prevOlder
		flagCacheAll = prevAll
		flagCacheDryRun = prevDry
		for _, name := range []string{"older-than", "all", "dry-run"} {
			if f := cachePruneCmd.Flags().Lookup(name); f != nil {
				f.Changed = false
			}
		}
	})
}

// seedFakeCache writes {root}/{digest}/layers.gob with the given size
// and mtime. Mirrors image.writeFakeCache (which is test-internal to
// the image package and not visible from cmd/_test.go).
func seedFakeCache(t *testing.T, root, digest string, size int64, mtime time.Time) {
	t.Helper()
	dir := filepath.Join(root, digest)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	path := filepath.Join(dir, "layers.gob")
	require.NoError(t, os.WriteFile(path, make([]byte, size), 0o600))
	require.NoError(t, os.Chtimes(path, mtime, mtime))
}
