package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

func TestFocusedSplitGetsMoreRows(t *testing.T) {
	top, bottom := splitPanelRows(16, focusTree)
	assert.Greater(t, top, bottom)
	assert.Equal(t, 15, top+bottom)
	top, bottom = splitPanelRows(16, focusTreeAgg)
	assert.Greater(t, bottom, top)
	assert.Equal(t, 15, top+bottom)
}

func TestWheelMovesThreeTreeRows(t *testing.T) {
	m := setupModelTallTree(100)
	t.Cleanup(m.fetchCancel)
	m.focus = focusTree
	m = send(m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	assert.Equal(t, 3, m.treeCursor)
}

func TestScrollThumbTracksPosition(t *testing.T) {
	first := scrollInfo{0, 100, 10}
	middle := scrollInfo{45, 100, 10}
	last := scrollInfo{90, 100, 10}
	assert.True(t, scrollThumb(1, 12, first))
	assert.True(t, scrollThumb(5, 12, middle) || scrollThumb(6, 12, middle))
	assert.True(t, scrollThumb(10, 12, last))
	assert.False(t, scrollThumb(1, 12, last))

	m := setupModelTallTree(100)
	t.Cleanup(m.fetchCancel)
	m.focus = focusTree
	m.width, m.height = 80, 24
	m = send(m, keyPress('G'))
	panel := ansi.Strip(m.renderRightPanel(m.width-m.leftPanelWidth()-1, m.height-8))
	assert.True(t, strings.Contains(panel, "┃"))
}
