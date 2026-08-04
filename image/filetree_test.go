package image

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeEffectiveSizes_File(t *testing.T) {
	tree := makeTree(makeFile("a", "/a", 500))
	tree.computeEffectiveSizes()
	assert.Equal(t, int64(500), tree.Root.Children[0].EffectiveSize)
}

func TestComputeEffectiveSizes_Dir(t *testing.T) {
	tree := makeTree(
		makeDir("etc", "/etc",
			makeFile("passwd", "/etc/passwd", 100),
			makeFile("group", "/etc/group", 200),
		),
	)
	tree.computeEffectiveSizes()
	dir := tree.Root.Children[0]
	assert.Equal(t, int64(300), dir.EffectiveSize)
	assert.Equal(t, int64(100), dir.Children[0].EffectiveSize)
	assert.Equal(t, int64(200), dir.Children[1].EffectiveSize)
}

func TestComputeEffectiveSizes_RemovedNodeIsZero(t *testing.T) {
	f := makeFile("gone", "/gone", 1024)
	f.DiffType = Removed
	tree := makeTree(f)
	tree.computeEffectiveSizes()
	assert.Equal(t, int64(0), tree.Root.Children[0].EffectiveSize)
}

func TestComputeEffectiveSizes_DirExcludesRemovedChildren(t *testing.T) {
	kept := makeFile("kept", "/etc/kept", 100)
	removed := makeFile("gone", "/etc/gone", 900)
	removed.DiffType = Removed
	dir := makeDir("etc", "/etc", kept, removed)
	tree := makeTree(dir)
	tree.computeEffectiveSizes()
	assert.Equal(t, int64(100), tree.Root.Children[0].EffectiveSize)
}

func TestComputeEffectiveSizes_RemovedDirIsZero(t *testing.T) {
	child := makeFile("passwd", "/etc/passwd", 500)
	dir := makeDir("etc", "/etc", child)
	dir.DiffType = Removed
	tree := makeTree(dir)
	tree.computeEffectiveSizes()
	assert.Equal(t, int64(0), tree.Root.Children[0].EffectiveSize)
}

func TestComputeEffectiveSizes_NilRoot(t *testing.T) {
	tree := &FileTree{Root: nil}
	// must not panic
	tree.computeEffectiveSizes()
}

func TestComputeEffectiveSizes_MatchesNodeEffectiveSize(t *testing.T) {
	// EffectiveSize after computeEffectiveSizes must agree with the
	// reference nodeEffectiveSize used by the TUI sort tests.
	added := makeFile("a", "/d/a", 10)
	added.DiffType = Added
	modified := makeFile("m", "/d/m", 20)
	modified.DiffType = Modified
	unchanged := makeFile("u", "/d/u", 30)
	dir := makeDir("d", "/d", added, modified, unchanged)
	tree := makeTree(dir)
	tree.computeEffectiveSizes()

	d := tree.Root.Children[0]
	assert.Equal(t, int64(60), d.EffectiveSize)
	for _, c := range d.Children {
		assert.Equal(t, c.Size, c.EffectiveSize, "leaf node EffectiveSize must equal Size")
	}
}
