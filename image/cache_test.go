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
	require.NoError(t, saveCache(cacheRoot, digest, layers))
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
	require.NoError(t, saveCache(root, digest, layers))

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
	require.NoError(t, saveCache(root, digest, nil))

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
	err := saveCache(root, "", nil)
	assert.ErrorIs(t, err, errBadDigest)
	err = saveCache(root, "../escape", nil)
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

	require.NoError(t, os.WriteFile(path, []byte("anything"), 0o000))
	defer func() { _ = os.Chmod(path, 0o600) }()

	_, ok, err := loadCache(root, digest)
	require.Error(t, err, "0o000 file must surface as transient I/O error")
	assert.False(t, ok)

	require.NoError(t, os.Chmod(path, 0o600))
	_, statErr := os.Stat(path)
	assert.NoError(t, statErr, "transient I/O failure must NOT evict cache")
}
