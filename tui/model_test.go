package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keyPress builds a KeyPressMsg for a printable character.
func keyPress(ch rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: ch, Text: string(ch)}
}

// keyPressSpecial builds a KeyPressMsg for a special key (Tab, Esc, etc.)
// whose String() derives from the keyTypeString map, not the Text field.
func keyPressSpecial(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code}
}

// send drives Update with one message and returns the resulting model.
func send(m model, msg tea.Msg) model {
	next, _ := m.Update(msg)
	return next.(model)
}

// viewContent extracts the plain string content from View.
func viewContent(v tea.View) string {
	return v.Content
}

// --- test fixtures -----------------------------------------------------------

func testAnalysis() *image.Analysis {
	layers := []image.Layer{
		{
			Index:   0,
			ID:      "a1b2c3d4e5f6",
			Size:    5600000,
			Command: "FROM golang:1.22-alpine AS builder",
			Tree:    image.NewFileTree(),
		},
		{
			Index:   1,
			ID:      "f7e8d9c0b1a2",
			Size:    42300000,
			Command: "RUN go mod download",
			Tree:    image.NewFileTree(),
		},
		{
			Index:   2,
			ID:      "1a2b3c4d5e6f",
			Size:    18700000,
			Command: "RUN go build -o /app/server ./cmd/server",
			Tree:    image.NewFileTree(),
		},
	}

	etc := &image.FileNode{Name: "etc", Path: "/etc", IsDir: true}
	etc.AddChild(&image.FileNode{Name: "passwd", Path: "/etc/passwd", Size: 1024})
	layers[0].Tree.Root.AddChild(etc)

	goDir := &image.FileNode{Name: "go", Path: "/go", IsDir: true}
	pkg := &image.FileNode{Name: "pkg", Path: "/go/pkg", IsDir: true}
	goDir.AddChild(pkg)
	layers[1].Tree.Root.AddChild(goDir)

	app := &image.FileNode{Name: "app", Path: "/app", IsDir: true}
	app.AddChild(&image.FileNode{Name: "server", Path: "/app/server", Size: 18700000})
	layers[2].Tree.Root.AddChild(app)

	stacked := image.Stack(layers)

	return &image.Analysis{
		ImageRef:     "test:latest",
		Layers:       layers,
		StackedTrees: stacked,
		TotalSize:    66600000,
	}
}

func setupModel() model {
	m := NewModel(Config{ImageRef: "test:latest"})
	m.width = 120
	m.height = 40
	m.state = stateReady
	m.analysis = testAnalysis()
	return m
}

// --- NewModel ----------------------------------------------------------------

func TestNewModelInitialState(t *testing.T) {
	m := NewModel(Config{ImageRef: "nginx:latest"})
	assert.Equal(t, stateLoading, m.state)
	assert.Equal(t, "nginx:latest", m.imageRef)
	assert.Equal(t, 0, m.layerCursor)
	assert.Equal(t, 0, m.treeCursor)
	assert.Equal(t, focusLayers, m.focus)
	assert.False(t, m.quitting)
}

// --- WindowSizeMsg -----------------------------------------------------------

func TestWindowSizeMsgUpdatesWidthAndHeight(t *testing.T) {
	m := NewModel(Config{ImageRef: "test:latest"})
	m = send(m, tea.WindowSizeMsg{Width: 200, Height: 50})
	assert.Equal(t, 200, m.width)
	assert.Equal(t, 50, m.height)
}

// --- analysisMsg success -----------------------------------------------------

func TestAnalysisMsgSuccessTransitionsToReady(t *testing.T) {
	m := NewModel(Config{ImageRef: "test:latest"})
	m.width = 120
	m.height = 40
	a := testAnalysis()
	m = send(m, analysisMsg{analysis: a})
	assert.Equal(t, stateReady, m.state)
	require.NotNil(t, m.analysis)
	assert.Equal(t, "test:latest", m.analysis.ImageRef)
	assert.Len(t, m.analysis.Layers, 3)
}

// --- analysisMsg error -------------------------------------------------------

func TestAnalysisMsgErrorTransitionsToError(t *testing.T) {
	m := NewModel(Config{ImageRef: "test:latest"})
	m.width = 120
	m.height = 40
	m = send(m, analysisMsg{err: errors.New("something went wrong")})
	assert.Equal(t, stateError, m.state)
	assert.Contains(t, m.errMsg, "something went wrong")
}

// --- Quit from all states ----------------------------------------------------

func TestQuitFromLoadingState(t *testing.T) {
	m := NewModel(Config{ImageRef: "test:latest"})
	m = send(m, keyPress('q'))
	assert.True(t, m.quitting)
}

func TestQuitFromReadyState(t *testing.T) {
	m := setupModel()
	m = send(m, keyPress('q'))
	assert.True(t, m.quitting)
}

func TestQuitFromErrorState(t *testing.T) {
	m := NewModel(Config{ImageRef: "test:latest"})
	m.state = stateError
	m.errMsg = "some error"
	m = send(m, keyPress('q'))
	assert.True(t, m.quitting)
}

func TestEscapeQuits(t *testing.T) {
	m := setupModel()
	m = send(m, keyPressSpecial(tea.KeyEscape))
	assert.True(t, m.quitting)
}

// --- Tab switches focus ------------------------------------------------------

func TestTabSwitchesFocusFromLayersToTree(t *testing.T) {
	m := setupModel()
	assert.Equal(t, focusLayers, m.focus)
	m = send(m, keyPressSpecial(tea.KeyTab))
	assert.Equal(t, focusTree, m.focus)
}

func TestTabSwitchesFocusFromTreeToLayers(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	m = send(m, keyPressSpecial(tea.KeyTab))
	assert.Equal(t, focusLayers, m.focus)
}

// --- Navigation disabled in loading state ------------------------------------

func TestNavigationDisabledInLoadingState(t *testing.T) {
	m := NewModel(Config{ImageRef: "test:latest"})
	m.width = 120
	m.height = 40

	before := m
	m = send(m, keyPressSpecial(tea.KeyTab))
	assert.Equal(t, before.focus, m.focus)

	m = send(m, keyPress('j'))
	assert.Equal(t, before.layerCursor, m.layerCursor)

	m = send(m, keyPress('k'))
	assert.Equal(t, before.treeCursor, m.treeCursor)
}

// --- Layer navigation --------------------------------------------------------

func TestLayerNavigationDown(t *testing.T) {
	m := setupModel()
	m.focus = focusLayers
	m = send(m, keyPress('j'))
	assert.Equal(t, 1, m.layerCursor)
}

func TestLayerNavigationUp(t *testing.T) {
	m := setupModel()
	m.focus = focusLayers
	m.layerCursor = 2
	m = send(m, keyPress('k'))
	assert.Equal(t, 1, m.layerCursor)
}

func TestLayerNavigationDownBoundary(t *testing.T) {
	m := setupModel()
	m.focus = focusLayers
	m.layerCursor = 2
	m = send(m, keyPress('j'))
	assert.Equal(t, 2, m.layerCursor, "cursor must not exceed last layer")
}

func TestLayerNavigationUpBoundary(t *testing.T) {
	m := setupModel()
	m.focus = focusLayers
	m.layerCursor = 0
	m = send(m, keyPress('k'))
	assert.Equal(t, 0, m.layerCursor, "cursor must not go below zero")
}

func TestLayerNavigationToTop(t *testing.T) {
	m := setupModel()
	m.focus = focusLayers
	m.layerCursor = 2
	m = send(m, keyPress('g'))
	assert.Equal(t, 0, m.layerCursor)
}

func TestLayerNavigationToBottom(t *testing.T) {
	m := setupModel()
	m.focus = focusLayers
	m.layerCursor = 0
	m = send(m, keyPress('G'))
	assert.Equal(t, 2, m.layerCursor)
}

// --- Tree navigation ---------------------------------------------------------

func TestTreeNavigationDown(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	m.layerCursor = 0
	m.treeCursor = 0
	m = send(m, keyPress('j'))
	assert.Equal(t, 1, m.treeCursor)
}

func TestTreeNavigationUp(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	m.layerCursor = 0
	m.treeCursor = 1
	m = send(m, keyPress('k'))
	assert.Equal(t, 0, m.treeCursor)
}

func TestTreeNavigationUpBoundary(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	m.treeCursor = 0
	m = send(m, keyPress('k'))
	assert.Equal(t, 0, m.treeCursor)
}

// --- Layer switch resets tree cursor -----------------------------------------

func TestLayerSwitchResetsCursors(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	m.treeCursor = 1
	m.treeOffset = 1

	// Switch focus back to layers then navigate to a different layer.
	m.focus = focusLayers
	m = send(m, keyPress('j'))

	assert.Equal(t, 0, m.treeCursor)
	assert.Equal(t, 0, m.treeOffset)
}

// --- leftPanelWidth ----------------------------------------------------------

func TestLeftPanelWidth(t *testing.T) {
	tests := []struct {
		totalWidth int
		want       int
	}{
		{50, 24},  // 35% = 17 → clamped to min 24
		{80, 28},  // 35% = 28 → within [24,44]
		{120, 42}, // 35% = 42 → within [24,44]
		{200, 44}, // 35% = 70 → clamped to max 44
	}
	for _, tc := range tests {
		m := model{width: tc.totalWidth}
		assert.Equal(t, tc.want, m.leftPanelWidth(), "width=%d", tc.totalWidth)
	}
}

func TestLeftPanelWidth_BothMode_WiderCap(t *testing.T) {
	tests := []struct {
		totalWidth int
		want       int
	}{
		{50, 24},  // still floored at 24
		{120, 42}, // 35% = 42, under both-mode cap of 56
		{160, 56}, // 35% = 56, exactly the cap
		{240, 56}, // 35% = 84 → clamped to both-mode max 56
	}
	for _, tc := range tests {
		m := model{width: tc.totalWidth, sizeMode: sizeColBoth}
		assert.Equal(t, tc.want, m.leftPanelWidth(), "both width=%d", tc.totalWidth)
	}
}

// --- View: loading state -----------------------------------------------------

func TestViewLoadingContainsLoadingAndImageRef(t *testing.T) {
	m := NewModel(Config{ImageRef: "nginx:latest"})
	m.width = 120
	m.height = 40
	m.loadPhase = image.PhasePulling
	v := m.View()
	content := viewContent(v)
	assert.Contains(t, content, "Pulling")
	assert.Contains(t, content, "nginx:latest")
}

// --- View: error state -------------------------------------------------------

func TestViewErrorContainsErrorMessage(t *testing.T) {
	m := NewModel(Config{ImageRef: "test:latest"})
	m.width = 120
	m.height = 40
	m.state = stateError
	m.errMsg = "Docker is not running. Please start Docker and try again."
	v := m.View()
	content := viewContent(v)
	assert.Contains(t, content, "Docker is not running")
}

// --- View: ready state -------------------------------------------------------

func TestViewReadyContainsBrandAndImageRef(t *testing.T) {
	m := setupModel()
	v := m.View()
	content := viewContent(v)
	assert.Contains(t, content, "layerx")
	assert.Contains(t, content, "test:latest")
}

func TestViewReadyHasAltScreen(t *testing.T) {
	m := setupModel()
	v := m.View()
	assert.True(t, v.AltScreen)
}

// --- View: terminal too narrow -----------------------------------------------

func TestViewTooNarrowShowsMessage(t *testing.T) {
	m := setupModel()
	m.width = 30
	v := m.View()
	content := viewContent(v)
	assert.Contains(t, content, "too narrow")
}

// --- View: terminal too short ------------------------------------------------

func TestViewTooShortShowsMessage(t *testing.T) {
	m := setupModel()
	m.height = 5
	v := m.View()
	content := viewContent(v)
	assert.Contains(t, content, "too short")
}

// --- View: quitting ----------------------------------------------------------

func TestViewQuittingReturnsEmptyContent(t *testing.T) {
	m := setupModel()
	m.quitting = true
	v := m.View()
	assert.Empty(t, viewContent(v))
}

// --- flattenTree -------------------------------------------------------------

func TestFlattenTreeEmptyRootReturnsNil(t *testing.T) {
	root := &image.FileNode{Name: "/", Path: "/", IsDir: true}
	result := flattenTree(root)
	assert.Empty(t, result)
}

func TestFlattenTreeWithChildrenReturnsDepthFirstList(t *testing.T) {
	root := &image.FileNode{Name: "/", Path: "/", IsDir: true}
	usr := &image.FileNode{Name: "usr", Path: "/usr", IsDir: true}
	bin := &image.FileNode{Name: "bin", Path: "/usr/bin", IsDir: true}
	sh := &image.FileNode{Name: "sh", Path: "/usr/bin/sh"}
	etc := &image.FileNode{Name: "etc", Path: "/etc", IsDir: true}

	usr.AddChild(bin)
	bin.AddChild(sh)
	root.AddChild(usr)
	root.AddChild(etc)

	result := flattenTree(root)
	require.Len(t, result, 4)
	assert.Equal(t, "/usr", result[0].Path)
	assert.Equal(t, "/usr/bin", result[1].Path)
	assert.Equal(t, "/usr/bin/sh", result[2].Path)
	assert.Equal(t, "/etc", result[3].Path)
}

func TestFlattenTreeNilRootReturnsNil(t *testing.T) {
	result := flattenTree(nil)
	assert.Empty(t, result)
}

// --- nodeIndent --------------------------------------------------------------

func TestNodeIndent(t *testing.T) {
	tests := []struct {
		path string
		want int
	}{
		{"/etc", 0},
		{"/etc/passwd", 1},
		{"/usr/local/bin", 2},
		{"/", 0},
	}
	for _, tc := range tests {
		node := &image.FileNode{Path: tc.path}
		assert.Equal(t, tc.want, nodeIndent(node), "path=%s", tc.path)
	}
}

// --- friendlyError -----------------------------------------------------------

func TestFriendlyErrorDaemonNotRunning(t *testing.T) {
	err := &image.ErrDaemonNotRunning{Cause: errors.New("connection refused")}
	msg := friendlyError(err)
	assert.Contains(t, msg, "Docker is not running")
}

func TestFriendlyErrorPullFailed(t *testing.T) {
	err := &image.ErrPullFailed{Ref: "badimage:latest", Cause: errors.New("404")}
	msg := friendlyError(err)
	assert.Contains(t, msg, "badimage:latest")
	assert.Contains(t, msg, "Failed to pull")
}

func TestFriendlyErrorImageNotFound(t *testing.T) {
	err := &image.ErrImageNotFound{Ref: "ghost:latest", Cause: errors.New("not found")}
	msg := friendlyError(err)
	assert.Contains(t, msg, "ghost:latest")
	assert.Contains(t, msg, "not found")
}

func TestFriendlyErrorGenericReturnsMessage(t *testing.T) {
	err := errors.New("unexpected failure")
	assert.Equal(t, "unexpected failure", friendlyError(err))
}

// --- renderLayers ------------------------------------------------------------

func TestRenderLayersDoesNotPanic(t *testing.T) {
	a := testAnalysis()
	assert.NotPanics(t, func() {
		renderLayers(a.Layers, 0, 0, 40, 20, true, sizeColDelta, 0)
	})
}

func TestRenderLayersEmptyDoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		renderLayers(nil, 0, 0, 40, 20, false, sizeColDelta, 0)
	})
}

// --- renderFileTree ----------------------------------------------------------

func TestRenderFileTreeDoesNotPanic(t *testing.T) {
	m := setupModel()
	files := m.displayTree()
	assert.NotPanics(t, func() {
		renderFileTree(files, 0, 0, 60, 20, true, false, "", true, nil, 0)
	})
}

func TestRenderFileTreeEmptyShowsPlaceholder(t *testing.T) {
	output := renderFileTree(nil, 0, 0, 60, 20, false, false, "", true, nil, 0)
	assert.Contains(t, output, "no filesystem changes")
}

func TestRenderFileTreeCollapsedDirShowsGlyph(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	files := m.displayTree()
	for i, f := range files {
		if f.Path == "/etc" {
			m.treeCursor = i
			break
		}
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(model)
	line := renderFileTree(um.displayTree(), um.treeCursor, 0, 80, 20, true, false, "", true, um.treeCollapsed, 0)
	assert.Contains(t, line, "▸")
}

// --- wrapCommandLines ---------------------------------------------------------

func TestWrapCommandLinesShortFitsOnOneLine(t *testing.T) {
	lines := wrapCommandLines("FROM alpine", 40, 3)
	assert.Equal(t, "FROM alpine", lines[0])
	assert.Empty(t, lines[1])
	assert.Empty(t, lines[2])
}

func TestWrapCommandLinesLongWrapsToSecondLine(t *testing.T) {
	cmd := "RUN apt-get install -y curl wget git build-essential"
	lines := wrapCommandLines(cmd, 30, 3)
	assert.True(t, len([]rune(lines[0])) <= 30, "line1 should fit within width")
	assert.NotEmpty(t, lines[1])
	assert.False(t, strings.Contains(lines[0], "\n"), "line1 must not contain newlines")
}

func TestWrapCommandLinesExactWidthFitsOnOneLine(t *testing.T) {
	cmd := strings.Repeat("x", 30)
	lines := wrapCommandLines(cmd, 30, 3)
	assert.Equal(t, cmd, lines[0])
	assert.Empty(t, lines[1])
}

// --- M06+M07 test fixtures ---------------------------------------------------

func testAnalysisWithDiffs() *image.Analysis {
	layers := []image.Layer{
		{
			Index:   0,
			ID:      "a1b2c3d4e5f6",
			Size:    10000000,
			Command: "FROM ubuntu:22.04",
			Tree:    image.NewFileTree(),
		},
		{
			Index:   1,
			ID:      "f7e8d9c0b1a2",
			Size:    5000000,
			Command: "RUN apt-get install -y nginx",
			Tree:    image.NewFileTree(),
		},
	}

	// Layer 0: base layer
	etc := &image.FileNode{Name: "etc", Path: "/etc", IsDir: true}
	etc.AddChild(&image.FileNode{Name: "passwd", Path: "/etc/passwd", Size: 2048})
	etc.AddChild(&image.FileNode{Name: "hostname", Path: "/etc/hostname", Size: 128})
	usr := &image.FileNode{Name: "usr", Path: "/usr", IsDir: true}
	bin := &image.FileNode{Name: "bin", Path: "/usr/bin", IsDir: true}
	bin.AddChild(&image.FileNode{Name: "bash", Path: "/usr/bin/bash", Size: 1200000})
	usr.AddChild(bin)
	layers[0].Tree.Root.AddChild(etc)
	layers[0].Tree.Root.AddChild(usr)

	// Layer 1: installs nginx
	nginxBin := &image.FileNode{Name: "nginx", Path: "/usr/bin/nginx", Size: 800000}
	nginxConf := &image.FileNode{Name: "nginx.conf", Path: "/etc/nginx.conf", Size: 4096}
	binL1 := &image.FileNode{Name: "bin", Path: "/usr/bin", IsDir: true}
	binL1.AddChild(nginxBin)
	usrL1 := &image.FileNode{Name: "usr", Path: "/usr", IsDir: true}
	usrL1.AddChild(binL1)
	etcL1 := &image.FileNode{Name: "etc", Path: "/etc", IsDir: true}
	etcL1.AddChild(nginxConf)
	layers[1].Tree.Root.AddChild(usrL1)
	layers[1].Tree.Root.AddChild(etcL1)

	stacked := image.Stack(layers)

	return &image.Analysis{
		ImageRef:     "test-diffs:latest",
		Layers:       layers,
		StackedTrees: stacked,
		TotalSize:    15000000,
	}
}

func setupModelWithDiffs() model {
	m := NewModel(Config{ImageRef: "test-diffs:latest"})
	m.width = 120
	m.height = 40
	m.state = stateReady
	m.analysis = testAnalysisWithDiffs()
	m.layerCursor = 1
	m.focus = focusTree
	return m
}

// --- Diff-only toggle --------------------------------------------------------

func TestDiffToggleFiltersDiffType(t *testing.T) {
	m := setupModelWithDiffs()
	allFiles := m.displayTree()

	m = send(m, keyPress('d'))
	assert.True(t, m.diffOnly)

	filtered := m.displayTree()
	assert.Less(t, len(filtered), len(allFiles))
	for _, f := range filtered {
		assert.NotEqual(t, image.Unchanged, f.DiffType)
	}
}

func TestDiffToggleOff(t *testing.T) {
	m := setupModelWithDiffs()
	m = send(m, keyPress('d'))
	m = send(m, keyPress('d'))
	assert.False(t, m.diffOnly)
}

func TestDiffToggleWorksFromLayersPanel(t *testing.T) {
	m := setupModelWithDiffs()
	m.focus = focusLayers
	m = send(m, keyPress('d'))
	assert.True(t, m.diffOnly)
}

func TestDiffToggleResetsCursor(t *testing.T) {
	m := setupModelWithDiffs()
	m.treeCursor = 3
	m = send(m, keyPress('d'))
	assert.Equal(t, 0, m.treeCursor)
	assert.Equal(t, 0, m.treeOffset)
}

// --- Filter ------------------------------------------------------------------

func TestFilterActivation(t *testing.T) {
	m := setupModelWithDiffs()
	m = send(m, keyPress('/'))
	assert.True(t, m.filterActive)
}

func TestFilterOnlyActivatesInTreePanel(t *testing.T) {
	m := setupModelWithDiffs()
	m.focus = focusLayers
	m = send(m, keyPress('/'))
	assert.False(t, m.filterActive)
}

func TestFilterTypingUpdatesQuery(t *testing.T) {
	m := setupModelWithDiffs()
	m = send(m, keyPress('/'))
	m = send(m, keyPress('n'))
	m = send(m, keyPress('g'))
	assert.Equal(t, "ng", m.filterQuery)
}

func TestFilterEscClearsQuery(t *testing.T) {
	m := setupModelWithDiffs()
	m = send(m, keyPress('/'))
	m = send(m, keyPress('n'))
	m = send(m, keyPressSpecial(tea.KeyEscape))
	assert.False(t, m.filterActive)
	assert.Equal(t, "", m.filterQuery)
}

func TestFilterEnterKeepsQuery(t *testing.T) {
	m := setupModelWithDiffs()
	m = send(m, keyPress('/'))
	m = send(m, keyPress('n'))
	m = send(m, keyPressSpecial(tea.KeyEnter))
	assert.False(t, m.filterActive)
	assert.Equal(t, "n", m.filterQuery)
	assert.Equal(t, viewNone, m.viewState, "Enter confirms search without opening file")
}

func TestEnterConfirmsSearchThenSecondEnterOpensFile(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	m.extractor = &mockExtractor{}
	m.filterActive = true
	m.filterQuery = "passwd"
	m.treeCursor = 0

	// First Enter: confirms search, does NOT open file
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(model)
	assert.Equal(t, "passwd", um.filterQuery, "filter query preserved")
	assert.False(t, um.filterActive, "filter input closed")
	assert.Equal(t, viewNone, um.viewState, "file not opened yet")

	// Second Enter: opens the selected file
	updated2, _ := um.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um2 := updated2.(model)
	assert.Equal(t, viewLoading, um2.viewState, "file opens on second Enter")
}

func TestBackspaceClearsFilterChip(t *testing.T) {
	m := setupModelWithDiffs()
	m.filterQuery = "nginx"
	m.filterActive = false
	m.treeCursor = 2

	m = send(m, keyPressSpecial(tea.KeyBackspace))
	assert.Equal(t, "", m.filterQuery)
	assert.Equal(t, 0, m.treeCursor)
}

func TestFilterBackspaceRemovesChar(t *testing.T) {
	m := setupModelWithDiffs()
	m = send(m, keyPress('/'))
	m = send(m, keyPress('a'))
	m = send(m, keyPress('b'))
	m = send(m, keyPressSpecial(tea.KeyBackspace))
	assert.Equal(t, "a", m.filterQuery)
}

func TestFilterSubstringMatchesCaseInsensitive(t *testing.T) {
	m := setupModelWithDiffs()
	m.filterQuery = "NGINX"
	files := m.displayTree()
	for _, f := range files {
		assert.Contains(t, strings.ToLower(f.Path), "nginx")
	}
}

func TestFilterSwallowsNavKeysWhenActive(t *testing.T) {
	m := setupModelWithDiffs()
	m = send(m, keyPress('/'))
	cursorBefore := m.treeCursor
	m = send(m, keyPress('j'))
	assert.Equal(t, cursorBefore, m.treeCursor)
	assert.Contains(t, m.filterQuery, "j")
}

func TestFilterSwallowsQWhenActive(t *testing.T) {
	m := setupModelWithDiffs()
	m = send(m, keyPress('/'))
	m = send(m, keyPress('j'))
	m = send(m, keyPress('q'))
	assert.False(t, m.quitting, "q must not quit while filter input is active")
	assert.Equal(t, "jq", m.filterQuery)
}

func TestFilterCtrlCStillQuits(t *testing.T) {
	m := setupModelWithDiffs()
	m = send(m, keyPress('/'))
	m = send(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	assert.True(t, m.quitting, "ctrl+c must always quit, even with filter active")
}

func TestViewerSearchSwallowsQWhenActive(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	m.viewContent = &image.FileContent{
		Path: "/test",
		Data: []byte("jquery and graphql"),
		Size: 18,
	}
	m.viewSearchActive = true

	m = send(m, keyPress('j'))
	m = send(m, keyPress('q'))
	assert.False(t, m.quitting, "q must not quit while viewer search is active")
	assert.Equal(t, "jq", m.viewSearchQuery)
}

func TestViewerSearchCtrlCStillQuits(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	m.viewContent = &image.FileContent{
		Path: "/test",
		Data: []byte("hello"),
		Size: 5,
	}
	m.viewSearchActive = true

	m = send(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	assert.True(t, m.quitting, "ctrl+c must always quit, even with viewer search active")
}

func TestQStillQuitsWhenNoTextInputActive(t *testing.T) {
	m := setupModelWithDiffs()
	m = send(m, keyPress('q'))
	assert.True(t, m.quitting, "q quits normally when no text input is capturing")
}

// --- Sort by size ------------------------------------------------------------

func TestSortCyclesThreeStates(t *testing.T) {
	m := setupModelWithDiffs()
	assert.Equal(t, sortNone, m.sortMode)

	m = send(m, keyPress('s'))
	assert.Equal(t, sortDesc, m.sortMode)

	m = send(m, keyPress('s'))
	assert.Equal(t, sortAsc, m.sortMode)

	m = send(m, keyPress('s'))
	assert.Equal(t, sortNone, m.sortMode)
}

func TestSortWorksFromLayersPanel(t *testing.T) {
	m := setupModelWithDiffs()
	m.focus = focusLayers
	m = send(m, keyPress('s'))
	assert.Equal(t, sortDesc, m.sortMode)
}

func TestSortDescLargestFirst(t *testing.T) {
	m := setupModelWithDiffs()
	m.sortMode = sortDesc
	files := m.displayTree()
	if len(files) >= 2 {
		for i := 0; i < len(files)-1; i++ {
			assert.GreaterOrEqual(t, nodeEffectiveSize(files[i]), nodeEffectiveSize(files[i+1]))
		}
	}
}

func TestSortAscSmallestFirst(t *testing.T) {
	m := setupModelWithDiffs()
	m.sortMode = sortAsc
	files := m.displayTree()
	if len(files) >= 2 {
		for i := 0; i < len(files)-1; i++ {
			assert.LessOrEqual(t, nodeEffectiveSize(files[i]), nodeEffectiveSize(files[i+1]))
		}
	}
}

func TestSortResetsOnLayerSwitch(t *testing.T) {
	m := setupModelWithDiffs()
	m.sortMode = sortDesc
	m.focus = focusLayers
	m.layerCursor = 0
	m = send(m, keyPress('j'))
	assert.Equal(t, sortNone, m.sortMode)
}

func TestSortResetsCursor(t *testing.T) {
	m := setupModelWithDiffs()
	m.treeCursor = 3
	m = send(m, keyPress('s'))
	assert.Equal(t, 0, m.treeCursor)
}

// --- Composability -----------------------------------------------------------

func TestDiffPlusFilterCompose(t *testing.T) {
	m := setupModelWithDiffs()
	m.diffOnly = true
	m.filterQuery = "nginx"
	files := m.displayTree()
	for _, f := range files {
		assert.NotEqual(t, image.Unchanged, f.DiffType)
		assert.Contains(t, strings.ToLower(f.Path), "nginx")
	}
}

// --- Esc precedence ----------------------------------------------------------

func TestEscQuitsWhenFilterNotActive(t *testing.T) {
	m := setupModelWithDiffs()
	m.filterActive = false
	m.showHelp = false
	m = send(m, keyPressSpecial(tea.KeyEscape))
	assert.True(t, m.quitting)
}

func TestEscClosesHelpBeforeQuit(t *testing.T) {
	m := setupModelWithDiffs()
	m.showHelp = true
	m = send(m, keyPressSpecial(tea.KeyEscape))
	assert.False(t, m.showHelp)
	assert.False(t, m.quitting)
}

// --- File Viewer (M08) -------------------------------------------------------

type mockExtractor struct {
	extractRawData []byte
	extractRawErr  error
}

func (e *mockExtractor) Extract(_ context.Context, _ string, path string) (*image.FileContent, error) {
	return &image.FileContent{
		Path: path,
		Data: []byte("mock content"),
		Size: 12,
	}, nil
}

func (e *mockExtractor) ExtractRaw(_ context.Context, _ string, _ string) ([]byte, error) {
	if e.extractRawErr != nil {
		return nil, e.extractRawErr
	}
	if e.extractRawData != nil {
		return e.extractRawData, nil
	}
	return []byte("mock raw content"), nil
}

func TestEnterOnFileTriggersViewing(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	m.extractor = &mockExtractor{}

	files := m.displayTree()
	for i, f := range files {
		if !f.IsDir {
			m.treeCursor = i
			break
		}
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(model)
	assert.Equal(t, viewLoading, um.viewState)
}

func TestEnterOnDirTogglesCollapse(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	m.extractor = &mockExtractor{}

	before := len(m.displayTree())
	etcIdx := -1
	for i, f := range m.displayTree() {
		if f.Path == "/etc" {
			etcIdx = i
			break
		}
	}
	require.GreaterOrEqual(t, etcIdx, 0)
	m.treeCursor = etcIdx

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(model)
	assert.Equal(t, viewNone, um.viewState)
	assert.Less(t, len(um.displayTree()), before)
	assert.True(t, isCollapsed(um.treeCollapsed, "/etc"))

	updated, _ = um.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um = updated.(model)
	assert.Equal(t, before, len(um.displayTree()))
	assert.False(t, isCollapsed(um.treeCollapsed, "/etc"))
}

func TestEnterOnDirWhileSortingShowsStatus(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	m = send(m, keyPress('s'))

	files := m.displayTree()
	for i, f := range files {
		if f.IsDir {
			m.treeCursor = i
			break
		}
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(model)
	assert.Contains(t, um.statusMsg, "unavailable")
}

func TestLayerChangeClearsCollapse(t *testing.T) {
	m := setupModel()
	m.treeCollapsed = map[string]bool{"/etc": true}
	m.focus = focusLayers

	m = send(m, keyPress('j'))
	assert.Nil(t, m.treeCollapsed)
}

func TestFilterDisablesCollapse(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	m.filterQuery = "etc"
	before := len(m.displayTree())

	etcIdx := -1
	for i, f := range m.displayTree() {
		if f.Path == "/etc" {
			etcIdx = i
			break
		}
	}
	require.GreaterOrEqual(t, etcIdx, 0)
	m.treeCursor = etcIdx

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(model)
	assert.Equal(t, before, len(um.displayTree()))
	assert.Contains(t, um.statusMsg, "unavailable")
}

func TestEnterOnRemovedFileShowsStatusMsg(t *testing.T) {
	m := setupModelWithDiffs()
	m.focus = focusTree
	m.extractor = &mockExtractor{}

	// Inject a removed file into the tree for this test.
	removedNode := &image.FileNode{
		Name:     "old.conf",
		Path:     "/etc/old.conf",
		Size:     256,
		DiffType: image.Removed,
	}
	m.analysis.StackedTrees[m.layerCursor].Root.AddChild(removedNode)

	files := m.displayTree()
	for i, f := range files {
		if f.DiffType == image.Removed {
			m.treeCursor = i
			break
		}
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(model)
	assert.Equal(t, viewNone, um.viewState)
	assert.Equal(t, "File removed in this layer", um.statusMsg)
}

func TestEscClosesFileViewer(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	m.viewContent = &image.FileContent{Path: "/test", Data: []byte("hi")}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	um := updated.(model)
	assert.Equal(t, viewNone, um.viewState)
	assert.Nil(t, um.viewContent)
}

func TestViewerScrollDown(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	m.viewContent = &image.FileContent{
		Path: "/test",
		Data: []byte(strings.Repeat("line\n", 100)),
		Size: 500,
	}
	m.viewOffset = 0
	m.height = 30

	updated, _ := m.Update(keyPress('j'))
	um := updated.(model)
	assert.Equal(t, 1, um.viewOffset)
}

func TestViewerScrollUpAtTopStays(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	m.viewContent = &image.FileContent{
		Path: "/test",
		Data: []byte("line1\nline2\n"),
		Size: 12,
	}
	m.viewOffset = 0

	updated, _ := m.Update(keyPress('k'))
	um := updated.(model)
	assert.Equal(t, 0, um.viewOffset)
}

func TestFileContentMsgSetsViewReady(t *testing.T) {
	m := setupModel()
	m.viewState = viewLoading

	content := &image.FileContent{Path: "/etc/hosts", Data: []byte("127.0.0.1 localhost"), Size: 19}
	updated, _ := m.Update(fileContentMsg{content: content})
	um := updated.(model)
	assert.Equal(t, viewReady, um.viewState)
	assert.Equal(t, content, um.viewContent)
}

func TestFileContentMsgErrorClearsViewState(t *testing.T) {
	m := setupModel()
	m.viewState = viewLoading

	updated, _ := m.Update(fileContentMsg{err: fmt.Errorf("not found")})
	um := updated.(model)
	assert.Equal(t, viewNone, um.viewState)
	assert.Contains(t, um.statusMsg, "not found")
}

func TestViewerBlocksNavigationKeys(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	m.viewContent = &image.FileContent{Path: "/test", Data: []byte("hi")}
	m.focus = focusLayers
	cursorBefore := m.layerCursor

	updated, _ := m.Update(keyPressSpecial(tea.KeyTab))
	um := updated.(model)
	assert.Equal(t, focusLayers, um.focus)
	assert.Equal(t, cursorBefore, um.layerCursor)
}

func TestViewLoadingBlocksAllKeys(t *testing.T) {
	m := setupModel()
	m.viewState = viewLoading

	updated, _ := m.Update(keyPress('j'))
	um := updated.(model)
	assert.Equal(t, viewLoading, um.viewState)
}

// --- Efficiency (M09) --------------------------------------------------------

func TestEfficiencyComputedOnAnalysisLoad(t *testing.T) {
	m := setupModel()
	m.efficiency = image.Efficiency(m.analysis.Layers)
	assert.NotNil(t, m.efficiency)
	assert.Equal(t, 1.0, m.efficiency.Score)
}

func TestEfficiencyBadgeInStatusBar(t *testing.T) {
	m := setupModel()
	m.efficiency = &image.EfficiencyResult{Score: 0.85, WastedBytes: 1500000}
	view := m.View()
	content := viewContent(view)
	assert.Contains(t, content, "Eff: 85%")
	assert.Contains(t, content, "wasted")
}

// --- File Extraction to Disk (M10) -------------------------------------------

func TestExtractKeyOnDirectoryShowsStatus(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	m.extractor = &mockExtractor{}

	files := m.displayTree()
	for i, f := range files {
		if f.IsDir {
			m.treeCursor = i
			break
		}
	}

	updated, _ := m.Update(keyPress('x'))
	um := updated.(model)
	assert.Equal(t, "Cannot extract directory", um.statusMsg)
}

func TestExtractKeyOnFileTriggersExtraction(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	m.extractor = &mockExtractor{}

	files := m.displayTree()
	for i, f := range files {
		if !f.IsDir {
			m.treeCursor = i
			break
		}
	}

	updated, cmd := m.Update(keyPress('x'))
	um := updated.(model)
	assert.Equal(t, "Extracting...", um.statusMsg)
	assert.NotNil(t, cmd)
}

func TestExtractKeyFromLayersPanelIsNoop(t *testing.T) {
	m := setupModel()
	m.focus = focusLayers
	m.extractor = &mockExtractor{}

	updated, cmd := m.Update(keyPress('x'))
	um := updated.(model)
	assert.Equal(t, "", um.statusMsg)
	assert.Nil(t, cmd)
}

func TestExtractKeyOnRemovedFile(t *testing.T) {
	m := setupModelWithDiffs()
	m.focus = focusTree
	m.extractor = &mockExtractor{}

	removedNode := &image.FileNode{
		Name:     "old.conf",
		Path:     "/etc/old.conf",
		Size:     256,
		DiffType: image.Removed,
	}
	m.analysis.StackedTrees[m.layerCursor].Root.AddChild(removedNode)

	files := m.displayTree()
	for i, f := range files {
		if f.DiffType == image.Removed {
			m.treeCursor = i
			break
		}
	}

	updated, _ := m.Update(keyPress('x'))
	um := updated.(model)
	assert.Equal(t, "File removed in this layer", um.statusMsg)
}

func TestFileSaveMsgSuccess(t *testing.T) {
	m := setupModel()
	var savedName string
	var savedData []byte
	m.writeFile = func(name string, data []byte, _ os.FileMode) error {
		savedName = name
		savedData = data
		return nil
	}

	updated, _ := m.Update(fileSaveMsg{filename: "test.txt", data: []byte("hello")})
	um := updated.(model)
	assert.Equal(t, "Saved: test.txt", um.statusMsg)
	assert.Equal(t, "test.txt", savedName)
	assert.Equal(t, []byte("hello"), savedData)
}

func TestFileSaveMsgExtractError(t *testing.T) {
	m := setupModel()
	m.writeFile = os.WriteFile

	updated, _ := m.Update(fileSaveMsg{filename: "test.txt", err: errors.New("connection refused")})
	um := updated.(model)
	assert.Equal(t, "Error: connection refused", um.statusMsg)
}

func TestFileSaveMsgWriteError(t *testing.T) {
	m := setupModel()
	m.writeFile = func(_ string, _ []byte, _ os.FileMode) error {
		return errors.New("permission denied")
	}

	updated, _ := m.Update(fileSaveMsg{filename: "test.txt", data: []byte("hello")})
	um := updated.(model)
	assert.Equal(t, "Error: permission denied", um.statusMsg)
}

// --- Clipboard (y/Y) ---

func TestCopyPathYKeyInTreePanel(t *testing.T) {
	m := setupModel()
	m.focus = focusTree

	updated, cmd := m.Update(keyPress('y'))
	um := updated.(model)
	assert.True(t, um.copyConfirm, "copyConfirm should be set")
	assert.NotNil(t, cmd, "should return clipboard command")
}

func TestCopyPathYKeyInLayersPanelIsNoop(t *testing.T) {
	m := setupModel()
	m.focus = focusLayers

	updated, cmd := m.Update(keyPress('y'))
	um := updated.(model)
	assert.False(t, um.copyConfirm, "no copy in layers panel")
	assert.Nil(t, cmd)
}

func TestCopyContentShiftYInViewer(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	m.viewContent = &image.FileContent{
		Path: "/etc/passwd",
		Data: []byte("root:x:0:0"),
		Size: 10,
	}

	updated, cmd := m.Update(keyPress('Y'))
	um := updated.(model)
	assert.True(t, um.copyConfirm)
	assert.NotNil(t, cmd)
}

func TestCopyContentShiftYInLayerPanel(t *testing.T) {
	m := setupModel()
	m.focus = focusLayers

	updated, cmd := m.Update(keyPress('Y'))
	um := updated.(model)
	assert.True(t, um.copyConfirm)
	assert.NotNil(t, cmd)
}

// --- Viewer Search (/n/N) ---

func TestViewerSearchActivatesOnSlash(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	m.viewContent = &image.FileContent{
		Path: "/etc/passwd",
		Data: []byte("root:x:0:0\nnobody:x:65534:65534"),
		Size: 30,
	}

	updated, _ := m.Update(keyPress('/'))
	um := updated.(model)
	assert.True(t, um.viewSearchActive)
}

func TestViewerSearchTypingBuildsQuery(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	m.viewContent = &image.FileContent{
		Path: "/etc/passwd",
		Data: []byte("root:x:0:0\nnobody:x:65534:65534"),
		Size: 30,
	}
	m.viewSearchActive = true

	updated, _ := m.Update(tea.KeyPressMsg{Text: "r"})
	um := updated.(model)
	assert.Equal(t, "r", um.viewSearchQuery)

	updated2, _ := um.Update(tea.KeyPressMsg{Text: "o"})
	um2 := updated2.(model)
	assert.Equal(t, "ro", um2.viewSearchQuery)
	assert.Greater(t, len(um2.viewSearchMatches), 0)
}

func TestViewerSearchEnterConfirms(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	m.viewContent = &image.FileContent{
		Path: "/test",
		Data: []byte("hello world"),
		Size: 11,
	}
	m.viewSearchActive = true
	m.viewSearchQuery = "hello"

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(model)
	assert.False(t, um.viewSearchActive, "input closed")
	assert.Equal(t, "hello", um.viewSearchQuery, "query preserved")
}

func TestViewerSearchEscClearsQuery(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	m.viewContent = &image.FileContent{
		Path: "/test",
		Data: []byte("hello world"),
		Size: 11,
	}
	m.viewSearchActive = true
	m.viewSearchQuery = "hello"
	m.viewSearchMatches = [][2]int{{0, 0}}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	um := updated.(model)
	assert.False(t, um.viewSearchActive)
	assert.Equal(t, "", um.viewSearchQuery)
	assert.Nil(t, um.viewSearchMatches)
}

func TestViewerSearchNextPrevMatch(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	m.viewContent = &image.FileContent{
		Path: "/test",
		Data: []byte("aaa\naaa\naaa"),
		Size: 11,
	}
	m.viewSearchQuery = "aaa"
	m.viewSearchMatches = [][2]int{{0, 0}, {1, 0}, {2, 0}}
	m.viewSearchCursor = 0

	updated, _ := m.Update(keyPress('n'))
	um := updated.(model)
	assert.Equal(t, 1, um.viewSearchCursor)

	updated2, _ := um.Update(keyPress('n'))
	um2 := updated2.(model)
	assert.Equal(t, 2, um2.viewSearchCursor)

	updated3, _ := um2.Update(keyPress('n'))
	um3 := updated3.(model)
	assert.Equal(t, 0, um3.viewSearchCursor, "wraps around")

	updated4, _ := um3.Update(keyPress('N'))
	um4 := updated4.(model)
	assert.Equal(t, 2, um4.viewSearchCursor, "wraps backwards")
}

func TestViewerScrollStillWorksWithoutSearch(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	m.viewContent = &image.FileContent{
		Path: "/test",
		Data: []byte(strings.Repeat("line\n", 100)),
		Size: 500,
	}
	m.viewOffset = 0

	updated, _ := m.Update(keyPress('j'))
	um := updated.(model)
	assert.Equal(t, 1, um.viewOffset)
}

func TestViewerEscCascadeWithSearch(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	m.viewContent = &image.FileContent{
		Path: "/test",
		Data: []byte("test"),
		Size: 4,
	}
	m.viewSearchActive = true
	m.viewSearchQuery = "test"

	// First Esc: clears search
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	um := updated.(model)
	assert.False(t, um.viewSearchActive)
	assert.Equal(t, "", um.viewSearchQuery)
	assert.Equal(t, viewReady, um.viewState, "viewer still open")

	// Second Esc: closes viewer
	updated2, _ := um.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	um2 := updated2.(model)
	assert.Equal(t, viewNone, um2.viewState, "viewer closed")
}