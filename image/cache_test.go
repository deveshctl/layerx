package image

import (
	"io/fs"
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
	assert.WithinDuration(t, now, env.CachedAt, time.Second)
}
