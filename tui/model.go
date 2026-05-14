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

	"github.com/deveshpharswan/layerx/image"
)

// Config holds the parameters needed to start the TUI.
type Config struct {
	ImageRef string
	Resolver image.Resolver
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

type viewState int

const (
	viewNone viewState = iota
	viewLoading
	viewReady
)

// fileContentMsg is sent when async file extraction completes.
type fileContentMsg struct {
	content *image.FileContent
	err     error
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
	filename string
	data     []byte
	err      error
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
	diffOnly     bool
	sortMode     sortMode
	viewState    viewState
	viewContent  *image.FileContent
	viewOffset   int
	extractor    image.Extractor
	efficiency   *image.EfficiencyResult
	writeFile    func(string, []byte, os.FileMode) error
}

// NewModel creates a new model wired to real Docker data.
func NewModel(cfg Config) model {
	ch := make(chan image.ProgressEvent, 16)
	return model{
		state:      stateLoading,
		imageRef:   cfg.ImageRef,
		resolver:   cfg.Resolver,
		progressCh: ch,
		writeFile:  os.WriteFile,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.fetchInspect(), m.fetchAnalysisWithProgress(m.progressCh), listenForProgress(m.progressCh), m.spinnerTick())
}

func (m model) fetchInspect() tea.Cmd {
	resolver := m.resolver
	imageRef := m.imageRef
	return func() tea.Msg {
		meta, err := resolver.Inspect(context.Background(), imageRef)
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
	return func() tea.Msg {
		defer close(progressCh)
		result, err := image.AnalyzeWithProgress(context.Background(), resolver, imageRef, progressCh)
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

	case fileContentMsg:
		if msg.err != nil {
			m.viewState = viewNone
			return m, nil
		}
		m.viewState = viewReady
		m.viewContent = msg.content
		m.viewOffset = 0
		return m, nil

	case fileSaveMsg:
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
		// Esc has precedence: viewer → filter (active) → filter (confirmed) → help → quit
		if msg.Code == tea.KeyEscape {
			if m.viewState != viewNone {
				m.viewState = viewNone
				m.viewContent = nil
				m.viewOffset = 0
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
			return m, tea.Quit
		}

		// Quit via q or ctrl+c always works.
		if key.Matches(msg, keys.Quit) {
			m.quitting = true
			return m, tea.Quit
		}

		// When filter input is active, capture all keys for text editing.
		if m.filterActive {
			return m.handleFilterInput(msg)
		}

		// Help toggle works when ready.
		if key.Matches(msg, keys.Help) && m.state == stateReady {
			m.showHelp = !m.showHelp
			return m, nil
		}

		// When help is shown, swallow all other keys.
		if m.showHelp {
			return m, nil
		}

		// When viewing a file, only scroll/close keys work.
		if m.viewState == viewReady {
			switch {
			case key.Matches(msg, keys.Down):
				m.scrollViewDown()
			case key.Matches(msg, keys.Up):
				m.scrollViewUp()
			case key.Matches(msg, keys.Top):
				m.viewOffset = 0
			case key.Matches(msg, keys.Bottom):
				maxOffset := fileViewLineCount(m.viewContent) - m.viewVisibleHeight()
				if maxOffset < 0 {
					maxOffset = 0
				}
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
		case key.Matches(msg, keys.Switch):
			if m.focus == focusLayers {
				m.focus = focusTree
			} else {
				m.focus = focusLayers
			}
			return m, nil

		case key.Matches(msg, keys.Down):
			m.moveDown()
			return m, nil

		case key.Matches(msg, keys.Up):
			m.moveUp()
			return m, nil

		case key.Matches(msg, keys.Top):
			m.moveToTop()
			return m, nil

		case key.Matches(msg, keys.Bottom):
			m.moveToBottom()
			return m, nil

		case key.Matches(msg, keys.Copy):
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

		case key.Matches(msg, keys.Filter):
			if m.focus == focusTree {
				m.filterActive = true
				return m, nil
			}
			return m, nil

		case msg.Code == tea.KeyEnter:
			if m.focus == focusTree && m.filterQuery != "" {
				m.filterQuery = ""
				m.treeCursor = 0
				m.treeOffset = 0
				return m, nil
			}
			if m.focus == focusTree {
				files := m.displayTree()
				if m.treeCursor < len(files) {
					f := files[m.treeCursor]
					if f.IsDir {
						m.statusMsg = "Cannot view directory"
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
					return m, tea.Batch(m.fetchFileContent(f.Path), m.spinnerTick())
				}
			}
			return m, nil

		case key.Matches(msg, keys.DiffOnly):
			m.diffOnly = !m.diffOnly
			m.treeCursor = 0
			m.treeOffset = 0
			return m, nil

		case key.Matches(msg, keys.Sort):
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

		case key.Matches(msg, keys.ExtractFile):
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
			return m, m.fetchFileRaw(f.Path)
		}
	}

	return m, nil
}

func (m model) handleFilterInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Code == tea.KeyEnter:
		m.filterActive = false
		m.treeCursor = 0
		m.treeOffset = 0
		return m, nil
	case msg.Code == tea.KeyBackspace:
		if len(m.filterQuery) > 0 {
			m.filterQuery = m.filterQuery[:len(m.filterQuery)-1]
			m.treeCursor = 0
			m.treeOffset = 0
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

func (m model) layers() []image.Layer {
	if m.analysis == nil {
		return nil
	}
	return m.analysis.Layers
}

func (m model) currentFlatTree() []*image.FileNode {
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
	return flattenTree(tree.Root)
}

func (m model) displayTree() []*image.FileNode {
	files := m.currentFlatTree()
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
			m.treeCursor = 0
			m.treeOffset = 0
			m.sortMode = sortNone
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
			m.treeCursor = 0
			m.treeOffset = 0
			m.sortMode = sortNone
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
		m.treeCursor = 0
		m.treeOffset = 0
		m.sortMode = sortNone
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
			m.treeCursor = 0
			m.treeOffset = 0
			m.sortMode = sortNone
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
	return m.height - 8
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

	var line1, line2 string

	switch m.loadPhase {
	case image.PhasePulling:
		line1 = fmt.Sprintf("%s Pulling %s ...", frame, m.imageRef)
		if m.pullTotal > 0 {
			line2 = fmt.Sprintf("   Layer %d/%d", m.pullLayers, m.pullTotal)
			if m.pullBytesMax > 0 {
				pct := int(m.pullBytes * 100 / m.pullBytesMax)
				barWidth := 20
				filled := barWidth * pct / 100
				bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
				line2 += fmt.Sprintf("  [%s]  %s / %s",
					bar,
					image.FormatBytes(m.pullBytes),
					image.FormatBytes(m.pullBytesMax))
			}
		}
	case image.PhaseExporting:
		sizeInfo := ""
		if m.imageSize > 0 {
			sizeInfo = " (" + image.FormatBytes(m.imageSize) + ")"
		}
		line1 = fmt.Sprintf("%s Loading %s%s ...", frame, m.imageRef, sizeInfo)
		line2 = "   Exporting layers..."
	case image.PhaseParsing:
		sizeInfo := ""
		if m.imageSize > 0 {
			sizeInfo = " (" + image.FormatBytes(m.imageSize) + ")"
		}
		line1 = fmt.Sprintf("%s Loading %s%s ...", frame, m.imageRef, sizeInfo)
		line2 = "   Parsing layers..."
	default:
		sizeInfo := ""
		if m.imageSize > 0 {
			sizeInfo = " (" + image.FormatBytes(m.imageSize) + ")"
		}
		line1 = fmt.Sprintf("%s Loading %s%s ...", frame, m.imageRef, sizeInfo)
	}

	msg := line1
	if line2 != "" {
		msg += "\n" + line2
	}

	content := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func (m model) viewError() tea.View {
	errStyle := lipgloss.NewStyle().Foreground(removedColor).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(statusDimColor)
	msg := errStyle.Render("Error: "+m.errMsg) + "\n\n" + hintStyle.Render("Press q or Esc to exit.")
	content := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func (m model) viewReady() tea.View {
	if m.width < 50 {
		v := tea.NewView(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			"Terminal too narrow\n(need 50+ cols)"))
		v.AltScreen = true
		return v
	}

	// chromeRows: header(1) + panel borders(2) + commandBar(3) + separator(1) + statusBar(1)
	const chromeRows = 8
	const minPanelRows = 3
	if m.height < chromeRows+minPanelRows {
		v := tea.NewView(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			"Terminal too short\n(need 11+ rows)"))
		v.AltScreen = true
		return v
	}

	leftWidth := m.leftPanelWidth()
	rightWidth := m.width - leftWidth - 1
	// header(1) + panel borders(2) + commandBar(3) + separator(1) + statusBar(1) = 8
	panelHeight := m.height - chromeRows

	header := m.renderHeader()
	left := renderLayers(m.layers(), m.layerCursor, m.layerOffset, leftWidth, panelHeight, m.focus == focusLayers)
	right := renderFileTree(m.displayTree(), m.treeCursor, m.treeOffset, rightWidth, panelHeight, m.focus == focusTree, m.filterActive, m.filterQuery, m.sortMode)

	panels := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)

	if m.viewState != viewNone {
		viewer := renderFileView(m.viewContent, m.viewOffset, m.width, panelHeight, m.viewState == viewLoading, m.spinnerFrame)
		panels = viewer
	}

	cmd := ""
	layers := m.layers()
	if m.layerCursor < len(layers) {
		cmd = layers[m.layerCursor].Command
	}
	commandBar := renderCommandBar(cmd, m.width)

	sep := lipgloss.NewStyle().Foreground(separatorColor).Render(strings.Repeat("-", m.width))
	status := m.renderStatusBar()

	content := lipgloss.JoinVertical(lipgloss.Left, header, panels, commandBar, sep, status)

	if m.showHelp {
		content = m.overlayHelp()
	}

	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func (m model) leftPanelWidth() int {
	w := m.width * 35 / 100
	if w < 24 {
		w = 24
	}
	if w > 44 {
		w = 44
	}
	return w
}

func (m model) renderHeader() string {
	brand := lipgloss.NewStyle().Foreground(accentColor).Background(statusBgColor).Bold(true).Render("layerx")
	sep := lipgloss.NewStyle().Foreground(headerSepColor).Background(statusBgColor).Render(" --- ")
	imageName := lipgloss.NewStyle().Foreground(selectedColor).Background(statusBgColor).Render(m.imageRef)
	left := brand + sep + imageName

	totalSize := image.FormatBytes(m.analysis.TotalSize)
	layerCount := fmt.Sprintf("%d layers", len(m.analysis.Layers))
	right := lipgloss.NewStyle().Foreground(headerDimColor).Background(statusBgColor).Render(layerCount + " · " + totalSize)

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 1 {
		gap = 1
	}

	bgStyle := lipgloss.NewStyle().Background(statusBgColor)
	return bgStyle.Render(" " + left + strings.Repeat(" ", gap) + right)
}

func (m model) renderStatusBar() string {
	if m.viewState != viewNone {
		return m.renderViewerStatusBar()
	}
	keyStyle := lipgloss.NewStyle().Foreground(statusKeyColor).Background(statusBgColor).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(statusDimColor).Background(statusBgColor)
	sepStyle := lipgloss.NewStyle().Foreground(headerSepColor).Background(statusBgColor)

	hints := fmt.Sprintf(" %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s",
		keyStyle.Render("Tab"), descStyle.Render("switch"),
		sepStyle.Render("·"),
		keyStyle.Render("j/k"), descStyle.Render("navigate"),
		sepStyle.Render("·"),
		keyStyle.Render("/"), descStyle.Render("filter"),
		sepStyle.Render("·"),
		keyStyle.Render("d"), descStyle.Render("diff"),
		sepStyle.Render("·"),
		keyStyle.Render("s"), descStyle.Render("sort"),
		sepStyle.Render("·"),
		keyStyle.Render("c"), descStyle.Render("copy"),
		sepStyle.Render("·"),
		keyStyle.Render("Enter"), descStyle.Render("view"),
		sepStyle.Render("·"),
		keyStyle.Render("x"), descStyle.Render("save"),
		sepStyle.Render("·"),
		keyStyle.Render("?"), descStyle.Render("help"),
		sepStyle.Render("·"),
		keyStyle.Render("q"), descStyle.Render("quit"),
	)

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
		rightDim := lipgloss.NewStyle().Foreground(statusDimColor).Background(statusBgColor).Render("/" + layerTotal + " · " + size)
		right = badges + rightHighlight + rightDim + " "
	}

	gap := m.width - lipgloss.Width(hints) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}

	bgStyle := lipgloss.NewStyle().Background(statusBgColor)
	return bgStyle.Render(hints + strings.Repeat(" ", gap) + right)
}

func (m model) renderViewerStatusBar() string {
	keyStyle := lipgloss.NewStyle().Foreground(statusKeyColor).Background(statusBgColor).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(statusDimColor).Background(statusBgColor)
	sepStyle := lipgloss.NewStyle().Foreground(headerSepColor).Background(statusBgColor)

	hints := " " +
		keyStyle.Render("j/k") + " " + descStyle.Render("scroll") + " " +
		sepStyle.Render("·") + " " +
		keyStyle.Render("g/G") + " " + descStyle.Render("top/bottom") + " " +
		sepStyle.Render("·") + " " +
		keyStyle.Render("Esc") + " " + descStyle.Render("close") + " " +
		sepStyle.Render("·") + " " +
		keyStyle.Render("q") + " " + descStyle.Render("quit")

	var right string
	if m.viewContent != nil && !m.viewContent.Binary && len(m.viewContent.Data) > 0 {
		total := fileViewLineCount(m.viewContent)
		line := m.viewOffset + 1
		pct := 0
		if total > 0 {
			pct = line * 100 / total
		}
		rightDim := lipgloss.NewStyle().Foreground(statusDimColor).Background(statusBgColor)
		right = rightDim.Render(fmt.Sprintf("Line %d/%d (%d%%) ", line, total, pct))
	}

	gap := m.width - lipgloss.Width(hints) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}

	bgStyle := lipgloss.NewStyle().Background(statusBgColor)
	return bgStyle.Render(hints + strings.Repeat(" ", gap) + right)
}

func (m model) overlayHelp() string {
	titleStyle := lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	keyStyle := lipgloss.NewStyle().Foreground(statusKeyColor)
	descStyle := lipgloss.NewStyle().Foreground(fileNameColor)
	dimStyle := lipgloss.NewStyle().Foreground(statusDimColor)
	sectionStyle := lipgloss.NewStyle().Foreground(modifiedColor).Bold(true)

	lines := []string{
		"",
		"  " + titleStyle.Render("layerx — Keyboard Shortcuts"),
		"",
		"  " + sectionStyle.Render("Navigation"),
		"  " + keyStyle.Render("j / k       ") + descStyle.Render("Move cursor down / up"),
		"  " + keyStyle.Render("g / G       ") + descStyle.Render("Jump to first / last item"),
		"  " + keyStyle.Render("Tab         ") + descStyle.Render("Switch panel (layers ↔ tree)"),
		"",
		"  " + sectionStyle.Render("File Tree"),
		"  " + keyStyle.Render("/           ") + descStyle.Render("Search files (substring filter)"),
		"  " + keyStyle.Render("Enter       ") + descStyle.Render("View file content"),
		"  " + keyStyle.Render("d           ") + descStyle.Render("Show only changed files"),
		"  " + keyStyle.Render("s           ") + descStyle.Render("Sort by size (↓ → ↑ → off)"),
		"  " + keyStyle.Render("x           ") + descStyle.Render("Save file to current directory"),
		"  " + keyStyle.Render("Esc         ") + descStyle.Render("Clear filter / close viewer"),
		"",
		"  " + sectionStyle.Render("File Viewer"),
		"  " + keyStyle.Render("j / k       ") + descStyle.Render("Scroll down / up"),
		"  " + keyStyle.Render("g / G       ") + descStyle.Render("Jump to top / bottom"),
		"  " + keyStyle.Render("Esc         ") + descStyle.Render("Return to file tree"),
		"",
		"  " + sectionStyle.Render("Other"),
		"  " + keyStyle.Render("c           ") + descStyle.Render("Copy layer command to clipboard"),
		"  " + keyStyle.Render("?           ") + descStyle.Render("Toggle this help"),
		"  " + keyStyle.Render("q / Ctrl+C  ") + descStyle.Render("Quit"),
		"",
		"  " + dimStyle.Render("Tip: directories and removed files cannot be viewed."),
		"",
	}

	body := strings.Join(lines, "\n")

	boxWidth := 56
	boxHeight := len(lines) + 2

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Width(boxWidth).
		Height(boxHeight)

	popup := borderStyle.Render(body)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, popup)
}

func (m model) fetchFileContent(path string) tea.Cmd {
	extractor := m.extractor
	imageRef := m.imageRef
	return func() tea.Msg {
		content, err := extractor.Extract(context.Background(), imageRef, path)
		return fileContentMsg{content: content, err: err}
	}
}

func (m model) fetchFileRaw(path string) tea.Cmd {
	extractor := m.extractor
	imageRef := m.imageRef
	return func() tea.Msg {
		data, err := extractor.ExtractRaw(context.Background(), imageRef, path)
		return fileSaveMsg{filename: filepath.Base(path), data: data, err: err}
	}
}

func (m *model) scrollViewDown() {
	maxOffset := fileViewLineCount(m.viewContent) - m.viewVisibleHeight()
	if maxOffset < 0 {
		maxOffset = 0
	}
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
	if h < 1 {
		h = 1
	}
	return h
}

func friendlyError(err error) string {
	var daemonErr *image.ErrDaemonNotRunning
	if errors.As(err, &daemonErr) {
		return "Docker is not running. Please start Docker and try again."
	}
	var pullErr *image.ErrPullFailed
	if errors.As(err, &pullErr) {
		return fmt.Sprintf("Failed to pull image %q. Check the image name and your network.", pullErr.Ref)
	}
	var notFoundErr *image.ErrImageNotFound
	if errors.As(err, &notFoundErr) {
		return fmt.Sprintf("Image %q not found.", notFoundErr.Ref)
	}
	return err.Error()
}

// Run starts the TUI program with the given configuration.
func Run(cfg Config) error {
	p := tea.NewProgram(NewModel(cfg))
	_, err := p.Run()
	return err
}
