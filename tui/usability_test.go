package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
)

func TestHelpFitsTerminalAndScrollsToLastLine(t *testing.T) {
	for _, size := range [][2]int{{20, 8}, {60, 20}, {80, 24}, {100, 30}, {120, 40}, {180, 50}} {
		m := setupModel()
		m.width, m.height = size[0], size[1]
		m.showHelp = true
		out := m.overlayHelp()
		assert.LessOrEqual(t, lipgloss.Width(out), m.width)
		assert.LessOrEqual(t, lipgloss.Height(out), m.height)
		layerCursor := m.layerCursor
		updated, _ := m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
		m = updated.(model)
		assert.Equal(t, m.helpMaxOffset(), m.helpOffset)
		assert.Equal(t, layerCursor, m.layerCursor)
		assert.Contains(t, m.overlayHelp(), "tree.")
	}
}

func TestHelpEscapePreservesViewerSearch(t *testing.T) {
	m := setupModel()
	m.showHelp = true
	m.viewState = viewReady
	m.viewSearchActive = true
	m.viewSearchQuery = "config"
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(model)
	assert.False(t, m.showHelp)
	assert.Equal(t, viewReady, m.viewState)
	assert.True(t, m.viewSearchActive)
	assert.Equal(t, "config", m.viewSearchQuery)
}

func TestViewerEscapeHintMatchesSearchState(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	m.viewSearchQuery = "config"
	assert.Contains(t, m.renderViewerStatusBar(), "clear search")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(model)
	assert.Equal(t, viewReady, m.viewState)
	assert.Contains(t, m.renderViewerStatusBar(), "close")
	assert.NotContains(t, m.renderViewerStatusBar(), "clear search")
}

func TestFileOriginsUseOneBasedLayerNumbers(t *testing.T) {
	m := setupModel()
	f := &image.FileNode{Name: "config", Path: "/config", IntroducedInLayer: 0, DiffType: image.Unchanged}
	tree := renderFileTree(m.theme, m.styles, []*image.FileNode{f}, 0, 0, 80, 8, true, false, "", false, false, nil, 2)
	assert.Contains(t, tree, "(L1)")
	viewer := renderFileView(viewerParams{
		theme: m.theme, styles: m.styles, width: 80, height: 8,
		content:     &image.FileContent{Path: "/config", Data: []byte("value"), Size: 5},
		originLayer: 0, currentLayer: 2, originCmd: "COPY config /config",
	})
	assert.Contains(t, viewer, "L1:")
}

func TestPullErrorKeepsRegistryRecoveryMessage(t *testing.T) {
	msg := friendlyError(&image.ErrPullFailed{
		Ref:   "registry.example.com/app:local",
		Cause: errors.New("unauthorized: authentication required; run docker login"),
	})
	assert.Contains(t, msg, "unauthorized")
	assert.Contains(t, msg, "docker login")
}

func TestTreeHintsOnlyOfferAvailableActions(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	m.width = 180
	dir := &image.FileNode{Path: "/etc", IsDir: true}
	m.treeCollapsed = map[string]bool{"/etc": true}
	assert.Contains(t, m.renderStatusBar([]*image.FileNode{dir}), "expand")
	m.sortMode = sortDesc
	bar := m.renderStatusBar([]*image.FileNode{dir})
	assert.NotContains(t, bar, "Enter")
	assert.NotContains(t, bar, "save")
	removed := &image.FileNode{Path: "/old", DiffType: image.Removed}
	bar = m.renderStatusBar([]*image.FileNode{removed})
	assert.NotContains(t, bar, "Enter")
	assert.NotContains(t, bar, "save")
	assert.Contains(t, bar, "copy path")
	bar = m.renderStatusBar(nil)
	assert.NotContains(t, bar, "Enter")
	assert.NotContains(t, bar, "copy path")
}
