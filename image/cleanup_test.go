package image

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLargeEfficientImageStillHasCleanupCandidates(t *testing.T) {
	a := &Analysis{Layers: []Layer{{Tree: makeTree(
		makeDir("app", "/app", makeFile("model", "/app/model", 1<<30)),
		makeDir("root", "/root", makeDir(".cache", "/root/.cache", makeDir("pip", "/root/.cache/pip",
			makeFile("wheel", "/root/.cache/pip/wheel", 1<<30)))),
	)}}}
	assert.Equal(t, 1.0, EfficiencyFromAnalysis(a).Score)
	assert.Zero(t, EfficiencyFromAnalysis(a).WastedBytes)
	got := CleanupCandidates(a)
	require.Len(t, got, 1)
	assert.Equal(t, "/root/.cache/pip", got[0].Path)
	assert.Equal(t, int64(1<<30), got[0].Bytes)
	assert.Contains(t, got[0].Reason, "pip")
}

func TestCleanupRootsAreConservative(t *testing.T) {
	for _, path := range []string{
		"/var/cache/apt/archives/pkg.deb", "/var/lib/apt/lists/index", "/var/cache/apk/pkg.apk",
		"/root/.cache/pip/wheel", "/home/app/.npm/_cacache/content", "/home/app/.cache/yarn/pkg",
		"/root/.cache/go-build/cache",
	} {
		root, reason := cleanupRoot(path)
		assert.NotEmpty(t, root, path)
		assert.NotEmpty(t, reason, path)
	}
	for _, path := range []string{
		"/app/node_modules/pkg", "/app/.cache/pip/data", "/var/cache/apt/archives-old/pkg",
		"/root/.cache/pip-tools/config", "/usr/lib/python/site-packages/pkg", "/app/model.bin",
	} {
		root, _ := cleanupRoot(path)
		assert.Empty(t, root, path)
	}
}

func TestCleanupCountsOnlyFinalLiveRegularPayload(t *testing.T) {
	cache := func(nodes ...*FileNode) *FileTree {
		return makeTree(makeDir("root", "/root", makeDir(".cache", "/root/.cache",
			makeDir("pip", "/root/.cache/pip", nodes...))))
	}
	link := makeFile("link", "/root/.cache/pip/link", 123)
	link.IsHardlink = true
	symlink := makeFile("sym", "/root/.cache/pip/sym", 456)
	symlink.Mode = fs.ModeSymlink
	a := &Analysis{Layers: []Layer{
		{Tree: cache(makeFile("a", "/root/.cache/pip/a", 100), makeFile("b", "/root/.cache/pip/b", 200))},
		{Tree: cache(makeFile("a", "/root/.cache/pip/a", 40), makeFile(".wh.b", "/root/.cache/pip/.wh.b", 0), link, symlink)},
		{Tree: makeTree(makeFile("app", "/app", 500))},
	}}
	got := CleanupCandidates(a)
	require.Len(t, got, 1)
	assert.Equal(t, int64(40), got[0].Bytes, "exclude earlier versions, deleted files, links and markers")
	assert.Equal(t, int64(300), EfficiencyFromAnalysis(a).WastedBytes, "candidates do not alter layer waste")
}

func TestCleanupAggregationAndOrder(t *testing.T) {
	tree := NewFileTree()
	for path, size := range map[string]int64{
		"root/.cache/pip/a": 30, "root/.cache/pip/b": 70,
		"home/app/.npm/_cacache/blob": 200, "var/cache/apk/pkg": 100,
	} {
		insertNode(tree.Root, path, size, false, false, "", 0644, 0, 0)
	}
	got := CleanupCandidates(&Analysis{Layers: []Layer{{Tree: tree}}})
	require.Len(t, got, 3)
	assert.Equal(t, "/home/app/.npm/_cacache", got[0].Path)
	assert.Equal(t, int64(200), got[0].Bytes)
	assert.Equal(t, "/root/.cache/pip", got[1].Path)
	assert.Equal(t, "/var/cache/apk", got[2].Path)
	assert.Empty(t, CleanupCandidates(nil))
}
