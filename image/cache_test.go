package image

import (
	"io/fs"
	"os"
	"path/filepath"
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
		Layers:        nil,
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
func TestCacheDTO_RoundTrip_AllPersistableFields(t *testing.T) {
	child := &FileNode{
		Name:  "sh",
		Path:  "/bin/sh",
		Size:  800,
		Mode:  fs.FileMode(0o755),
		UID:   1,
		GID:   2,
		IsDir: false,
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

	dtos := toCachedLayers(layers)
	rehydrated := fromCachedLayers(dtos)

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
	assert.Equal(t, int64(800), gotSh.Size)
	assert.Equal(t, fs.FileMode(0o755), gotSh.Mode)
	assert.Equal(t, 1, gotSh.UID)
	assert.Equal(t, 2, gotSh.GID)
	assert.False(t, gotSh.IsDir)
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
