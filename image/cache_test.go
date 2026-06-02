package image

import (
	"encoding/gob"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheDTO_RoundTrip_PreservesPersistedFields(t *testing.T) {
	layers := []Layer{
		{
			Index: 0, ID: "aabb", Size: 1000, Command: "FROM alpine",
			Tree: makeTree(
				makeDir("bin", "/bin",
					makeFile("sh", "/bin/sh", 800),
				),
			),
		},
		{Index: 1, ID: "ccdd", Size: 0, Command: "RUN noop", Tree: nil},
	}
	// Simulate post-Stack values that must NOT round-trip.
	layers[0].NetDelta = 800
	layers[0].Tree.Root.Children[0].DiffType = Modified
	layers[0].Tree.Root.Children[0].IntroducedInLayer = 7

	dtos := toCachedLayers(layers)
	rehydrated := fromCachedLayers(dtos)

	require.Len(t, rehydrated, 2)
	assert.Equal(t, 0, rehydrated[0].Index)
	assert.Equal(t, "aabb", rehydrated[0].ID)
	assert.Equal(t, int64(1000), rehydrated[0].Size)
	assert.Equal(t, "FROM alpine", rehydrated[0].Command)
	require.NotNil(t, rehydrated[0].Tree)
	require.NotNil(t, rehydrated[0].Tree.Root)

	bin := rehydrated[0].Tree.Root.FindChild("bin")
	require.NotNil(t, bin)
	assert.True(t, bin.IsDir)
	sh := bin.FindChild("sh")
	require.NotNil(t, sh)
	assert.Equal(t, int64(800), sh.Size)

	// Derived/TUI fields are NOT preserved.
	assert.Equal(t, int64(0), rehydrated[0].NetDelta, "NetDelta is derived; not persisted")
	assert.Equal(t, Unchanged, sh.DiffType, "DiffType is recomputed by Stack()")
	assert.Equal(t, 0, sh.IntroducedInLayer, "IntroducedInLayer is recomputed by Stack()")

	// Layer 2 had nil tree; round-trip preserves that.
	assert.Nil(t, rehydrated[1].Tree)
}

func TestCacheDTO_RoundTrip_PreservesPermsAndIDs(t *testing.T) {
	root := &FileNode{Name: "/", Path: "/", IsDir: true, Mode: fs.FileMode(0o755), UID: 0, GID: 0}
	root.AddChild(&FileNode{Name: "secret", Path: "/secret", Size: 4, Mode: fs.FileMode(0o600), UID: 1000, GID: 1001})
	layers := []Layer{{Index: 0, ID: "ee", Size: 1, Tree: &FileTree{Root: root}}}

	dtos := toCachedLayers(layers)
	rehydrated := fromCachedLayers(dtos)

	secret := rehydrated[0].Tree.Root.FindChild("secret")
	require.NotNil(t, secret)
	assert.Equal(t, fs.FileMode(0o600), secret.Mode)
	assert.Equal(t, 1000, secret.UID)
	assert.Equal(t, 1001, secret.GID)
}

func TestCacheEnvelope_HoldsMetadata(t *testing.T) {
	now := time.Now().UTC()
	env := cacheEnvelope{
		Digest:        "ff00",
		SchemaVersion: SchemaVersion,
		CachedAt:      now,
	}
	assert.Equal(t, "ff00", env.Digest)
	assert.Equal(t, SchemaVersion, env.SchemaVersion)
	assert.Equal(t, now, env.CachedAt)
}

// TestCacheDTO_RoundTrip_AllPersistableFields is the silent-drift guard.
//
// IMPORTANT for future maintainers: when adding a field to FileNode or Layer
// that should persist across cache round-trips, you MUST update BOTH
// cache_dto.go (cachedLayer/cachedNode + the to/from converters) AND this
// test (set the field on the input and assert it on the rehydrated output).
// Fields deliberately excluded from persistence (DiffType, IntroducedInLayer,
// NetDelta) are covered by TestCacheDTO_RoundTrip_PreservesPersistedFields.
//
// The test exercises the on-disk round trip via saveCache + loadCache so a
// gob-incompatible shape (unexported field, channel, function) fails here
// rather than silently shipping.
func TestCacheDTO_RoundTrip_AllPersistableFields(t *testing.T) {
	child := &FileNode{
		Name:       "sh",
		Path:       "/bin/sh",
		Linkname:   "/bin/busybox",
		Size:       800,
		Mode:       fs.FileMode(0o755),
		UID:        1,
		GID:        2,
		IsDir:      false,
		IsHardlink: true,
	}
	bin := &FileNode{
		Name:  "bin",
		Path:  "/bin",
		Size:  4096,
		Mode:  fs.FileMode(0o755) | fs.ModeDir,
		UID:   3,
		GID:   4,
		IsDir: true,
	}
	bin.AddChild(child)
	root := &FileNode{
		Name:  "/",
		Path:  "/",
		Size:  0,
		Mode:  fs.FileMode(0o755) | fs.ModeDir,
		UID:   5,
		GID:   6,
		IsDir: true,
	}
	root.AddChild(bin)

	layers := []Layer{{
		Index:   7,
		ID:      "deadbeef",
		Size:    9999,
		Command: "RUN cp /bin/sh /bin/sh",
		Tree:    &FileTree{Root: root},
	}}

	cacheRoot := t.TempDir()
	digest := "sha256:driftguard"
	require.NoError(t, saveCache(cacheRoot, digest, layers, nil))
	rehydrated, ok, err := loadCache(cacheRoot, digest)
	require.NoError(t, err)
	require.True(t, ok)

	require.Len(t, rehydrated, 1)
	got := rehydrated[0]
	// Every field on Layer (except deliberately-excluded NetDelta).
	assert.Equal(t, 7, got.Index)
	assert.Equal(t, "deadbeef", got.ID)
	assert.Equal(t, int64(9999), got.Size)
	assert.Equal(t, "RUN cp /bin/sh /bin/sh", got.Command)
	require.NotNil(t, got.Tree)
	require.NotNil(t, got.Tree.Root)

	// Every persistable field on the root FileNode.
	gotRoot := got.Tree.Root
	assert.Equal(t, "/", gotRoot.Name)
	assert.Equal(t, "/", gotRoot.Path)
	assert.Equal(t, int64(0), gotRoot.Size)
	assert.Equal(t, fs.FileMode(0o755)|fs.ModeDir, gotRoot.Mode)
	assert.Equal(t, 5, gotRoot.UID)
	assert.Equal(t, 6, gotRoot.GID)
	assert.True(t, gotRoot.IsDir)
	require.Len(t, gotRoot.Children, 1)

	// Children persisted (via at least one nested child).
	gotBin := gotRoot.Children[0]
	assert.Equal(t, "bin", gotBin.Name)
	assert.Equal(t, "/bin", gotBin.Path)
	assert.Equal(t, int64(4096), gotBin.Size)
	assert.Equal(t, fs.FileMode(0o755)|fs.ModeDir, gotBin.Mode)
	assert.Equal(t, 3, gotBin.UID)
	assert.Equal(t, 4, gotBin.GID)
	assert.True(t, gotBin.IsDir)
	require.Len(t, gotBin.Children, 1)

	gotSh := gotBin.Children[0]
	assert.Equal(t, "sh", gotSh.Name)
	assert.Equal(t, "/bin/sh", gotSh.Path)
	assert.Equal(t, "/bin/busybox", gotSh.Linkname)
	assert.Equal(t, int64(800), gotSh.Size)
	assert.Equal(t, fs.FileMode(0o755), gotSh.Mode)
	assert.Equal(t, 1, gotSh.UID)
	assert.Equal(t, 2, gotSh.GID)
	assert.False(t, gotSh.IsDir)
	assert.True(t, gotSh.IsHardlink)
	assert.Empty(t, gotSh.Children)
}

// TestCacheDTO_RoundTrip_NilRoot_BecomesNilTree documents an asymmetry:
// a Layer with Tree: &FileTree{Root: nil} (non-nil wrapper, nil Root) round-trips
// to Tree: nil. toCachedNode(nil) returns nil, so cachedLayer.Tree is nil, and
// fromCachedLayers leaves the rehydrated Layer.Tree at its zero value.
func TestCacheDTO_RoundTrip_NilRoot_BecomesNilTree(t *testing.T) {
	layers := []Layer{{Index: 0, ID: "aa", Tree: &FileTree{Root: nil}}}

	rehydrated := fromCachedLayers(toCachedLayers(layers))

	require.Len(t, rehydrated, 1)
	assert.Nil(t, rehydrated[0].Tree)
}

func TestCacheDir_PrefersLAYERX_CACHE_DIR(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LAYERX_CACHE_DIR", dir)
	got, err := CacheDir()
	require.NoError(t, err)
	assert.Equal(t, dir, got)
}

func TestCacheDir_FallsBackToUserCacheDir(t *testing.T) {
	t.Setenv("LAYERX_CACHE_DIR", "")
	got, err := CacheDir()
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(got), "cache dir must be absolute")
	assert.Equal(t, "layerx", filepath.Base(got))

	uc, err := os.UserCacheDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(uc, "layerx"), got)
}

func TestCacheDir_RejectsUnusableOverride(t *testing.T) {
	// Pointing at a path that is a regular file is not a usable directory.
	f, err := os.CreateTemp(t.TempDir(), "not-a-dir")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	t.Setenv("LAYERX_CACHE_DIR", f.Name())

	got, err := CacheDir()
	require.NoError(t, err, "fallback should succeed even if override is bad")
	assert.NotEqual(t, f.Name(), got)
}

func TestSaveLoadCache_RoundTrip(t *testing.T) {
	root := t.TempDir()
	digest := "sha256:abcdef"

	layers := []Layer{
		{
			Index: 0, ID: "aabb", Size: 100, Command: "FROM alpine",
			Tree: makeTree(makeFile("a", "/a", 50)),
		},
	}
	require.NoError(t, saveCache(root, digest, layers, nil))

	got, ok, err := loadCache(root, digest)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, got, 1)
	assert.Equal(t, "aabb", got[0].ID)
	require.NotNil(t, got[0].Tree)
	assert.NotNil(t, got[0].Tree.Root.FindChild("a"))
}

func TestLoadCache_Miss_NoFile(t *testing.T) {
	root := t.TempDir()
	got, ok, err := loadCache(root, "sha256:nope")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestLoadCache_SchemaMismatch_DeletesAndMisses(t *testing.T) {
	root := t.TempDir()
	digest := "sha256:badschema"
	path, err := cachePath(root, digest)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))

	f, err := os.Create(path)
	require.NoError(t, err)
	norm, err := normalizeDigest(digest)
	require.NoError(t, err)
	env := cacheEnvelope{
		Digest:        norm,
		SchemaVersion: SchemaVersion + 1,
		CachedAt:      time.Now().UTC(),
		Layers:        nil,
	}
	require.NoError(t, gob.NewEncoder(f).Encode(env))
	require.NoError(t, f.Close())

	_, ok, err := loadCache(root, digest)
	require.NoError(t, err)
	assert.False(t, ok, "schema mismatch must be a miss")
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "miss must delete the bad file")
}

func TestLoadCache_DigestMismatch_DeletesAndMisses(t *testing.T) {
	root := t.TempDir()
	dirDigest := "sha256:aaaaaa"
	path, err := cachePath(root, dirDigest)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))

	f, err := os.Create(path)
	require.NoError(t, err)
	other, err := normalizeDigest("sha256:bbbbbb")
	require.NoError(t, err)
	env := cacheEnvelope{
		Digest:        other,
		SchemaVersion: SchemaVersion,
		CachedAt:      time.Now().UTC(),
		Layers:        nil,
	}
	require.NoError(t, gob.NewEncoder(f).Encode(env))
	require.NoError(t, f.Close())

	_, ok, err := loadCache(root, dirDigest)
	require.NoError(t, err)
	assert.False(t, ok, "digest mismatch must be a miss")
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestLoadCache_CorruptFile_DeletesAndMisses(t *testing.T) {
	root := t.TempDir()
	digest := "sha256:corrupt"
	path, err := cachePath(root, digest)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("not a gob blob"), 0o600))

	_, ok, err := loadCache(root, digest)
	require.NoError(t, err)
	assert.False(t, ok)
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestSaveCache_NoTempFileLingers(t *testing.T) {
	root := t.TempDir()
	digest := "sha256:keep"
	require.NoError(t, saveCache(root, digest, nil, nil))

	path, err := cachePath(root, digest)
	require.NoError(t, err)
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), "layers.gob.tmp-"),
			"temp file lingered: %s", e.Name())
	}
}

func TestNormalizeDigest_StripsSha256Prefix(t *testing.T) {
	got, err := normalizeDigest("sha256:abcdef")
	require.NoError(t, err)
	assert.Equal(t, "abcdef", got)

	got, err = normalizeDigest("abcdef")
	require.NoError(t, err)
	assert.Equal(t, "abcdef", got)
}

func TestNormalizeDigest_RejectsUnsafe(t *testing.T) {
	cases := []string{
		"",
		"sha256:",
		".",
		"..",
		"sha256:..",
		"../../etc/passwd",
		"sha256:../etc",
		"foo/bar",
		`foo\bar`,
		"prefix..suffix",
	}
	for _, c := range cases {
		_, err := normalizeDigest(c)
		assert.ErrorIs(t, err, errBadDigest, "should reject %q", c)
	}
}

func TestSaveCache_RejectsBadDigest(t *testing.T) {
	root := t.TempDir()
	err := saveCache(root, "", nil, nil)
	assert.ErrorIs(t, err, errBadDigest)
	err = saveCache(root, "../escape", nil, nil)
	assert.ErrorIs(t, err, errBadDigest)
}

func TestLoadCache_TransientIOError_KeepsFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0o000 does not block file open on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root; chmod 0o000 does not block open")
	}
	root := t.TempDir()
	digest := "sha256:transient"
	path, err := cachePath(root, digest)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))

	require.NoError(t, os.WriteFile(path, []byte("anything"), 0o600))
	require.NoError(t, os.Chmod(path, 0o000))
	defer func() { _ = os.Chmod(path, 0o600) }()

	_, ok, err := loadCache(root, digest)
	require.Error(t, err, "0o000 file must surface as transient I/O error")
	assert.False(t, ok)

	require.NoError(t, os.Chmod(path, 0o600))
	_, statErr := os.Stat(path)
	assert.NoError(t, statErr, "transient I/O failure must NOT evict cache")
}

// writeFakeCache creates {root}/{digest}/layers.gob with the given size
// and mtime. Used to seed prune fixtures without invoking saveCache.
// The digest must be a valid normalized form (no "sha256:" prefix).
func writeFakeCache(t *testing.T, root, digest string, size int64, mtime time.Time) {
	t.Helper()
	dir := filepath.Join(root, digest)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	path := filepath.Join(dir, "layers.gob")
	// Content must decode as a non-empty cacheEnvelope to satisfy any
	// future loadCache call against the fixture; for prune tests we
	// only care about file size + mtime, but writing a valid envelope
	// keeps the fixture realistic.
	env := cacheEnvelope{
		Digest:        digest,
		SchemaVersion: SchemaVersion,
		CachedAt:      mtime,
		Layers:        []cachedLayer{{Index: 0, ID: digest, Size: size}},
	}
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, gob.NewEncoder(f).Encode(env))
	require.NoError(t, f.Close())
	// Pad or truncate to exactly `size` bytes so the size-cap tests can
	// reason about totals deterministically.
	require.NoError(t, os.Truncate(path, size))
	require.NoError(t, os.Chtimes(path, mtime, mtime))
}

// drainProgress collects all events from ch without blocking the
// caller. Returns when no event is available. Must be called after
// the producing call has returned.
func drainProgress(ch chan ProgressEvent) []ProgressEvent {
	var out []ProgressEvent
	for {
		select {
		case ev := <-ch:
			out = append(out, ev)
		default:
			return out
		}
	}
}

// withFrozenNow overwrites nowFn for the duration of the test and
// restores it on cleanup. Returns the frozen instant for caller use.
func withFrozenNow(t *testing.T) time.Time {
	t.Helper()
	frozen := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	prev := nowFn
	nowFn = func() time.Time { return frozen }
	t.Cleanup(func() { nowFn = prev })
	return frozen
}

func TestPruneCache_TTL_EvictsOldKeepsFresh(t *testing.T) {
	root := t.TempDir()
	now := withFrozenNow(t)

	old := strings.Repeat("a", 64)
	fresh := strings.Repeat("b", 64)
	writeFakeCache(t, root, old, 1024, now.Add(-31*24*time.Hour))
	writeFakeCache(t, root, fresh, 1024, now.Add(-1*time.Hour))

	t.Setenv("LAYERX_CACHE_TTL_DAYS", "30")
	t.Setenv("LAYERX_CACHE_MAX_BYTES", "0")

	pruneCache(root, "", nil)

	_, errOld := os.Stat(filepath.Join(root, old))
	assert.True(t, os.IsNotExist(errOld), "stale digest should have been evicted")
	_, errFresh := os.Stat(filepath.Join(root, fresh))
	assert.NoError(t, errFresh, "fresh digest should survive")
}

func TestPruneCache_TTL_RespectsKeepDigest(t *testing.T) {
	root := t.TempDir()
	now := withFrozenNow(t)

	keep := strings.Repeat("a", 64)
	other := strings.Repeat("b", 64)
	writeFakeCache(t, root, keep, 1024, now.Add(-31*24*time.Hour))
	writeFakeCache(t, root, other, 1024, now.Add(-31*24*time.Hour))

	t.Setenv("LAYERX_CACHE_TTL_DAYS", "30")
	t.Setenv("LAYERX_CACHE_MAX_BYTES", "0")

	pruneCache(root, keep, nil)

	_, errKeep := os.Stat(filepath.Join(root, keep))
	assert.NoError(t, errKeep, "keepDigest must survive even when TTL-stale")
	_, errOther := os.Stat(filepath.Join(root, other))
	assert.True(t, os.IsNotExist(errOther), "other stale entry should be evicted")
}

func TestPruneCache_SizeCap_EvictsOldestFirst(t *testing.T) {
	root := t.TempDir()
	now := withFrozenNow(t)

	a := strings.Repeat("a", 64)
	b := strings.Repeat("b", 64)
	c := strings.Repeat("c", 64)
	const sz = 40 * 1024 * 1024
	writeFakeCache(t, root, a, sz, now.Add(-3*time.Hour))
	writeFakeCache(t, root, b, sz, now.Add(-2*time.Hour))
	writeFakeCache(t, root, c, sz, now.Add(-1*time.Hour))

	t.Setenv("LAYERX_CACHE_TTL_DAYS", "0")
	t.Setenv("LAYERX_CACHE_MAX_BYTES", "62914560") // 60 MB

	pruneCache(root, "", nil)

	_, errA := os.Stat(filepath.Join(root, a))
	_, errB := os.Stat(filepath.Join(root, b))
	_, errC := os.Stat(filepath.Join(root, c))
	assert.True(t, os.IsNotExist(errA), "oldest should be evicted")
	assert.True(t, os.IsNotExist(errB), "second-oldest should be evicted")
	assert.NoError(t, errC, "freshest should survive")
}

func TestPruneCache_SizeCap_RespectsKeepDigest_WarnsOnOversizedKeep(t *testing.T) {
	root := t.TempDir()
	now := withFrozenNow(t)

	keep := strings.Repeat("a", 64)
	other := strings.Repeat("b", 64)
	writeFakeCache(t, root, keep, 100*1024*1024, now.Add(-1*time.Hour))
	writeFakeCache(t, root, other, 10*1024*1024, now.Add(-2*time.Hour))

	t.Setenv("LAYERX_CACHE_TTL_DAYS", "0")
	t.Setenv("LAYERX_CACHE_MAX_BYTES", "52428800") // 50 MB

	progress := make(chan ProgressEvent, 16)
	pruneCache(root, keep, progress)

	_, errKeep := os.Stat(filepath.Join(root, keep))
	_, errOther := os.Stat(filepath.Join(root, other))
	assert.NoError(t, errKeep, "keepDigest must survive")
	assert.True(t, os.IsNotExist(errOther), "other must be evicted to make room")

	events := drainProgress(progress)
	var found bool
	for _, ev := range events {
		if ev.Phase == PhaseCacheWarn && strings.Contains(ev.Message, "exceeded by single entry") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected one PhaseCacheWarn about single-entry overflow; got: %+v", events)
}

func TestPruneCache_BothLimitsZero_IsNoop(t *testing.T) {
	root := t.TempDir()
	now := withFrozenNow(t)

	d := strings.Repeat("a", 64)
	writeFakeCache(t, root, d, 10*1024*1024*1024, now.Add(-365*24*time.Hour))

	t.Setenv("LAYERX_CACHE_TTL_DAYS", "0")
	t.Setenv("LAYERX_CACHE_MAX_BYTES", "0")

	pruneCache(root, "", nil)

	_, err := os.Stat(filepath.Join(root, d))
	assert.NoError(t, err, "with both limits disabled, nothing should be evicted")
}

func TestLoadPruneLimits_EnvVarParsing(t *testing.T) {
	cases := []struct {
		name        string
		ttlEnv      string
		maxEnv      string
		wantTTL     time.Duration
		wantMax     int64
		wantWarnSub string
	}{
		{
			name:    "defaults when unset",
			wantTTL: 30 * 24 * time.Hour,
			wantMax: 1 << 30,
		},
		{
			name:    "ttl override 7 days",
			ttlEnv:  "7",
			wantTTL: 7 * 24 * time.Hour,
			wantMax: 1 << 30,
		},
		{
			name:    "max override zero disables cap",
			maxEnv:  "0",
			wantTTL: 30 * 24 * time.Hour,
			wantMax: 0,
		},
		{
			name:        "ttl unparseable falls back, warns",
			ttlEnv:      "foo",
			wantTTL:     30 * 24 * time.Hour,
			wantMax:     1 << 30,
			wantWarnSub: "ignoring LAYERX_CACHE_TTL_DAYS",
		},
		{
			name:        "ttl negative falls back, warns",
			ttlEnv:      "-3",
			wantTTL:     30 * 24 * time.Hour,
			wantMax:     1 << 30,
			wantWarnSub: "negative",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.ttlEnv == "" {
				t.Setenv("LAYERX_CACHE_TTL_DAYS", "")
			} else {
				t.Setenv("LAYERX_CACHE_TTL_DAYS", tc.ttlEnv)
			}
			if tc.maxEnv == "" {
				t.Setenv("LAYERX_CACHE_MAX_BYTES", "")
			} else {
				t.Setenv("LAYERX_CACHE_MAX_BYTES", tc.maxEnv)
			}

			progress := make(chan ProgressEvent, 16)
			ttl, max := loadPruneLimits(progress)
			assert.Equal(t, tc.wantTTL, ttl)
			assert.Equal(t, tc.wantMax, max)

			events := drainProgress(progress)
			if tc.wantWarnSub == "" {
				assert.Empty(t, events, "expected no warnings")
			} else {
				var matched bool
				for _, ev := range events {
					if ev.Phase == PhaseCacheWarn && strings.Contains(ev.Message, tc.wantWarnSub) {
						matched = true
						break
					}
				}
				assert.True(t, matched, "expected warn containing %q; got %+v", tc.wantWarnSub, events)
			}
		})
	}
}

func TestPruneCache_ReadDirFailure_WarnsAndReturns(t *testing.T) {
	withFrozenNow(t)

	tmp, err := os.CreateTemp(t.TempDir(), "not-a-dir-*")
	require.NoError(t, err)
	require.NoError(t, tmp.Close())

	t.Setenv("LAYERX_CACHE_TTL_DAYS", "30")
	t.Setenv("LAYERX_CACHE_MAX_BYTES", "0")

	progress := make(chan ProgressEvent, 16)
	require.NotPanics(t, func() {
		pruneCache(tmp.Name(), "", progress)
	})

	events := drainProgress(progress)
	var found bool
	for _, ev := range events {
		if ev.Phase == PhaseCacheWarn && strings.Contains(ev.Message, "cache prune skipped") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected 'cache prune skipped' warn; got %+v", events)
}

func TestPruneCache_BrokenDigestDir_SilentlySkipped(t *testing.T) {
	root := t.TempDir()
	now := withFrozenNow(t)

	stale := strings.Repeat("a", 64)
	writeFakeCache(t, root, stale, 1024, now.Add(-31*24*time.Hour))

	broken := strings.Repeat("b", 64)
	require.NoError(t, os.MkdirAll(filepath.Join(root, broken), 0o700))

	require.NoError(t, os.MkdirAll(filepath.Join(root, "not-a-digest"), 0o700))

	t.Setenv("LAYERX_CACHE_TTL_DAYS", "30")
	t.Setenv("LAYERX_CACHE_MAX_BYTES", "0")

	progress := make(chan ProgressEvent, 16)
	pruneCache(root, "", progress)

	_, errStale := os.Stat(filepath.Join(root, stale))
	assert.True(t, os.IsNotExist(errStale))
	_, errBroken := os.Stat(filepath.Join(root, broken))
	assert.NoError(t, errBroken, "broken digest dir should be untouched")
	_, errForeign := os.Stat(filepath.Join(root, "not-a-digest"))
	assert.NoError(t, errForeign, "non-digest dir should be untouched")

	for _, ev := range drainProgress(progress) {
		assert.NotEqual(t, PhaseCacheWarn, ev.Phase,
			"per-entry skips must not warn; saw: %+v", ev)
	}
}

func TestPruneCache_ForeignFilesAtRoot_Untouched(t *testing.T) {
	root := t.TempDir()
	now := withFrozenNow(t)

	stale := strings.Repeat("a", 64)
	writeFakeCache(t, root, stale, 1024, now.Add(-31*24*time.Hour))

	readme := filepath.Join(root, "README.md")
	require.NoError(t, os.WriteFile(readme, []byte("hands off"), 0o644))

	t.Setenv("LAYERX_CACHE_TTL_DAYS", "30")
	t.Setenv("LAYERX_CACHE_MAX_BYTES", "0")

	pruneCache(root, "", nil)

	_, errStale := os.Stat(filepath.Join(root, stale))
	assert.True(t, os.IsNotExist(errStale))
	_, errReadme := os.Stat(readme)
	assert.NoError(t, errReadme, "foreign files must not be touched")
}

func TestSaveCache_TriggersPrune(t *testing.T) {
	root := t.TempDir()
	now := withFrozenNow(t)

	// Pre-seed a stale digest that should be evicted by the cap.
	stale := strings.Repeat("a", 64)
	writeFakeCache(t, root, stale, 5*1024*1024, now.Add(-31*24*time.Hour))

	// Now write a fresh entry via saveCache. With a tight cap the
	// stale one must go; the fresh one must survive.
	t.Setenv("LAYERX_CACHE_TTL_DAYS", "30")
	t.Setenv("LAYERX_CACHE_MAX_BYTES", "1048576") // 1 MB

	freshDigest := strings.Repeat("c", 64)
	layers := []Layer{{
		Index: 0, ID: freshDigest, Size: 1, Command: "FROM scratch",
		Tree: makeTree(makeFile("/x", "/x", 1)),
	}}
	require.NoError(t, saveCache(root, freshDigest, layers, nil))

	// Stale evicted by TTL (also would by size cap).
	_, errStale := os.Stat(filepath.Join(root, stale))
	assert.True(t, os.IsNotExist(errStale), "stale digest should have been pruned")

	// Fresh kept.
	_, errFresh := os.Stat(filepath.Join(root, freshDigest, "layers.gob"))
	assert.NoError(t, errFresh, "fresh digest must survive its own write")
}
