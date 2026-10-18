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
	assert.Contains(t, ansi.Strip(m.renderWasteOverlay()), "Replaced in layer 2")
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
	out := ansi.Strip(m.renderWasteOverlay())
	assert.Contains(t, out, "Live files may still offer cleanup")
	assert.Contains(t, out, "Layer waste:       0 B")
	assert.Contains(t, out, "Potential cleanup: 1.0 GB")
	assert.Contains(t, out, "Tab Review cleanup")
	m = send(m, keyPressSpecial(tea.KeyTab))
	assert.True(t, m.wasteShowCleanup)
	require.Len(t, m.visibleWasteRows(), 1)
	assert.Equal(t, int64(1<<30), m.visibleWasteRows()[0].Wasted)
	out = ansi.Strip(m.renderWasteOverlay())
	assert.Contains(t, out, "Layer waste:       0 B", "both totals remain visible after switching views")
	assert.Contains(t, out, "Tab View layer waste")
	assert.NotContains(t, out, "Layer 3", "cache rows should not imply that the final layer created the cache")
	m = send(m, keyPress('i'))
	require.NotNil(t, m.wasteDetail)
	assert.Contains(t, ansi.Strip(m.renderWasteOverlay()), "Potential cleanup")
	assert.Contains(t, ansi.Strip(m.renderWasteOverlay()), "check before removing")
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
		assert.Contains(t, out, "Esc Back")
		assert.Contains(t, out, fmt.Sprintf("%d/%d", last+1, last+1))
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
		assert.Contains(t, out, "Esc Close")
		assert.LessOrEqual(t, lipgloss.Height(out), m.height)
	}
	offset := m.wasteOffset
	m = send(m, keyPress('i'))
	m = send(m, keyPressSpecial(tea.KeyEscape))
	assert.Equal(t, 99, m.wasteCursor)
	assert.Equal(t, offset, m.wasteOffset)
}

func TestWasteDeletionExplanationFitsAtMinimumSize(t *testing.T) {
	m := setupModel()
	// The summary must explain the file's remaining cost before any technical
	// history, and leave navigation available in a short, narrow terminal.
	m.width, m.height = 50, 11
	m.wasteDetail = &wasteDetailState{
		row: wasteRow{Path: "/tmp/build-artifact", Wasted: 16 << 20},
		explanation: image.WasteExplanation{TotalWasted: 16 << 20, Events: []image.WasteEvent{
			{Layer: 0, Change: image.Added, Size: 16 << 20, WastedBytes: 16 << 20, Reason: "Deleted in L2", Command: "RUN download"},
			{Layer: 1, Change: image.Removed, Reason: "Deletion does not reclaim earlier layer bytes", Command: "RUN rm /tmp/build-artifact"},
		}},
		cursor: 1,
	}
	out := ansi.Strip(m.renderWasteDetail())
	assert.Contains(t, out, "Deleted later; 16.0 MB remains stored.")
	assert.Contains(t, out, "Layer 2 deleted the file")
	assert.Contains(t, out, "Deleting later keeps earlier layer bytes.")
	assert.Contains(t, out, "Enter Go to layer")
	assert.Contains(t, out, "Esc Back")
	assert.NotContains(t, out, "wasted 0 B")
	assert.Less(t, strings.Index(out, "Deleted later"), strings.Index(out, "Layer 2"))
	assert.LessOrEqual(t, lipgloss.Height(out), m.height)
}
