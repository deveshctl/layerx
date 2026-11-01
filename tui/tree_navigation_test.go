package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTreeHorizontalNavigation(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	idx := -1
	for i, f := range m.displayTree() {
		if f.Path == "/etc" {
			idx = i
			break
		}
	}
	require.GreaterOrEqual(t, idx, 0)
	m.treeCursor = idx
	m = send(m, tea.KeyPressMsg{Code: tea.KeyRight})
	assert.Equal(t, idx+1, m.treeCursor)
	m = send(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	assert.Equal(t, idx, m.treeCursor)
	m = send(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	assert.True(t, isCollapsed(m.treeCollapsed, "/etc"))
	m = send(m, tea.KeyPressMsg{Code: tea.KeyRight})
	assert.False(t, isCollapsed(m.treeCollapsed, "/etc"))
}

func TestSizeListContainsFilesOnly(t *testing.T) {
	m := setupModelWithDiffs()
	m = send(m, keyPress('s'))
	files := m.displayTree()
	require.NotEmpty(t, files)
	for _, f := range files {
		assert.False(t, f.IsDir, f.Path)
	}
	assert.Contains(t, ansi.Strip(m.View().Content), "files by size")
}

func TestNamesViewKeepsDiffMarkerAndFileName(t *testing.T) {
	m := setupModelWithDiffs()
	m.focus = focusTree
	m = send(m, keyPress('m'))
	assert.True(t, m.compactTreeMeta)
	out := ansi.Strip(m.View().Content)
	assert.Contains(t, out, "names")
	assert.NotContains(t, out, "Permission")
	assert.Contains(t, out, "Filetree")
	assert.True(t, strings.Contains(out, "+ ") || strings.Contains(out, "~ "))
	m = send(m, keyPress('m'))
	assert.False(t, m.compactTreeMeta)
}
