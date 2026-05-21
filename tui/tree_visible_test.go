package tui

import (
	"testing"

	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testTreeFixture() *image.FileNode {
	root := &image.FileNode{Name: "/", Path: "/", IsDir: true}
	usr := &image.FileNode{Name: "usr", Path: "/usr", IsDir: true}
	bin := &image.FileNode{Name: "bin", Path: "/usr/bin", IsDir: true}
	sh := &image.FileNode{Name: "sh", Path: "/usr/bin/sh"}
	etc := &image.FileNode{Name: "etc", Path: "/etc", IsDir: true}

	usr.AddChild(bin)
	bin.AddChild(sh)
	root.AddChild(usr)
	root.AddChild(etc)
	return root
}

func TestVisibleTreeEmptyCollapseMatchesFlatten(t *testing.T) {
	root := testTreeFixture()
	flat := flattenTree(root)
	visible := visibleTree(root, nil)
	require.Equal(t, len(flat), len(visible))
	for i := range flat {
		assert.Equal(t, flat[i].Path, visible[i].Path)
	}
}

func TestVisibleTreeCollapsedHidesDescendants(t *testing.T) {
	root := testTreeFixture()
	collapsed := map[string]bool{"/usr": true}
	visible := visibleTree(root, collapsed)

	paths := make([]string, len(visible))
	for i, n := range visible {
		paths[i] = n.Path
	}
	assert.Equal(t, []string{"/usr", "/etc"}, paths)
}

func TestVisibleTreeNilRootReturnsNil(t *testing.T) {
	assert.Empty(t, visibleTree(nil, nil))
}

func TestToggleCollapsed(t *testing.T) {
	var collapsed map[string]bool
	collapsed = toggleCollapsed(collapsed, "/usr")
	assert.True(t, isCollapsed(collapsed, "/usr"))
	collapsed = toggleCollapsed(collapsed, "/usr")
	assert.False(t, isCollapsed(collapsed, "/usr"))
	_, ok := collapsed["/usr"]
	assert.False(t, ok)
}
