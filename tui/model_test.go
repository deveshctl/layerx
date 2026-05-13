package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/deveshpharswan/layerx/image"
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

// --- View: loading state -----------------------------------------------------

func TestViewLoadingContainsLoadingAndImageRef(t *testing.T) {
	m := NewModel(Config{ImageRef: "nginx:latest"})
	m.width = 120
	m.height = 40
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
		renderLayers(a.Layers, 0, 0, 40, 20, true)
	})
}

func TestRenderLayersEmptyDoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		renderLayers(nil, 0, 0, 40, 20, false)
	})
}

// --- renderFileTree ----------------------------------------------------------

func TestRenderFileTreeDoesNotPanic(t *testing.T) {
	m := setupModel()
	files := m.currentFlatTree()
	assert.NotPanics(t, func() {
		renderFileTree(files, 0, 0, 60, 20, true, false, "", sortNone)
	})
}

func TestRenderFileTreeEmptyShowsPlaceholder(t *testing.T) {
	output := renderFileTree(nil, 0, 0, 60, 20, false, false, "", sortNone)
	assert.Contains(t, output, "no filesystem changes")
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
