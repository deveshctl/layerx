package tui

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/image"
	"github.com/deveshctl/layerx/internal/sizebudget"
)

// CompareConfig supplies streams explicitly so the comparison UI never writes
// progress to a process-global stderr. After loading, the caller receives the
// analyses for the comparison report.
type CompareConfig struct {
	Inputs    [2]image.CompareInput
	Options   image.CompareRunOptions
	Theme     string
	Input     io.Reader
	Output    io.Writer
	CheckSize func(before, after *image.Analysis) ([]sizebudget.Result, error)
}

// compareSession owns synchronization outside the value-based Bubble Tea model.
// start is also called during cleanup: if the program exits before Init's
// command executes, the cancelled runner still completes and is joined safely.
type compareSession struct {
	ctx     context.Context
	cancel  context.CancelFunc
	once    sync.Once
	done    chan struct{}
	inputs  [2]image.CompareInput
	opts    image.CompareRunOptions
	results [2]image.CompareImage
	err     error
}

func newCompareSession(ctx context.Context, cfg CompareConfig) *compareSession {
	ctx, cancel := context.WithCancel(ctx)
	opts := cfg.Options
	opts.Progress = &image.CompareProgress{}
	return &compareSession{ctx: ctx, cancel: cancel, done: make(chan struct{}), inputs: cfg.Inputs, opts: opts}
}

func (s *compareSession) start() {
	s.once.Do(func() {
		go func() {
			defer close(s.done)
			s.results, s.err = image.AnalyzePair(s.ctx, s.inputs, s.opts)
		}()
	})
}

func (s *compareSession) close() {
	s.cancel()
	s.start()
	<-s.done
}

type compareLoadedMsg struct{}
type compareTickMsg struct{}

type compareModel struct {
	session        *compareSession
	keys           keyMap
	theme          Theme
	progress       [2]image.CompareSideProgress
	width, height  int
	frame          int
	result         *image.CompareResult
	checks         []sizebudget.Result
	checkSize      func(before, after *image.Analysis) ([]sizebudget.Result, error)
	checkError     string
	help           bool
	cursor, offset int
	fileView       bool
	fileSort       int
	fileMetadata   bool
	fileHelp       bool
	fileIndex      *compareFileIndex
	fileCursor     int
	fileOffset     int
	selectedPath   string
	contentOpen    bool
	content        *compareContent
	contentError   string
	contentCursor  int
	contentOffset  int
	contentRequest int
	contentCancel  context.CancelFunc
}

func (m compareModel) Init() tea.Cmd {
	return tea.Batch(func() tea.Msg {
		m.session.start()
		<-m.session.done
		return compareLoadedMsg{}
	}, compareTick())
}

func compareTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return compareTickMsg{} })
}

func (m compareModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.adjustViewport()
		m.adjustFileViewport()
		m.adjustContentViewport()
	case tea.KeyPressMsg:
		if m.contentOpen {
			if msg.Code == tea.KeyEscape || key.Matches(msg, m.keys.Quit) {
				m.closeContent()
				return m, nil
			}
			m.updateContentKeys(msg)
			return m, nil
		}
		if key.Matches(msg, m.keys.Quit) {
			if m.result == nil {
				m.session.cancel()
			}
			return m, tea.Quit
		}
		if m.result != nil {
			if key.Matches(msg, m.keys.Help) {
				if m.fileView {
					m.fileHelp = !m.fileHelp
				} else {
					m.help = !m.help
					m.adjustViewport()
				}
				return m, nil
			}
			if key.Matches(msg, m.keys.Switch) {
				m.fileView = !m.fileView
				return m, nil
			}
			if msg.Code == '1' {
				m.fileView = false
				return m, nil
			}
			if msg.Code == '2' {
				m.fileView = true
				return m, nil
			}
			if m.fileView {
				if msg.Code == tea.KeyEnter || msg.Code == 'v' {
					return m.openContent()
				}
				m.updateFileKeys(msg)
				return m, nil
			}
			switch {
			case key.Matches(msg, m.keys.Down):
				m.cursor++
			case key.Matches(msg, m.keys.Up):
				m.cursor--
			case key.Matches(msg, m.keys.PageDown):
				m.cursor += m.pageSize()
			case key.Matches(msg, m.keys.PageUp):
				m.cursor -= m.pageSize()
			case key.Matches(msg, m.keys.HalfPageDown):
				m.cursor += max(1, m.pageSize()/2)
			case key.Matches(msg, m.keys.HalfPageUp):
				m.cursor -= max(1, m.pageSize()/2)
			case key.Matches(msg, m.keys.Top):
				m.cursor = 0
			case key.Matches(msg, m.keys.Bottom):
				m.cursor = len(m.result.LayerDiffs) - 1
			}
			m.adjustViewport()
		}
	case compareLoadedMsg:
		if m.session.err != nil {
			return m, tea.Quit
		}
		m.result = image.CompareAnalysis(m.session.results[0].Analysis, m.session.results[1].Analysis)
		if m.checkSize != nil {
			var err error
			m.checks, err = m.checkSize(m.session.results[0].Analysis, m.session.results[1].Analysis)
			if err != nil {
				m.checkError = err.Error()
			}
		}
		m.adjustViewport()
		return m, buildCompareFileIndex(m.session.ctx, m.result.FileDiffs, m.session.results)
	case compareFileIndexMsg:
		if msg.index != nil {
			m.fileIndex = msg.index
			if m.selectedPath != "" {
				for row, index := range m.fileIndex.order[m.fileSort] {
					if m.result.FileDiffs[index].Path == m.selectedPath {
						m.fileCursor = row
						break
					}
				}
			}
			m.adjustFileViewport()
		}
	case compareContentMsg:
		if m.contentOpen && msg.request == m.contentRequest {
			if m.contentCancel != nil {
				m.contentCancel()
				m.contentCancel = nil
			}
			m.content, m.contentError = msg.content, msg.err
			m.adjustContentViewport()
		}
	case compareTickMsg:
		if m.result != nil {
			return m, nil
		}
		m.progress = m.session.opts.Progress.Snapshot()
		m.frame++
		return m, compareTick()
	}
	return m, nil
}

func (m compareModel) View() tea.View {
	if m.result != nil {
		if m.contentOpen {
			return m.contentView()
		}
		if m.fileView {
			return m.fileBrowserView()
		}
		return m.browserView()
	}
	lines := []string{styleWithFg(m.theme.FocusedBorder).Bold(true).Render("Compare images"), ""}
	for side, input := range m.session.inputs {
		status := compareProgressText(m.progress[side])
		spinner := string("|/-\\"[m.frame%4])
		if m.progress[side].Ready {
			spinner = "✓"
		}
		lines = append(lines, fmt.Sprintf("Image %c %s %s: %s", 'A'+side, spinner, strconv.Quote(input.Ref), status))
		if warning := m.progress[side].Warning; warning != "" {
			lines = append(lines, "  Warning: "+strconv.Quote(warning))
		}
	}
	lines = append(lines, "", "q / Ctrl+C cancel")
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(0, m.width), "…")
	}
	lines = lines[:min(len(lines), max(0, m.height))]
	return finalizeViewNoMouse(tea.NewView(strings.Join(lines, "\n")), m.theme.MainBg)
}

func (m compareModel) pageSize() int {
	extra := len(m.checks)
	if m.checkError != "" {
		extra++
	}
	if m.help {
		extra++
	}
	return max(1, m.height-7-extra)
}

func (m *compareModel) adjustViewport() {
	if m.result == nil {
		return
	}
	m.cursor = min(max(0, m.cursor), max(0, len(m.result.LayerDiffs)-1))
	m.offset = min(m.offset, m.cursor)
	if m.cursor >= m.offset+m.pageSize() {
		m.offset = m.cursor - m.pageSize() + 1
	}
	m.offset = max(0, m.offset)
}

func (m compareModel) browserView() tea.View {
	r := m.result
	verdict := "PASS"
	if r.IsRegression() {
		verdict = "REGRESSION"
	}
	for _, check := range m.checks {
		if !check.Passed {
			verdict = "REGRESSION"
		}
	}
	if m.checkError != "" {
		verdict = "CHECK ERROR"
	}
	lines := []string{
		"Compare images",
		"Verdict: " + verdict,
		fmt.Sprintf("Stored %s → %s (%s)   Layers %d → %d (%+d)", image.FormatBytes(r.Before.TotalSize), image.FormatBytes(r.After.TotalSize), signedBytes(r.After.TotalSize-r.Before.TotalSize), r.Before.LayerCount, r.After.LayerCount, r.After.LayerCount-r.Before.LayerCount),
		fmt.Sprintf("Efficiency %.1f%% → %.1f%% (%+.1f points)   Waste %s → %s (%s)", r.BeforeEfficiency.Score*100, r.AfterEfficiency.Score*100, r.AfterEfficiency.ScoreDelta*100, image.FormatBytes(r.BeforeEfficiency.WastedBytes), image.FormatBytes(r.AfterEfficiency.WastedBytes), signedBytes(r.AfterEfficiency.WastedBytesDelta)),
		"Pairs follow image B order; unmatched A layers follow. Crossed layer numbers are possible.",
	}
	for _, check := range m.checks {
		status := "PASS"
		if !check.Passed {
			status = "FAIL"
		}
		lines = append(lines, fmt.Sprintf("%s %s: %s / %s (%s)", status, check.Name, sizebudget.Format(check.ActualBytes), sizebudget.Format(check.LimitBytes), check.Limit))
	}
	if m.checkError != "" {
		lines = append(lines, "Size check: "+m.checkError)
	}
	if m.help {
		lines = append(lines, "Tab/1/2 views; ↑↓ or j/k move; PgUp/PgDn page; Ctrl+u/d half page; g/G ends; ? help; q exits")
	}
	wide := m.width >= 72
	if wide {
		column := max(12, (m.width-20)/2)
		lines = append(lines, fmt.Sprintf("  %-*s %-*s Match", column, "Image A", column, "Image B"))
	} else {
		lines = append(lines, "  A ↔ B  | match | size delta")
	}
	if len(r.LayerDiffs) == 0 {
		lines = append(lines, "No layers")
	}
	for i := m.offset; i < min(len(r.LayerDiffs), m.offset+m.pageSize()); i++ {
		d := r.LayerDiffs[i]
		marker := " "
		if i == m.cursor {
			marker = ">"
		}
		left, right := compareLayerText(d.BeforeIndex, d.BeforeCommand), compareLayerText(d.AfterIndex, d.AfterCommand)
		if wide {
			column := max(12, (m.width-20)/2)
			lines = append(lines, fmt.Sprintf("%s %-*s %-*s %s", marker, column, ansi.Truncate(left, column, "…"), column, ansi.Truncate(right, column, "…"), compareMatch(d.Match)))
		} else {
			lines = append(lines, fmt.Sprintf("%s %s ↔ %s | %s | %+d B", marker, compareLayerNumber(d.BeforeIndex), compareLayerNumber(d.AfterIndex), compareMatch(d.Match), d.SizeDelta))
		}
	}
	lines = append(lines, fmt.Sprintf("%d/%d  Tab/1/2 view  ↑↓ move  ? help  q exit", min(m.cursor+1, len(r.LayerDiffs)), len(r.LayerDiffs)))
	for i := range lines {
		lines[i] = ansi.Truncate(ansi.Strip(lines[i]), max(0, m.width), "…")
	}
	lines = fitCompareLines(lines, m.height)
	return finalizeViewNoMouse(tea.NewView(strings.Join(lines, "\n")), m.theme.MainBg)
}

func compareLayerNumber(index int) string {
	if index < 0 {
		return "— absent"
	}
	return fmt.Sprintf("#%d", index+1)
}

func compareLayerText(index int, command string) string {
	if index < 0 {
		return "— absent"
	}
	return compareLayerNumber(index) + " " + strconv.Quote(command)
}

func compareMatch(match string) string {
	switch match {
	case "shared":
		return "shared DiffID"
	case "command":
		return "command match?"
	default:
		return match
	}
}

func signedBytes(n int64) string {
	if n >= 0 {
		return "+" + image.FormatBytes(n)
	}
	return "-" + image.FormatBytes(-n)
}

func fitCompareLines(lines []string, height int) []string {
	if height <= 0 {
		return nil
	}
	if len(lines) <= height {
		return lines
	}
	if height == 1 {
		return lines[len(lines)-1:]
	}
	return append(lines[:height-1:height-1], lines[len(lines)-1])
}

func compareProgressText(p image.CompareSideProgress) string {
	if p.Ready {
		return "ready"
	}
	switch p.Event.Phase {
	case image.PhasePulling:
		if p.Event.BytesTotal > 0 {
			percent := min(100.0, max(0.0, 100*float64(p.Event.BytesCurr)/float64(p.Event.BytesTotal)))
			return fmt.Sprintf("pulling %.0f%% (%s / %s)", percent, image.FormatBytes(p.Event.BytesCurr), image.FormatBytes(p.Event.BytesTotal))
		}
		return "pulling (total unknown)"
	case image.PhaseExporting:
		return "exporting"
	case image.PhaseParsing:
		return "parsing / indexing"
	case image.PhaseCacheLoad:
		return "indexing cached layers"
	default:
		return "resolving"
	}
}

// RunCompareAnalysis cancels and joins analysis even if terminal setup fails or
// the program exits before its first command runs. The parent context controls
// the program; the child work context can be cancelled independently by keys.
func RunCompareAnalysis(ctx context.Context, cfg CompareConfig) ([2]image.CompareImage, error) {
	session := newCompareSession(ctx, cfg)
	defer session.close()
	m := compareModel{session: session, keys: defaultKeys(), theme: themeFor(cfg.Theme), width: 80, height: 24, checkSize: cfg.CheckSize}
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(cfg.Input), tea.WithOutput(cfg.Output))
	if _, err := p.Run(); err != nil {
		return [2]image.CompareImage{}, err
	}
	if err := session.ctx.Err(); err != nil {
		return [2]image.CompareImage{}, err
	}
	// The normal completion message is published only after done is closed.
	// Joining also covers external Quit messages without that completion message.
	select {
	case <-session.done:
		return session.results, session.err
	default:
		return [2]image.CompareImage{}, context.Canceled
	}
}
