package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"

	"github.com/deveshctl/layerx/image"
)

// Config holds the parameters needed to start the TUI.
type Config struct {
	ImageRef string
	Resolver image.Resolver
	NoCache  bool
}

type focus int

const (
	focusLayers focus = iota
	focusTree
)

type appState int

const (
	stateLoading appState = iota
	stateReady
	stateError
)

type sortMode int

const (
	sortNone sortMode = iota
	sortDesc
	sortAsc
)

type sizeColMode int

const (
	sizeColDelta sizeColMode = iota
	sizeColBlob
	sizeColBoth
)

type viewState int

const (
	viewNone viewState = iota
	viewLoading
	viewReady
)

// fileContentMsg is sent when async file extraction completes.
type fileContentMsg struct {
	requestID uint64
	content   *image.FileContent
	err       error
}

// analysisMsg is sent when the background fetch completes.
type analysisMsg struct {
	analysis *image.Analysis
	err      error
}

// inspectMsg is sent when the quick image inspect completes.
type inspectMsg struct {
	meta *image.ImageMeta
	err  error
}

// progressMsg reports loading progress from the resolver.
type progressMsg struct {
	event image.ProgressEvent
}

// spinnerTickMsg triggers a spinner frame advance.
type spinnerTickMsg struct{}

// clearCopyMsg clears the "Copied!" confirmation after a timeout.
type clearCopyMsg struct{}

// clearStatusMsg clears the transient status bar message after a timeout.
type clearStatusMsg struct{}

// fileSaveMsg is sent when async file extraction for save-to-disk completes.
type fileSaveMsg struct {
	requestID uint64
	filename  string
	data      []byte
	err       error
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type model struct {
	width        int
	height       int
	focus        focus
	state        appState
	imageRef     string
	analysis     *image.Analysis
	layerCursor  int
	layerOffset  int
	treeCursor   int
	treeOffset   int
	errMsg       string
	quitting     bool
	resolver     image.Resolver
	spinnerFrame int
	imageSize    int64
	loadPhase    image.ProgressPhase
	pullLayers   int
	pullTotal    int
	pullBytes    int64
	pullBytesMax int64
	progressCh   chan image.ProgressEvent
	copyConfirm  bool
	statusMsg    string
	showHelp     bool
	filterActive bool
	filterQuery  string
	diffOnly      bool
	sortMode      sortMode
	treeCollapsed map[string]bool
	viewState    viewState
	viewContent      *image.FileContent
	viewHighlightedLines []string
	viewOffset       int
	viewOriginLayer  int
	viewOriginCmd    string
	viewSearchActive bool
	viewSearchQuery  string
	viewSearchMatches [][2]int
	viewSearchCursor int
	viewRequestID    uint64
	viewerCancel     context.CancelFunc
	saveRequestID    uint64
	saveCancel       context.CancelFunc
	extractor        image.Extractor
	efficiency       *image.EfficiencyResult
	writeFile        func(string, []byte, os.FileMode) error
	keys             keyMap
	showWaste     bool
	wasteCursor   int
	wasteOffset   int
	wasteExpanded bool
	wasteRows     []wasteRow
	sizeMode      sizeColMode
	noCache       bool

	fetchCtx    context.Context
	fetchCancel context.CancelFunc
}

// NewModel creates a new model wired to real Docker data.
func NewModel(cfg Config) model {
	ch := make(chan image.ProgressEvent, 16)
	ctx, cancel := context.WithCancel(context.Background())
	return model{
		state:       stateLoading,
		imageRef:    cfg.ImageRef,
		resolver:    cfg.Resolver,
		progressCh:  ch,
		writeFile:   os.WriteFile,
		keys:        defaultKeys(),
		noCache:     cfg.NoCache,
		fetchCtx:    ctx,
		fetchCancel: cancel,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.fetchInspect(), m.fetchAnalysisWithProgress(m.progressCh), listenForProgress(m.progressCh), m.spinnerTick())
}

func (m model) fetchInspect() tea.Cmd {
	resolver := m.resolver
	imageRef := m.imageRef
	ctx := m.fetchCtx
	return func() tea.Msg {
		meta, err := resolver.Inspect(ctx, imageRef)
		return inspectMsg{meta: meta, err: err}
	}
}

func (m model) spinnerTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

func (m model) fetchAnalysisWithProgress(progressCh chan<- image.ProgressEvent) tea.Cmd {
	resolver := m.resolver
	imageRef := m.imageRef
	noCache := m.noCache
	ctx := m.fetchCtx
	return func() tea.Msg {
		defer close(progressCh)
		result, err := image.AnalyzeWithOptions(ctx, resolver, imageRef,
			image.AnalyzeOptions{NoCache: noCache, Progress: progressCh})
		return analysisMsg{analysis: result, err: err}
	}
}

func listenForProgress(ch <-chan image.ProgressEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return nil
		}
		return progressMsg{event: event}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.state == stateReady {
			m.clampCursors()
		}
		return m, nil

	case spinnerTickMsg:
		if m.state == stateLoading || m.viewState == viewLoading {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
			return m, m.spinnerTick()
		}
		return m, nil

	case inspectMsg:
		if msg.err == nil && msg.meta != nil {
			m.imageSize = msg.meta.Size
		}
		return m, nil

	case progressMsg:
		// PhaseCacheWarn is a non-fatal diagnostic. Surface it without
		// overwriting the active loading phase — the analyze pipeline is
		// still running and the user wants to see what it's doing.
		if msg.event.Phase == image.PhaseCacheWarn {
			m.statusMsg = "cache: " + msg.event.Message
			return m, tea.Batch(
				listenForProgress(m.progressCh),
				tea.Tick(4*time.Second, func(time.Time) tea.Msg {
					return clearStatusMsg{}
				}),
			)
		}
		m.loadPhase = msg.event.Phase
		m.pullLayers = msg.event.LayersDone
		m.pullTotal = msg.event.LayersTotal
		m.pullBytes = msg.event.BytesCurr
		m.pullBytesMax = msg.event.BytesTotal
		return m, listenForProgress(m.progressCh)

	case analysisMsg:
		if msg.err != nil {
			m.state = stateError
			m.errMsg = friendlyError(msg.err)
			return m, nil
		}
		m.state = stateReady
		m.analysis = msg.analysis
		m.efficiency = image.Efficiency(msg.analysis.Layers)
		if src, ok := m.resolver.(image.ExtractorSource); ok {
			m.extractor = src.NewExtractor()
		}
		m.clampCursors()
		return m, nil

	case clearCopyMsg:
		m.copyConfirm = false
		return m, nil

	case clearStatusMsg:
		m.statusMsg = ""
		return m, nil

	case tea.MouseWheelMsg:
		if m.quitting || m.showHelp || m.showWaste || m.filterActive {
			return m, nil
		}
		if m.state != stateReady {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseWheelUp:
			switch m.viewState {
			case viewReady:
				m.scrollViewUp()
			case viewNone:
				m.moveUp()
			}
		case tea.MouseWheelDown:
			switch m.viewState {
			case viewReady:
				m.scrollViewDown()
			case viewNone:
				m.moveDown()
			}
		}
		return m, nil

	case fileContentMsg:
		if msg.requestID != m.viewRequestID {
			return m, nil
		}
		if m.viewerCancel != nil {
			m.viewerCancel()
			m.viewerCancel = nil
		}
		if msg.err != nil {
			m.viewState = viewNone
			m.statusMsg = "Error: " + msg.err.Error()
			return m, tea.Tick(3*time.Second, func(time.Time) tea.Msg {
				return clearStatusMsg{}
			})
		}
		m.viewState = viewReady
		m.viewContent = msg.content
		m.viewHighlightedLines = nil
		if msg.content != nil && !msg.content.Binary && len(msg.content.Data) > 0 {
			m.viewHighlightedLines = highlightFileLines(msg.content.Path, msg.content.Data)
		}
		m.viewOffset = 0
		return m, nil

	case fileSaveMsg:
		if msg.requestID != m.saveRequestID {
			return m, nil
		}
		if m.saveCancel != nil {
			m.saveCancel()
			m.saveCancel = nil
		}
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
			return m, tea.Tick(3*time.Second, func(time.Time) tea.Msg {
				return clearStatusMsg{}
			})
		}
		err := m.writeFile(msg.filename, msg.data, 0644)
		if err != nil {
			m.statusMsg = "Error: " + err.Error()
			return m, tea.Tick(3*time.Second, func(time.Time) tea.Msg {
				return clearStatusMsg{}
			})
		}
		m.statusMsg = "Saved: " + msg.filename
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
			return clearStatusMsg{}
		})

	case tea.KeyPressMsg:
		// Esc has precedence: viewer search → viewer → filter (active) → filter (confirmed) → help → quit
		if msg.Code == tea.KeyEscape {
			if m.viewState != viewNone {
				if m.viewSearchActive {
					m.viewSearchActive = false
					m.viewSearchQuery = ""
					m.viewSearchMatches = nil
					m.viewSearchCursor = 0
					return m, nil
				}
				if m.viewSearchQuery != "" {
					m.viewSearchQuery = ""
					m.viewSearchMatches = nil
					m.viewSearchCursor = 0
					return m, nil
				}
				m.viewState = viewNone
				m.viewContent = nil
				m.viewHighlightedLines = nil
				m.viewOffset = 0
				m.viewRequestID++
				if m.viewerCancel != nil {
					m.viewerCancel()
					m.viewerCancel = nil
				}
				return m, nil
			}
			if m.showWaste {
				m.closeWaste()
				return m, nil
			}
			if m.filterActive {
				m.filterActive = false
				m.filterQuery = ""
				m.treeCursor = 0
				m.treeOffset = 0
				return m, nil
			}
			if m.filterQuery != "" {
				m.filterQuery = ""
				m.treeCursor = 0
				m.treeOffset = 0
				return m, nil
			}
			if m.showHelp {
				m.showHelp = false
				return m, nil
			}
			m.quitting = true
			if m.fetchCancel != nil {
				m.fetchCancel()
			}
			return m, tea.Quit
		}

		// Quit via q or ctrl+c. When a text input is active (filter or
		// viewer search), 'q' must reach the input handler so users can
		// type queries containing 'q' (e.g. "jquery"). ctrl+c still quits.
		if key.Matches(msg, m.keys.Quit) {
			inTextInput := m.filterActive || (m.viewState == viewReady && m.viewSearchActive)
			if !inTextInput || msg.String() == "ctrl+c" {
				m.quitting = true
				if m.fetchCancel != nil {
					m.fetchCancel()
				}
				return m, tea.Quit
			}
		}

		// When filter input is active, capture all keys.
		if m.filterActive {
			return m.handleFilterInput(msg)
		}

		// When waste overlay is open, capture all keys.
		if m.showWaste {
			return m.handleWasteOverlay(msg)
		}

		// Help toggle works when ready.
		if key.Matches(msg, m.keys.Help) && m.state == stateReady {
			m.showHelp = !m.showHelp
			return m, nil
		}

		// When help is shown, swallow all other keys.
		if m.showHelp {
			return m, nil
		}

		// When viewing a file, only scroll/close/search keys work.
		if m.viewState == viewReady {
			if m.viewSearchActive {
				return m.handleViewerSearchInput(msg)
			}
			switch {
			case key.Matches(msg, m.keys.ViewerSearch):
				m.viewSearchActive = true
				return m, nil
			case key.Matches(msg, m.keys.NextMatch):
				if len(m.viewSearchMatches) > 0 {
					m.viewSearchCursor = (m.viewSearchCursor + 1) % len(m.viewSearchMatches)
					m.scrollToViewerMatch()
				}
				return m, nil
			case key.Matches(msg, m.keys.PrevMatch):
				if len(m.viewSearchMatches) > 0 {
					m.viewSearchCursor = (m.viewSearchCursor - 1 + len(m.viewSearchMatches)) % len(m.viewSearchMatches)
					m.scrollToViewerMatch()
				}
				return m, nil
			case key.Matches(msg, m.keys.CopyContent):
				if m.viewContent != nil && !m.viewContent.Binary && len(m.viewContent.Data) > 0 {
					m.copyConfirm = true
					return m, tea.Batch(
						tea.SetClipboard(string(m.viewContent.Data)),
						tea.Tick(2*time.Second, func(time.Time) tea.Msg {
							return clearCopyMsg{}
						}),
					)
				}
				return m, nil
			case key.Matches(msg, m.keys.Down):
				m.scrollViewDown()
			case key.Matches(msg, m.keys.Up):
				m.scrollViewUp()
			case key.Matches(msg, m.keys.Top):
				m.viewOffset = 0
			case key.Matches(msg, m.keys.Bottom):
				maxOffset := max(fileViewLineCount(m.viewContent)-m.viewVisibleHeight(), 0)
				m.viewOffset = maxOffset
			}
			return m, nil
		}
		if m.viewState == viewLoading {
			return m, nil
		}

		// Navigation only works when ready.
		if m.state != stateReady {
			return m, nil
		}

		switch {
		case key.Matches(msg, m.keys.Switch):
			if m.focus == focusLayers {
				m.focus = focusTree
			} else {
				m.focus = focusLayers
			}
			return m, nil

		case key.Matches(msg, m.keys.SizeColumn):
			if m.focus != focusLayers {
				return m, nil
			}
			switch m.sizeMode {
			case sizeColDelta:
				m.sizeMode = sizeColBlob
			case sizeColBlob:
				m.sizeMode = sizeColBoth
			case sizeColBoth:
				m.sizeMode = sizeColDelta
			}
			return m, nil

		case key.Matches(msg, m.keys.Waste):
			if m.viewState != viewNone || m.filterActive || m.showHelp || m.showWaste {
				return m, nil
			}
			m.openWaste()
			return m, nil

		case key.Matches(msg, m.keys.Down):
			m.moveDown()
			return m, nil

		case key.Matches(msg, m.keys.Up):
			m.moveUp()
			return m, nil

		case key.Matches(msg, m.keys.Top):
			m.moveToTop()
			return m, nil

		case key.Matches(msg, m.keys.Bottom):
			m.moveToBottom()
			return m, nil

		case key.Matches(msg, m.keys.Copy):
			layers := m.layers()
			if m.layerCursor < len(layers) {
				cmd := layers[m.layerCursor].Command
				m.copyConfirm = true
				return m, tea.Batch(
					tea.SetClipboard(cmd),
					tea.Tick(2*time.Second, func(time.Time) tea.Msg {
						return clearCopyMsg{}
					}),
				)
			}
			return m, nil

		case key.Matches(msg, m.keys.CopyPath):
			if m.focus == focusTree {
				files := m.displayTree()
				if m.treeCursor < len(files) {
					m.copyConfirm = true
					return m, tea.Batch(
						tea.SetClipboard(files[m.treeCursor].Path),
						tea.Tick(2*time.Second, func(time.Time) tea.Msg {
							return clearCopyMsg{}
						}),
					)
				}
			}
			return m, nil

		case key.Matches(msg, m.keys.CopyContent):
			if m.focus == focusLayers {
				layers := m.layers()
				if m.layerCursor < len(layers) {
					m.copyConfirm = true
					return m, tea.Batch(
						tea.SetClipboard(layers[m.layerCursor].Command),
						tea.Tick(2*time.Second, func(time.Time) tea.Msg {
							return clearCopyMsg{}
						}),
					)
				}
			}
			return m, nil

		case key.Matches(msg, m.keys.Filter):
			if m.focus == focusTree {
				m.filterActive = true
				return m, nil
			}
			return m, nil

		case msg.Code == tea.KeyBackspace:
			if m.focus == focusTree && !m.filterActive && m.filterQuery != "" {
				m.filterQuery = ""
				m.treeCursor = 0
				m.treeOffset = 0
				return m, nil
			}

		case msg.Code == tea.KeyEnter:
			if m.focus == focusTree {
				return m.tryOpenSelectedFile()
			}
			return m, nil

		case key.Matches(msg, m.keys.DiffOnly):
			m.diffOnly = !m.diffOnly
			m.treeCursor = 0
			m.treeOffset = 0
			return m, nil

		case key.Matches(msg, m.keys.Sort):
			switch m.sortMode {
			case sortNone:
				m.sortMode = sortDesc
			case sortDesc:
				m.sortMode = sortAsc
			case sortAsc:
				m.sortMode = sortNone
			}
			m.treeCursor = 0
			m.treeOffset = 0
			return m, nil

		case key.Matches(msg, m.keys.ExtractFile):
			if m.focus != focusTree {
				return m, nil
			}
			files := m.displayTree()
			if m.treeCursor >= len(files) {
				return m, nil
			}
			f := files[m.treeCursor]
			if f.IsDir {
				m.statusMsg = "Cannot extract directory"
				return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
					return clearStatusMsg{}
				})
			}
			if f.DiffType == image.Removed {
				m.statusMsg = "File removed in this layer"
				return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
					return clearStatusMsg{}
				})
			}
			if m.extractor == nil {
				m.statusMsg = "Extractor unavailable"
				return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
					return clearStatusMsg{}
				})
			}
			m.statusMsg = "Extracting..."
			m.saveRequestID++
			if m.saveCancel != nil {
				m.saveCancel()
			}
			ctx, cancel := context.WithCancel(context.Background())
			m.saveCancel = cancel
			return m, m.fetchFileRaw(ctx, f.Path, m.saveRequestID)
		}
	}

	return m, nil
}

func (m model) handleFilterInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.Code {
	case tea.KeyEnter:
		m.filterActive = false
		return m, nil
	case tea.KeyBackspace:
		if len(m.filterQuery) > 0 {
			runes := []rune(m.filterQuery)
			m.filterQuery = string(runes[:len(runes)-1])
			m.treeCursor = 0
			m.treeOffset = 0
		} else {
			m.filterActive = false
		}
		return m, nil
	default:
		if msg.Text != "" {
			m.filterQuery += msg.Text
			m.treeCursor = 0
			m.treeOffset = 0
		}
		return m, nil
	}
}

func (m model) handleViewerSearchInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.Code {
	case tea.KeyEnter:
		m.viewSearchActive = false
		return m, nil
	case tea.KeyBackspace:
		if len(m.viewSearchQuery) > 0 {
			runes := []rune(m.viewSearchQuery)
			m.viewSearchQuery = string(runes[:len(runes)-1])
			m.recomputeViewerMatches()
		} else {
			m.viewSearchActive = false
		}
		return m, nil
	default:
		if msg.Text != "" {
			m.viewSearchQuery += msg.Text
			m.recomputeViewerMatches()
		}
		return m, nil
	}
}

func (m *model) recomputeViewerMatches() {
	m.viewSearchMatches = nil
	m.viewSearchCursor = 0
	if m.viewSearchQuery == "" || m.viewContent == nil || m.viewContent.Binary {
		return
	}
	query := strings.ToLower(m.viewSearchQuery)
	lines := splitFileLines(m.viewContent.Data)
	for lineIdx, line := range lines {
		lower := strings.ToLower(line)
		offset := 0
		for {
			idx := strings.Index(lower[offset:], query)
			if idx < 0 {
				break
			}
			m.viewSearchMatches = append(m.viewSearchMatches, [2]int{lineIdx, offset + idx})
			offset += idx + len(query)
		}
	}
}

func (m *model) scrollToViewerMatch() {
	if len(m.viewSearchMatches) == 0 {
		return
	}
	targetLine := m.viewSearchMatches[m.viewSearchCursor][0]
	visHeight := m.viewVisibleHeight()
	desired := max(targetLine-visHeight/2, 0)
	maxOffset := max(fileViewLineCount(m.viewContent)-visHeight, 0)
	if desired > maxOffset {
		desired = maxOffset
	}
	m.viewOffset = desired
}

func (m model) tryOpenSelectedFile() (tea.Model, tea.Cmd) {
	files := m.displayTree()
	if m.treeCursor >= len(files) {
		return m, nil
	}
	f := files[m.treeCursor]
	if f.IsDir {
		if m.useTreeCollapse() {
			m.treeCollapsed = toggleCollapsed(m.treeCollapsed, f.Path)
			mp := &m
			mp.clampCursors()
			return *mp, nil
		}
		var msg string
		switch {
		case m.sortMode != sortNone:
			msg = "Collapse unavailable while sorting"
		case m.filterQuery != "":
			msg = "Collapse unavailable while filtering"
		case m.diffOnly:
			msg = "Collapse unavailable in diff-only mode"
		}
		m.statusMsg = msg
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
			return clearStatusMsg{}
		})
	}
	if f.DiffType == image.Removed {
		m.statusMsg = "File removed in this layer"
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
			return clearStatusMsg{}
		})
	}
	if m.extractor == nil {
		m.statusMsg = "Extractor unavailable"
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
			return clearStatusMsg{}
		})
	}
	m.viewState = viewLoading
	m.viewOriginLayer = f.IntroducedInLayer
	layers := m.layers()
	if f.IntroducedInLayer < len(layers) {
		m.viewOriginCmd = layers[f.IntroducedInLayer].Command
	} else {
		m.viewOriginCmd = ""
	}
	m.viewRequestID++
	if m.viewerCancel != nil {
		m.viewerCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.viewerCancel = cancel
	return m, tea.Batch(m.fetchFileContent(ctx, f.Path, m.viewRequestID), m.spinnerTick())
}

func (m model) layers() []image.Layer {
	if m.analysis == nil {
		return nil
	}
	return m.analysis.Layers
}

// finalLiveSize returns the merged-filesystem live byte total at the
// last layer, computed as Σ Δfs[i]. Used by the layer panel to color
// large step growths relative to the image's final on-disk footprint.
func (m model) finalLiveSize() int64 {
	if m.analysis == nil {
		return 0
	}
	var total int64
	for _, l := range m.analysis.Layers {
		total += l.NetDelta
	}
	return total
}

func (m model) currentTreeRoot() *image.FileNode {
	if m.analysis == nil {
		return nil
	}
	if m.layerCursor >= len(m.analysis.StackedTrees) {
		return nil
	}
	tree := m.analysis.StackedTrees[m.layerCursor]
	if tree == nil {
		return nil
	}
	return tree.Root
}

func (m model) useTreeCollapse() bool {
	return m.sortMode == sortNone && m.filterQuery == "" && !m.diffOnly
}

func (m *model) clearTreeCollapsed() {
	m.treeCollapsed = nil
}

func (m *model) resetTreeForLayerChange() {
	m.treeCursor = 0
	m.treeOffset = 0
	m.sortMode = sortNone
	m.clearTreeCollapsed()
}

func (m model) displayTree() []*image.FileNode {
	var files []*image.FileNode
	if m.useTreeCollapse() {
		files = visibleTree(m.currentTreeRoot(), m.treeCollapsed)
	} else {
		files = flattenTree(m.currentTreeRoot())
	}
	if m.diffOnly {
		files = applyDiffFilter(files)
	}
	if m.filterQuery != "" {
		files = applySubstringFilter(files, m.filterQuery)
	}
	files = applySortBySize(files, m.sortMode)
	return files
}

func flattenTree(root *image.FileNode) []*image.FileNode {
	var result []*image.FileNode
	var walk func(node *image.FileNode)
	walk = func(node *image.FileNode) {
		for _, child := range node.Children {
			result = append(result, child)
			if child.IsDir {
				walk(child)
			}
		}
	}
	if root != nil {
		walk(root)
	}
	return result
}

func nodeIndent(node *image.FileNode) int {
	p := strings.TrimPrefix(node.Path, "/")
	parts := strings.Split(p, "/")
	return len(parts) - 1
}

func (m *model) moveDown() {
	switch m.focus {
	case focusLayers:
		layers := m.layers()
		if m.layerCursor < len(layers)-1 {
			m.layerCursor++
			m.resetTreeForLayerChange()
			m.adjustLayerScroll()
		}
	case focusTree:
		files := m.displayTree()
		if m.treeCursor < len(files)-1 {
			m.treeCursor++
			m.adjustTreeScroll()
		}
	}
}

func (m *model) moveUp() {
	switch m.focus {
	case focusLayers:
		if m.layerCursor > 0 {
			m.layerCursor--
			m.resetTreeForLayerChange()
			m.adjustLayerScroll()
		}
	case focusTree:
		if m.treeCursor > 0 {
			m.treeCursor--
			m.adjustTreeScroll()
		}
	}
}

func (m *model) moveToTop() {
	switch m.focus {
	case focusLayers:
		m.layerCursor = 0
		m.layerOffset = 0
		m.resetTreeForLayerChange()
	case focusTree:
		m.treeCursor = 0
		m.treeOffset = 0
	}
}

func (m *model) moveToBottom() {
	switch m.focus {
	case focusLayers:
		layers := m.layers()
		if len(layers) > 0 {
			m.layerCursor = len(layers) - 1
			m.resetTreeForLayerChange()
			m.adjustLayerScroll()
		}
	case focusTree:
		files := m.displayTree()
		if len(files) > 0 {
			m.treeCursor = len(files) - 1
			m.adjustTreeScroll()
		}
	}
}

func (m *model) adjustTreeScroll() {
	visibleHeight := m.treeVisibleHeight()
	if visibleHeight <= 0 {
		return
	}
	if m.treeCursor < m.treeOffset {
		m.treeOffset = m.treeCursor
	}
	if m.treeCursor >= m.treeOffset+visibleHeight {
		m.treeOffset = m.treeCursor - visibleHeight + 1
	}
}

func (m *model) adjustLayerScroll() {
	visibleHeight := m.layerVisibleHeight()
	if visibleHeight <= 0 {
		return
	}
	if m.layerCursor < m.layerOffset {
		m.layerOffset = m.layerCursor
	}
	if m.layerCursor >= m.layerOffset+visibleHeight {
		m.layerOffset = m.layerCursor - visibleHeight + 1
	}
}

func (m *model) layerVisibleHeight() int {
	// header(1) + panel borders(2) + commandBar(3) + separator(1) + statusBar(1) = 8
	return m.height - 8
}

func (m *model) treeVisibleHeight() int {
	h := m.height - 8 - 1 // -1 for column header
	if m.filterQuery != "" || m.filterActive {
		h--
	}
	return h
}

func (m *model) clampCursors() {
	layers := m.layers()
	if len(layers) == 0 {
		return
	}
	if m.layerCursor >= len(layers) {
		m.layerCursor = len(layers) - 1
	}
	if m.layerCursor < 0 {
		m.layerCursor = 0
	}
	m.adjustLayerScroll()
	files := m.displayTree()
	if len(files) == 0 {
		m.treeCursor = 0
		m.treeOffset = 0
		return
	}
	if m.treeCursor >= len(files) {
		m.treeCursor = len(files) - 1
	}
	if m.treeCursor < 0 {
		m.treeCursor = 0
	}
	m.adjustTreeScroll()
}

func (m model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	if m.width == 0 || m.height == 0 {
		return tea.NewView("Initializing...")
	}

	switch m.state {
	case stateLoading:
		return m.viewLoading()
	case stateError:
		return m.viewError()
	default:
		return m.viewReady()
	}
}

func (m model) viewLoading() tea.View {
	frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]

	var lines []string
	lines = append(lines, "")
	lines = append(lines, "  "+lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render("◆ layerx"))
	lines = append(lines, "")

	switch m.loadPhase {
	case image.PhasePulling:
		lines = append(lines, fmt.Sprintf("  %s Pulling %s …", frame, m.imageRef))
		if m.pullTotal > 0 {
			detail := fmt.Sprintf("    Layer %d/%d", m.pullLayers, m.pullTotal)
			if m.pullBytesMax > 0 {
				pct := int(m.pullBytes * 100 / m.pullBytesMax)
				bytesText := fmt.Sprintf("  %s / %s",
					image.FormatBytes(m.pullBytes),
					image.FormatBytes(m.pullBytesMax))
				barWidth := 20
				if m.width > 0 {
					budget := m.width - 4 - lipgloss.Width(detail) - len("  []") - lipgloss.Width(bytesText)
					if budget < barWidth {
						barWidth = budget
					}
				}
				if barWidth >= 4 {
					filled := barWidth * pct / 100
					bar := lipgloss.NewStyle().Foreground(accentColor).Render(strings.Repeat("━", filled)) +
						lipgloss.NewStyle().Foreground(separatorColor).Render(strings.Repeat("─", barWidth-filled))
					detail += fmt.Sprintf("  [%s]%s", bar, bytesText)
				} else {
					detail += bytesText
				}
			}
			lines = append(lines, detail)
		}
	case image.PhaseExporting:
		sizeInfo := ""
		if m.imageSize > 0 {
			sizeInfo = " (" + image.FormatBytes(m.imageSize) + ")"
		}
		lines = append(lines, fmt.Sprintf("  %s Loading %s%s …", frame, m.imageRef, sizeInfo))
		lines = append(lines, "    Exporting layers…")
	case image.PhaseParsing:
		sizeInfo := ""
		if m.imageSize > 0 {
			sizeInfo = " (" + image.FormatBytes(m.imageSize) + ")"
		}
		lines = append(lines, fmt.Sprintf("  %s Loading %s%s …", frame, m.imageRef, sizeInfo))
		lines = append(lines, "    Parsing layers…")
	case image.PhaseCacheLoad:
		lines = append(lines, fmt.Sprintf("  %s %s — loaded from cache", frame, m.imageRef))
	default:
		sizeInfo := ""
		if m.imageSize > 0 {
			sizeInfo = " (" + image.FormatBytes(m.imageSize) + ")"
		}
		lines = append(lines, fmt.Sprintf("  %s Loading %s%s …", frame, m.imageRef, sizeInfo))
	}

	lines = append(lines, "")

	boxWidth := 52
	for _, ln := range lines {
		if w := lipgloss.Width(ln); w+2 > boxWidth {
			boxWidth = w + 2
		}
	}
	if m.width > 0 && m.width-2 < boxWidth {
		boxWidth = m.width - 2
	}
	boxHeight := len(lines)
	if boxHeight < 7 {
		for len(lines) < 7 {
			lines = append(lines, "")
		}
		boxHeight = 7
	}

	body := strings.Join(lines, "\n")
	panel := renderPanel(body, "Loading", true, boxWidth, boxHeight, false, false)
	content := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
	return finalizeView(tea.NewView(content))
}

func (m model) viewError() tea.View {
	errStyle := lipgloss.NewStyle().Foreground(removedColor).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(statusDimColor)
	msg := errStyle.Render("Error: "+m.errMsg) + "\n\n" + hintStyle.Render("Press q or Esc to exit.")
	content := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
	return finalizeView(tea.NewView(content))
}

func (m model) viewReady() tea.View {
	if m.width < 50 {
		return finalizeView(tea.NewView(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			"Terminal too narrow\n(need 50+ cols)")))
	}

	// chromeRows: header(1) + panel borders(2) + commandBar(3) + separator(1) + statusBar(1)
	const chromeRows = 8
	const minPanelRows = 3
	if m.height < chromeRows+minPanelRows {
		return finalizeView(tea.NewView(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			"Terminal too short\n(need 11+ rows)")))
	}

	leftWidth := m.leftPanelWidth()
	rightWidth := m.width - leftWidth - 1
	// header(1) + panel borders(2) + commandBar(3) + separator(1) + statusBar(1) = 8
	panelHeight := m.height - chromeRows

	header := m.renderHeader()
	treeFiles := m.displayTree()
	left := renderLayers(m.layers(), m.layerCursor, m.layerOffset, leftWidth, panelHeight, m.focus == focusLayers, m.sizeMode, m.finalLiveSize())
	right := renderFileTree(treeFiles, m.treeCursor, m.treeOffset, rightWidth, panelHeight, m.focus == focusTree, m.filterActive, m.filterQuery, m.useTreeCollapse(), m.treeCollapsed, m.layerCursor)

	panels := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)

	if m.viewState != viewNone {
		viewer := renderFileView(viewerParams{
			content:       m.viewContent,
			offset:        m.viewOffset,
			width:         m.width,
			height:        panelHeight,
			loading:       m.viewState == viewLoading,
			spinnerFrame:  m.spinnerFrame,
			originLayer:   m.viewOriginLayer,
			originCmd:     m.viewOriginCmd,
			currentLayer:  m.layerCursor,
			searchQuery:   m.viewSearchQuery,
			searchMatches: m.viewSearchMatches,
			searchCursor:  m.viewSearchCursor,
			searchActive:  m.viewSearchActive,
			highlightedLines: m.viewHighlightedLines,
		})
		panels = viewer
	}

	cmd := ""
	layers := m.layers()
	if m.layerCursor < len(layers) {
		cmd = layers[m.layerCursor].Command
	}
	commandBar := renderCommandBar(cmd, m.width)

	sep := lipgloss.NewStyle().Foreground(separatorColor).Render(strings.Repeat("─", m.width))
	status := m.renderStatusBar(treeFiles)

	content := lipgloss.JoinVertical(lipgloss.Left, header, panels, commandBar, sep, status)

	if m.showHelp {
		content = m.overlayHelp()
	}
	if m.showWaste {
		content = m.renderWasteOverlay()
	}

	return finalizeView(tea.NewView(content))
}

func (m model) leftPanelWidth() int {
	w := m.width * 35 / 100
	mx := 44
	if m.sizeMode == sizeColBoth {
		// Both columns need ~25 fixed chars; widen so the command column
		// stays readable on terminals that can spare it.
		mx = 56
	}
	if w < 24 {
		w = 24
	}
	if w > mx {
		w = mx
	}
	return w
}

func (m model) renderHeader() string {
	glyph := lipgloss.NewStyle().Foreground(accentColor).Background(statusBgColor).Render("◆")
	brand := lipgloss.NewStyle().Foreground(accentColor).Background(statusBgColor).Bold(true).Render(" layerx")
	sep := lipgloss.NewStyle().Foreground(headerSepColor).Background(statusBgColor).Render(" │ ")
	imageName := lipgloss.NewStyle().Foreground(selectedColor).Background(statusBgColor).Render(m.imageRef)
	left := glyph + brand + sep + imageName

	totalSize := image.FormatBytes(m.analysis.TotalSize)
	layerCount := fmt.Sprintf("%d layers", len(m.analysis.Layers))
	right := lipgloss.NewStyle().Foreground(headerDimColor).Background(statusBgColor).Render(layerCount + " · " + totalSize)

	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right)-1, 1)

	bgStyle := lipgloss.NewStyle().Background(statusBgColor)
	return bgStyle.Render(" " + left + strings.Repeat(" ", gap) + right)
}

func (m model) renderStatusBar(treeFiles []*image.FileNode) string {
	if m.viewState != viewNone {
		return m.renderViewerStatusBar()
	}
	keyStyle := lipgloss.NewStyle().Foreground(statusKeyColor).Background(statusBgColor).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(statusDimColor).Background(statusBgColor)
	sepStyle := lipgloss.NewStyle().Foreground(headerSepColor).Background(statusBgColor)

	type hint struct{ key, desc string }
	var hints []hint

	compact := m.width < 90

	if m.focus == focusLayers {
		hints = []hint{
			{"Tab", "switch"},
			{"j/k", "navigate"},
			{"g/G", "top/bottom"},
			{"S", "size"},
			{"d", "diff"},
			{"s", "sort"},
			{"w", "wasted"},
			{"?", "help"},
			{"q", "quit"},
		}
	} else {
		enterDesc := "view"
		if !compact && m.useTreeCollapse() &&
			m.treeCursor < len(treeFiles) && treeFiles[m.treeCursor].IsDir {
			enterDesc = "toggle"
		}
		hints = []hint{
			{"Tab", "switch"},
			{"j/k", "navigate"},
			{"/", "filter"},
			{"d", "diff"},
			{"s", "sort"},
			{"w", "wasted"},
			{"Enter", enterDesc},
			{"x", "save"},
			{"y", "copy path"},
			{"?", "help"},
		}
	}

	var hintStr string
	if compact {
		parts := make([]string, len(hints))
		for i, h := range hints {
			parts[i] = keyStyle.Render(h.key)
		}
		hintStr = " " + strings.Join(parts, " ")
	} else {
		var parts []string
		for _, h := range hints {
			parts = append(parts, keyStyle.Render(h.key)+" "+descStyle.Render(h.desc))
		}
		hintStr = " " + strings.Join(parts, " "+sepStyle.Render("│")+" ")
	}

	layers := m.layers()
	var right string
	if m.statusMsg != "" {
		msgStyle := lipgloss.NewStyle().Foreground(modifiedColor).Background(statusBgColor).Bold(true)
		right = msgStyle.Render(m.statusMsg) + " "
	} else if m.copyConfirm {
		copiedStyle := lipgloss.NewStyle().Foreground(addedColor).Background(statusBgColor).Bold(true)
		right = copiedStyle.Render("Copied!") + " "
	} else {
		badges := ""
		if m.efficiency != nil {
			pct := int(m.efficiency.Score * 100)
			effStr := fmt.Sprintf("Eff: %d%%", pct)
			if m.efficiency.WastedBytes > 0 {
				effStr += " · " + image.FormatBytes(m.efficiency.WastedBytes) + " wasted"
			}
			badges += lipgloss.NewStyle().Foreground(accentColor).Background(statusBgColor).Render("["+effStr+"]") + " "
		}
		if m.diffOnly {
			badges += lipgloss.NewStyle().Foreground(modifiedColor).Background(statusBgColor).Render("[diff]") + " "
		}
		switch m.sortMode {
		case sortDesc:
			badges += lipgloss.NewStyle().Foreground(accentColor).Background(statusBgColor).Render("[↓size]") + " "
		case sortAsc:
			badges += lipgloss.NewStyle().Foreground(accentColor).Background(statusBgColor).Render("[↑size]") + " "
		}

		layerNum := fmt.Sprintf("%d", m.layerCursor+1)
		layerTotal := fmt.Sprintf("%d", len(layers))
		size := ""
		if m.layerCursor < len(layers) {
			size = image.FormatBytes(layers[m.layerCursor].Size)
		}
		rightHighlight := lipgloss.NewStyle().Foreground(selectedColor).Background(statusBgColor).Bold(true).Render("Layer " + layerNum)
		sizeLabel := "stored " + size
		if m.focus == focusLayers && m.layerCursor < len(layers) {
			switch m.sizeMode {
			case sizeColDelta:
				sizeLabel = "change " + image.FormatSignedBytes(layers[m.layerCursor].NetDelta)
			case sizeColBoth:
				sizeLabel = "stored " + size + " · change " + image.FormatSignedBytes(layers[m.layerCursor].NetDelta)
			}
		}
		rightDim := lipgloss.NewStyle().Foreground(statusDimColor).Background(statusBgColor).Render("/" + layerTotal + " · " + sizeLabel)
		right = badges + rightHighlight + rightDim + " "
	}

	gap := max(m.width-lipgloss.Width(hintStr)-lipgloss.Width(right), 0)

	bgStyle := lipgloss.NewStyle().Background(statusBgColor)
	return bgStyle.Render(hintStr + strings.Repeat(" ", gap) + right)
}

func (m model) renderViewerStatusBar() string {
	keyStyle := lipgloss.NewStyle().Foreground(statusKeyColor).Background(statusBgColor).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(statusDimColor).Background(statusBgColor)
	sepStyle := lipgloss.NewStyle().Foreground(headerSepColor).Background(statusBgColor)

	hints := " " +
		keyStyle.Render("j/k") + " " + descStyle.Render("scroll") + " " +
		sepStyle.Render("│") + " " +
		keyStyle.Render("/") + " " + descStyle.Render("search") + " " +
		sepStyle.Render("│") + " " +
		keyStyle.Render("n/N") + " " + descStyle.Render("next/prev") + " " +
		sepStyle.Render("│") + " " +
		keyStyle.Render("Y") + " " + descStyle.Render("copy") + " " +
		sepStyle.Render("│") + " " +
		keyStyle.Render("Esc") + " " + descStyle.Render("close") + " " +
		sepStyle.Render("│") + " " +
		keyStyle.Render("q") + " " + descStyle.Render("quit")

	var right string
	if m.copyConfirm {
		copiedStyle := lipgloss.NewStyle().Foreground(addedColor).Background(statusBgColor).Bold(true)
		right = copiedStyle.Render("Copied!") + " "
	} else if len(m.viewSearchMatches) > 0 {
		matchStyle := lipgloss.NewStyle().Foreground(searchCurrentBg).Background(statusBgColor).Bold(true)
		right = matchStyle.Render(fmt.Sprintf("Match %d/%d ", m.viewSearchCursor+1, len(m.viewSearchMatches)))
	} else if m.viewContent != nil && !m.viewContent.Binary && len(m.viewContent.Data) > 0 {
		total := fileViewLineCount(m.viewContent)
		line := m.viewOffset + 1
		pct := 0
		if total > 0 {
			pct = line * 100 / total
		}
		rightDim := lipgloss.NewStyle().Foreground(statusDimColor).Background(statusBgColor)
		right = rightDim.Render(fmt.Sprintf("Line %d/%d (%d%%) ", line, total, pct))
	}

	gap := max(m.width-lipgloss.Width(hints)-lipgloss.Width(right), 0)

	bgStyle := lipgloss.NewStyle().Background(statusBgColor)
	return bgStyle.Render(hints + strings.Repeat(" ", gap) + right)
}

func (m model) fetchFileContent(ctx context.Context, path string, requestID uint64) tea.Cmd {
	extractor := m.extractor
	imageRef := m.imageRef
	layer := m.layerCursor
	return func() tea.Msg {
		content, err := extractor.ExtractFromLayer(ctx, imageRef, path, layer)
		return fileContentMsg{requestID: requestID, content: content, err: err}
	}
}

func (m model) fetchFileRaw(ctx context.Context, path string, requestID uint64) tea.Cmd {
	extractor := m.extractor
	imageRef := m.imageRef
	layer := m.layerCursor
	return func() tea.Msg {
		data, err := extractor.ExtractRawFromLayer(ctx, imageRef, path, layer)
		return fileSaveMsg{requestID: requestID, filename: filepath.Base(path), data: data, err: err}
	}
}

func (m *model) scrollViewDown() {
	maxOffset := max(fileViewLineCount(m.viewContent)-m.viewVisibleHeight(), 0)
	if m.viewOffset < maxOffset {
		m.viewOffset++
	}
}

func (m *model) scrollViewUp() {
	if m.viewOffset > 0 {
		m.viewOffset--
	}
}

func (m *model) viewVisibleHeight() int {
	h := m.height - 8
	if m.viewContent != nil && m.viewContent.Truncated {
		h--
	}
	if m.viewSearchActive || m.viewSearchQuery != "" {
		h--
	}
	if h < 1 {
		h = 1
	}
	return h
}

func friendlyError(err error) string {
	if _, ok := errors.AsType[*image.ErrDaemonNotRunning](err); ok {
		return "Docker is not running. Please start Docker and try again."
	}
	if pullErr, ok := errors.AsType[*image.ErrPullFailed](err); ok {
		return fmt.Sprintf("Failed to pull image %q. Check the image name and your network.", pullErr.Ref)
	}
	if notFoundErr, ok := errors.AsType[*image.ErrImageNotFound](err); ok {
		return fmt.Sprintf("Image %q not found.", notFoundErr.Ref)
	}
	return err.Error()
}

// Run starts the TUI program with the given configuration.
func Run(cfg Config) error {
	m := NewModel(cfg)
	defer m.fetchCancel()
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
