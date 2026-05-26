package tui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildIntroIndex(t *testing.T) {
	root := &image.FileNode{Name: "", Path: "/", IsDir: true}
	etc := &image.FileNode{Name: "etc", Path: "/etc", IsDir: true, IntroducedInLayer: 0}
	etc.AddChild(&image.FileNode{Name: "passwd", Path: "/etc/passwd", IntroducedInLayer: 0})
	root.AddChild(etc)

	app := &image.FileNode{Name: "app", Path: "/app", IsDir: true, IntroducedInLayer: 2}
	app.AddChild(&image.FileNode{Name: "server", Path: "/app/server", IntroducedInLayer: 2})
	root.AddChild(app)

	idx := buildIntroIndex(root)

	assert.Equal(t, 0, idx["/etc/passwd"])
	assert.Equal(t, 2, idx["/app/server"])

	_, hasEtcDir := idx["/etc"]
	_, hasAppDir := idx["/app"]
	assert.False(t, hasEtcDir, "directories should not appear in the index")
	assert.False(t, hasAppDir, "directories should not appear in the index")

	idxNil := buildIntroIndex(nil)
	assert.NotNil(t, idxNil)
	assert.Empty(t, idxNil)
}

// efficiencyOf returns a deterministic EfficiencyResult with N wasted entries,
// sorted desc by TotalWasted to match image.Efficiency()'s contract.
func efficiencyOf(n int) *image.EfficiencyResult {
	r := &image.EfficiencyResult{Score: 0.5, WastedBytes: int64(n) * 100}
	for i := range n {
		r.WastedFiles = append(r.WastedFiles, image.WastedFile{
			Path:        fmt.Sprintf("/path/file_%03d", i),
			TotalWasted: int64(n-i) * 100,
			LayerCount:  2,
		})
	}
	return r
}

func TestWasteOpen(t *testing.T) {
	m := setupModel()
	m.efficiency = efficiencyOf(3)

	m.openWaste()

	assert.True(t, m.showWaste)
	require.Len(t, m.wasteRows, 3)
	// Rows in same order as efficiency.WastedFiles (already desc-sorted).
	assert.Equal(t, "/path/file_000", m.wasteRows[0].Path)
	assert.Equal(t, int64(300), m.wasteRows[0].Wasted)
	assert.Equal(t, "/path/file_002", m.wasteRows[2].Path)
	assert.Equal(t, 0, m.wasteCursor)
	assert.False(t, m.wasteExpanded)
}

func TestWasteOpen500Cap(t *testing.T) {
	m := setupModel()
	m.efficiency = efficiencyOf(750)

	m.openWaste()

	assert.True(t, m.showWaste)
	assert.Len(t, m.wasteRows, 500, "should cap at 500 rows")
}

func TestWasteEmpty(t *testing.T) {
	m := setupModel()
	m.efficiency = &image.EfficiencyResult{Score: 1.0, WastedBytes: 0}

	m.openWaste()

	assert.True(t, m.showWaste)
	assert.Empty(t, m.wasteRows)

	// j/k/Enter/y/a are no-ops on empty list.
	updated, _ := m.Update(keyPress('j'))
	um := updated.(model)
	assert.Equal(t, 0, um.wasteCursor)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um = updated.(model)
	assert.True(t, um.showWaste, "Enter on empty list should be no-op")
	assert.Nil(t, cmd)

	updated, cmd = m.Update(keyPress('y'))
	um = updated.(model)
	assert.False(t, um.copyConfirm)
	assert.Nil(t, cmd)

	updated, _ = m.Update(keyPress('a'))
	um = updated.(model)
	assert.True(t, um.wasteExpanded)
}

func TestWasteNavigate(t *testing.T) {
	m := setupModel()
	m.efficiency = efficiencyOf(5)
	m.openWaste()

	m = send(m, keyPress('j'))
	assert.Equal(t, 1, m.wasteCursor)

	m = send(m, keyPress('j'))
	assert.Equal(t, 2, m.wasteCursor)

	m = send(m, keyPress('k'))
	assert.Equal(t, 1, m.wasteCursor)

	m = send(m, keyPress('G'))
	assert.Equal(t, 4, m.wasteCursor)

	// Bottom-clamped.
	m = send(m, keyPress('j'))
	assert.Equal(t, 4, m.wasteCursor)

	m = send(m, keyPress('g'))
	assert.Equal(t, 0, m.wasteCursor)

	// Top-clamped.
	m = send(m, keyPress('k'))
	assert.Equal(t, 0, m.wasteCursor)
}

func TestWasteExpand(t *testing.T) {
	m := setupModel()
	m.efficiency = efficiencyOf(50)
	m.openWaste()

	// Default visible slice is top 20.
	assert.Len(t, m.visibleWasteRows(), 20)

	// Move cursor before expanding.
	m.wasteCursor = 7
	m = send(m, keyPress('a'))

	assert.True(t, m.wasteExpanded)
	assert.Len(t, m.visibleWasteRows(), 50, "expanded shows all rows up to 500")
	assert.Equal(t, 0, m.wasteCursor, "cursor resets to 0 on expand toggle")

	// Toggle back collapses to 20.
	m = send(m, keyPress('a'))
	assert.False(t, m.wasteExpanded)
	assert.Len(t, m.visibleWasteRows(), 20)
	assert.Equal(t, 0, m.wasteCursor)
}

func TestWasteOverlayExpandKeyAcceptsBothCases(t *testing.T) {
	for _, ch := range []rune{'a', 'A'} {
		t.Run(string(ch), func(t *testing.T) {
			m := setupModel()
			m.efficiency = efficiencyOf(50)
			m.openWaste()
			require.False(t, m.wasteExpanded)

			m = send(m, keyPress(ch))
			assert.True(t, m.wasteExpanded, "case-%c must toggle expand", ch)
		})
	}
}

func TestWasteAutoExpandOnDown(t *testing.T) {
	m := setupModel()
	m.efficiency = efficiencyOf(25)
	m.openWaste()

	require.False(t, m.wasteExpanded)
	require.Len(t, m.visibleWasteRows(), 20)
	m.wasteCursor = 19

	m = send(m, keyPress('j'))

	assert.True(t, m.wasteExpanded, "auto-expand on Down past collapsed limit")
	assert.Equal(t, 20, m.wasteCursor, "cursor advances by 1 across the boundary")
	assert.Len(t, m.visibleWasteRows(), 25, "now showing the full list")
	assert.LessOrEqual(t, m.wasteOffset, m.wasteCursor)
	assert.Less(t, m.wasteCursor, m.wasteOffset+m.wasteVisibleHeight())
}

func TestWasteAutoExpandOnBottom(t *testing.T) {
	m := setupModel()
	m.efficiency = efficiencyOf(25)
	m.openWaste()

	require.False(t, m.wasteExpanded)
	require.Equal(t, 0, m.wasteCursor)

	m = send(m, keyPress('G'))

	assert.True(t, m.wasteExpanded, "auto-expand on Bottom from collapsed list")
	assert.Equal(t, 24, m.wasteCursor, "cursor jumps to last row of full list, not collapsed slice")
	assert.LessOrEqual(t, m.wasteOffset, m.wasteCursor)
	assert.Less(t, m.wasteCursor, m.wasteOffset+m.wasteVisibleHeight())
}

func TestWasteNoAutoExpandWhenFullListFits(t *testing.T) {
	m := setupModel()
	m.efficiency = efficiencyOf(12)
	m.openWaste()

	for range 11 {
		m = send(m, keyPress('j'))
	}
	require.Equal(t, 11, m.wasteCursor)
	require.False(t, m.wasteExpanded)

	m = send(m, keyPress('j'))
	assert.Equal(t, 11, m.wasteCursor, "cursor clamped, no movement past end")
	assert.False(t, m.wasteExpanded, "no expansion needed — already showing everything")

	m = send(m, keyPress('G'))
	assert.False(t, m.wasteExpanded, "Bottom is no-op when collapsed view already covers full list")
	assert.Equal(t, 11, m.wasteCursor)
}

func TestWasteTitleShowsPositionCounter(t *testing.T) {
	m := setupModel()
	m.efficiency = efficiencyOf(31)
	m.openWaste()
	m.wasteCursor = 4

	out := m.renderWasteOverlay()

	assert.Contains(t, out, "Wasted Files 5/31",
		"panel title shows 1-based cursor position over total wasted files")
	assert.NotContains(t, out, "Top 20 of",
		"body header should no longer use Top 20 framing")
}

func TestWasteTitleEmptyState(t *testing.T) {
	m := setupModel()
	m.efficiency = &image.EfficiencyResult{Score: 1.0, WastedBytes: 0}
	m.openWaste()

	out := m.renderWasteOverlay()

	assert.Contains(t, out, "Wasted Files 0/0",
		"empty waste list shows 0/0 in panel title")
}

func TestWasteJumpClearsFilter(t *testing.T) {
	m := setupModel()
	m.efficiency = image.Efficiency(m.analysis.Layers)
	// testAnalysis() has no duplicate paths, so build a synthetic row pointing
	// at a real file in layer 0.
	m.openWaste()
	m.wasteRows = []wasteRow{
		{Path: "/etc/passwd", Wasted: 1024, LayerCount: 2, IntroLayer: 0},
	}
	// Persisted filter (filterActive=false but query set) — w can only be
	// pressed with filterActive=false, so this is the realistic scenario.
	m.filterQuery = "noisy-filter"

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(model)

	assert.False(t, um.showWaste, "modal closes on jump")
	assert.False(t, um.filterActive, "filter inactive")
	assert.Equal(t, "", um.filterQuery, "filter query cleared")
	assert.Equal(t, 0, um.layerCursor, "layerCursor set to introducing layer")
	assert.Contains(t, um.statusMsg, "/etc/passwd")
}

func TestWasteJumpUnknownIntroPreservesFilter(t *testing.T) {
	m := setupModel()
	m.efficiency = image.Efficiency(m.analysis.Layers)
	m.openWaste()
	// Row with IntroLayer=-1 (intro unknown) — jump should be a no-op,
	// preserving the user's filter rather than wiping it.
	m.wasteRows = []wasteRow{
		{Path: "/some/path", Wasted: 1024, LayerCount: 2, IntroLayer: -1},
	}
	m.filterActive = true
	m.filterQuery = "important"
	m.layerCursor = 1

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(model)

	assert.False(t, um.showWaste, "overlay still closes")
	assert.True(t, um.filterActive, "filterActive must be preserved on no-op jump")
	assert.Equal(t, "important", um.filterQuery, "filterQuery must be preserved on no-op jump")
	assert.Equal(t, 1, um.layerCursor, "layerCursor must not change on no-op jump")
	assert.Contains(t, um.statusMsg, "Intro layer unknown")
}

func TestWasteJumpClearsDiffOnly(t *testing.T) {
	m := setupModel()
	m.layerCursor = 2
	m.openWaste()
	m.wasteRows = []wasteRow{
		{Path: "/etc/passwd", Wasted: 1024, LayerCount: 2, IntroLayer: 0},
	}
	m.diffOnly = true

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(model)

	assert.False(t, um.showWaste)
	assert.Equal(t, 0, um.layerCursor)
	assert.Contains(t, um.statusMsg, "/etc/passwd")
}

func TestWasteJumpFileNotInView(t *testing.T) {
	m := setupModel()
	m.openWaste()
	m.wasteRows = []wasteRow{
		{Path: "/does/not/exist", Wasted: 1024, LayerCount: 2, IntroLayer: 0},
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(model)

	assert.False(t, um.showWaste, "modal still closes")
	assert.Equal(t, 0, um.treeCursor, "cursor pinned to 0 when path missing")
	assert.Equal(t, "File not visible in current view", um.statusMsg)
}

func TestWasteCopyPath(t *testing.T) {
	m := setupModel()
	m.efficiency = efficiencyOf(3)
	m.openWaste()
	m.wasteCursor = 1

	updated, cmd := m.Update(keyPress('y'))
	um := updated.(model)

	assert.True(t, um.showWaste, "modal stays open after copy")
	assert.True(t, um.copyConfirm, "copy confirmation set")
	assert.NotNil(t, cmd, "clipboard cmd issued")
}

func TestWasteEscCloses(t *testing.T) {
	m := setupModel()
	m.efficiency = efficiencyOf(3)
	m.openWaste()
	m.wasteCursor = 2
	m.diffOnly = true // unrelated state must survive

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	um := updated.(model)

	assert.False(t, um.showWaste)
	assert.Empty(t, um.wasteRows)
	assert.True(t, um.diffOnly, "Esc on waste must not affect other state")
	assert.False(t, um.quitting, "Esc on waste must not quit")
}

func TestWasteWGuard(t *testing.T) {
	// Filter active -> w no-op.
	m := setupModel()
	m.efficiency = efficiencyOf(3)
	m.filterActive = true
	updated, _ := m.Update(keyPress('w'))
	um := updated.(model)
	assert.False(t, um.showWaste, "w should be swallowed by filter")

	// Help open -> w no-op.
	m = setupModel()
	m.efficiency = efficiencyOf(3)
	m.showHelp = true
	updated, _ = m.Update(keyPress('w'))
	um = updated.(model)
	assert.False(t, um.showWaste, "w should be swallowed when help is open")

	// Viewer open -> w no-op.
	m = setupModel()
	m.efficiency = efficiencyOf(3)
	m.viewState = viewReady
	m.viewContent = &image.FileContent{Path: "/x", Data: []byte("hi"), Size: 2}
	updated, _ = m.Update(keyPress('w'))
	um = updated.(model)
	assert.False(t, um.showWaste, "w should not open while viewer is up")

	// Already open -> w stays a no-op (handler swallows, no double-open).
	m = setupModel()
	m.efficiency = efficiencyOf(3)
	m.openWaste()
	m.wasteCursor = 2
	updated, _ = m.Update(keyPress('w'))
	um = updated.(model)
	assert.True(t, um.showWaste, "still open")
	assert.Equal(t, 2, um.wasteCursor, "cursor not reset")
}

func TestWasteHelpBlocked(t *testing.T) {
	m := setupModel()
	m.efficiency = efficiencyOf(3)
	m.openWaste()

	updated, _ := m.Update(keyPress('?'))
	um := updated.(model)

	assert.False(t, um.showHelp, "help must not toggle while waste overlay is open")
	assert.True(t, um.showWaste, "waste overlay still up")
}

func TestWasteRenderNarrow(t *testing.T) {
	m := setupModel()
	m.width = 60 // boxWidth=56, innerWidth=54, wide check: 52 >= 60 → false

	root := &image.FileNode{Name: "", Path: "/", IsDir: true}
	root.AddChild(&image.FileNode{Name: "short.txt", Path: "/short.txt", IntroducedInLayer: 0})
	m.analysis = &image.Analysis{StackedTrees: []*image.FileTree{{Root: root}}}

	m.efficiency = &image.EfficiencyResult{
		Score: 0.5, WastedBytes: 1024,
		WastedFiles: []image.WastedFile{
			{Path: "/short.txt", TotalWasted: 1024, LayerCount: 4},
		},
	}
	m.openWaste()

	out := m.renderWasteOverlay()
	// Wide layout would include the "x4" count column. Narrow drops it.
	assert.NotContains(t, out, "x4", "narrow render must drop the count column")
	assert.Contains(t, out, "L1", "layer column always present")
}

func TestWasteRenderCopyConfirm(t *testing.T) {
	m := setupModel()
	m.efficiency = efficiencyOf(3)
	m.openWaste()

	// Default render: no Copied! banner, footer hints visible.
	before := m.renderWasteOverlay()
	assert.NotContains(t, before, "Copied!")
	assert.Contains(t, before, "jump", "footer hints visible by default")

	// After y: copyConfirm flips, banner replaces footer hints.
	updated, _ := m.Update(keyPress('y'))
	um := updated.(model)
	out := um.renderWasteOverlay()
	assert.Contains(t, out, "Copied!", "copy banner shows in overlay")
	assert.NotContains(t, out, "Enter jump", "footer hints replaced while copyConfirm")
}
