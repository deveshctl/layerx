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
	"github.com/charmbracelet/x/ansi"

	"github.com/deveshctl/layerx/image"
)

// Config holds the parameters needed to start the TUI.
type Config struct {
	ImageRef string
	Resolver image.Resolver
	NoCache  bool
	// Platform is the canonical "os/arch[/variant]" string the user passed
	// via --platform, or "" when no pin was set. Display-only: shown in the
	// header so the operator knows which variant of a multi-platform image
	// is on screen. Resolver behaviour is governed by image.WithPlatform,
	// not this field.
	Platform string
}

type focus int

const (
	focusLayers focus = iota
	focusTree
	// focusTreeAgg is reachable only when aggregated mode is on. The tree
	// panel splits into two stacked sub-panels: focusTree drives the top
	// (per-layer Δ from StackedTrees), focusTreeAgg drives the bottom
	// (cumulative provenance from AggregatedTrees). Tab cycles
	// focusLayers → focusTree → focusTreeAgg → focusLayers in agg mode.
	focusTreeAgg
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

const maxFilterLen = 256

// fileContentMsg is sent when async file extraction completes.
type fileContentMsg struct {
	requestID uint64
	content   *image.FileContent
	err       error
}

// highlightedMsg carries the result of running Chroma syntax highlighting
// on a previously-rendered file. Highlighting is deferred to a tea.Cmd
// because chroma's lexer can spend hundreds of milliseconds tokenising a
// large source file; running it inline in Update would freeze the TUI
// (no key handling, no spinner, no resize) for that whole window.
//
// The requestID is the same one stamped on the originating fileContentMsg;
// a stale highlight (user already navigated to another file) is discarded.
// lines may be nil when the file is binary, the lexer is unavailable, or
// the highlight step errored — in any of those cases the renderer falls
// back to the plain-text path.
type highlightedMsg struct {
	requestID uint64
	lines     []string
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

// pullLayerSnapshot is a single line in pullLayerLog — one completed pull
// layer, captured at the moment LayersDone incremented past it. bytesCurr
// and bytesMax are aggregate values (the daemon does not split bytes per
// layer); they are stored as-of-snapshot so the rendered log shows the
// progress totals at completion rather than the live totals.
type pullLayerSnapshot struct {
	layerNum  int
	bytesCurr int64
	bytesMax  int64
}

// spinnerTickMsg triggers a spinner frame advance.
type spinnerTickMsg struct{}

// cacheHitDoneMsg fires after the cache-hit min-visibility hold elapses.
// Carries the pending analysis (or error) captured when analysisMsg
// arrived mid-hold; the Update handler then transitions to
// stateReady/stateError.
type cacheHitDoneMsg struct {
	analysis *image.Analysis
	err      error
}

// clearCopyMsg clears the "Copied!" confirmation after a timeout.
type clearCopyMsg struct{}

// clearStatusMsg clears the transient status bar message after a timeout.
// gen identifies which status set scheduled this tick; an older tick
// whose gen no longer matches m.statusGen is ignored so it cannot erase
// a newer message that overwrote the original mid-window.
type clearStatusMsg struct{ gen uint64 }

// statusKind classifies a transient status-bar message so the renderer
// can colour it consistently: success transients (Copied, Saved, Jumped)
// in addedColor; failures (extraction errors, save errors) in removedColor;
// neutral progress notes (Extracting…, "File removed in this layer") in
// modifiedColor.
type statusKind int

const (
	statusInfo statusKind = iota
	statusOK
	statusErr
)

// setStatus assigns msg to the status bar and bumps statusGen so any
// previously-scheduled clearStatusMsg ticks become stale and no-ops.
// Defaults to the neutral colour (statusInfo); callers that want
// success/error tinting use setStatusKind.
func (m *model) setStatus(msg string) {
	m.statusMsg = msg
	m.statusKind = statusInfo
	m.statusGen++
}

// setStatusKind is the kind-aware variant of setStatus.
func (m *model) setStatusKind(msg string, kind statusKind) {
	m.statusMsg = msg
	m.statusKind = kind
	m.statusGen++
}

// scheduleStatusClear returns a tea.Cmd that clears the *current* status
// message after d. The bumped gen is captured by value, so a later
// setStatus invalidates this tick before it fires.
func (m *model) scheduleStatusClear(d time.Duration) tea.Cmd {
	gen := m.statusGen
	return tea.Tick(d, func(time.Time) tea.Msg {
		return clearStatusMsg{gen: gen}
	})
}

// fileSaveMsg is sent when async file extraction for save-to-disk completes.
type fileSaveMsg struct {
	requestID uint64
	filename  string
	data      []byte
	err       error
}

// fileSavedMsg is sent when the off-thread write completes.
// `target` is the actual path written (may differ from `original` when
// uniquePath bumped a `.N` suffix to avoid clobbering an existing file).
type fileSavedMsg struct {
	requestID uint64
	original  string
	target    string
	err       error
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type model struct {
	width        int
	height       int
	focus        focus
	state        appState
	imageRef     string
	platform     string
	analysis     *image.Analysis
	layerCursor  int
	layerOffset  int
	treeCursor   int
	treeOffset   int
	errMsg       string
	errHint      string
	quitting     bool
	resolver     image.Resolver
	spinnerFrame int
	imageSize    int64
	loadPhase    image.ProgressPhase
	pullLayers   int
	pullTotal    int
	pullBytes    int64
	pullBytesMax int64
	// pullLayerLog accumulates one snapshot per completed pull layer so the
	// loading panel can render a multi-line history of finished blobs while
	// the live layer keeps streaming. Appended only when LayersDone
	// increases — the daemon does not break bytes down per layer, so the
	// snapshot captures the aggregate (BytesCurr / BytesTotal) at the
	// moment the layer transitioned to done. Reset on every new loading
	// session via beginLoadingSession.
	pullLayerLog []pullLayerSnapshot
	progressCh   chan image.ProgressEvent
	copyConfirm  bool
	statusMsg    string
	statusKind   statusKind
	statusGen    uint64
	// loadStartedAt is stamped in NewModel and drives the elapsed-time line
	// on the loading screen. Spinner ticks redraw the panel; each redraw
	// recomputes time.Since.
	loadStartedAt time.Time
	// cacheHitAt is stamped the first time progressMsg{PhaseCacheLoad}
	// arrives. Zero value means "no cache hit yet". The analysisMsg handler
	// uses it to hold the cache-hit success line on screen for a brief
	// minimum window so a hot-cache load is visibly cached rather than
	// instantaneous.
	cacheHitAt time.Time
	// viewLoadStartedAt is stamped on every viewState transition into
	// viewLoading (file-extract loading). Reset on every new open so
	// successive file opens start fresh.
	viewLoadStartedAt time.Time
	showHelp     bool
	filterActive bool
	filterQuery  string
	diffOnly      bool
	aggregated    bool
	sortMode      sortMode
	treeCollapsed map[string]bool
	// aggCursor / aggOffset / aggCollapsed mirror treeCursor / treeOffset /
	// treeCollapsed but drive the bottom (Cumulative) sub-panel when
	// aggregated mode is on. Kept independent so the two panes can scroll
	// and collapse separately — the value of the split view is being able to
	// inspect "what just changed" and "the full carry-forward state" without
	// losing one's place in either.
	aggCursor    int
	aggOffset    int
	aggCollapsed map[string]bool
	viewState    viewState
	viewContent      *image.FileContent
	viewHighlightedLines []string
	viewOffset       int
	viewHOffset      int
	viewCursorCol    int
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
	statFile         func(string) (os.FileInfo, error)
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
		state:         stateLoading,
		imageRef:      cfg.ImageRef,
		platform:      cfg.Platform,
		resolver:      cfg.Resolver,
		progressCh:    ch,
		writeFile:     atomicWriteFile,
		statFile:      os.Stat,
		keys:          defaultKeys(),
		noCache:       cfg.NoCache,
		fetchCtx:      ctx,
		fetchCancel:   cancel,
		loadStartedAt: time.Now(),
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
			m.setStatus("cache: " + msg.event.Message)
			return m, tea.Batch(
				listenForProgress(m.progressCh),
				m.scheduleStatusClear(4*time.Second),
			)
		}
		m.loadPhase = msg.event.Phase
		// Stamp cacheHitAt the first time we see PhaseCacheLoad so the
		// analysisMsg handler can hold the success line on screen for a
		// brief minimum window. Subsequent events (none expected in the
		// cache path) leave the original timestamp intact.
		if msg.event.Phase == image.PhaseCacheLoad && m.cacheHitAt.IsZero() {
			m.cacheHitAt = time.Now()
		}
		// Snapshot any newly-completed layers so the panel can render a
		// running history. Daemon byte fields are aggregate, not per-layer,
		// so each snapshot captures the totals at completion — good enough
		// for a visible "Layer N done" trail without inventing per-blob
		// numbers that aren't reported. m.pullLayers stores the highest
		// LayersDone observed so far; on an upward transition, every
		// layer index in (prev, new] becomes a completed snapshot.
		if msg.event.Phase == image.PhasePulling && msg.event.LayersDone > m.pullLayers {
			for n := m.pullLayers + 1; n <= msg.event.LayersDone; n++ {
				m.pullLayerLog = append(m.pullLayerLog, pullLayerSnapshot{
					layerNum:  n,
					bytesCurr: msg.event.BytesCurr,
					bytesMax:  msg.event.BytesTotal,
				})
			}
		}
		m.pullLayers = msg.event.LayersDone
		m.pullTotal = msg.event.LayersTotal
		m.pullBytes = msg.event.BytesCurr
		m.pullBytesMax = msg.event.BytesTotal
		return m, listenForProgress(m.progressCh)

	case analysisMsg:
		// Cache-hit min-visibility hold: when PhaseCacheLoad arrived less
		// than 300ms ago, keep the loading panel visible for the
		// remainder of the window so the "loaded from cache" line is
		// legible. The analysis is already complete; we are only
		// deferring the View() switch.
		if !m.cacheHitAt.IsZero() {
			remaining := 300*time.Millisecond - time.Since(m.cacheHitAt)
			if remaining > 0 {
				analysis := msg.analysis
				err := msg.err
				return m, tea.Tick(remaining, func(time.Time) tea.Msg {
					return cacheHitDoneMsg{analysis: analysis, err: err}
				})
			}
		}
		if msg.err != nil {
			m.state = stateError
			m.errMsg = friendlyError(msg.err)
			m.errHint = errorHint(msg.err)
			return m, nil
		}
		m.state = stateReady
		m.analysis = msg.analysis
		m.efficiency = image.EfficiencyFromAnalysis(msg.analysis)
		if src, ok := m.resolver.(image.ExtractorSource); ok {
			m.extractor = src.NewExtractor()
		}
		m.clampCursors()
		return m, nil

	case cacheHitDoneMsg:
		if msg.err != nil {
			m.state = stateError
			m.errMsg = friendlyError(msg.err)
			m.errHint = errorHint(msg.err)
			return m, nil
		}
		m.state = stateReady
		m.analysis = msg.analysis
		m.efficiency = image.EfficiencyFromAnalysis(msg.analysis)
		if src, ok := m.resolver.(image.ExtractorSource); ok {
			m.extractor = src.NewExtractor()
		}
		m.clampCursors()
		return m, nil

	case clearCopyMsg:
		m.copyConfirm = false
		return m, nil

	case clearStatusMsg:
		if msg.gen == m.statusGen {
			m.statusMsg = ""
		}
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
			m.setStatusKind("Error: "+msg.err.Error(), statusErr)
			return m, m.scheduleStatusClear(3 * time.Second)
		}
		m.viewState = viewReady
		m.viewContent = msg.content
		m.viewHighlightedLines = nil
		m.viewOffset = 0
		m.viewHOffset = 0
		m.viewCursorCol = 0
		// Defer Chroma syntax highlighting to a tea.Cmd. Tokenising even a
		// few hundred KB of source can take hundreds of ms; running it
		// inline here would freeze the TUI (no input, no spinner, no
		// resize handling) until the lexer returned. The plain-text
		// renderer is used until highlightedMsg arrives, then the colored
		// lines swap in.
		if msg.content != nil && !msg.content.Binary && len(msg.content.Data) > 0 {
			return m, highlightFileCmd(msg.requestID, msg.content.Path, msg.content.Data)
		}
		return m, nil

	case highlightedMsg:
		// A late highlight for a file the user has already navigated away
		// from — discard. Without the gate, switching between large files
		// quickly would leave the wrong colors painted on the wrong file.
		if msg.requestID != m.viewRequestID {
			return m, nil
		}
		m.viewHighlightedLines = msg.lines
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
			m.setStatusKind("Error: "+msg.err.Error(), statusErr)
			return m, m.scheduleStatusClear(3 * time.Second)
		}
		// Run stat + write off-thread so a slow disk (network mount, encrypted
		// volume, USB) can't freeze the TUI. Re-arm saveCancel against a fresh
		// context so quit/Esc aborts the probe-and-write phase as well, not
		// just the prior extract phase.
		ctx, cancel := context.WithCancel(m.fetchCtx)
		m.saveCancel = cancel
		return m, saveFileCmd(ctx, m.statFile, m.writeFile, msg.requestID, msg.filename, msg.data)

	case fileSavedMsg:
		if msg.requestID != m.saveRequestID {
			return m, nil
		}
		if msg.err != nil {
			m.setStatusKind("Error: "+msg.err.Error(), statusErr)
			return m, m.scheduleStatusClear(3 * time.Second)
		}
		if msg.target != msg.original {
			m.setStatusKind(fmt.Sprintf("Saved: %s (existed → wrote %s)", msg.original, msg.target), statusOK)
		} else {
			m.setStatusKind("Saved: "+msg.target, statusOK)
		}
		return m, m.scheduleStatusClear(2 * time.Second)

	case tea.KeyPressMsg:
		// Esc has precedence: viewer search → viewer → waste → filter
		// (active) → filter (confirmed) → help. In stateReady Esc is
		// dismiss-only — falling through to tea.Quit caused mash-Esc on a
		// closed viewer to silently quit the app, which Gate C in M08
		// flagged as a regression. Esc still exits the loading and error
		// screens, where it is the documented escape hatch ("Press q or
		// Esc to exit").
		if msg.Code == tea.KeyEscape {
			if m.viewState != viewNone {
				if m.viewSearchActive {
					m.viewSearchActive = false
					m.viewSearchQuery = ""
					m.viewSearchMatches = nil
					m.viewSearchCursor = 0
					m.viewHOffset = 0
					m.viewCursorCol = 0
					return m, nil
				}
				if m.viewSearchQuery != "" {
					m.viewSearchQuery = ""
					m.viewSearchMatches = nil
					m.viewSearchCursor = 0
					m.viewHOffset = 0
					m.viewCursorCol = 0
					return m, nil
				}
				m.viewState = viewNone
				m.viewContent = nil
				m.viewHighlightedLines = nil
				m.viewOffset = 0
				m.viewHOffset = 0
				m.viewCursorCol = 0
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
				m.aggCursor = 0
				m.aggOffset = 0
				return m, nil
			}
			if m.filterQuery != "" {
				m.filterQuery = ""
				m.treeCursor = 0
				m.treeOffset = 0
				m.aggCursor = 0
				m.aggOffset = 0
				return m, nil
			}
			if m.showHelp {
				m.showHelp = false
				return m, nil
			}
			// In stateReady, Esc has nothing left to dismiss — swallow it.
			// Quit is reserved for q / ctrl+c. In stateLoading and
			// stateError the documented UX is "Press q or Esc to exit",
			// so honour that fall-through there.
			if m.state == stateReady {
				return m, nil
			}
			m.quitting = true
			m.cancelInflight()
			return m, tea.Quit
		}

		// Quit via q or ctrl+c. When a text input is active (filter or
		// viewer search), 'q' must reach the input handler so users can
		// type queries containing 'q' (e.g. "jquery"). ctrl+c still quits.
		if key.Matches(msg, m.keys.Quit) {
			inTextInput := m.filterActive || (m.viewState == viewReady && m.viewSearchActive)
			if !inTextInput || msg.String() == "ctrl+c" {
				m.quitting = true
				m.cancelInflight()
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
			case key.Matches(msg, m.keys.Left):
				m.scrollViewLeft()
			case key.Matches(msg, m.keys.Right):
				m.scrollViewRight()
			case key.Matches(msg, m.keys.Top):
				m.viewOffset = 0
				m.viewHOffset = 0
				m.viewCursorCol = 0
			case key.Matches(msg, m.keys.Bottom):
				maxOffset := max(fileViewLineCount(m.viewContent)-m.viewVisibleHeight(), 0)
				m.viewOffset = maxOffset
				m.viewHOffset = 0
				m.viewCursorCol = 0
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
			// Tab cycles panels. When aggregated mode is off the file tree is
			// a single pane: layers ↔ tree. With aggregated on the tree
			// panel splits horizontally into two sub-panes (per-layer Δ on
			// top, cumulative state on bottom), and Tab cycles three states:
			// layers → tree(top) → tree(bottom) → layers.
			switch m.focus {
			case focusLayers:
				m.focus = focusTree
			case focusTree:
				if m.aggregated {
					m.focus = focusTreeAgg
				} else {
					m.focus = focusLayers
				}
			case focusTreeAgg:
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
			if m.isTreeFocused() {
				files := m.displayTreeFor(m.focus)
				cur := m.treeCursorFor(m.focus)
				if cur < len(files) {
					m.copyConfirm = true
					return m, tea.Batch(
						tea.SetClipboard(files[cur].Path),
						tea.Tick(2*time.Second, func(time.Time) tea.Msg {
							return clearCopyMsg{}
						}),
					)
				}
			}
			return m, nil

		case key.Matches(msg, m.keys.Filter):
			if m.isTreeFocused() {
				m.filterActive = true
				return m, nil
			}
			return m, nil

		case msg.Code == tea.KeyBackspace:
			if m.isTreeFocused() && !m.filterActive && m.filterQuery != "" {
				m.filterQuery = ""
				m.treeCursor = 0
				m.treeOffset = 0
				m.aggCursor = 0
				m.aggOffset = 0
				return m, nil
			}

		case msg.Code == tea.KeyEnter:
			if m.isTreeFocused() {
				return m.tryOpenSelectedFile()
			}
			return m, nil

		case key.Matches(msg, m.keys.DiffOnly):
			m.diffOnly = !m.diffOnly
			m.treeCursor = 0
			m.treeOffset = 0
			m.aggCursor = 0
			m.aggOffset = 0
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
			m.aggCursor = 0
			m.aggOffset = 0
			return m, nil

		case key.Matches(msg, m.keys.Aggregate):
			// Toggle the split-pane aggregated view. m.filterQuery,
			// m.diffOnly, and m.sortMode are intentionally NOT reset — they
			// are path/DiffType-level filters that apply identically to both
			// trees and the user typically wants them carried across.
			//
			// Cursor/collapse state IS reset on each pane: the trees change
			// shape between the per-layer Δ view and the cumulative view, so
			// a saved cursor index would land somewhere unexpected.
			m.aggregated = !m.aggregated
			m.treeCursor = 0
			m.treeOffset = 0
			m.aggCursor = 0
			m.aggOffset = 0
			m.clearTreeCollapsed()
			m.clearAggCollapsed()
			// Turning off aggregated mode while focused on the bottom pane
			// would leave focus on a now-invisible sub-panel. Snap back to
			// the (single) tree pane.
			if !m.aggregated && m.focus == focusTreeAgg {
				m.focus = focusTree
			}
			return m, nil

		case key.Matches(msg, m.keys.ExtractFile):
			if !m.isTreeFocused() {
				return m, nil
			}
			files := m.displayTreeFor(m.focus)
			cur := m.treeCursorFor(m.focus)
			if cur >= len(files) {
				return m, nil
			}
			f := files[cur]
			if f.IsDir {
				m.setStatus("Cannot extract directory")
				return m, m.scheduleStatusClear(2 * time.Second)
			}
			if f.DiffType == image.Removed {
				m.setStatus("File removed in this layer")
				return m, m.scheduleStatusClear(2 * time.Second)
			}
			if m.extractor == nil {
				m.setStatus("Extractor unavailable")
				return m, m.scheduleStatusClear(2 * time.Second)
			}
			m.setStatus("Extracting...")
			m.saveRequestID++
			if m.saveCancel != nil {
				m.saveCancel()
			}
			ctx, cancel := context.WithCancel(m.fetchCtx)
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
			m.aggCursor = 0
			m.aggOffset = 0
		} else {
			m.filterActive = false
		}
		return m, nil
	default:
		if msg.Text != "" {
			queryRunes := []rune(m.filterQuery)
			textRunes := []rune(msg.Text)
			remaining := maxFilterLen - len(queryRunes)
			if remaining <= 0 {
				return m, nil
			}
			if len(textRunes) > remaining {
				textRunes = textRunes[:remaining]
			}
			m.filterQuery += string(textRunes)
			m.treeCursor = 0
			m.treeOffset = 0
			m.aggCursor = 0
			m.aggOffset = 0
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
			queryRunes := []rune(m.viewSearchQuery)
			textRunes := []rune(msg.Text)
			remaining := maxFilterLen - len(queryRunes)
			if remaining <= 0 {
				return m, nil
			}
			if len(textRunes) > remaining {
				textRunes = textRunes[:remaining]
			}
			m.viewSearchQuery += string(textRunes)
			m.recomputeViewerMatches()
		}
		return m, nil
	}
}

func (m *model) recomputeViewerMatches() {
	m.viewSearchMatches = nil
	m.viewSearchCursor = 0
	m.viewHOffset = 0
	m.viewCursorCol = 0
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
	// Vim-style incremental search: as the user types, follow the first match
	// so it is visible immediately. Without this jump, matches past the line
	// end (long minified JSON, base64 blobs, etc.) stayed clipped off-screen
	// and the status bar's "Match 1/N" referred to a highlight no one could
	// see — which is the exact bug this fixed.
	if len(m.viewSearchMatches) > 0 {
		m.scrollToViewerMatch()
	}
}

func (m *model) scrollToViewerMatch() {
	if len(m.viewSearchMatches) == 0 {
		return
	}
	targetLine := m.viewSearchMatches[m.viewSearchCursor][0]
	targetCol := m.viewSearchMatches[m.viewSearchCursor][1]
	visHeight := m.viewVisibleHeight()
	lines := splitFileLines(m.viewContent.Data)
	totalLines := len(lines)
	desired := max(targetLine-visHeight/2, 0)
	maxOffset := max(totalLines-visHeight, 0)
	if desired > maxOffset {
		desired = maxOffset
	}
	m.viewOffset = desired

	// Translate the rune-indexed match column into a display column so wide
	// characters before the match shift the offset correctly. ansi.StringWidth
	// is grapheme-aware and matches the renderer's truncate metric.
	displayCol := targetCol
	if targetLine < totalLines {
		runes := []rune(lines[targetLine])
		if targetCol <= len(runes) {
			displayCol = ansi.StringWidth(string(runes[:targetCol]))
		}
	}

	visWidth := m.viewVisibleWidthFor(totalLines)
	if visWidth <= 0 {
		m.viewHOffset = 0
		m.viewCursorCol = displayCol
		return
	}
	// Bring the match into view if it's outside [hOffset, hOffset+visWidth).
	// Vim's sidescroll behavior centers the match horizontally when it would
	// otherwise be off-screen, mirroring how vertical scroll centers it.
	if displayCol < m.viewHOffset || displayCol >= m.viewHOffset+visWidth {
		m.viewHOffset = max(displayCol-visWidth/2, 0)
	}
	// Park the cursor on the match so subsequent h/l moves continue from
	// here rather than jumping back to column 0.
	m.viewCursorCol = displayCol
}

// viewVisibleWidth returns the column budget available for line content
// (panel width minus the gutter and panel borders). Used by horizontal
// scrolling to decide when a match has fallen off the right edge. The 2
// columns subtracted match renderFileView's contentWidth = m.width - 2.
func (m *model) viewVisibleWidth() int {
	if m.viewContent == nil {
		return 0
	}
	return m.viewVisibleWidthFor(fileViewLineCount(m.viewContent))
}

// viewVisibleWidthFor is the totalLines-cached form, used inside
// scrollToViewerMatch where the line split has already happened.
func (m *model) viewVisibleWidthFor(totalLines int) int {
	if m.width <= 0 {
		return 0
	}
	contentWidth := m.width - 2
	gutterDigits := len(fmt.Sprintf("%d", max(totalLines, 1))) + 1
	// gutter is "%*d " — gutterDigits already accounts for the trailing space.
	return max(contentWidth-gutterDigits, 1)
}

func (m model) tryOpenSelectedFile() (tea.Model, tea.Cmd) {
	pane := m.activeTreeFocus()
	files := m.displayTreeFor(pane)
	cur := m.treeCursorFor(pane)
	if cur >= len(files) {
		return m, nil
	}
	f := files[cur]
	if f.IsDir {
		if m.useTreeCollapse() {
			if pane == focusTreeAgg {
				m.aggCollapsed = toggleCollapsed(m.aggCollapsed, f.Path)
			} else {
				m.treeCollapsed = toggleCollapsed(m.treeCollapsed, f.Path)
			}
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
		m.setStatus(msg)
		return m, m.scheduleStatusClear(2 * time.Second)
	}
	if f.DiffType == image.Removed {
		m.setStatus("File removed in this layer")
		return m, m.scheduleStatusClear(2 * time.Second)
	}
	if m.extractor == nil {
		m.setStatus("Extractor unavailable")
		return m, m.scheduleStatusClear(2 * time.Second)
	}
	m.viewState = viewLoading
	m.viewLoadStartedAt = time.Now()
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
	ctx, cancel := context.WithCancel(m.fetchCtx)
	m.viewerCancel = cancel
	// The Init() spinner tick keeps firing while viewState == viewLoading,
	// so don't dispatch a second ticker here — doubling the tick rate also
	// doubles the cmd queue per file open.
	return m, m.fetchFileContent(ctx, f.Path, m.viewRequestID)
}

// cancelInflight tears down every in-flight Docker SDK call: the analysis
// fetch, the file viewer extract, and the save-to-disk extract. The viewer
// and save contexts are children of fetchCtx, so cancelling fetchCtx
// already propagates — but cancel is idempotent and keeping all three
// explicit guards against future re-parenting that would silently break
// the quit-cancels-everything contract.
func (m model) cancelInflight() {
	if m.fetchCancel != nil {
		m.fetchCancel()
	}
	if m.viewerCancel != nil {
		m.viewerCancel()
	}
	if m.saveCancel != nil {
		m.saveCancel()
	}
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

// isTreeFocused reports whether the focused panel is the file tree (in either
// single-pane mode or one of the two split-pane sub-panels).
func (m model) isTreeFocused() bool {
	return m.focus == focusTree || m.focus == focusTreeAgg
}

// treeCursorFor returns the cursor index for the given focus. focusTree
// drives the per-layer-Δ pane (top in split mode, the only pane otherwise);
// focusTreeAgg drives the cumulative pane (bottom of the split). Any other
// focus value returns 0 — callers should guard with isTreeFocused first.
func (m model) treeCursorFor(f focus) int {
	if f == focusTreeAgg {
		return m.aggCursor
	}
	return m.treeCursor
}

// treeOffsetFor mirrors treeCursorFor for scroll offsets.
func (m model) treeOffsetFor(f focus) int {
	if f == focusTreeAgg {
		return m.aggOffset
	}
	return m.treeOffset
}

// rootFor returns the FileTree root for the given focus, picking the
// per-layer-Δ tree (StackedTrees) for focusTree and the cumulative tree
// (AggregatedTrees) for focusTreeAgg. Single-pane mode (aggregated=false)
// always uses the per-layer Δ tree because focusTreeAgg is unreachable.
func (m model) rootFor(f focus) *image.FileNode {
	if m.analysis == nil {
		return nil
	}
	trees := m.analysis.StackedTrees
	if f == focusTreeAgg {
		trees = m.analysis.AggregatedTrees
	}
	if m.layerCursor >= len(trees) {
		return nil
	}
	tree := trees[m.layerCursor]
	if tree == nil {
		return nil
	}
	return tree.Root
}

func (m model) collapsedFor(f focus) map[string]bool {
	if f == focusTreeAgg {
		return m.aggCollapsed
	}
	return m.treeCollapsed
}

// displayTreeFor flattens, filters, and sorts the tree visible in the given
// pane. The same composition rules (collapse → diff-only → filter → sort)
// apply to both panes.
func (m model) displayTreeFor(f focus) []*image.FileNode {
	root := m.rootFor(f)
	var files []*image.FileNode
	if m.useTreeCollapse() {
		files = visibleTree(root, m.collapsedFor(f))
	} else {
		files = flattenTree(root)
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

func (m model) useTreeCollapse() bool {
	return m.sortMode == sortNone && m.filterQuery == "" && !m.diffOnly
}

// currentTreeRoot returns the root for the *active* pane: the focused
// sub-panel in split mode, otherwise the single tree pane. In single-pane
// (aggregated=false) mode the result is StackedTrees[layerCursor]; in
// split-pane mode it follows the focused sub-panel (top vs bottom).
func (m model) currentTreeRoot() *image.FileNode {
	return m.rootFor(m.activeTreeFocus())
}

// activeTreeFocus returns the pane whose state should drive shared
// operations (path-copy, status bar "tree cursor of N", Enter). When the
// layers panel itself is focused but aggregated mode is on, defer to the
// top sub-panel — that is what the user last navigated.
func (m model) activeTreeFocus() focus {
	if m.focus == focusTreeAgg {
		return focusTreeAgg
	}
	return focusTree
}

func (m *model) clearTreeCollapsed() {
	m.treeCollapsed = nil
}

func (m *model) clearAggCollapsed() {
	m.aggCollapsed = nil
}

func (m *model) resetTreeForLayerChange() {
	m.treeCursor = 0
	m.treeOffset = 0
	m.aggCursor = 0
	m.aggOffset = 0
	m.sortMode = sortNone
	m.clearTreeCollapsed()
	m.clearAggCollapsed()
}

// displayTree returns the file slice for the *active* pane. Kept for
// callers that don't need to specify a pane (status bar, viewer open,
// existing tests). Internally delegates to displayTreeFor.
func (m model) displayTree() []*image.FileNode {
	return m.displayTreeFor(m.activeTreeFocus())
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
		files := m.displayTreeFor(focusTree)
		if m.treeCursor < len(files)-1 {
			m.treeCursor++
			m.adjustTreeScrollFor(focusTree)
		}
	case focusTreeAgg:
		files := m.displayTreeFor(focusTreeAgg)
		if m.aggCursor < len(files)-1 {
			m.aggCursor++
			m.adjustTreeScrollFor(focusTreeAgg)
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
			m.adjustTreeScrollFor(focusTree)
		}
	case focusTreeAgg:
		if m.aggCursor > 0 {
			m.aggCursor--
			m.adjustTreeScrollFor(focusTreeAgg)
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
	case focusTreeAgg:
		m.aggCursor = 0
		m.aggOffset = 0
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
		files := m.displayTreeFor(focusTree)
		if len(files) > 0 {
			m.treeCursor = len(files) - 1
			m.adjustTreeScrollFor(focusTree)
		}
	case focusTreeAgg:
		files := m.displayTreeFor(focusTreeAgg)
		if len(files) > 0 {
			m.aggCursor = len(files) - 1
			m.adjustTreeScrollFor(focusTreeAgg)
		}
	}
}

// adjustTreeScrollFor brings the cursor of the named pane into its visible
// window. The visible window itself depends on whether the panel is split:
// in single-pane mode the tree gets the full content rows; in split mode
// each sub-panel gets roughly half (treeVisibleHeightFor handles that).
func (m *model) adjustTreeScrollFor(f focus) {
	visibleHeight := m.treeVisibleHeightFor(f)
	if visibleHeight <= 0 {
		return
	}
	cur := m.treeCursorFor(f)
	off := m.treeOffsetFor(f)
	if cur < off {
		off = cur
	}
	if cur >= off+visibleHeight {
		off = cur - visibleHeight + 1
	}
	if f == focusTreeAgg {
		m.aggOffset = off
	} else {
		m.treeOffset = off
	}
}

// adjustTreeScroll is a thin wrapper around adjustTreeScrollFor for the
// active pane. Kept so existing callers (mouse wheel, file open) need not
// know about the split.
func (m *model) adjustTreeScroll() {
	m.adjustTreeScrollFor(m.activeTreeFocus())
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

// treeVisibleHeightFor returns the visible content rows for the given
// pane. In single-pane mode (aggregated=false) every focus value returns
// the full panel; in split mode each sub-panel takes roughly half, with
// the column-header and filter-bar overhead subtracted from each.
func (m *model) treeVisibleHeightFor(f focus) int {
	totalContent := m.height - 8 // chrome rows excluded; matches viewReady
	if !m.aggregated {
		// Single pane: one column header, plus filter bar when present.
		h := totalContent - 1
		if m.filterQuery != "" || m.filterActive {
			h--
		}
		return h
	}
	// Split: top pane gets ceil(half), bottom gets floor(half), minus a
	// 1-row horizontal divider between them. Each half pays its own
	// column-header (1 row); the filter bar belongs only to the focused
	// half so the un-focused half keeps an extra row of content.
	topHalf, bottomHalf := splitPanelRows(totalContent)
	if f == focusTreeAgg {
		h := bottomHalf - 1 // column header
		if (m.filterQuery != "" || m.filterActive) && m.focus == focusTreeAgg {
			h--
		}
		return h
	}
	h := topHalf - 1
	if (m.filterQuery != "" || m.filterActive) && m.focus != focusTreeAgg {
		h--
	}
	return h
}

// splitPanelRows divides total content rows between top and bottom
// sub-panels with a 1-row divider between them. The top half is given any
// odd extra row so a 9-row panel yields 4-1-4 (top, divider, bottom).
func splitPanelRows(total int) (top, bottom int) {
	if total < 4 {
		// Below 4 rows the split is meaningless; let both panes render
		// a degenerate single line and let the renderer clamp.
		return total, 0
	}
	usable := total - 1 // reserve one row for the divider
	top = (usable + 1) / 2
	bottom = usable - top
	return top, bottom
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
	m.clampTreeCursor(focusTree)
	if m.aggregated {
		m.clampTreeCursor(focusTreeAgg)
	}
}

func (m *model) clampTreeCursor(f focus) {
	files := m.displayTreeFor(f)
	if len(files) == 0 {
		if f == focusTreeAgg {
			m.aggCursor = 0
			m.aggOffset = 0
		} else {
			m.treeCursor = 0
			m.treeOffset = 0
		}
		return
	}
	cur := m.treeCursorFor(f)
	if cur >= len(files) {
		cur = len(files) - 1
	}
	if cur < 0 {
		cur = 0
	}
	if f == focusTreeAgg {
		m.aggCursor = cur
	} else {
		m.treeCursor = cur
	}
	m.adjustTreeScrollFor(f)
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

// formatElapsed renders d as "m:ss" for the loading panel timer line.
// Capped at 999:59 so a forgotten background pull does not widen the
// panel by an unbounded amount.
func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d / time.Second)
	mins := total / 60
	secs := total % 60
	if mins > 999 {
		mins = 999
		secs = 59
	}
	return fmt.Sprintf("%d:%02d", mins, secs)
}

// phaseGlyph returns the per-phase indicator used inline with the
// progress detail line. The set is intentionally tiny — one character
// per phase keeps the loading panel from getting noisier as more
// phases land.
func phaseGlyph(p image.ProgressPhase) string {
	switch p {
	case image.PhasePulling:
		return "↓"
	case image.PhaseExporting:
		return "▣"
	case image.PhaseParsing:
		return "≡"
	case image.PhaseCacheLoad:
		return "✓"
	default:
		return "…"
	}
}

// renderPhaseStepper draws a two-line "Pull ── Export ── Parse" rail
// above three dots. The active step is rendered in accentColor; reached
// steps in statusDimColor; not-yet-reached steps also in statusDimColor.
// PhaseCacheLoad collapses all three dots to ✓ in accent — the cache
// hit supersedes the live pipeline.
func renderPhaseStepper(active image.ProgressPhase) string {
	labels := []string{"Pull", "Export", "Parse"}
	phases := []image.ProgressPhase{image.PhasePulling, image.PhaseExporting, image.PhaseParsing}

	accent := lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	dim := lipgloss.NewStyle().Foreground(statusDimColor)

	// Cache-hit stepper: three ✓ dots in accent, labels dim.
	if active == image.PhaseCacheLoad {
		var labelRow, dotRow strings.Builder
		for i, label := range labels {
			if i > 0 {
				labelRow.WriteString(dim.Render(" ── "))
				dotRow.WriteString("    ")
			}
			labelRow.WriteString(dim.Render(label))
			dotRow.WriteString(centerOver(accent.Render("✓"), label))
		}
		return labelRow.String() + "\n" + dotRow.String()
	}

	activeIdx := -1
	for i, p := range phases {
		if p == active {
			activeIdx = i
			break
		}
	}
	// Pre-first-event default: treat Pull as active.
	if activeIdx < 0 {
		activeIdx = 0
	}

	var labelRow, dotRow strings.Builder
	for i, label := range labels {
		if i > 0 {
			labelRow.WriteString(dim.Render(" ── "))
			dotRow.WriteString("    ")
		}
		var labelRendered, dotRendered string
		switch {
		case i < activeIdx:
			labelRendered = dim.Render(label)
			dotRendered = dim.Render("●")
		case i == activeIdx:
			labelRendered = accent.Render(label)
			dotRendered = accent.Render("●")
		default:
			labelRendered = dim.Render(label)
			dotRendered = dim.Render("○")
		}
		labelRow.WriteString(labelRendered)
		dotRow.WriteString(centerOver(dotRendered, label))
	}
	return labelRow.String() + "\n" + dotRow.String()
}

// centerOver returns dot padded with spaces so it visually centers
// beneath an unstyled label of the given text. Display-width aware.
func centerOver(dot, label string) string {
	labelW := lipgloss.Width(label)
	dotW := lipgloss.Width(dot)
	if labelW <= dotW {
		return dot
	}
	left := (labelW - dotW) / 2
	right := labelW - dotW - left
	return strings.Repeat(" ", left) + dot + strings.Repeat(" ", right)
}

// centerLine pads s with leading spaces so it appears horizontally centered
// inside a panel of contentWidth display columns. Display-width aware via
// lipgloss.Width — leading ANSI escapes and wide runes do not skew the
// padding. Safe on narrow widths: if s is already wider than the budget,
// the original string is returned untouched (renderPanel clips anything
// that overshoots).
func centerLine(line string, width int) string {
	if width <= 0 {
		return line
	}
	w := lipgloss.Width(line)
	if w >= width {
		return line
	}
	pad := (width - w) / 2
	if pad <= 0 {
		return line
	}
	return strings.Repeat(" ", pad) + line
}

func (m model) viewLoading() tea.View {
	if m.width > 0 && m.width < 10 {
		return finalizeView(tea.NewView("loading…"))
	}
	frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
	glyph := phaseGlyph(m.loadPhase)
	dimStyle := lipgloss.NewStyle().Foreground(statusDimColor)

	var lines []string
	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render("◆ layerx"))
	lines = append(lines, "")

	switch m.loadPhase {
	case image.PhasePulling:
		lines = append(lines, fmt.Sprintf("%s %s Pulling %s …", frame, glyph, m.imageRef))
		// Render the running history of completed layers (dimmed) followed
		// by the in-flight layer (accent). Capped at maxPullLayerRows
		// so a 200-layer pull cannot push the panel past the terminal
		// height — the live layer always stays visible by trimming the
		// oldest completed rows first.
		const maxPullLayerRows = 5
		barBudget := 20
		liveBarMax := m.pullBytesMax
		if liveBarMax > 0 {
			// Reserve panel width for the longest possible layer-log line
			// shape: "Layer NN/NN  [bar]  X.X / Y.Y GB". Mirror the
			// approach used for the existing progress detail line below.
			sample := fmt.Sprintf("Layer %d/%d  []  %s / %s",
				max(m.pullTotal, 1), max(m.pullTotal, 1),
				image.FormatBytes(liveBarMax), image.FormatBytes(liveBarMax))
			if m.width > 0 {
				budget := m.width - 6 - lipgloss.Width(sample)
				if budget < barBudget {
					barBudget = budget
				}
			}
		}
		liveLayer := m.pullLayers
		if liveLayer < 1 {
			liveLayer = 1
		}
		if m.pullTotal > 0 && liveLayer > m.pullTotal {
			liveLayer = m.pullTotal
		}

		log := m.pullLayerLog
		// Keep the most recent completed rows when capped: budget reserves
		// one slot for the in-flight layer line.
		if len(log) > maxPullLayerRows-1 {
			log = log[len(log)-(maxPullLayerRows-1):]
		}
		for _, snap := range log {
			line := renderPullLayerLine(snap.layerNum, m.pullTotal, snap.bytesCurr, snap.bytesMax, barBudget, true)
			lines = append(lines, dimStyle.Render(line))
		}
		if m.pullTotal > 0 {
			liveLine := renderPullLayerLine(liveLayer, m.pullTotal, m.pullBytes, m.pullBytesMax, barBudget, false)
			lines = append(lines, lipgloss.NewStyle().Foreground(accentColor).Render(liveLine))
		}
	case image.PhaseExporting:
		sizeInfo := ""
		if m.imageSize > 0 {
			sizeInfo = " (" + image.FormatBytes(m.imageSize) + ")"
		}
		lines = append(lines, fmt.Sprintf("%s %s Loading %s%s …", frame, glyph, m.imageRef, sizeInfo))
		lines = append(lines, "Exporting layers…")
	case image.PhaseParsing:
		sizeInfo := ""
		if m.imageSize > 0 {
			sizeInfo = " (" + image.FormatBytes(m.imageSize) + ")"
		}
		lines = append(lines, fmt.Sprintf("%s %s Loading %s%s …", frame, glyph, m.imageRef, sizeInfo))
		lines = append(lines, "Parsing layers…")
	case image.PhaseCacheLoad:
		// No spinner during cache-hit — the glyph alone signals "done,
		// just confirming". Held for ~300ms by the analysisMsg shortcut
		// so the user actually sees it on a hot-cache run.
		successStyle := lipgloss.NewStyle().Foreground(addedColor).Bold(true)
		lines = append(lines, successStyle.Render(glyph+" "+m.imageRef+" — loaded from cache"))
	default:
		sizeInfo := ""
		if m.imageSize > 0 {
			sizeInfo = " (" + image.FormatBytes(m.imageSize) + ")"
		}
		lines = append(lines, fmt.Sprintf("%s %s Loading %s%s …", frame, glyph, m.imageRef, sizeInfo))
	}

	lines = append(lines, "")
	stepper := renderPhaseStepper(m.loadPhase)
	stepperLines := strings.Split(stepper, "\n")
	stepperWidth := 0
	for _, sl := range stepperLines {
		if w := lipgloss.Width(sl); w > stepperWidth {
			stepperWidth = w
		}
	}
	// The stepper is a two-row rail (labels + dots) that must remain
	// vertically aligned. Centre it as a single visual unit so column
	// positions inside the rail stay consistent with each other.
	for _, sl := range stepperLines {
		// Left-pad each row to the stepper's max line width so dots line
		// up beneath their labels even after centering.
		if w := lipgloss.Width(sl); w < stepperWidth {
			sl += strings.Repeat(" ", stepperWidth-w)
		}
		lines = append(lines, sl)
	}
	lines = append(lines, "")

	if !m.loadStartedAt.IsZero() {
		elapsed := time.Since(m.loadStartedAt)
		var timeLine string
		if m.loadPhase == image.PhaseCacheLoad {
			// Cache hit completes fast; show ready-in seconds with one
			// decimal so a 0.3s hold reads as "ready in 0.3s" rather
			// than rounding to 0:00.
			secs := elapsed.Seconds()
			if secs < 0 {
				secs = 0
			}
			timeLine = fmt.Sprintf("ready in %.1fs", secs)
		} else {
			timeLine = "elapsed " + formatElapsed(elapsed)
		}
		lines = append(lines, dimStyle.Render(timeLine))
	}

	lines = append(lines, dimStyle.Render("Press q or Esc to exit."))

	boxWidth := 52
	for _, ln := range lines {
		if w := lipgloss.Width(ln); w+2 > boxWidth {
			boxWidth = w + 2
		}
	}
	if m.width > 0 && m.width-2 < boxWidth {
		boxWidth = m.width - 2
	}

	contentWidth := boxWidth
	centered := make([]string, 0, len(lines)+2)
	centered = append(centered, "")
	for _, ln := range lines {
		centered = append(centered, centerLine(ln, contentWidth))
	}
	centered = append(centered, "")

	boxHeight := len(centered)
	body := strings.Join(centered, "\n")
	panel := renderPanel(body, "Loading", true, boxWidth, boxHeight, false, false)
	content := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
	return finalizeView(tea.NewView(content))
}

// renderPullLayerLine formats one row of the pull-layer log. When totals
// are unknown (bytesMax == 0) the bar and bytes text are dropped — only
// the "Layer N/M" counter survives, which matches the existing fallback
// the loading panel uses on opaque registries that never report sizes.
// completed flips the bar-fill style to "all filled" so a finished
// layer always shows a full rail regardless of bar width rounding.
func renderPullLayerLine(layerNum, layerTotal int, bytesCurr, bytesMax int64, barWidth int, completed bool) string {
	total := layerTotal
	if total < 1 {
		total = 1
	}
	if layerNum < 1 {
		layerNum = 1
	}
	counter := fmt.Sprintf("Layer %d/%d", layerNum, total)
	if bytesMax <= 0 {
		return counter
	}
	pct := 100
	if !completed {
		pct = min(int(bytesCurr*100/bytesMax), 100)
	}
	bytesText := fmt.Sprintf("  %s / %s", image.FormatBytes(bytesCurr), image.FormatBytes(bytesMax))
	if completed {
		bytesText = fmt.Sprintf("  %s / %s", image.FormatBytes(bytesMax), image.FormatBytes(bytesMax))
	}
	if barWidth < 4 {
		return counter + bytesText
	}
	filled := barWidth * pct / 100
	if completed {
		filled = barWidth
	}
	bar := strings.Repeat("━", filled) + strings.Repeat("─", barWidth-filled)
	return fmt.Sprintf("%s  [%s]%s", counter, bar, bytesText)
}

// renderRightPanel paints the file-tree side of the screen. In single-pane
// (aggregated=false) mode it delegates to renderFileTree; in split-pane
// (aggregated=true) mode it renders both StackedTrees and AggregatedTrees
// stacked vertically inside one panel border.
func (m model) renderRightPanel(width, height int) string {
	if !m.aggregated {
		treeFiles := m.displayTreeFor(focusTree)
		return renderFileTree(treeFiles, m.treeCursor, m.treeOffset,
			width, height, m.focus == focusTree, m.filterActive,
			m.filterQuery, m.useTreeCollapse(), false,
			m.treeCollapsed, m.layerCursor)
	}
	topFiles := m.displayTreeFor(focusTree)
	botFiles := m.displayTreeFor(focusTreeAgg)
	return renderSplitFileTree(splitTreeInput{
		width:        width,
		height:       height,
		currentLayer: m.layerCursor,
		treeMode:     m.useTreeCollapse(),
		topFiles:     topFiles,
		topCursor:    m.treeCursor,
		topOffset:    m.treeOffset,
		topFocused:   m.focus == focusTree,
		topCollapsed: m.treeCollapsed,
		botFiles:     botFiles,
		botCursor:    m.aggCursor,
		botOffset:    m.aggOffset,
		botFocused:   m.focus == focusTreeAgg,
		botCollapsed: m.aggCollapsed,
		filterActive: m.filterActive,
		filterQuery:  m.filterQuery,
	})
}

func (m model) viewError() tea.View {
	if m.width > 0 && m.width < 10 {
		return finalizeView(tea.NewView("error"))
	}
	errStyle := lipgloss.NewStyle().Foreground(removedColor).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(statusDimColor)
	hintStyle := lipgloss.NewStyle().Foreground(modifiedColor)

	var lines []string
	lines = append(lines, errStyle.Render("✕ Error"))
	lines = append(lines, "")
	lines = append(lines, strings.Split(m.errMsg, "\n")...)
	if m.errHint != "" {
		lines = append(lines, "")
		lines = append(lines, hintStyle.Render(m.errHint))
	}
	lines = append(lines, "")
	lines = append(lines, dimStyle.Render("Press q or Esc to exit."))

	boxWidth := 52
	for _, ln := range lines {
		if w := lipgloss.Width(ln); w+2 > boxWidth {
			boxWidth = w + 2
		}
	}
	if m.width > 0 && m.width-2 < boxWidth {
		boxWidth = m.width - 2
	}

	centered := make([]string, 0, len(lines)+2)
	centered = append(centered, "")
	for _, ln := range lines {
		centered = append(centered, centerLine(ln, boxWidth))
	}
	centered = append(centered, "")

	boxHeight := len(centered)
	body := strings.Join(centered, "\n")
	panel := renderPanel(body, "Error", true, boxWidth, boxHeight, false, false)
	content := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
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
	right := m.renderRightPanel(rightWidth, panelHeight)

	panels := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)

	if m.viewState != viewNone {
		var loadingPath string
		var elapsed time.Duration
		if m.viewState == viewLoading {
			files := m.displayTreeFor(m.activeTreeFocus())
			cur := m.treeCursorFor(m.activeTreeFocus())
			if cur < len(files) {
				loadingPath = files[cur].Path
			}
			if !m.viewLoadStartedAt.IsZero() {
				elapsed = time.Since(m.viewLoadStartedAt)
			}
		}
		viewer := renderFileView(viewerParams{
			content:       m.viewContent,
			offset:        m.viewOffset,
			hOffset:       m.viewHOffset,
			cursorCol:     m.viewCursorCol,
			width:         m.width,
			height:        panelHeight,
			loading:       m.viewState == viewLoading,
			loadingPath:   loadingPath,
			elapsed:       elapsed,
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

	// Release mouse capture when the file viewer is open so the terminal
	// handles mouse events natively, enabling text selection by click+drag.
	if m.viewState != viewNone {
		return finalizeViewNoMouse(tea.NewView(content))
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
	// Append the active platform after the image name when --platform is
	// set. Multi-platform images otherwise give no visual cue which variant
	// is on screen — easy to misread an arm64 layout as amd64.
	if m.platform != "" {
		platformStyle := lipgloss.NewStyle().Foreground(headerDimColor).Background(statusBgColor)
		left += sep + platformStyle.Render(m.platform)
	}

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

	// `?` is pinned to the rightmost slot of the hint cluster — see
	// renderHelpHint below. The lists here are everything *but* help.
	if m.focus == focusLayers {
		hints = []hint{
			{"Tab", "switch"},
			{"j/k", "navigate"},
			{"g/G", "top/bottom"},
			{"S", "size"},
			{"d", "diff"},
			{"s", "sort"},
			{"c", "copy cmd"},
			{"w", "wasted"},
			{"A", "split"},
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
			{"A", "split"},
			{"Enter", enterDesc},
			{"x", "save"},
			{"y", "copy path"},
		}
	}

	helpHint := keyStyle.Render("?") + " " + descStyle.Render("help")

	var hintStr string
	if compact {
		parts := make([]string, len(hints))
		for i, h := range hints {
			parts[i] = keyStyle.Render(h.key)
		}
		// Compact mode shows keys only for the main hints, but keeps the
		// `? help` desc so a user new to the TUI can still discover the
		// help overlay. If even that won't fit, the gap calculation
		// below will collapse the helpHint to keys only.
		hintStr = " " + strings.Join(parts, " ") + " " + sepStyle.Render("│") + " " + helpHint
	} else {
		var parts []string
		for _, h := range hints {
			parts = append(parts, keyStyle.Render(h.key)+" "+descStyle.Render(h.desc))
		}
		hintStr = " " + strings.Join(parts, " "+sepStyle.Render("│")+" ") +
			" " + sepStyle.Render("│") + " " + helpHint
	}

	layers := m.layers()
	var right string
	if m.statusMsg != "" {
		var color = modifiedColor
		switch m.statusKind {
		case statusOK:
			color = addedColor
		case statusErr:
			color = removedColor
		}
		msgStyle := lipgloss.NewStyle().Foreground(color).Background(statusBgColor).Bold(true)
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
		if m.aggregated {
			badges += lipgloss.NewStyle().Foreground(accentColor).Background(statusBgColor).Render("[split]") + " "
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

	// When `?` help still wouldn't fit alongside everything else, fall
	// back to the bare key so the discoverability hint never disappears
	// entirely. The minus-1 keeps a single space between the hint cluster
	// and the right-side status block.
	if lipgloss.Width(hintStr)+lipgloss.Width(right) > m.width-1 {
		// Strip the description from the help hint.
		hintStr = strings.TrimSuffix(hintStr, " "+sepStyle.Render("│")+" "+helpHint)
		hintStr += " " + sepStyle.Render("│") + " " + keyStyle.Render("?")
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
		keyStyle.Render("j/k") + " " + descStyle.Render("up/down") + " " +
		sepStyle.Render("│") + " " +
		keyStyle.Render("h/l") + " " + descStyle.Render("left/right") + " " +
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

// saveFileCmd resolves a non-clobbering target via uniquePath, writes the
// bytes, and returns a fileSavedMsg. Runs inside a tea.Cmd goroutine — the
// caller's Update returns immediately so the TUI does not block on slow
// I/O. 0644 is honoured on POSIX; on Windows os.WriteFile ignores the
// mode bits beyond the read-only attribute.
func saveFileCmd(ctx context.Context, stat func(string) (os.FileInfo, error),
	write func(string, []byte, os.FileMode) error,
	requestID uint64, filename string, data []byte) tea.Cmd {
	return func() tea.Msg {
		target, err := uniquePath(ctx, stat, filename)
		if err != nil {
			return fileSavedMsg{requestID: requestID, original: filename, target: filename, err: err}
		}
		if err := ctx.Err(); err != nil {
			return fileSavedMsg{requestID: requestID, original: filename, target: target, err: err}
		}
		if err := write(target, data, 0644); err != nil {
			return fileSavedMsg{requestID: requestID, original: filename, target: target, err: err}
		}
		return fileSavedMsg{requestID: requestID, original: filename, target: target}
	}
}

// uniquePath returns filename if no file exists at that path, otherwise
// the first available "<name>.<N>" (or "<name>.<N><ext>") variant. Probes
// up to 1000 candidates and returns an error if all are taken — never
// silently clobbers a pre-existing file by reusing the .999 candidate.
// The probe loop honours ctx so a quit can abort a slow filesystem.
func uniquePath(ctx context.Context, stat func(string) (os.FileInfo, error), filename string) (string, error) {
	if stat == nil {
		return filename, nil
	}
	if _, err := stat(filename); errors.Is(err, os.ErrNotExist) {
		return filename, nil
	}
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	for i := 1; i < 1000; i++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		candidate := fmt.Sprintf("%s.%d%s", base, i, ext)
		if _, err := stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("save target busy: 1000 candidates already exist for %s", filename)
}

// atomicWriteFile writes data to name via a same-directory temp file and an
// os.Rename — the production default for model.writeFile. Tests inject a
// plain in-memory function and never touch this code.
//
// Why the indirection: the previous implementation called os.WriteFile, which
// truncates and writes in place. Two failure modes followed:
//
//  1. Process kill (Ctrl+C, SIGKILL, OOM, power loss) mid-write left a
//     partially-written file at the user's chosen path — silent corruption,
//     no error surfaced. Users running `x` to extract a config file from a
//     production image and aborting because the daemon froze would end up
//     with a half-truncated config that looked legitimate.
//  2. Write errors after partial bytes had reached disk left the same kind
//     of partial-content corruption with the error visible — but by that
//     point the user-visible file at `name` already had the wrong contents.
//
// Atomic-replace via tempfile + rename guarantees that `name` either holds
// the complete pre-write content (if it existed) or the complete new content;
// it never holds a mix. On POSIX rename is atomic. Go's os.Rename on Windows
// is implemented via MoveFileEx with MOVEFILE_REPLACE_EXISTING, so it
// replaces an existing target atomically — uniquePath's pre-probe handles
// the user-visible "don't overwrite" intent at a higher level.
//
// The temp file is created in the same directory as the target so rename
// stays within one filesystem (cross-fs rename returns EXDEV). It is
// removed on any failure path so a crashed save doesn't leak a stray
// .layerx-save-XXXXXX file next to the user's data.
//
// Permissions are applied via the open file descriptor (tmp.Chmod) before
// Close, eliminating the post-close window in which a virus scanner or
// indexer can hold the path open and reject a path-based Chmod on Windows.
//
// Symlinks: if `name` resolves through a symlink, the rename targets the
// link's resolved path so the symlink itself is preserved (matching the
// semantics of os.WriteFile, which opens with O_TRUNC and writes through
// the symlink to its target). Only `not exist` errors fall back to writing
// at `name` directly — other resolution failures (EACCES, ELOOP) surface
// to the caller rather than silently writing at the link.
//
// Durability: after rename, the parent directory is fsynced on POSIX so
// the rename survives power loss. On Windows the directory fsync call is
// a no-op (the OS does not expose this primitive); the rename itself is
// already MFT-journaled so power-loss safety still holds via NTFS rather
// than via this code.
func atomicWriteFile(name string, data []byte, perm os.FileMode) error {
	target := name
	if resolved, err := filepath.EvalSymlinks(name); err == nil {
		target = resolved
	} else if !os.IsNotExist(err) {
		return err
	}
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".layerx-save-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// On any error path below, remove the temp; on success the rename has
	// already moved it so Remove is a no-op.
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(applyUmask(perm)); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		cleanup()
		return err
	}
	syncDir(dir)
	return nil
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

// scrollViewRight moves the logical cursor one cell to the right, then
// shifts the viewport only when the cursor would cross the right edge —
// matching vim's h/l semantics, where every keystroke advances exactly
// one column and the window only scrolls when the cursor would otherwise
// leave it.
func (m *model) scrollViewRight() {
	maxCol := m.viewMaxCursorCol()
	if m.viewCursorCol >= maxCol {
		return
	}
	m.viewCursorCol++
	visWidth := m.viewVisibleWidth()
	if visWidth > 0 && m.viewCursorCol >= m.viewHOffset+visWidth {
		m.viewHOffset = m.viewCursorCol - visWidth + 1
	}
}

// scrollViewLeft is the mirror of scrollViewRight: one cell at a time,
// shift the viewport only if the cursor would fall left of the visible
// region.
func (m *model) scrollViewLeft() {
	if m.viewCursorCol == 0 {
		m.viewHOffset = 0
		return
	}
	m.viewCursorCol--
	if m.viewCursorCol < m.viewHOffset {
		m.viewHOffset = m.viewCursorCol
	}
}

// viewMaxCursorCol bounds the cursor by the longest line in the visible
// region. Bounding by the whole file would force a full O(N·W) scan on
// every h/l press; bounding by visible lines keeps the cost proportional
// to viewport height and matches what the user can actually see.
func (m *model) viewMaxCursorCol() int {
	if m.viewContent == nil {
		return 0
	}
	lines := splitFileLines(m.viewContent.Data)
	if len(lines) == 0 {
		return 0
	}
	end := min(m.viewOffset+m.viewVisibleHeight(), len(lines))
	if m.viewOffset >= end {
		return 0
	}
	maxW := 0
	for _, line := range lines[m.viewOffset:end] {
		if w := ansi.StringWidth(line); w > maxW {
			maxW = w
		}
	}
	return maxW
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
	if archErr, ok := errors.AsType[*image.ErrArchiveNotFound](err); ok {
		return fmt.Sprintf("Archive %q not found. Check the file path.", archErr.Path)
	}
	if permErr, ok := errors.AsType[*image.ErrArchivePermission](err); ok {
		return fmt.Sprintf("Permission denied opening archive %q. Check file permissions.", permErr.Path)
	}
	if invalidErr, ok := errors.AsType[*image.ErrInvalidArchive](err); ok {
		return fmt.Sprintf("Not a valid image archive: %q. Expected a docker-save or OCI layout tarball.", invalidErr.Path)
	}
	if infraErr, ok := errors.AsType[*image.ErrArchiveInfra](err); ok {
		return fmt.Sprintf("Could not %s: %v. Free up disk space or set TMPDIR to a writable location and try again.", infraErr.Op, infraErr.Cause)
	}
	// Platform errors are already user-readable; pass them through verbatim
	// so the multi-line "Available platforms:" list keeps its formatting.
	if pErr, ok := errors.AsType[*image.ErrPlatformNotInImage](err); ok {
		return pErr.Error()
	}
	if pErr, ok := errors.AsType[*image.ErrPlatformInvalid](err); ok {
		return pErr.Error()
	}
	return err.Error()
}

// errorHint returns a short follow-up sentence for errors with an
// obvious next action (e.g. daemon unreachable → suggest archive mode).
// Returns "" for errors with no clearly correct hint.
func errorHint(err error) string {
	if _, ok := errors.AsType[*image.ErrDaemonNotRunning](err); ok {
		return "Start Docker, or pass a saved archive path instead (no daemon needed)."
	}
	if _, ok := errors.AsType[*image.ErrNoEngineFound](err); ok {
		return "Start Docker or Podman, or pass a saved archive path instead (no daemon needed)."
	}
	return ""
}

// Run starts the TUI program with the given configuration.
func Run(cfg Config) error {
	m := NewModel(cfg)
	defer m.fetchCancel()
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
