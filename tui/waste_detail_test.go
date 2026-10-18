package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func wasteHistoryAnalysis() *image.Analysis {
	a := testAnalysis()
	etc := &image.FileNode{Name: "etc", Path: "/etc", IsDir: true}
	etc.AddChild(&image.FileNode{Name: "passwd", Path: "/etc/passwd", Size: 2048})
	a.Layers[1].Tree.Root.AddChild(etc)
	a.StackedTrees = image.Stack(a.Layers)
	return a
}

func TestWasteExplainReturnAndJump(t *testing.T) {
	m := setupModel()
	m.analysis = wasteHistoryAnalysis()
	m.efficiency = image.EfficiencyFromAnalysis(m.analysis)
	m.aggregated = true
	m.focus = focusTreeAgg
	m.openWaste()
	m = send(m, keyPress('i'))
	require.NotNil(t, m.wasteDetail)
	require.Len(t, m.wasteDetail.explanation.Events, 2)
	assert.Equal(t, int64(1024), m.wasteDetail.explanation.TotalWasted)
	assert.Contains(t, ansi.Strip(m.renderWasteOverlay()), "Overwritten in L2")
	before := m
	m = send(m, keyPress('j'))
	assert.Equal(t, 1, m.wasteDetail.cursor)
	assert.Zero(t, before.wasteDetail.cursor, "Update must not mutate its input model")
	m = send(m, keyPressSpecial(tea.KeyEscape))
	assert.Nil(t, m.wasteDetail)
	assert.True(t, m.showWaste, "Esc returns to list before closing overlay")
	assert.Zero(t, m.wasteCursor)
	m = send(m, keyPress('i'))
	m = send(m, keyPress('j'))
	m = send(m, keyPressSpecial(tea.KeyEnter))
	assert.False(t, m.showWaste)
	assert.Equal(t, 1, m.layerCursor)
	assert.Equal(t, focusTree, m.focus, "jump must focus the tree whose cursor was selected")
	files := m.displayTree()
	require.Less(t, m.treeCursor, len(files))
	assert.Equal(t, "/etc/passwd", files[m.treeCursor].Path)
}

func TestWasteOriginUsesWastedCopyBeforeReinstall(t *testing.T) {
	a := testAnalysis()
	etc := &image.FileNode{Name: "etc", Path: "/etc", IsDir: true}
	etc.AddChild(&image.FileNode{Name: ".wh.passwd", Path: "/etc/.wh.passwd"})
	a.Layers[1].Tree.Root.AddChild(etc)
	etc = &image.FileNode{Name: "etc", Path: "/etc", IsDir: true}
	etc.AddChild(&image.FileNode{Name: "passwd", Path: "/etc/passwd", Size: 512})
	a.Layers[2].Tree.Root.AddChild(etc)
	a.StackedTrees = image.Stack(a.Layers)
	m := setupModel()
	m.analysis = a
	m.efficiency = image.EfficiencyFromAnalysis(a)
	m.openWaste()
	require.Len(t, m.wasteRows, 1)
	assert.Equal(t, 0, m.wasteRows[0].IntroLayer)
	m = send(m, keyPressSpecial(tea.KeyEnter))
	assert.Equal(t, 0, m.layerCursor)
	assert.Equal(t, "/etc/passwd", m.displayTree()[m.treeCursor].Path)
}

func TestWasteCleanupAccessibleAtFullEfficiency(t *testing.T) {
	m := setupModel()
	a := testAnalysis()
	root := &image.FileNode{Name: "root", Path: "/root", IsDir: true}
	cache := &image.FileNode{Name: ".cache", Path: "/root/.cache", IsDir: true}
	pip := &image.FileNode{Name: "pip", Path: "/root/.cache/pip", IsDir: true}
	pip.AddChild(&image.FileNode{Name: "wheel", Path: "/root/.cache/pip/wheel", Size: 1 << 30})
	cache.AddChild(pip)
	root.AddChild(cache)
	a.Layers[0].Tree.Root.AddChild(root)
	a.StackedTrees = image.Stack(a.Layers)
	m.analysis = a
	m.efficiency = image.EfficiencyFromAnalysis(a)
	require.Equal(t, 1.0, m.efficiency.Score)
	m.openWaste()
	assert.Contains(t, ansi.Strip(m.renderWasteOverlay()), "Layer efficiency is not a size assessment")
	m = send(m, keyPressSpecial(tea.KeyTab))
	assert.True(t, m.wasteShowCleanup)
	require.Len(t, m.visibleWasteRows(), 1)
	assert.Equal(t, int64(1<<30), m.visibleWasteRows()[0].Wasted)
	m = send(m, keyPress('i'))
	require.NotNil(t, m.wasteDetail)
	assert.Contains(t, ansi.Strip(m.renderWasteOverlay()), "Potential cleanup")
	assert.Contains(t, ansi.Strip(m.renderWasteOverlay()), "needed at runtime")
	m = send(m, keyPressSpecial(tea.KeyEscape))
	assert.True(t, m.wasteShowCleanup)
	m = send(m, keyPressSpecial(tea.KeyEnter))
	assert.False(t, m.showWaste)
	assert.Equal(t, len(a.Layers)-1, m.layerCursor)
	assert.Equal(t, "/root/.cache/pip", m.displayTree()[m.treeCursor].Path)
}

func TestWasteDetailFitsAndNavigatesAfterResize(t *testing.T) {
	m := setupModel()
	m.analysis = wasteHistoryAnalysis()
	m.efficiency = image.EfficiencyFromAnalysis(m.analysis)
	m.openWaste()
	m = send(m, keyPress('i'))
	// A long history and a multiline Unicode command must keep the footer
	// visible and the selected event reachable at the minimum terminal size.
	for i := range 100 {
		m.wasteDetail.explanation.Events = append(m.wasteDetail.explanation.Events,
			image.WasteEvent{Layer: i, Command: strings.Repeat("命令\n", 100), Reason: "Overwritten"})
	}
	m = send(m, keyPress('G'))
	last := len(m.wasteDetail.explanation.Events) - 1
	assert.Equal(t, last, m.wasteDetail.cursor)
	for _, size := range [][2]int{{50, 11}, {60, 14}, {120, 40}} {
		m = send(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		out := ansi.Strip(m.renderWasteOverlay())
		assert.Equal(t, last, m.wasteDetail.cursor)
		assert.Contains(t, out, "Esc back")
		assert.Contains(t, out, fmt.Sprintf("History %d/%d", last+1, last+1))
		assert.LessOrEqual(t, lipgloss.Height(out), m.height)
		for _, line := range strings.Split(out, "\n") {
			assert.LessOrEqual(t, ansi.StringWidth(line), m.width)
		}
	}
}

func TestWasteDetailCopyAndQuit(t *testing.T) {
	m := setupModel()
	m.analysis = wasteHistoryAnalysis()
	m.efficiency = image.EfficiencyFromAnalysis(m.analysis)
	m.openWaste()
	m = send(m, keyPress('i'))
	updated, cmd := m.Update(keyPress('y'))
	m = updated.(model)
	assert.NotNil(t, cmd)
	assert.True(t, m.copyConfirm)
	assert.Contains(t, ansi.Strip(m.renderWasteOverlay()), "Copied!")
	m = send(m, clearCopyMsg{})
	updated, cmd = m.Update(keyPress('c'))
	m = updated.(model)
	assert.NotNil(t, cmd)
	assert.True(t, m.copyConfirm)
	m = send(m, keyPress('q'))
	assert.True(t, m.quitting)
}

func TestWasteListRetainsSelectionAfterResizeAndDetails(t *testing.T) {
	m := setupModel()
	m.efficiency = efficiencyOf(100)
	m.openWaste()
	m = send(m, keyPress('G'))
	for _, size := range [][2]int{{50, 11}, {60, 20}, {120, 40}} {
		m = send(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		assert.Equal(t, 99, m.wasteCursor)
		assert.Less(t, m.wasteCursor, m.wasteOffset+m.wasteVisibleHeight())
		out := ansi.Strip(m.renderWasteOverlay())
		assert.Contains(t, out, "file_099")
		assert.Contains(t, out, "Esc close")
		assert.LessOrEqual(t, lipgloss.Height(out), m.height)
	}
	offset := m.wasteOffset
	m = send(m, keyPress('i'))
	m = send(m, keyPressSpecial(tea.KeyEscape))
	assert.Equal(t, 99, m.wasteCursor)
	assert.Equal(t, offset, m.wasteOffset)
}
