package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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

// openViewer sets the viewer content and its derived line-split cache together,
// mirroring what the fileContentMsg handler does in production. Tests that set
// m.viewContent directly must keep m.viewLines in sync, or the viewer's cached
// hot paths (scroll clamp, search indexing, render) see an empty file.
func openViewer(m *model, fc *image.FileContent) {
	m.viewContent = fc
	m.viewLines = splitFileLines(fc.Data)
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

func TestEscapeDoesNotQuitInStateReady(t *testing.T) {
	// Round-9 fix: Esc is dismiss-only in stateReady. Mashing Esc after
	// closing a viewer used to fall through to tea.Quit; the new contract
	// reserves quitting for q / ctrl+c. Quit on Esc still applies in
	// stateLoading and stateError, where it is the documented escape
	// hatch.
	m := setupModel()
	m = send(m, keyPressSpecial(tea.KeyEscape))
	assert.False(t, m.quitting, "Esc in stateReady with nothing to dismiss must not quit")
}

// Esc must still quit the loading screen — the documented "Press q or Esc to
// exit" UX runs on stateLoading and stateError.
func TestEscapeQuitsInStateLoading(t *testing.T) {
	m := NewModel(Config{ImageRef: "test:latest"})
	m.width = 120
	m.height = 40
	// state defaults to stateLoading
	m = send(m, keyPressSpecial(tea.KeyEscape))
	assert.True(t, m.quitting, "Esc in stateLoading must quit (documented escape hatch)")
	// quit-cancels-everything contract (model.go cancelInflight): the
	// fetch context must be cancelled so the in-flight Docker pull does
	// not outlive the TUI. A refactor that drops cancelInflight() from
	// the Esc-quit branch would silently leak the pull goroutine.
	assert.ErrorIs(t, m.fetchCtx.Err(), context.Canceled,
		"Esc-quit must cancel fetchCtx so the in-flight pull is torn down")
}

func TestEscapeQuitsInStateError(t *testing.T) {
	m := NewModel(Config{ImageRef: "test:latest"})
	m.width = 120
	m.height = 40
	m.state = stateError
	m.errMsg = "test error"
	m = send(m, keyPressSpecial(tea.KeyEscape))
	assert.True(t, m.quitting, "Esc in stateError must quit (documented escape hatch)")
	assert.ErrorIs(t, m.fetchCtx.Err(), context.Canceled,
		"Esc-quit must cancel fetchCtx so the in-flight pull is torn down")
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

// maxPanelLineWidth returns the widest visible line in the rendered view,
// measured with lipgloss.Width so ANSI styling is excluded.
func maxPanelLineWidth(content string) int {
	maxW := 0
	for ln := range strings.SplitSeq(content, "\n") {
		if w := lipgloss.Width(ln); w > maxW {
			maxW = w
		}
	}
	return maxW
}

// TestViewLoadingPullProgressFitsInBox asserts that a multi-GB pull-progress
// line — long enough to overflow the legacy 52-col box — is rendered without
// truncating the bytes-total. Regression: prior to the fix, the bytes-total
// was clipped mid-value because boxWidth was hard-coded to 52.
func TestViewLoadingPullProgressFitsInBox(t *testing.T) {
	m := NewModel(Config{ImageRef: "ai/qwen3"})
	m.width = 120
	m.height = 40
	m.loadPhase = image.PhasePulling
	m.pullLayers = 1
	m.pullTotal = 3
	m.pullBytes = 254 * 1024 * 1024     // "254.0 MB"
	m.pullBytesMax = 4 * 1024 * 1024 * 1024 // "4.0 GB"

	content := viewContent(m.View())

	assert.Contains(t, content, "254.0 MB", "current bytes must render in full")
	assert.Contains(t, content, "4.0 GB", "total bytes must render in full")
	assert.Contains(t, content, "Layer 1/3", "layer counter must render in full")
}

// TestViewLoadingLongImageRefFitsInBox asserts a long registry/path image ref
// is not truncated by the loading box. dive users frequently inspect images
// like registry.example.com/team/service:tag-2026-05-23.
func TestViewLoadingLongImageRefFitsInBox(t *testing.T) {
	longRef := "registry.example.com/platform/services/inference-engine:v2026.05.23-rc1"
	m := NewModel(Config{ImageRef: longRef})
	m.width = 200
	m.height = 40
	m.loadPhase = image.PhasePulling

	content := viewContent(m.View())

	assert.Contains(t, content, longRef, "full image ref must be visible")
}

// TestViewLoadingPanelNeverExceedsTerminalWidth asserts the loading panel
// never renders wider than the terminal, even when content would naturally
// overflow. Worst-case: long ref + GB-scale progress on a typical 100-col term.
func TestViewLoadingPanelNeverExceedsTerminalWidth(t *testing.T) {
	cases := []struct {
		name  string
		width int
	}{
		{"narrow 60", 60},
		{"typical 100", 100},
		{"wide 200", 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(Config{ImageRef: "registry.example.com/platform/inference:v2026.05.23"})
			m.width = tc.width
			m.height = 40
			m.loadPhase = image.PhasePulling
			m.pullLayers = 7
			m.pullTotal = 12
			m.pullBytes = 8 * 1024 * 1024 * 1024
			m.pullBytesMax = 12 * 1024 * 1024 * 1024

			content := viewContent(m.View())
			require.LessOrEqual(t, maxPanelLineWidth(content), tc.width,
				"no rendered line may be wider than terminal width %d", tc.width)
		})
	}
}

// TestViewLoadingPullProgressNoBytesTotal exercises the branch where
// pullTotal>0 but pullBytesMax is still 0 (Docker has reported the layer
// count but not the total size yet). The "Layer N/M" line alone must render.
func TestViewLoadingPullProgressNoBytesTotal(t *testing.T) {
	m := NewModel(Config{ImageRef: "nginx:latest"})
	m.width = 120
	m.height = 40
	m.loadPhase = image.PhasePulling
	m.pullLayers = 2
	m.pullTotal = 5
	m.pullBytes = 0
	m.pullBytesMax = 0

	content := viewContent(m.View())
	assert.Contains(t, content, "Layer 2/5")
	assert.NotContains(t, content, "━", "no progress bar when total bytes unknown")
	assert.NotContains(t, content, "MB", "no byte counts when total bytes unknown")
}

// TestViewLoadingPullProgressShrinksBarOnNarrowTerminal asserts the progress
// bar shrinks (rather than overflowing or being truncated) when the terminal
// is too narrow for the default 20-cell bar plus counter and bytes.
// Regression: the bytes-total ("4.7 GB") was being clipped mid-value because
// the bar was hard-coded to 20 cells regardless of terminal width.
func TestViewLoadingPullProgressShrinksBarOnNarrowTerminal(t *testing.T) {
	m := NewModel(Config{ImageRef: "ai/qwen3"})
	m.width = 50
	m.height = 40
	m.loadPhase = image.PhasePulling
	m.pullLayers = 1
	m.pullTotal = 3
	m.pullBytes = 69 * 1024 * 1024
	m.pullBytesMax = 5046586573 // ~4.7 GB

	content := viewContent(m.View())

	assert.Contains(t, content, "4.7 GB", "bytes-total must not be truncated on narrow terminal")
	assert.Contains(t, content, "Layer 1/3")
	require.LessOrEqual(t, maxPanelLineWidth(content), 50,
		"panel must not exceed terminal width")
}

// TestViewLoadingPullProgressDropsBarWhenTooNarrow asserts that on a very
// narrow terminal the bar is dropped entirely (rather than rendered with
// fewer than 4 cells, which looks broken).
func TestViewLoadingPullProgressDropsBarWhenTooNarrow(t *testing.T) {
	m := NewModel(Config{ImageRef: "x"})
	m.width = 40
	m.height = 40
	m.loadPhase = image.PhasePulling
	m.pullLayers = 1
	m.pullTotal = 3
	m.pullBytes = 69 * 1024 * 1024
	m.pullBytesMax = 5046586573 // ~4.7 GB

	content := viewContent(m.View())

	assert.Contains(t, content, "4.7 GB")
	assert.Contains(t, content, "Layer 1/3")
	assert.NotContains(t, content, "━", "bar should be omitted when terminal cannot fit ≥4 cells")
	require.LessOrEqual(t, maxPanelLineWidth(content), 40)
}

// TestViewLoadingPullProgressBytesTotalNotClipped asserts that across a sweep
// of common terminal widths, the bytes-total unit suffix ("MB" / "GB") is
// always rendered in full. Regression: at certain widths, the rounding in the
// budget calc left the bytes text exactly at the right border, causing the
// trailing unit ("MB") to be clipped mid-value.
func TestViewLoadingPullProgressBytesTotalNotClipped(t *testing.T) {
	cases := []struct {
		name      string
		bytes     int64
		bytesMax  int64
		expectMax string
	}{
		{"MB-range", 81920000, 361 * 1024 * 1024, "361.0 MB"},
		{"GB-range", 2 * 1024 * 1024 * 1024, 4*1024*1024*1024 + 700*1024*1024, "4.7 GB"},
		{"large-GB", 8 * 1024 * 1024 * 1024, 12 * 1024 * 1024 * 1024, "12.0 GB"},
	}
	for _, tc := range cases {
		for w := 40; w <= 120; w++ {
			t.Run(fmt.Sprintf("%s/width=%d", tc.name, w), func(t *testing.T) {
				m := NewModel(Config{ImageRef: "node"})
				m.width = w
				m.height = 40
				m.loadPhase = image.PhasePulling
				m.pullLayers = 2
				m.pullTotal = 8
				m.pullBytes = tc.bytes
				m.pullBytesMax = tc.bytesMax

				content := viewContent(m.View())
				assert.Contains(t, content, tc.expectMax,
					"bytes-total %q must not be clipped at width %d", tc.expectMax, w)
				require.LessOrEqual(t, maxPanelLineWidth(content), w,
					"panel must not exceed terminal width %d", w)
			})
		}
	}
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

// TestViewErrorWrapsLongMessage asserts a long daemon-down message (the
// engine-down line plus its archive-mode hint) wraps within the bounded
// width instead of overflowing a standard 80-column terminal on one line.
func TestViewErrorWrapsLongMessage(t *testing.T) {
	m := NewModel(Config{ImageRef: "test:latest"})
	m.width = 80
	m.height = 24
	m.state = stateError
	m.errMsg = "Docker is not running. Please start Docker and try again. " +
		"Or run layerx on a saved-image archive instead (no engine needed)."
	v := m.View()
	content := viewContent(v)
	assert.Contains(t, content, "Docker is not running")
	assert.Contains(t, content, "archive")
	assert.LessOrEqual(t, maxPanelLineWidth(content), m.width)
}

// --- View: ready state -------------------------------------------------------

func TestViewReadyContainsBrandAndImageRef(t *testing.T) {
	m := setupModel()
	v := m.View()
	// Strip ANSI sequences before asserting — the image ref is gradient-rendered
	// with per-character colour codes so "test:latest" won't appear as a plain
	// substring in the raw content.
	stripped := ansi.Strip(viewContent(v))
	assert.Contains(t, stripped, "layerx")
	assert.Contains(t, stripped, "test:latest")
}

func TestViewReadyHasAltScreen(t *testing.T) {
	m := setupModel()
	v := m.View()
	assert.True(t, v.AltScreen)
	assert.Equal(t, tea.MouseModeCellMotion, v.MouseMode)
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
	// Untagged ErrDaemonNotRunning renders engine-agnostic — the tui
	// must NOT hardcode "Docker" for an error whose engine is unknown.
	err := &image.ErrDaemonNotRunning{Cause: errors.New("connection refused")}
	msg := friendlyError(err)
	assert.Contains(t, msg, "Container engine is not reachable")
	assert.NotContains(t, msg, "Docker")
	assert.NotContains(t, msg, "Podman")
}

func TestFriendlyErrorDaemonNotRunning_DockerEngine(t *testing.T) {
	// A docker-tagged error keeps the Docker-specific wording. This is
	// the path most users hit today; verifying it lets the untagged
	// branch stay generic without regressing docker's loading screen.
	err := &image.ErrDaemonNotRunning{
		Engine: "docker",
		Cause:  errors.New("connection refused"),
	}
	msg := friendlyError(err)
	assert.Contains(t, msg, "Docker is not running")
	// Every daemon-down path points at the archive fallback so a user
	// without a running engine is not dead-ended in the TUI.
	assert.Contains(t, msg, "archive")
}

func TestFriendlyErrorDaemonNotRunning_PodmanEngine(t *testing.T) {
	// TUI variant of the same regression: the loading screen must not
	// tell a Podman user to check on Docker.
	err := &image.ErrDaemonNotRunning{
		Engine: "podman",
		Host:   "ssh://user@host/run/podman.sock",
		Cause:  errors.New("connect: connection refused"),
	}
	msg := friendlyError(err)
	assert.Contains(t, msg, "Podman")
	assert.Contains(t, msg, "ssh://user@host/run/podman.sock")
	assert.NotContains(t, msg, "Docker is not running")
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
		renderLayers(CatppuccinMocha(), a.Layers, 0, 0, 40, 20, true, sizeColDelta, 0)
	})
}

func TestRenderLayersEmptyDoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		renderLayers(CatppuccinMocha(), nil, 0, 0, 40, 20, false, sizeColDelta, 0)
	})
}

// --- renderFileTree ----------------------------------------------------------

func TestRenderFileTreeDoesNotPanic(t *testing.T) {
	m := setupModel()
	files := m.displayTree()
	assert.NotPanics(t, func() {
		renderFileTree(CatppuccinMocha(), files, 0, 0, 60, 20, true, false, "", true, false, nil, 0)
	})
}

func TestRenderFileTreeEmptyShowsPlaceholder(t *testing.T) {
	output := renderFileTree(CatppuccinMocha(), nil, 0, 0, 60, 20, false, false, "", true, false, nil, 0)
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
	line := renderFileTree(CatppuccinMocha(), um.displayTree(), um.treeCursor, 0, 80, 20, true, false, "", true, false, um.treeCollapsed, 0)
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
	aggregated := image.BuildAggregatedTrees(layers)

	return &image.Analysis{
		ImageRef:        "test-diffs:latest",
		Layers:          layers,
		StackedTrees:    stacked,
		AggregatedTrees: aggregated,
		TotalSize:       15000000,
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

func TestFilterQuery_LengthCapped(t *testing.T) {
	m := setupModelWithDiffs()
	m = send(m, keyPress('/'))
	for range maxFilterLen + 50 {
		m = send(m, keyPress('a'))
	}
	assert.LessOrEqual(t, len([]rune(m.filterQuery)), maxFilterLen,
		"filterQuery must not grow past maxFilterLen")
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
	openViewer(&m, &image.FileContent{
		Path: "/test",
		Data: []byte("jquery and graphql"),
		Size: 18,
	})
	m.viewSearchActive = true

	m = send(m, keyPress('j'))
	m = send(m, keyPress('q'))
	assert.False(t, m.quitting, "q must not quit while viewer search is active")
	assert.Equal(t, "jq", m.viewSearchQuery)
}

func TestViewerSearchCtrlCStillQuits(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	openViewer(&m, &image.FileContent{
		Path: "/test",
		Data: []byte("hello"),
		Size: 5,
	})
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

func TestEscDoesNotQuitWhenNothingDismissable(t *testing.T) {
	// Round-9 fix: in stateReady Esc must not quit even when nothing is
	// dismissable. Pre-fix this caused mash-Esc on a closed viewer to
	// silently exit the app (M08 Gate C regression).
	m := setupModelWithDiffs()
	m.filterActive = false
	m.showHelp = false
	m = send(m, keyPressSpecial(tea.KeyEscape))
	assert.False(t, m.quitting, "Esc with nothing to dismiss must be a no-op in stateReady")
}

func TestEscClosesHelpDoesNotQuit(t *testing.T) {
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

func (e *mockExtractor) ExtractFromLayer(_ context.Context, _ string, path string, _ int) (*image.FileContent, error) {
	return &image.FileContent{
		Path: path,
		Data: []byte("mock content"),
		Size: 12,
	}, nil
}

func (e *mockExtractor) ExtractRawFromLayer(_ context.Context, _ string, _ string, _ int) ([]byte, error) {
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
	assert.Equal(t, "Error: file removed in this layer", um.statusMsg)
}

func TestEscClosesFileViewer(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	openViewer(&m, &image.FileContent{Path: "/test", Data: []byte("hi")})

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	um := updated.(model)
	assert.Equal(t, viewNone, um.viewState)
	assert.Nil(t, um.viewContent)
}

// Round-9 regression: mashing Esc on a file viewer used to close the viewer
// on the first press and then quit the app on the second. M08 Gate C calls
// "mash Esc — no state corruption" out as a checked behaviour, and silent
// quit-on-second-Esc is exactly the regression the bug-scan flagged.
func TestEscMashOnFileViewerDoesNotQuit(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	openViewer(&m, &image.FileContent{Path: "/test", Data: []byte("hi")})

	// First Esc: closes viewer.
	m = send(m, keyPressSpecial(tea.KeyEscape))
	assert.Equal(t, viewNone, m.viewState)
	assert.False(t, m.quitting)

	// Second Esc (the "mash"): must NOT quit.
	m = send(m, keyPressSpecial(tea.KeyEscape))
	assert.False(t, m.quitting, "second Esc after closing viewer must not quit the app")
}

func TestViewerScrollDown(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	openViewer(&m, &image.FileContent{
		Path: "/test",
		Data: []byte(strings.Repeat("line\n", 100)),
		Size: 500,
	})
	m.viewOffset = 0
	m.height = 30

	updated, _ := m.Update(keyPress('j'))
	um := updated.(model)
	assert.Equal(t, 1, um.viewOffset)
}

func TestViewerScrollUpAtTopStays(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	openViewer(&m, &image.FileContent{
		Path: "/test",
		Data: []byte("line1\nline2\n"),
		Size: 12,
	})
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

func TestFileContentMsgPopulatesHighlightCache(t *testing.T) {
	// Highlighting now defers to a tea.Cmd that emits highlightedMsg —
	// running chroma inline in Update was freezing the TUI on large
	// source files. The cache is populated by the second message, not
	// the first. This test pins the two-step async flow so a regression
	// to inline highlighting (which would re-introduce the freeze) is
	// caught.
	m := setupModel()
	m.viewState = viewLoading

	content := &image.FileContent{Path: "main.go", Data: []byte("package main\n"), Size: 13}
	updated, cmd := m.Update(fileContentMsg{requestID: m.viewRequestID, content: content})
	um := updated.(model)
	assert.Nil(t, um.viewHighlightedLines, "highlight must defer to a tea.Cmd, not run inline in Update")
	require.NotNil(t, cmd, "non-binary text content must dispatch a highlight cmd")

	hm, ok := cmd().(highlightedMsg)
	require.True(t, ok, "expected highlightedMsg from highlight cmd, got %T", cmd())
	updated2, _ := um.Update(hm)
	require.NotNil(t, updated2.(model).viewHighlightedLines, "highlight cache must be populated after the async cmd's message arrives")
}

func TestEscClearsHighlightCache(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	openViewer(&m, &image.FileContent{Path: "main.go", Data: []byte("package main\n")})
	m.viewHighlightedLines = []string{"package main"}

	updated, _ := m.Update(keyPressSpecial(tea.KeyEscape))
	um := updated.(model)
	assert.Nil(t, um.viewHighlightedLines)
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
	openViewer(&m, &image.FileContent{Path: "/test", Data: []byte("hi")})
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

// With the viewer open, viewReady must skip the file-tree pipeline (whose
// result the viewer status bar never consumes) yet still render a correct
// viewer status bar. Guards the PERF optimisation that passes a nil treeFiles
// through renderStatusBar's early viewer branch.
func TestViewerOpen_RendersViewerStatusBar(t *testing.T) {
	m := setupModel()
	m.width = 80
	m.height = 30
	m.efficiency = &image.EfficiencyResult{Score: 0.85, WastedBytes: 1500000}
	m.viewState = viewReady
	openViewer(&m, &image.FileContent{
		Path: "/etc/hosts",
		Data: []byte("line1\nline2\nline3\n"),
		Size: 18,
	})

	content := viewContent(m.View())
	// Viewer status bar hints and the line counter are present…
	assert.Contains(t, content, "search", "viewer status bar must render while the viewer is open")
	assert.Contains(t, content, "Line 1/3", "viewer status bar must show the line counter")
	// …and the normal (non-viewer) tree status bar is not.
	assert.NotContains(t, content, "Eff:", "efficiency badge belongs to the non-viewer status bar")
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
	assert.Equal(t, "Error: cannot extract directory", um.statusMsg)
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
	assert.Equal(t, "Error: file removed in this layer", um.statusMsg)
}

func TestFileSaveMsgSuccess(t *testing.T) {
	m := setupModel()
	m.saveRequestID = 7
	var savedName string
	var savedData []byte
	m.writeFile = func(name string, data []byte, _ os.FileMode) error {
		savedName = name
		savedData = data
		return nil
	}
	m.statFile = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }

	updated, cmd := m.Update(fileSaveMsg{requestID: 7, filename: "test.txt", data: []byte("hello")})
	require.NotNil(t, cmd, "fileSaveMsg must dispatch the off-thread save cmd")
	saved := cmd().(fileSavedMsg)
	assert.Equal(t, "test.txt", savedName)
	assert.Equal(t, []byte("hello"), savedData)
	assert.Equal(t, "test.txt", saved.target)
	assert.Equal(t, "test.txt", saved.original)
	assert.NoError(t, saved.err)

	updated2, _ := updated.(model).Update(saved)
	assert.Equal(t, "Saved: test.txt", updated2.(model).statusMsg)
}

func TestFileSaveMsgExtractError(t *testing.T) {
	m := setupModel()
	m.saveRequestID = 1
	m.writeFile = os.WriteFile

	updated, _ := m.Update(fileSaveMsg{requestID: 1, filename: "test.txt", err: errors.New("connection refused")})
	um := updated.(model)
	assert.Equal(t, "Error: connection refused", um.statusMsg)
}

func TestFileSaveMsgWriteError(t *testing.T) {
	m := setupModel()
	m.saveRequestID = 2
	m.writeFile = func(_ string, _ []byte, _ os.FileMode) error {
		return errors.New("permission denied")
	}
	m.statFile = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }

	updated, cmd := m.Update(fileSaveMsg{requestID: 2, filename: "test.txt", data: []byte("hello")})
	require.NotNil(t, cmd)
	saved := cmd().(fileSavedMsg)
	require.Error(t, saved.err)

	updated2, _ := updated.(model).Update(saved)
	assert.Equal(t, "Error: permission denied", updated2.(model).statusMsg)
}

func TestFileSaveMsgExistingFileAutoRenames(t *testing.T) {
	m := setupModel()
	m.saveRequestID = 3
	var savedName string
	m.writeFile = func(name string, _ []byte, _ os.FileMode) error {
		savedName = name
		return nil
	}
	// stat: test.txt and test.1.txt exist, test.2.txt does not.
	m.statFile = func(name string) (os.FileInfo, error) {
		if name == "test.txt" || name == "test.1.txt" {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}

	updated, cmd := m.Update(fileSaveMsg{requestID: 3, filename: "test.txt", data: []byte("hello")})
	require.NotNil(t, cmd)
	saved := cmd().(fileSavedMsg)
	assert.Equal(t, "test.txt", saved.original)
	assert.Equal(t, "test.2.txt", saved.target)
	assert.Equal(t, "test.2.txt", savedName)

	updated2, _ := updated.(model).Update(saved)
	status := updated2.(model).statusMsg
	assert.Contains(t, status, "test.txt")
	assert.Contains(t, status, "test.2.txt")
	assert.Contains(t, status, "existed")
}

func TestFileSaveMsgStaleRequestIDIgnored(t *testing.T) {
	// A late-arriving fileSaveMsg from a superseded extraction must be
	// dropped without dispatching a write. Without the requestID guard the
	// user sees a "Saved" status for a file they already moved on from.
	m := setupModel()
	m.saveRequestID = 5
	called := false
	m.writeFile = func(string, []byte, os.FileMode) error {
		called = true
		return nil
	}
	m.statFile = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }

	_, cmd := m.Update(fileSaveMsg{requestID: 4, filename: "stale.txt", data: []byte("x")})
	assert.Nil(t, cmd, "stale requestID must not dispatch a save cmd")
	assert.False(t, called)
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
	openViewer(&m, &image.FileContent{
		Path: "/etc/passwd",
		Data: []byte("root:x:0:0"),
		Size: 10,
	})

	updated, cmd := m.Update(keyPress('Y'))
	um := updated.(model)
	assert.True(t, um.copyConfirm)
	assert.NotNil(t, cmd)
}

func TestCopyContentShiftYInLayerPanelIsNoOp(t *testing.T) {
	// 'Y' must do nothing in the layers panel — that role belongs to 'c'
	// (copy Dockerfile command). Y is reserved for the file viewer's
	// copy-content action. See keymap.CopyContent and tui/help.go.
	m := setupModel()
	m.focus = focusLayers

	updated, cmd := m.Update(keyPress('Y'))
	um := updated.(model)
	assert.False(t, um.copyConfirm)
	assert.Nil(t, cmd)
}

// --- Viewer Search (/n/N) ---

func TestViewerSearchActivatesOnSlash(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	openViewer(&m, &image.FileContent{
		Path: "/etc/passwd",
		Data: []byte("root:x:0:0\nnobody:x:65534:65534"),
		Size: 30,
	})

	updated, _ := m.Update(keyPress('/'))
	um := updated.(model)
	assert.True(t, um.viewSearchActive)
}

func TestViewerSearchTypingBuildsQuery(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	openViewer(&m, &image.FileContent{
		Path: "/etc/passwd",
		Data: []byte("root:x:0:0\nnobody:x:65534:65534"),
		Size: 30,
	})
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
	openViewer(&m, &image.FileContent{
		Path: "/test",
		Data: []byte("hello world"),
		Size: 11,
	})
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
	openViewer(&m, &image.FileContent{
		Path: "/test",
		Data: []byte("hello world"),
		Size: 11,
	})
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
	openViewer(&m, &image.FileContent{
		Path: "/test",
		Data: []byte("aaa\naaa\naaa"),
		Size: 11,
	})
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
	openViewer(&m, &image.FileContent{
		Path: "/test",
		Data: []byte(strings.Repeat("line\n", 100)),
		Size: 500,
	})
	m.viewOffset = 0

	updated, _ := m.Update(keyPress('j'))
	um := updated.(model)
	assert.Equal(t, 1, um.viewOffset)
}

func TestViewerEscCascadeWithSearch(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	openViewer(&m, &image.FileContent{
		Path: "/test",
		Data: []byte("test"),
		Size: 4,
	})
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

func TestMouseWheelDownScrollsViewer(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, fmt.Sprintf("line%d", i))
	}
	data := []byte(strings.Join(lines, "\n") + "\n")
	openViewer(&m, &image.FileContent{
		Path: "test.txt",
		Data: data,
		Size: int64(len(data)),
	})
	m.height = 20

	m = send(m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	assert.Equal(t, 1, m.viewOffset)
}

func TestMouseWheelUpScrollsViewer(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	openViewer(&m, &image.FileContent{
		Path: "test.txt",
		Data: []byte("line1\nline2\nline3\n"),
		Size: 18,
	})
	m.viewOffset = 1
	m.height = 20

	m = send(m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	assert.Equal(t, 0, m.viewOffset)
}

func TestMouseWheelMovesLayerCursor(t *testing.T) {
	m := setupModelWithDiffs()
	m.focus = focusLayers
	m.layerCursor = 0

	m = send(m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	assert.Equal(t, 1, m.layerCursor)
}

func TestMouseWheelIgnoredWhenHelpOpen(t *testing.T) {
	m := setupModelWithDiffs()
	m.showHelp = true
	m.layerCursor = 0

	m = send(m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	assert.Equal(t, 0, m.layerCursor)
}

// --- Status bar Enter hint ---------------------------------------------------

func TestStatusBarEnterHintFileShowsView(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	files := m.displayTree()
	found := false
	for i, f := range files {
		if !f.IsDir {
			m.treeCursor = i
			found = true
			break
		}
	}
	assert.True(t, found, "expected at least one regular file in fixture tree")
	bar := m.renderStatusBar(files)
	assert.Contains(t, bar, "view")
	assert.NotContains(t, bar, "toggle")
}

func TestStatusBarEnterHintDirShowsToggleNotView(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	files := m.displayTree()
	found := false
	for i, f := range files {
		if f.IsDir {
			m.treeCursor = i
			found = true
			break
		}
	}
	assert.True(t, found, "expected at least one directory in fixture tree")
	bar := m.renderStatusBar(files)
	assert.Contains(t, bar, "toggle")
	// "view" must not appear as a hint description; the trailing-space anchor
	// avoids false matches on words like "viewport" if any other hint text
	// grows that way later.
	assert.NotContains(t, bar, " view")
}

// atomicWriteFile must materialize the target only after the bytes are fully
// written and synced — never as a partial truncation. This pins the
// temp+rename contract: the caller-visible target either does not exist
// or holds the complete payload, never a half-write.
func TestAtomicWriteFile_TargetVisibleOnlyOnCompletion(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.txt")
	payload := []byte("complete content")

	require.NoError(t, atomicWriteFile(target, payload, 0644))

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, payload, got)

	// No stray temp file is left next to the target.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".layerx-save-"),
			"stray temp file %q must not survive a successful save", e.Name())
	}
}

// atomicWriteFile must replace an existing file atomically, never leaving
// the user with the empty intermediate state os.WriteFile produced when
// killed mid-write.
func TestAtomicWriteFile_ReplacesExistingAtomically(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.txt")
	require.NoError(t, os.WriteFile(target, []byte("OLD"), 0644))

	require.NoError(t, atomicWriteFile(target, []byte("NEW PAYLOAD"), 0644))

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, []byte("NEW PAYLOAD"), got)
}

// atomicWriteFile must not leak a temp file when the target directory does
// not exist. Pre-fix os.WriteFile failed straight away with the same
// behaviour; the rename-based implementation must match.
func TestAtomicWriteFile_FailsCleanlyOnMissingDir(t *testing.T) {
	// CreateTemp inside a non-existent dir fails before any write happens —
	// no leaked temp anywhere, matching os.WriteFile's behaviour.
	err := atomicWriteFile(filepath.Join(t.TempDir(), "no-such-dir", "out.txt"), []byte("x"), 0644)
	require.Error(t, err)
}

// fileContentMsg must dispatch a tea.Cmd that performs Chroma highlighting
// asynchronously. The Update goroutine returns immediately; the highlighted
// lines arrive via highlightedMsg in a later turn. Without this seam, a
// large source file could freeze the TUI for hundreds of milliseconds while
// chroma tokenised it.
func TestFileContentMsg_DispatchesHighlightCmd(t *testing.T) {
	m := setupModel()
	m.viewRequestID = 9
	src := []byte("package main\n\nfunc main() {}\n")
	updated, cmd := m.Update(fileContentMsg{
		requestID: 9,
		content: &image.FileContent{
			Path: "main.go",
			Data: src,
		},
	})
	um := updated.(model)

	// Highlight must NOT have been computed inside Update — that's the bug
	// we're fixing. Lines arrive in a follow-up message.
	assert.Nil(t, um.viewHighlightedLines, "highlight must defer to a tea.Cmd, not run inline")
	require.NotNil(t, cmd, "fileContentMsg must dispatch a highlight cmd for non-binary text")

	// Run the dispatched Cmd; expect a highlightedMsg with matching requestID.
	msg := cmd()
	hm, ok := msg.(highlightedMsg)
	require.True(t, ok, "expected highlightedMsg, got %T", msg)
	assert.Equal(t, uint64(9), hm.requestID)

	// Apply it; viewHighlightedLines is now populated (or nil if Chroma
	// declined to highlight; the contract is that it's set, not its content).
	updated2, _ := um.Update(hm)
	_ = updated2
}

// A late highlight from a previous extract must be discarded once the user
// has navigated to a different file. Without the requestID gate, switching
// files quickly would leave stale colors painted on the wrong file.
func TestHighlightedMsg_StaleRequestIDDiscarded(t *testing.T) {
	m := setupModel()
	m.viewRequestID = 12
	m.viewHighlightedLines = []string{"current"}

	updated, _ := m.Update(highlightedMsg{requestID: 11, lines: []string{"stale"}})
	um := updated.(model)
	assert.Equal(t, []string{"current"}, um.viewHighlightedLines,
		"stale highlight must not overwrite the current one")
}

// --- Horizontal scroll for off-screen search matches -------------------------

// Bug: pressing 'n' on a match whose column was past the visible width
// updated the status bar to "Match found" but the highlight stayed clipped
// off the right edge — invisible. The fix centers viewHOffset on the match's
// display column whenever the column falls outside the visible window.
func TestScrollToViewerMatch_AdjustsHOffsetForOffScreenMatch(t *testing.T) {
	m := setupModel()
	m.width = 80
	m.height = 30
	// One long line; the match starts at column 200 — well past the
	// ~78-column display window after the gutter is subtracted.
	prefix := strings.Repeat("x", 200)
	data := []byte(prefix + "needle and rest of the line")
	m.viewState = viewReady
	openViewer(&m, &image.FileContent{Path: "/long.txt", Data: data, Size: int64(len(data))})
	m.viewSearchQuery = "needle"
	m.recomputeViewerMatches()

	require.Len(t, m.viewSearchMatches, 1, "expected exactly one match")
	assert.Equal(t, [2]int{0, 200}, m.viewSearchMatches[0])
	assert.Greater(t, m.viewHOffset, 0,
		"hOffset must shift right so the match is visible; got %d", m.viewHOffset)

	// The match's display column (200) must lie inside [hOffset, hOffset+visWidth).
	visWidth := m.viewVisibleWidth()
	require.Greater(t, visWidth, 0)
	assert.GreaterOrEqual(t, 200, m.viewHOffset, "match must be at or past hOffset")
	assert.Less(t, 200, m.viewHOffset+visWidth, "match must be before hOffset+visWidth")
}

func TestScrollToViewerMatch_LeavesHOffsetWhenMatchAlreadyVisible(t *testing.T) {
	m := setupModel()
	m.width = 80
	m.height = 30
	// Short line with the match well within the viewport.
	data := []byte("hello needle world")
	m.viewState = viewReady
	openViewer(&m, &image.FileContent{Path: "/short.txt", Data: data, Size: int64(len(data))})
	m.viewSearchQuery = "needle"
	m.recomputeViewerMatches()

	require.Len(t, m.viewSearchMatches, 1)
	assert.Equal(t, 0, m.viewHOffset, "no scrolling needed when match already on-screen")
}

// recomputeViewerMatches must reset hOffset before scrolling — otherwise a
// stale offset from the previous query would persist when the new query's
// first match falls in the un-shifted region of the line.
func TestRecomputeViewerMatches_ResetsHOffsetOnEmptyQuery(t *testing.T) {
	m := setupModel()
	openViewer(&m, &image.FileContent{Path: "/x", Data: []byte("abc"), Size: 3})
	m.viewHOffset = 999

	m.viewSearchQuery = ""
	m.recomputeViewerMatches()
	assert.Equal(t, 0, m.viewHOffset, "clearing the query must clear hOffset")
}

// Esc on a confirmed search query clears the query and any horizontal scroll
// it caused — the next view of the file should be from column 0, not stuck
// in the middle of a long line where the last match landed.
func TestViewerEsc_ClearsHOffsetWithSearch(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	openViewer(&m, &image.FileContent{Path: "/x", Data: []byte("hello world"), Size: 11})
	m.viewSearchQuery = "world"
	m.viewHOffset = 50

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	um := updated.(model)
	assert.Equal(t, "", um.viewSearchQuery)
	assert.Equal(t, 0, um.viewHOffset, "Esc must reset horizontal scroll alongside the query")
}

// h moves the logical cursor left; the viewport (hOffset) only shifts
// when the cursor would otherwise fall off the left edge. From column 0
// with no horizontal scroll the row stays put — vim's sidescroll model.
func TestViewerHKey_MovesCursorLeft(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	// Long line so the cursor has somewhere to be; place the viewport
	// past the start so a leftward step on the cursor at the left edge
	// drags the viewport but no further than column 0.
	long := strings.Repeat("x", 200)
	openViewer(&m, &image.FileContent{Path: "/x", Data: []byte(long), Size: int64(len(long))})
	m.viewHOffset = 50
	m.viewCursorCol = 50 // at the visible left edge

	um := send(m, keyPress('h'))
	assert.Less(t, um.viewCursorCol, 50, "h must move the cursor left")
	assert.GreaterOrEqual(t, um.viewCursorCol, 0)
	assert.LessOrEqual(t, um.viewHOffset, um.viewCursorCol,
		"viewport must follow the cursor across the left edge")
}

// l moves the logical cursor right within the visible area; the viewport
// stays put as long as the cursor is on-screen. Pressing l from column 0
// in a wide window must NOT shift hOffset — that was the bug.
func TestViewerLKey_KeepsViewportStable(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	long := strings.Repeat("x", 200)
	openViewer(&m, &image.FileContent{Path: "/x", Data: []byte(long), Size: int64(len(long))})
	m.viewHOffset = 0
	m.viewCursorCol = 0

	um := send(m, keyPress('l'))
	assert.Greater(t, um.viewCursorCol, 0, "l must advance the cursor")
	assert.Equal(t, 0, um.viewHOffset,
		"cursor inside the visible window must not shift the viewport")
}

// When the cursor walks past the right edge, the viewport scrolls just
// enough to keep the cursor visible — no more.
func TestViewerLKey_ScrollsViewportAtRightEdge(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	long := strings.Repeat("x", 500)
	openViewer(&m, &image.FileContent{Path: "/x", Data: []byte(long), Size: int64(len(long))})
	visWidth := m.viewVisibleWidth()
	require.Greater(t, visWidth, 0)
	// Park the cursor at the rightmost visible column. Next l takes it
	// past the edge and must drag the viewport along.
	m.viewHOffset = 0
	m.viewCursorCol = visWidth - 1

	um := send(m, keyPress('l'))
	assert.Greater(t, um.viewHOffset, 0, "viewport must scroll when cursor crosses right edge")
	assert.GreaterOrEqual(t, um.viewCursorCol, um.viewHOffset)
	assert.Less(t, um.viewCursorCol, um.viewHOffset+visWidth,
		"cursor must remain inside the visible window after the scroll")
}

func TestViewerHKey_ClampsAtZero(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	openViewer(&m, &image.FileContent{Path: "/x", Data: []byte("line"), Size: 4})
	m.viewHOffset = 0

	um := send(m, keyPress('h'))
	assert.Equal(t, 0, um.viewHOffset, "h at column 0 must not go negative")
}

// g and G in the viewer should reset horizontal scroll along with vertical —
// matching vim's behavior where line-jump commands return to column 0.
func TestViewerGTop_ResetsHOffset(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	openViewer(&m, &image.FileContent{Path: "/x", Data: []byte("a\nb\nc\n"), Size: 6})
	m.viewOffset = 2
	m.viewHOffset = 50

	um := send(m, keyPress('g'))
	assert.Equal(t, 0, um.viewOffset)
	assert.Equal(t, 0, um.viewHOffset, "g must reset hOffset")
}

func TestViewerGBottom_ResetsHOffset(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	openViewer(&m, &image.FileContent{Path: "/x", Data: []byte("a\nb\nc\n"), Size: 6})
	m.viewHOffset = 50

	um := send(m, keyPress('G'))
	assert.Equal(t, 0, um.viewHOffset, "G must reset hOffset")
}

// File open (fileContentMsg) must reset hOffset — opening a new file from
// the tree should start at the beginning of every line, not where the
// previous file's hOffset happened to land.
func TestFileContentMsg_ResetsHOffset(t *testing.T) {
	m := setupModel()
	m.viewHOffset = 100
	m.viewRequestID = 5

	updated, _ := m.Update(fileContentMsg{
		requestID: 5,
		content:   &image.FileContent{Path: "/x", Data: []byte("hi"), Size: 2},
	})
	um := updated.(model)
	assert.Equal(t, 0, um.viewHOffset, "opening a new file must reset hOffset")
}

// --- Aggregated layer view toggle (A) ----------------------------------------

func TestModel_AggregateToggle_FlipsModeAndResetsTreeCursor(t *testing.T) {
	m := setupModelWithDiffs()
	m.treeCursor = 5
	m.treeOffset = 3
	originalLayer := m.layerCursor

	m = send(m, keyPress('A'))

	assert.True(t, m.aggregated, "A must flip aggregated on")
	assert.Equal(t, 0, m.treeCursor, "treeCursor must reset on toggle")
	assert.Equal(t, 0, m.treeOffset, "treeOffset must reset on toggle")
	assert.Equal(t, originalLayer, m.layerCursor, "layerCursor must survive toggle")
}

func TestModel_AggregateToggle_RoutesCurrentTreeRoot(t *testing.T) {
	m := setupModelWithDiffs()
	require.NotNil(t, m.analysis.AggregatedTrees, "fixture must populate AggregatedTrees")

	stackedRoot := m.analysis.StackedTrees[m.layerCursor].Root
	aggregatedRoot := m.analysis.AggregatedTrees[m.layerCursor].Root
	require.NotNil(t, stackedRoot)
	require.NotNil(t, aggregatedRoot)

	assert.Same(t, stackedRoot, m.currentTreeRoot(), "default mode must point at StackedTrees")

	// Toggle into split mode. The top sub-pane (Layer Δ) keeps focus, so
	// currentTreeRoot still points at StackedTrees.
	m = send(m, keyPress('A'))
	assert.Same(t, stackedRoot, m.currentTreeRoot(),
		"split mode with top pane focused: current root is StackedTrees")

	// Tab from layers → top → bottom. Now the cumulative pane is active.
	m.focus = focusTreeAgg
	assert.Same(t, aggregatedRoot, m.currentTreeRoot(),
		"split mode with bottom pane focused: current root is AggregatedTrees")
}

func TestModel_AggregateToggle_NoOpWhenViewerOpen(t *testing.T) {
	m := setupModelWithDiffs()
	m.viewState = viewReady
	openViewer(&m, &image.FileContent{Path: "/x", Data: []byte("data"), Size: 4})

	m = send(m, keyPress('A'))

	assert.False(t, m.aggregated, "viewer-open guard must suppress A toggle")
}

func TestModel_AggregateToggle_FilterInputSwallowsA(t *testing.T) {
	m := setupModelWithDiffs()
	m.filterActive = true
	m.filterQuery = ""

	m = send(m, keyPress('A'))

	assert.False(t, m.aggregated, "filter-active guard must suppress A toggle")
	assert.Equal(t, "A", m.filterQuery, "A must be appended to filter query while filter input is active")
}

func TestModel_AggregateToggle_PreservesFilterQueryAndDiffOnly(t *testing.T) {
	m := setupModelWithDiffs()
	m.filterQuery = "etc"
	m.diffOnly = true
	m.sortMode = sortDesc

	m = send(m, keyPress('A'))

	assert.True(t, m.aggregated)
	assert.Equal(t, "etc", m.filterQuery, "filter query must survive toggle")
	assert.True(t, m.diffOnly, "diffOnly must survive toggle")
	assert.Equal(t, sortDesc, m.sortMode, "sortMode must survive toggle")
}

func TestModel_AggregateRender_NilAggregatedTreesFallsBackGracefully(t *testing.T) {
	m := setupModelWithDiffs()
	m.analysis.AggregatedTrees = nil
	m.aggregated = true
	m.focus = focusTreeAgg

	assert.Nil(t, m.currentTreeRoot(), "nil AggregatedTrees on focused agg pane must yield nil root, not panic")
	assert.Empty(t, m.displayTree(), "nil root must produce an empty display slice")
}

// --- Split-pane focus cycling -----------------------------------------------

// Tab cycle in split mode: layers → top tree (Δ) → bottom tree (cumulative)
// → layers. Without aggregated on, Tab is two-state (layers ↔ tree). The
// extra step lets the user visit the cumulative pane without a separate
// keybind.
func TestSplitMode_TabCyclesThreePanes(t *testing.T) {
	m := setupModelWithDiffs()
	m.aggregated = true
	m.focus = focusLayers

	m = send(m, keyPressSpecial(tea.KeyTab))
	assert.Equal(t, focusTree, m.focus, "Tab #1: layers → top tree")

	m = send(m, keyPressSpecial(tea.KeyTab))
	assert.Equal(t, focusTreeAgg, m.focus, "Tab #2: top → bottom tree")

	m = send(m, keyPressSpecial(tea.KeyTab))
	assert.Equal(t, focusLayers, m.focus, "Tab #3: bottom → layers")
}

func TestSplitMode_TabIsTwoStateWhenAggregatedOff(t *testing.T) {
	m := setupModelWithDiffs()
	m.aggregated = false
	m.focus = focusLayers

	m = send(m, keyPressSpecial(tea.KeyTab))
	assert.Equal(t, focusTree, m.focus)

	m = send(m, keyPressSpecial(tea.KeyTab))
	assert.Equal(t, focusLayers, m.focus, "without split, Tab is just layers ↔ tree")
}

// Toggling out of split mode while focused on the bottom (agg) sub-panel
// must not strand focus on a now-invisible pane. The active focus should
// snap back to the (single) tree pane.
func TestSplitMode_TogglingOffSnapsFocusBack(t *testing.T) {
	m := setupModelWithDiffs()
	m.aggregated = true
	m.focus = focusTreeAgg

	m = send(m, keyPress('A'))
	assert.False(t, m.aggregated)
	assert.Equal(t, focusTree, m.focus,
		"toggling off while focused on bottom must move focus to single tree pane")
}

// Each sub-pane has its own cursor — j on the top pane must not move the
// bottom pane's cursor and vice versa. Without independent cursors, the
// split view's value (compare two views without losing your place) is
// lost.
func TestSplitMode_PaneCursorsAreIndependent(t *testing.T) {
	m := setupModelWithDiffs()
	m.aggregated = true

	// Move top-pane cursor.
	m.focus = focusTree
	m = send(m, keyPress('j'))
	assert.Equal(t, 1, m.treeCursor)
	assert.Equal(t, 0, m.aggCursor, "moving top must not touch bottom cursor")

	// Move bottom-pane cursor.
	m.focus = focusTreeAgg
	m = send(m, keyPress('j'))
	m = send(m, keyPress('j'))
	assert.Equal(t, 2, m.aggCursor)
	assert.Equal(t, 1, m.treeCursor, "moving bottom must not touch top cursor")
}

// In split mode the tree-flat-list helper m.displayTree() returns the
// active pane's slice. The two panes share node *structure* (both show
// the cumulative filesystem at the layer cursor) but differ in DiffType
// labels: per-layer Δ marks only what this layer changed, cumulative
// preserves labels carried forward from earlier layers. The active-pane
// switch is what changes which labels the user sees.
func TestSplitMode_DisplayTreeFollowsFocus(t *testing.T) {
	m := setupModelWithDiffs()
	m.aggregated = true

	m.focus = focusTree
	topFiles := m.displayTree()
	m.focus = focusTreeAgg
	botFiles := m.displayTree()

	// Find a path that is Unchanged in the Δ view but Added in the
	// cumulative view: an L0 file like /etc/passwd. L1 didn't touch it,
	// so Stack labels it Unchanged; AggregatedTrees carries forward L0's
	// Added label. Different focus must produce different labels for the
	// same path.
	const probe = "/etc/passwd"
	topLabel := findDiffType(topFiles, probe)
	botLabel := findDiffType(botFiles, probe)
	require.NotEqual(t, image.DiffType(-1), topLabel, "probe path must exist in top pane")
	require.NotEqual(t, image.DiffType(-1), botLabel, "probe path must exist in bottom pane")
	assert.NotEqual(t, topLabel, botLabel,
		"split panes show the same path with different DiffTypes: %s top=%v bot=%v",
		probe, topLabel, botLabel)
}

// findDiffType returns the DiffType of the named path in files, or -1 if
// missing. Test helper kept local because the productive callers route
// node lookup through FindChild on the tree, not the flat slice.
func findDiffType(files []*image.FileNode, path string) image.DiffType {
	for _, f := range files {
		if f.Path == path {
			return f.DiffType
		}
	}
	return image.DiffType(-1)
}

// Filter applied while focused on the bottom pane must filter the bottom
// pane's slice — not just the top one. Both panes share the query but
// each pane filters its own files.
func TestSplitMode_FilterAppliesToBothPanes(t *testing.T) {
	m := setupModelWithDiffs()
	m.aggregated = true
	m.filterQuery = "nginx"

	m.focus = focusTree
	topFiltered := m.displayTree()
	for _, f := range topFiltered {
		assert.Contains(t, strings.ToLower(f.Path), "nginx")
	}

	m.focus = focusTreeAgg
	botFiltered := m.displayTree()
	for _, f := range botFiltered {
		assert.Contains(t, strings.ToLower(f.Path), "nginx")
	}
}

// --- Page navigation (#76) ---------------------------------------------------

// setupModelTallTree returns a ready model whose selected layer has many
// root-level files and a deliberately short viewport, so a page step is a
// handful of rows rather than larger than the whole list. This lets the page
// math and its boundaries be observed instead of always clamping to an end.
func setupModelTallTree(fileCount int) model {
	root := image.NewFileTree()
	for i := range fileCount {
		name := fmt.Sprintf("file%03d", i)
		root.Root.AddChild(&image.FileNode{
			Name: name,
			Path: "/" + name,
			Size: int64(i + 1),
		})
	}
	layers := []image.Layer{{Index: 0, ID: "tall", Command: "RUN touch files", Tree: root}}

	m := NewModel(Config{ImageRef: "test:latest"})
	m.width = 120
	// height 20 → treeVisibleHeightFor(focusTree) = 20 - 8 - 1 = 11 rows.
	m.height = 20
	m.state = stateReady
	m.analysis = &image.Analysis{
		ImageRef:     "test:latest",
		Layers:       layers,
		StackedTrees: image.Stack(layers),
	}
	m.focus = focusTree
	return m
}

func TestPageDownKeyIsWired(t *testing.T) {
	m := setupModelTallTree(100)
	before := m.treeCursor
	m = send(m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	assert.Greater(t, m.treeCursor, before, "ctrl+f should advance the tree cursor")
}

func TestPageDownPgDnAliasIsWired(t *testing.T) {
	m := setupModelTallTree(100)
	before := m.treeCursor
	m = send(m, keyPressSpecial(tea.KeyPgDown))
	assert.Greater(t, m.treeCursor, before, "PgDn should advance the tree cursor")
}

func TestFullPageMovesByVisibleHeight(t *testing.T) {
	m := setupModelTallTree(100)
	h := m.treeVisibleHeightFor(focusTree)
	require.Positive(t, h)
	m.moveByPage(1, false)
	assert.Equal(t, h, m.treeCursor, "full page should advance by one viewport height")
}

func TestHalfPageMovesByHalfVisibleHeight(t *testing.T) {
	m := setupModelTallTree(100)
	h := m.treeVisibleHeightFor(focusTree)
	require.Positive(t, h)
	m.moveByPage(1, true)
	assert.Equal(t, h/2, m.treeCursor, "half page should advance by half a viewport height")
}

func TestPageDownClampsAtLastItem(t *testing.T) {
	m := setupModelTallTree(5) // fewer items than a page
	files := m.displayTreeFor(focusTree)
	m.moveByPage(1, false)
	assert.Equal(t, len(files)-1, m.treeCursor, "page down past the end clamps to the last item")
}

func TestPageUpClampsAtFirstItem(t *testing.T) {
	m := setupModelTallTree(100)
	m.treeCursor = 3
	m.moveByPage(-1, false) // a full page back from row 3 undershoots 0
	assert.Equal(t, 0, m.treeCursor, "page up past the start clamps to the first item")
}

func TestPageDownThenUpIsReversible(t *testing.T) {
	m := setupModelTallTree(100)
	h := m.treeVisibleHeightFor(focusTree)
	m.moveByPage(1, false)
	m.moveByPage(1, false)
	assert.Equal(t, 2*h, m.treeCursor)
	m.moveByPage(-1, false)
	assert.Equal(t, h, m.treeCursor, "a page up should undo a page down of the same size")
}

func TestPageOnLayersPaneChangesSelectedLayer(t *testing.T) {
	m := setupModel() // 3 layers, focus starts on layers
	m.focus = focusLayers
	m.layerCursor = 0
	m.moveByPage(1, false)
	assert.Positive(t, m.layerCursor, "paging the layers pane advances the selected layer")
	assert.Equal(t, 0, m.treeCursor, "changing layer resets the tree cursor")
}

func TestPageOnEmptyTreeIsNoop(t *testing.T) {
	m := setupModelTallTree(0)
	m.focus = focusTree
	m.moveByPage(1, false)
	assert.Equal(t, 0, m.treeCursor, "paging an empty tree must not move or panic")
}

// --- displayTreeFor cache invalidation ---------------------------------------
//
// These tests exercise the memoization added to displayTreeFor. Each drives one
// input-mutation path and asserts the returned slice reflects the new state.
// They fail if an invalidation key is missing from the cache — the regression
// class the cache introduces.

func TestDisplayTreeCacheReflectsDiffOnlyToggle(t *testing.T) {
	m := setupModelWithDiffs()
	before := len(m.displayTreeFor(focusTree)) // warms the cache

	m.diffOnly = true
	filtered := m.displayTreeFor(focusTree)

	assert.Less(t, len(filtered), before, "diff-only must drop unchanged files even after the cache is warm")
	for _, f := range filtered {
		assert.NotEqual(t, image.Unchanged, f.DiffType)
	}
}

func TestDisplayTreeCacheReflectsFilterQuery(t *testing.T) {
	m := setupModelWithDiffs()
	all := m.displayTreeFor(focusTree) // warms the cache
	require.NotEmpty(t, all)

	m.filterQuery = "nginx"
	filtered := m.displayTreeFor(focusTree)

	assert.NotEmpty(t, filtered)
	assert.Less(t, len(filtered), len(all), "a filter query must narrow the cached slice")
	for _, f := range filtered {
		assert.Contains(t, strings.ToLower(f.Path), "nginx")
	}
}

func TestDisplayTreeCacheReflectsSortMode(t *testing.T) {
	m := setupModelWithDiffs()
	unsorted := m.displayTreeFor(focusTree) // warms the cache
	require.NotEmpty(t, unsorted)

	m.sortMode = sortDesc
	sorted := m.displayTreeFor(focusTree)

	// Sorting by size descending must yield non-increasing effective sizes.
	for i := 1; i < len(sorted); i++ {
		assert.GreaterOrEqual(t, nodeEffectiveSize(sorted[i-1]), nodeEffectiveSize(sorted[i]),
			"sortDesc must return files by descending effective size, not a stale unsorted slice")
	}
}

func TestDisplayTreeCacheReflectsLayerChange(t *testing.T) {
	m := setupModelWithDiffs()
	m.layerCursor = 0
	base := m.displayTreeFor(focusTree) // warms the cache for layer 0

	m.layerCursor = 1
	next := m.displayTreeFor(focusTree)

	// The two layers have different tree shapes; a stale cache would return the
	// layer-0 slice for layer 1.
	assert.NotEqual(t, base, next, "changing the selected layer must recompute the tree")
}

func TestDisplayTreeCacheReflectsCollapseToggle(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	expanded := m.displayTreeFor(focusTree) // warms the cache
	require.NotEmpty(t, expanded)

	// Collapse the first directory in the visible list.
	var dirPath string
	for _, f := range expanded {
		if f.IsDir {
			dirPath = f.Path
			break
		}
	}
	require.NotEmpty(t, dirPath, "fixture must contain at least one directory")

	m.treeCollapsed = toggleCollapsed(m.treeCollapsed, dirPath)
	m.collapsedGen++
	collapsed := m.displayTreeFor(focusTree)

	assert.Less(t, len(collapsed), len(expanded),
		"collapsing a directory must hide its descendants even after the cache is warm")
}

func TestDisplayTreeCachePanesAreIndependent(t *testing.T) {
	m := setupModelWithDiffs()
	m.aggregated = true

	// Rendering split mode asks for both panes in one frame; the two cache
	// slots must not clobber each other.
	top := m.displayTreeFor(focusTree)
	bot := m.displayTreeFor(focusTreeAgg)
	topAgain := m.displayTreeFor(focusTree)
	botAgain := m.displayTreeFor(focusTreeAgg)

	require.NotEmpty(t, top)
	require.NotEmpty(t, bot)
	assert.Equal(t, &top[0], &topAgain[0], "the top pane slot must survive a bot-pane lookup in the same frame")
	assert.Equal(t, &bot[0], &botAgain[0], "the bot pane slot must survive a top-pane lookup in the same frame")
	assert.NotEqual(t, &top[0], &bot[0], "the two panes must not share one cache slot")
}

func TestDisplayTreeCacheHitReturnsIdenticalSlice(t *testing.T) {
	m := setupModelWithDiffs()
	first := m.displayTreeFor(focusTree)
	second := m.displayTreeFor(focusTree)

	// A warm hit with unchanged inputs should return the very same backing
	// slice, not recompute a fresh one.
	require.NotEmpty(t, first)
	assert.Equal(t, &first[0], &second[0], "an unchanged repeat lookup should return the cached slice")
}

func TestDisplayTreeCacheReflectsReanalysis(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	m.layerCursor = 0
	before := m.displayTreeFor(focusTree) // warms the cache for the first analysis
	require.NotEmpty(t, before)

	// A second analysis replaces m.analysis while layerCursor and every filter
	// stay put. Without the analysisGen key the warm slot would return the old
	// analysis's nodes; the gen bump in the analysisMsg handler must force a
	// recompute against the new tree.
	other := testAnalysis()
	root := other.StackedTrees[0].Root
	root.AddChild(&image.FileNode{Name: "REANALYZED", Path: "/REANALYZED", Size: 1})
	m = send(m, analysisMsg{analysis: other})

	after := m.displayTreeFor(focusTree)
	var found bool
	for _, f := range after {
		if f.Path == "/REANALYZED" {
			found = true
			break
		}
	}
	assert.True(t, found, "a replaced analysis must invalidate the warm tree cache")
}

