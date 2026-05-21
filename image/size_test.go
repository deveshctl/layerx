package image

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTreeLiveFileBytes_NilTree(t *testing.T) {
	assert.Equal(t, int64(0), TreeLiveFileBytes(nil))
	assert.Equal(t, int64(0), TreeLiveFileBytes(&FileTree{Root: nil}))
}

func TestTreeLiveFileBytes_FilesOnly(t *testing.T) {
	tree := makeTree(
		makeFile("a", "/a", 100),
		makeFile("b", "/b", 250),
		makeDir("etc", "/etc",
			makeFile("passwd", "/etc/passwd", 50),
		),
	)
	assert.Equal(t, int64(400), TreeLiveFileBytes(tree))
}

func TestTreeLiveFileBytes_SkipsDirectories(t *testing.T) {
	dir := makeDir("etc", "/etc")
	dir.Size = 4096
	tree := makeTree(
		dir,
		makeFile("a", "/a", 100),
	)
	assert.Equal(t, int64(100), TreeLiveFileBytes(tree))
}

func TestTreeLiveFileBytes_SkipsRemovedFile(t *testing.T) {
	removed := makeFile("gone", "/gone", 999)
	removed.DiffType = Removed
	tree := makeTree(
		makeFile("kept", "/kept", 100),
		removed,
	)
	assert.Equal(t, int64(100), TreeLiveFileBytes(tree))
}

func TestTreeLiveFileBytes_DoesNotDescendRemovedSubtree(t *testing.T) {
	dir := makeDir("etc", "/etc",
		makeFile("passwd", "/etc/passwd", 500),
	)
	dir.DiffType = Removed
	tree := makeTree(
		makeFile("kept", "/kept", 100),
		dir,
	)
	assert.Equal(t, int64(100), TreeLiveFileBytes(tree))
}
