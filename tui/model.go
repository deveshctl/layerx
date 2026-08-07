package tui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/deveshctl/layerx/image"
)

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
	// Theme is the name of the colour theme to use. Empty string means the
	// built-in default (tokyo-night). Valid values match the names accepted
	// by the theme: key in .layerx.yaml.
	Theme string
	// TransparentBg strips all background colours from the TUI so the
	// terminal's own background shows through. Opt-in; default false.
	TransparentBg bool
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

type analysisMsg struct {
	analysis *image.Analysis
	err      error
}

type inspectMsg struct {
	meta *image.ImageMeta
	err  error
}

type progressMsg struct {
	event image.ProgressEvent
}

type spinnerTickMsg struct{}

type clearCopyMsg struct{}

// clearStatusMsg clears the transient status bar message after a timeout.
// gen identifies which status set scheduled this tick; an older tick
// whose gen no longer matches m.statusGen is ignored so it cannot erase
// a newer message that overwrote the original mid-window.
type clearStatusMsg struct{ gen uint64 }

// setStatus assigns msg to the status bar and bumps statusGen so any
// previously-scheduled clearStatusMsg ticks become stale and no-ops.
func (m *model) setStatus(msg string) {
	m.statusMsg = msg
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

// treeCache memoizes the flatten→filter→sort output of displayTreeFor between
// frames. A single keystroke in split+filter+sort mode drives displayTreeFor up
// to seven times per round-trip (cursor bounds, clamp, status bar, both render
// passes); without a cache each call re-walks the whole FileNode tree.
//
// The cache lives behind a pointer on the model because displayTreeFor has a
// value receiver and the model is copied by value on every Update — a value
// field would be written to a throwaway copy and never survive. All copies of
// the model share one *treeCache; staleness is caught by comparing the stored
// key against the current inputs, not by assuming a copy carried fresh data.
//
// Both panes (focusTree and focusTreeAgg) get an independent slot so split-mode
// rendering, which asks for both trees in the same frame, does not thrash a
// single slot. The generation counter that keys collapse state lives on the
// model (collapsedGen), because toggleCollapsed mutates a map in place and
// returns the same reference — neither map identity nor contents can be
// compared cheaply, so the counter is bumped on every collapse mutation
// instead (see the toggleCollapsed call sites / clear*Collapsed).
type treeCacheSlot struct {
	valid        bool
	layerCursor  int
	filterQuery  string
	diffOnly     bool
	sortMode     sortMode
	collapsedGen uint64
	analysisGen  uint64
	files        []*image.FileNode
}

type treeCache struct {
	top treeCacheSlot // focusTree
	bot treeCacheSlot // focusTreeAgg
}

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
	statusGen    uint64
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
	// viewLines is the plain-text split of viewContent.Data, computed once when
	// the file opens. The viewer's hot paths (scroll clamp, cursor-column bound,
	// search indexing, render) read this instead of re-running splitFileLines —
	// which copies the whole body and allocates per line — on every keystroke
	// and every frame. nil when no file is open; parallels viewHighlightedLines.
	viewLines        []string
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
	theme         Theme
	transparentBg bool
	// renderedImageRef is the gradient-coloured image ref for the header,
	// precomputed once in NewModel. Both inputs (imageRef, theme gradient
	// stops) are immutable for the session, so renderHeader must not recompute
	// the per-rune colour interpolation on every frame.
	renderedImageRef string

	// collapsedGen is bumped whenever a collapse map is mutated; it is the
	// invalidation key for the displayTreeFor cache (see treeCache).
	collapsedGen uint64
	// analysisGen is bumped whenever m.analysis is replaced. It keys the
	// displayTreeFor cache against the analysis identity: a valid slot whose
	// layerCursor/filters are unchanged would otherwise return the previous
	// analysis's FileNode slice after a re-analysis. Today analysis is set
	// exactly once, so this only ever reaches 1 — it is future-proofing, not a
	// live fix, and costs one comparison per lookup.
	analysisGen uint64
	// treeCache memoizes displayTreeFor across frames. Behind a pointer so the
	// value-receiver method can write through it and all model copies share it.
	treeCache *treeCache
	// styles holds lipgloss.Style values derived from the session theme.
	// Built once in NewModel; reused every frame to avoid per-call allocation.
	styles themeStyles

	fetchCtx    context.Context
	fetchCancel context.CancelFunc
}

// themeFor resolves a theme name (as accepted by --theme / .layerx.yaml)
// to a Theme value. Unknown or empty names fall back to TokyoNight.
func themeFor(name string) Theme {
	switch name {
	case "catppuccin-mocha":
		return CatppuccinMocha()
	case "tokyo-night":
		return TokyoNight()
	case "kanagawa":
		return Kanagawa()
	case "gruvbox-dark":
		return GruvboxDark()
	case "rose-pine":
		return RosePine()
	case "dracula":
		return Dracula()
	case "oxocarbon":
		return Oxocarbon()
	case "cyberdream":
		return Cyberdream()
	default:
		return TokyoNight()
	}
}

func NewModel(cfg Config) model {
	ch := make(chan image.ProgressEvent, 16)
	ctx, cancel := context.WithCancel(context.Background())
	theme := themeFor(cfg.Theme)
	return model{
		state:       stateLoading,
		imageRef:    cfg.ImageRef,
		platform:    cfg.Platform,
		resolver:    cfg.Resolver,
		progressCh:  ch,
		writeFile:   atomicWriteFile,
		statFile:    os.Lstat,
		keys:        defaultKeys(),
		noCache:     cfg.NoCache,
		theme:       theme,
		transparentBg: cfg.TransparentBg,
		treeCache:        &treeCache{},
		styles:           newThemeStyles(theme),
		renderedImageRef: renderGradient(cfg.ImageRef, theme.GradientStart, theme.GradientEnd),
		fetchCtx:    ctx,
		fetchCancel: cancel,
	}
}

// viewBg returns the theme background colour, or nil when transparent mode is
// active. Passed to finalizeView so the terminal background shows through.
func (m model) viewBg() color.Color {
	if m.transparentBg {
		return nil
	}
	return m.theme.MainBg
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
		} else if msg.err != nil && !errors.Is(msg.err, context.Canceled) && m.state == stateLoading {
			// Inspect runs concurrently with the analysis fetch. If it fails
			// fast (e.g. connection refused) while analysis is still blocked
			// on a slow pull, this is the only diagnostic the user gets until
			// analysisMsg arrives. No scheduleStatusClear: analysisMsg will
			// overwrite it, or the error state will replace the whole screen.
			m.setStatus("Inspect failed: " + friendlyError(msg.err))
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
		m.analysisGen++
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
			m.setStatus(friendlyError(msg.err))
			return m, m.scheduleStatusClear(3 * time.Second)
		}
		m.viewState = viewReady
		m.viewContent = msg.content
		m.viewHighlightedLines = nil
		m.viewLines = splitFileLines(msg.content.Data)
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
			return m, highlightFileCmd(msg.requestID, msg.content.Path, msg.content.Data, m.theme.ChromaStyle)
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
			m.setStatus(friendlyError(msg.err))
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
			m.setStatus(friendlySaveError(msg.err, msg.original))
			return m, m.scheduleStatusClear(3 * time.Second)
		}
		if msg.target != msg.original {
			m.setStatus(fmt.Sprintf("Saved: %s (existed → wrote %s)", msg.original, msg.target))
		} else {
			m.setStatus("Saved: " + msg.target)
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
				m.viewLines = nil
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

		if m.filterActive {
			return m.handleFilterInput(msg)
		}

		if m.showWaste {
			return m.handleWasteOverlay(msg)
		}

		if key.Matches(msg, m.keys.Help) && m.state == stateReady {
			m.showHelp = !m.showHelp
			return m, nil
		}

		// When help is shown, swallow all other keys.
		if m.showHelp {
			return m, nil
		}

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
				maxOffset := max(m.viewLineCount()-m.viewVisibleHeight(), 0)
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

		case key.Matches(msg, m.keys.HalfPageDown):
			m.moveByPage(1, true)
			return m, nil

		case key.Matches(msg, m.keys.HalfPageUp):
			m.moveByPage(-1, true)
			return m, nil

		case key.Matches(msg, m.keys.PageDown):
			m.moveByPage(1, false)
			return m, nil

		case key.Matches(msg, m.keys.PageUp):
			m.moveByPage(-1, false)
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
				m.setStatus("Error: cannot extract directory")
				return m, m.scheduleStatusClear(2 * time.Second)
			}
			if f.DiffType == image.Removed {
				m.setStatus("Error: file removed in this layer")
				return m, m.scheduleStatusClear(2 * time.Second)
			}
			if m.extractor == nil {
				m.setStatus("Error: extractor unavailable")
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
	lines := m.viewLines
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
	totalLines := len(m.viewLines)
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
		runes := []rune(m.viewLines[targetLine])
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
	return m.viewVisibleWidthFor(m.viewLineCount())
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
			m.collapsedGen++
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
		m.setStatus("Error: file removed in this layer")
		return m, m.scheduleStatusClear(2 * time.Second)
	}
	if m.extractor == nil {
		m.setStatus("Error: extractor unavailable")
		return m, m.scheduleStatusClear(2 * time.Second)
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
//
// The result is memoized per pane (see treeCache). A cache miss recomputes and
// stores; a hit returns the stored slice untouched. When m.treeCache is nil
// (bare model{} literals in tests) it computes through without caching, so the
// observable output is identical with or without the cache.
func (m model) displayTreeFor(f focus) []*image.FileNode {
	if m.treeCache == nil {
		return m.computeDisplayTreeFor(f)
	}
	slot := &m.treeCache.top
	if f == focusTreeAgg {
		slot = &m.treeCache.bot
	}
	if slot.valid &&
		slot.layerCursor == m.layerCursor &&
		slot.filterQuery == m.filterQuery &&
		slot.diffOnly == m.diffOnly &&
		slot.sortMode == m.sortMode &&
		slot.collapsedGen == m.collapsedGen &&
		slot.analysisGen == m.analysisGen {
		return slot.files
	}
	files := m.computeDisplayTreeFor(f)
	*slot = treeCacheSlot{
		valid:        true,
		layerCursor:  m.layerCursor,
		filterQuery:  m.filterQuery,
		diffOnly:     m.diffOnly,
		sortMode:     m.sortMode,
		collapsedGen: m.collapsedGen,
		analysisGen:  m.analysisGen,
		files:        files,
	}
	return files
}

func (m model) computeDisplayTreeFor(f focus) []*image.FileNode {
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
	m.collapsedGen++
}

func (m *model) clearAggCollapsed() {
	m.aggCollapsed = nil
	m.collapsedGen++
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

// moveByPage jumps the focused pane's cursor by a screenful. dir is +1 for
// down/forward and -1 for up/back; when fraction is true the jump is half a
// page (the ^d/^u motion), otherwise a full page (^f/^b/PgDn/PgUp). The step
// is derived from the pane's own visible height so it stays correct in split
// mode and on resize. The cursor is clamped to the item range and the pane's
// scroll offset is brought back into view, matching moveDown/moveUp. On the
// layers pane a page jump changes the selected layer, so the tree is reset to
// follow it exactly as a single-step move would.
func (m *model) moveByPage(dir int, fraction bool) {
	step := func(visibleHeight int) int {
		if visibleHeight < 1 {
			visibleHeight = 1
		}
		if fraction {
			return max(visibleHeight/2, 1)
		}
		return visibleHeight
	}

	switch m.focus {
	case focusLayers:
		layers := m.layers()
		if len(layers) == 0 {
			return
		}
		target := clampIndex(m.layerCursor+dir*step(m.layerVisibleHeight()), len(layers))
		if target == m.layerCursor {
			return
		}
		m.layerCursor = target
		m.resetTreeForLayerChange()
		m.adjustLayerScroll()
	case focusTree:
		files := m.displayTreeFor(focusTree)
		if len(files) == 0 {
			return
		}
		m.treeCursor = clampIndex(m.treeCursor+dir*step(m.treeVisibleHeightFor(focusTree)), len(files))
		m.adjustTreeScrollFor(focusTree)
	case focusTreeAgg:
		files := m.displayTreeFor(focusTreeAgg)
		if len(files) == 0 {
			return
		}
		m.aggCursor = clampIndex(m.aggCursor+dir*step(m.treeVisibleHeightFor(focusTreeAgg)), len(files))
		m.adjustTreeScrollFor(focusTreeAgg)
	}
}

// clampIndex keeps i within [0, n-1] for a non-empty range.
func clampIndex(i, n int) int {
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
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

func (m model) viewLoading() tea.View {
	if m.width > 0 && m.width < 10 {
		return finalizeView(tea.NewView("loading…"), m.viewBg())
	}
	frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]

	var lines []string
	lines = append(lines, "")
	lines = append(lines, "  "+m.styles.accent.Bold(true).Render("◆ layerx"))
	lines = append(lines, "")

	switch m.loadPhase {
	case image.PhasePulling:
		lines = append(lines, fmt.Sprintf("  %s Pulling %s …", frame, m.imageRef))
		if m.pullTotal > 0 {
			detail := fmt.Sprintf("    Layer %d/%d", m.pullLayers, m.pullTotal)
			if m.pullBytesMax > 0 {
				pct := min(int(m.pullBytes*100/m.pullBytesMax), 100)
				bytesText := fmt.Sprintf("  %s / %s",
					image.FormatBytes(m.pullBytes),
					image.FormatBytes(m.pullBytesMax))
				// Budget reserves 2 inner-padding cells (m.width - 6 instead
				// of m.width - 4) so the bytes total never butts up against
				// the right border when boxWidth is clamped to m.width - 2.
				barWidth := 20
				if m.width > 0 {
					budget := m.width - 6 - lipgloss.Width(detail) - len("  []") - lipgloss.Width(bytesText)
					if budget < barWidth {
						barWidth = budget
					}
				}
				if barWidth >= 4 {
					filled := barWidth * pct / 100
					bar := m.styles.accent.Render(strings.Repeat("━", filled)) +
						m.styles.separator.Render(strings.Repeat("─", barWidth-filled))
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
	lines = append(lines, "  "+m.styles.statusDimRaw.Render("Press q or Esc to exit."))
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
	panel := renderPanel(m.theme, body, "Loading", true, boxWidth, boxHeight, false, false)
	content := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
	return finalizeView(tea.NewView(content), m.viewBg())
}

// renderRightPanel paints the file-tree side of the screen. In single-pane
// (aggregated=false) mode it delegates to renderFileTree; in split-pane
// (aggregated=true) mode it renders both StackedTrees and AggregatedTrees
// stacked vertically inside one panel border.
func (m model) renderRightPanel(width, height int) string {
	if !m.aggregated {
		treeFiles := m.displayTreeFor(focusTree)
		return renderFileTree(m.theme, m.styles, treeFiles, m.treeCursor, m.treeOffset,
			width, height, m.focus == focusTree, m.filterActive,
			m.filterQuery, m.useTreeCollapse(), false,
			m.treeCollapsed, m.layerCursor)
	}
	topFiles := m.displayTreeFor(focusTree)
	botFiles := m.displayTreeFor(focusTreeAgg)
	return renderSplitFileTree(splitTreeInput{
		theme:        m.theme,
		styles:       m.styles,
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
	// Bound the message width so a long error (e.g. a daemon-down line with
	// its archive-mode hint) wraps instead of overflowing a narrow terminal.
	wrapWidth := 60
	if m.width > 0 && m.width-4 < wrapWidth {
		wrapWidth = m.width - 4
	}
	if wrapWidth < 1 {
		wrapWidth = 1
	}
	errStyle := lipgloss.NewStyle().Foreground(m.theme.Removed).Bold(true).Width(wrapWidth)
	msg := errStyle.Render("Error: "+m.errMsg) + "\n\n" + m.styles.statusDimRaw.Render("Press q or Esc to exit.")
	content := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
	return finalizeView(tea.NewView(content), m.viewBg())
}

func (m model) viewReady() tea.View {
	if m.width < 50 {
		return finalizeView(tea.NewView(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			"Terminal too narrow\n(need 50+ cols)")), m.viewBg())
	}

	// chromeRows: header(1) + panel borders(2) + commandBar(3) + separator(1) + statusBar(1)
	const chromeRows = 8
	const minPanelRows = 3
	if m.height < chromeRows+minPanelRows {
		return finalizeView(tea.NewView(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			"Terminal too short\n(need 11+ rows)")), m.viewBg())
	}

	leftWidth := m.leftPanelWidth()
	rightWidth := m.width - leftWidth - 1
	// header(1) + panel borders(2) + commandBar(3) + separator(1) + statusBar(1) = 8
	panelHeight := m.height - chromeRows
	header := m.renderHeader()
	// The status bar only consumes treeFiles in its non-viewer branch; when the
	// viewer is open renderStatusBar returns early via renderViewerStatusBar and
	// never reads it. Skip the tree pipeline entirely in that case.
	var treeFiles []*image.FileNode
	if m.viewState == viewNone {
		treeFiles = m.displayTree()
	}
	left := renderLayers(m.theme, m.styles, m.layers(), m.layerCursor, m.layerOffset, leftWidth, panelHeight, m.focus == focusLayers, m.sizeMode, m.finalLiveSize())
	right := m.renderRightPanel(rightWidth, panelHeight)

	panels := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)

	if m.viewState != viewNone {
		viewer := renderFileView(viewerParams{
			content:       m.viewContent,
			lines:         m.viewLines,
			offset:        m.viewOffset,
			hOffset:       m.viewHOffset,
			cursorCol:     m.viewCursorCol,
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
			theme:            m.theme,
			styles:           m.styles,
		})
		panels = viewer
	}

	cmd := ""
	layers := m.layers()
	if m.layerCursor < len(layers) {
		cmd = layers[m.layerCursor].Command
	}
	commandBar := renderCommandBar(m.styles, cmd, m.width)

	sep := m.styles.separator.Render(strings.Repeat("─", m.width))
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
		return finalizeViewNoMouse(tea.NewView(content), m.viewBg())
	}
	return finalizeView(tea.NewView(content), m.viewBg())
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
	glyph := m.styles.accentBg.Render("◆")
	brand := m.styles.accentBg.Bold(true).Render(" layerx")
	sep := m.styles.headerSep.Render(" │ ")
	imageName := m.styles.bgOnly.Render(m.renderedImageRef)
	left := glyph + brand + sep + imageName
	// Append the active platform after the image name when --platform is
	// set. Multi-platform images otherwise give no visual cue which variant
	// is on screen — easy to misread an arm64 layout as amd64.
	if m.platform != "" {
		left += sep + m.styles.headerDimBg.Render(m.platform)
	}

	totalSize := image.FormatBytes(m.analysis.TotalSize)
	layerCount := fmt.Sprintf("%d layers", len(m.analysis.Layers))
	right := m.styles.headerDimBg.Render(layerCount + " · " + totalSize)

	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right)-1, 1)

	return m.styles.bgOnly.Render(" " + left + strings.Repeat(" ", gap) + right)
}

func (m model) renderStatusBar(treeFiles []*image.FileNode) string {
	if m.viewState != viewNone {
		return m.renderViewerStatusBar()
	}
	keyStyle := m.styles.statusKey
	descStyle := m.styles.statusDim
	sepStyle := m.styles.headerSep

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
			{"c", "copy cmd"},
			{"w", "wasted"},
			{"A", "split"},
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
			{"A", "split"},
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
		msgStyle := m.styles.addedBg
		if strings.HasPrefix(m.statusMsg, "Error:") {
			msgStyle = m.styles.removedStatusBg
		}
		right = msgStyle.Render(m.statusMsg) + " "
	} else if m.copyConfirm {
		right = m.styles.addedBg.Render("Copied!") + " "
	} else {
		badges := ""
		if m.efficiency != nil {
			pct := int(m.efficiency.Score * 100)
			effStr := fmt.Sprintf("Eff: %d%%", pct)
			if m.efficiency.WastedBytes > 0 {
				effStr += " · " + image.FormatBytes(m.efficiency.WastedBytes) + " wasted"
			}
			badges += m.styles.accentBg.Render("["+effStr+"]") + " "
		}
		if m.diffOnly {
			badges += m.styles.modifiedBg.Render("[diff]") + " "
		}
		if m.aggregated {
			badges += m.styles.accentBg.Render("[split]") + " "
		}
		switch m.sortMode {
		case sortDesc:
			badges += m.styles.accentBg.Render("[↓size]") + " "
		case sortAsc:
			badges += m.styles.accentBg.Render("[↑size]") + " "
		}

		layerNum := fmt.Sprintf("%d", m.layerCursor+1)
		layerTotal := fmt.Sprintf("%d", len(layers))
		size := ""
		if m.layerCursor < len(layers) {
			size = image.FormatBytes(layers[m.layerCursor].Size)
		}
		rightHighlight := m.styles.selectedStatusBg.Render("Layer " + layerNum)
		sizeLabel := "stored " + size
		if m.focus == focusLayers && m.layerCursor < len(layers) {
			switch m.sizeMode {
			case sizeColDelta:
				sizeLabel = "change " + image.FormatSignedBytes(layers[m.layerCursor].NetDelta)
			case sizeColBoth:
				sizeLabel = "stored " + size + " · change " + image.FormatSignedBytes(layers[m.layerCursor].NetDelta)
			}
		}
		rightDim := m.styles.statusDim.Render("/" + layerTotal + " · " + sizeLabel)
		right = badges + rightHighlight + rightDim + " "
	}

	gap := max(m.width-lipgloss.Width(hintStr)-lipgloss.Width(right), 0)

	return m.styles.bgOnly.Render(hintStr + strings.Repeat(" ", gap) + right)
}

func (m model) renderViewerStatusBar() string {
	keyStyle := m.styles.statusKey
	descStyle := m.styles.statusDim
	sepStyle := m.styles.headerSep

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
		right = m.styles.addedBg.Render("Copied!") + " "
	} else if len(m.viewSearchMatches) > 0 {
		matchStyle := m.styles.searchMatchStyle
		right = matchStyle.Render(fmt.Sprintf("Match %d/%d ", m.viewSearchCursor+1, len(m.viewSearchMatches)))
	} else if m.viewContent != nil && !m.viewContent.Binary && len(m.viewContent.Data) > 0 {
		total := m.viewLineCount()
		line := m.viewOffset + 1
		pct := 0
		if total > 0 {
			pct = line * 100 / total
		}
		right = m.styles.statusDim.Render(fmt.Sprintf("Line %d/%d (%d%%) ", line, total, pct))
	}

	gap := max(m.width-lipgloss.Width(hints)-lipgloss.Width(right), 0)

	return m.styles.bgOnly.Render(hints + strings.Repeat(" ", gap) + right)
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
//
// Uses lstat (does not follow symlinks) so that a dangling symlink at
// filename is treated as "present" and bumped to filename.1 rather than
// silently clobbered.
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
	} else if os.IsNotExist(err) {
		// EvalSymlinks returns ErrNotExist for both "nothing at this path" and
		// "dangling symlink" — distinguish them with Lstat. If a symlink entry
		// exists at name (even a broken one), refuse to replace it: uniquePath
		// should have already bumped the filename, so arriving here with a
		// symlink at the destination means something changed under us.
		if info, lstatErr := os.Lstat(name); lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to overwrite symlink at %s", name)
		}
		// Nothing at this path — write directly.
	} else {
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


// viewLineCount returns the viewer's rendered line count from the cached split
// in m.viewLines, preserving fileViewLineCount's contract: non-empty text whose
// only content is a trailing newline counts as one line. Reads the cache so
// scroll clamping does not re-split the file body on every keystroke.
func (m *model) viewLineCount() int {
	if m.viewContent == nil || m.viewContent.Binary || len(m.viewContent.Data) == 0 {
		return 0
	}
	if len(m.viewLines) == 0 {
		return 1
	}
	return len(m.viewLines)
}

func (m *model) scrollViewDown() {
	maxOffset := max(m.viewLineCount()-m.viewVisibleHeight(), 0)
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
	lines := m.viewLines
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
	if daemonErr, ok := errors.AsType[*image.ErrDaemonNotRunning](err); ok {
		// Both engines are optional: layerx reads a saved-image archive
		// straight from disk, so a daemon-down user is not dead-ended.
		const archiveHint = " Or run layerx on a saved-image archive instead (no engine needed)."
		switch daemonErr.Engine {
		case "docker":
			if daemonErr.Host != "" {
				return fmt.Sprintf("Docker daemon at %s is not reachable. Please check the daemon and try again.", daemonErr.Host) + archiveHint
			}
			return "Docker is not running. Please start Docker and try again." + archiveHint
		case "podman":
			if daemonErr.Host != "" {
				return fmt.Sprintf("Podman connection at %s is not reachable. Please check the connection and try again.", daemonErr.Host) + archiveHint
			}
			return "Podman is not reachable. Please check the connection and try again." + archiveHint
		default:
			if daemonErr.Host != "" {
				return fmt.Sprintf("Container engine at %s is not reachable. Please check the endpoint and try again.", daemonErr.Host) + archiveHint
			}
			return "Container engine is not reachable. Please check the endpoint and try again." + archiveHint
		}
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
		// The disk-space hint is only trustworthy when the cause is actually a
		// full disk. ErrArchiveInfra also covers seek/temp-file/I/O failures
		// (e.g. on a network mount), where telling the user to free disk space
		// misdirects them. Show the hint only on ENOSPC, matching the CLI.
		if errors.Is(infraErr.Cause, syscall.ENOSPC) {
			return fmt.Sprintf("Could not %s: %v. Free up disk space or set TMPDIR to a writable location and try again.", infraErr.Op, infraErr.Cause)
		}
		return fmt.Sprintf("Could not %s: %v.", infraErr.Op, infraErr.Cause)
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

// friendlySaveError renders a file-save write failure for the status bar.
// The write path spools through a temp file, so the raw os error on a full
// disk leaks the internal .tmp path ("write /out/.tmp-123: no space left on
// device"). Gate the disk-space case on ENOSPC — matching the CLI and the
// ErrArchiveInfra path in friendlyError — and name only the user's file, not
// the temp spool. name is the path the user asked to save to.
func friendlySaveError(err error, name string) string {
	base := filepath.Base(name)
	if errors.Is(err, syscall.ENOSPC) {
		return fmt.Sprintf("Could not save %s: not enough disk space. Free space or choose another directory.", base)
	}
	return fmt.Sprintf("Could not save %s: %v", base, err)
}

// Run starts the TUI program with the given configuration.
func Run(cfg Config) error {
	m := NewModel(cfg)
	defer m.fetchCancel()
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
