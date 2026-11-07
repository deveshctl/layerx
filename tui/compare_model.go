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
	Inputs        [2]image.CompareInput
	Options       image.CompareRunOptions
	Theme         string
	TransparentBg bool
	Input         io.Reader
	Output        io.Writer
	CheckSize     func(before, after *image.Analysis) ([]sizebudget.Result, error)
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
	inFrame                          bool
	overview                         bool
	guideOpen                        bool
	guideOffset                      int
	directoryCursor, directoryOffset int
	session                          *compareSession
	keys                             keyMap
	theme                            Theme
	progress                         [2]image.CompareSideProgress
	width, height                    int
	frame                            int
	result                           *image.CompareResult
	checks                           []sizebudget.Result
	checkSize                        func(before, after *image.Analysis) ([]sizebudget.Result, error)
	checkError                       string
	help                             bool
	cursor, offset                   int
	fileView                         bool
	fileSort                         int
	fileMetadata                     bool
	fileHelp                         bool
	fileIndex                        *compareFileIndex
	fileFilter                       string
	fileDirectory                    string
	fileFiltering                    bool
	fileFiltered                     []int
	contentHOffset                   int
	fileCursor                       int
	fileOffset                       int
	selectedPath                     string
	contentOpen                      bool
	content                          *compareContent
	contentError                     string
	contentCursor                    int
	contentOffset                    int
	contentRequest                   int
	contentCancel                    context.CancelFunc
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
	case tea.PasteMsg:
		if m.fileView && m.fileFiltering && !m.contentOpen {
			m.updateFileFilter(tea.KeyPressMsg{Text: msg.Content})
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.adjustViewport()
		m.adjustOverview()
		m.adjustFileViewport()
		m.adjustContentViewport()
		m.adjustCompareGuide()
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			if m.contentCancel != nil {
				m.contentCancel()
			}
			if m.session != nil {
				m.session.cancel()
			}
			return m, tea.Quit
		}
		if m.fileView && m.fileFiltering && !m.contentOpen {
			m.updateFileFilter(msg)
			return m, nil
		}
		if m.guideOpen {
			if msg.Code == tea.KeyEscape || key.Matches(msg, m.keys.Help, m.keys.Quit) {
				m.guideOpen = false
			} else {
				m.updateCompareGuide(msg)
			}
			return m, nil
		}
		if key.Matches(msg, m.keys.Help) && m.result != nil {
			m.guideOpen = true
			m.guideOffset = 0
			return m, nil
		}
		if m.contentOpen {
			if msg.Code == 'l' && m.content != nil && m.content.note != "" {
				if row := m.contentOriginRow(); row >= 0 {
					m.closeContent()
					m.overview, m.fileView, m.cursor = false, false, row
					m.adjustViewport()
					return m, nil
				}
			}
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

			if key.Matches(msg, m.keys.Switch) {
				if m.overview {
					m.overview = false
					m.fileView = false
				} else if !m.fileView {
					m.fileView = true
				} else {
					m.overview = true
					m.fileView = false
				}
				return m, nil
			}
			if msg.Code == '3' {
				m.overview = true
				m.fileView = false
				return m, nil
			}
			if msg.Code == '1' {
				m.overview = false
				m.fileView = false
				return m, nil
			}
			if msg.Code == '2' {
				m.fileDirectory = ""
				m.fileFilter = ""
				m.rebuildFileFilter()
				m.adjustFileViewport()
				m.overview = false
				m.fileView = true
				return m, nil
			}
			if m.overview {
				switch {
				case msg.Code == tea.KeyEnter:
					if m.fileIndex != nil && len(m.fileIndex.directories) > 0 {
						m.fileDirectory = m.fileIndex.directories[m.directoryCursor].path
						m.fileFilter = ""
						m.rebuildFileFilter()
						m.fileCursor = 0
						m.fileOffset = 0
						m.overview = false
						m.fileView = true
					}
				case key.Matches(msg, m.keys.Down):
					m.directoryCursor++
				case key.Matches(msg, m.keys.Up):
					m.directoryCursor--
				case key.Matches(msg, m.keys.PageDown):
					m.directoryCursor += m.overviewPageSize()
				case key.Matches(msg, m.keys.PageUp):
					m.directoryCursor -= m.overviewPageSize()
				case key.Matches(msg, m.keys.HalfPageDown):
					m.directoryCursor += max(1, m.overviewPageSize()/2)
				case key.Matches(msg, m.keys.HalfPageUp):
					m.directoryCursor -= max(1, m.overviewPageSize()/2)
				case key.Matches(msg, m.keys.Top):
					m.directoryCursor = 0
				case key.Matches(msg, m.keys.Bottom):
					if m.fileIndex != nil {
						m.directoryCursor = len(m.fileIndex.directories) - 1
					}
				}
				m.adjustOverview()
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
			m.adjustOverview()
			m.rebuildFileFilter()
			if m.selectedPath != "" {
				for row := 0; row < m.fileCount(); row++ {
					if m.fileDiffAt(row).Path == m.selectedPath {
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

// compareSize reserves the same chrome space for rendering and navigation.
func (m compareModel) compareSize() (int, int) {
	if !m.inFrame && m.width >= 40 && m.height >= 16 {
		return m.width - 4, m.height - 7
	}
	return m.width, m.height
}

func (m compareModel) View() tea.View {
	width, height := m.compareSize()
	if width == m.width {
		v := m.compareBodyView()
		if v.Content != "" {
			lines := strings.Split(v.Content, "\n")
			lines[0] = ansi.Truncate(renderBrand(m.theme)+" │ "+lines[0], max(0, m.width), "…")
			if len(lines) > 1 {
				lines[len(lines)-1] = m.compareFooter(m.width)
			}
			v.Content = strings.Join(lines, "\n")
		}
		return v
	}
	outer := m
	m.width, m.height, m.inFrame = width, height, true
	body := strings.Split(m.compareBodyView().Content, "\n")
	// Body renderers reserve their last row for actions; the shared footer
	// renders that row outside the panel, like the normal inspector status bar.
	if len(body) > 0 {
		body = body[:len(body)-1]
	}
	refs := [2]string{"unknown", "unknown"}
	if m.session != nil {
		for side := range refs {
			refs[side] = m.session.inputs[side].Ref
		}
	} else if m.result != nil {
		refs = [2]string{m.result.Before.ImageRef, m.result.After.ImageRef}
	}
	tabs := "3 Overview   1 Layers   2 Files"
	selected := "1 Layers"
	if m.overview {
		selected = "3 Overview"
	}
	if m.fileView {
		selected = "2 Files"
	}
	tabs = strings.Replace(tabs, selected, styleWithFg(m.theme.Accent).Background(m.theme.StatusBg).Bold(true).Render("["+selected+"]"), 1)
	if m.contentOpen {
		tabs = "File details · Esc back"
	}
	if m.guideOpen {
		tabs = "Comparison guide · Esc back"
	}
	if m.result != nil {
		tabs += "   │   " + m.compareStatus(m.comparisonVerdict())
	}
	header := []string{
		styleWithFg(m.theme.FileName).Render("A · Before  " + compareImageRef(refs[0], width-12)),
		styleWithFg(m.theme.FileName).Render("B · After   " + compareImageRef(refs[1], width-12)),
		tabs + "   │   ? guide",
	}
	panelContent := append(header, strings.Repeat("─", width))
	panelContent = append(panelContent, body...)
	panel := renderPanel(m.theme, strings.Join(panelContent, "\n"), "Image comparison", true, width, outer.height-4, false, false)
	lines := strings.Split(panel, "\n")
	lines[len(header)+1] = styleWithFg(m.theme.FocusedBorder).Render("├" + strings.Repeat("─", width) + "┤")
	for i := range lines {
		lines[i] = " " + lines[i] + " "
	}
	brand := renderBrand(m.theme) + styleWithFg(m.theme.HeaderSep).Background(m.theme.StatusBg).Render(" │ ") + styleWithFg(m.theme.HeaderDim).Background(m.theme.StatusBg).Render("Image comparison")
	brand += styleWithFg(m.theme.HeaderDim).Background(m.theme.StatusBg).Render(strings.Repeat(" ", max(0, outer.width-2-ansi.StringWidth(brand))))
	lines = append([]string{" " + brand + " "}, lines...)
	lines = append(lines, " "+outer.compareFooter(outer.width-2)+" ")
	return m.finalCompareView(strings.Join(lines, "\n"))
}

func (m compareModel) finalCompareView(content string) tea.View {
	return finalizeViewNoMouse(tea.NewView(styleWithFg(m.theme.FileName).Render(content)), m.theme.MainBg)
}

func compareImageRef(ref string, width int) string {
	quoted := strconv.Quote(ref)
	cells := ansi.StringWidth(quoted)
	if cells > width {
		return "…" + ansi.Cut(quoted, cells-max(0, width-1), cells)
	}
	return quoted
}

func (m compareModel) compareBodyView() tea.View {
	if m.result != nil {
		if m.guideOpen {
			return m.compareGuideView()
		}
		if m.overview {
			return m.overviewView()
		}
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
		line := fmt.Sprintf("Image %c %s %s", 'A'+side, spinner, status)
		if !m.inFrame {
			line += ": " + strconv.Quote(input.Ref)
		}
		lines = append(lines, line)
		if warning := m.progress[side].Warning; warning != "" {
			lines = append(lines, "  Warning: "+strconv.Quote(warning))
		}
	}
	lines = append(lines, "", "q / Ctrl+C cancel")
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(0, m.width), "…")
	}
	lines = lines[:min(len(lines), max(0, m.height))]
	return m.finalCompareView(strings.Join(lines, "\n"))
}

func (m compareModel) pageSize() int {
	extra := len(m.checks)
	if m.checkError != "" {
		extra++
	}
	if m.help {
		extra++
	}
	_, height := m.compareSize()
	return max(1, height-8-extra)
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

func (m compareModel) comparisonVerdict() string {
	verdict := "PASS"
	if m.result != nil && m.result.IsRegression() {
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
	return verdict
}

func (m compareModel) browserView() tea.View {
	r := m.result
	verdict := m.comparisonVerdict()
	lines := []string{
		"Compare images",
		"Verdict: " + m.compareStatus(verdict),
		fmt.Sprintf("Stored %s → %s (%s)   Layers %d → %d (%+d)", sizebudget.Format(r.Before.TotalSize), sizebudget.Format(r.After.TotalSize), m.compareDelta(r.After.TotalSize-r.Before.TotalSize), r.Before.LayerCount, r.After.LayerCount, r.After.LayerCount-r.Before.LayerCount),
		fmt.Sprintf("Efficiency %.1f%% → %.1f%% (%+.1f points)   Waste %s → %s (%s)", r.BeforeEfficiency.Score*100, r.AfterEfficiency.Score*100, r.AfterEfficiency.ScoreDelta*100, sizebudget.Format(r.BeforeEfficiency.WastedBytes), sizebudget.Format(r.AfterEfficiency.WastedBytes), m.compareDelta(r.AfterEfficiency.WastedBytesDelta)),
		"Press 2 to investigate files; 3 returns to the overview; ? explains layer matching.",
	}
	for _, check := range m.checks {
		status := "PASS"
		if !check.Passed {
			status = "FAIL"
		}
		lines = append(lines, fmt.Sprintf("%s %s: %s / %s (%s)", status, check.Name, sizebudget.Format(check.ActualBytes), sizebudget.Format(check.LimitBytes), check.Limit))
	}
	if m.checkError != "" {
		lines = append(lines, "Size check: "+strconv.Quote(m.checkError))
	}
	if m.help {
		lines = append(lines, "Ctrl+u/d half page; g/G ends; shared DiffID = identical; command match? = heuristic; pairs follow B order")
	}
	wide := m.width >= 72
	if wide {
		column := max(12, (m.width-36)/2)
		lines = append(lines, fmt.Sprintf("  %-*s %-*s Match", column, "Image A", column, "Image B")+"        Δ stored")
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
			column := max(12, (m.width-36)/2)
			lines = append(lines, fmt.Sprintf("%s %s %s %s", marker, compareColumn(left, column), compareColumn(right, column), compareColumn(compareMatch(d.Match), 14))+" "+m.compareDelta(d.SizeDelta))
		} else {
			lines = append(lines, fmt.Sprintf("%s %s ↔ %s | %s | %+d B", marker, compareLayerNumber(d.BeforeIndex), compareLayerNumber(d.AfterIndex), compareMatch(d.Match), d.SizeDelta))
		}
		if i == m.cursor {
			lines[len(lines)-1] = styleWithFg(m.theme.Selected).Background(m.theme.SelectedBg).Render(lines[len(lines)-1])
		}
	}
	if len(r.LayerDiffs) > 0 {
		d := r.LayerDiffs[m.cursor]
		lines = append(lines, fmt.Sprintf("Selected layer · filesystem change %s → %s · %s", signedBytes(d.BeforeNetDelta), signedBytes(d.AfterNetDelta), compareMatch(d.Match)))
	}
	lines = append(lines, fmt.Sprintf("%d/%d  Tab/1/2 view  ↑↓ move  ? help  q exit", min(m.cursor+1, len(r.LayerDiffs)), len(r.LayerDiffs)))
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(0, m.width), "…")
	}
	lines = fitCompareLines(lines, m.height)
	return m.finalCompareView(strings.Join(lines, "\n"))
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
		return "+" + sizebudget.Format(n)
	}
	return "-" + sizebudget.Format(-n)
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
			return fmt.Sprintf("pulling %.0f%% (%s / %s)", percent, sizebudget.Format(p.Event.BytesCurr), sizebudget.Format(p.Event.BytesTotal))
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
	m := compareModel{session: session, keys: defaultKeys(), theme: themeFor(cfg.Theme), width: 80, height: 24, overview: true, fileSort: 1, checkSize: cfg.CheckSize}
	if cfg.TransparentBg {
		m.theme.MainBg = nil
	}
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

func (m compareModel) compareStatus(status string) string {
	color := m.theme.Added
	if status != "PASS" {
		color = m.theme.Removed
	}
	return styleWithFg(color).Bold(true).Render(status)
}
func (m compareModel) compareDelta(delta int64) string {
	color := m.theme.Unchanged
	if delta > 0 {
		color = m.theme.Removed
	} else if delta < 0 {
		color = m.theme.Added
	}
	return styleWithFg(color).Render(signedBytes(delta))
}

// Pad by terminal cells, including wide Unicode and styled content.
func compareColumn(text string, width int) string {
	text = ansi.Truncate(text, max(0, width), "…")
	return text + strings.Repeat(" ", max(0, width-ansi.StringWidth(text)))
}

func (m compareModel) compareFooter(width int) string {
	hints := []statusHint{{"q", "exit"}, {"?", "guide"}}
	right := ""
	switch {
	case m.result == nil:
		hints = []statusHint{{"q", "cancel"}, {"Ctrl+C", "cancel"}}
		right = "Loading images"
	case m.guideOpen:
		hints = []statusHint{{"Esc", "back"}, {"?", "close guide"}, {"↑↓", "scroll"}, {"PgUp/PgDn", "page"}}
		_, height := m.compareSize()
		right = fmt.Sprintf("%d/%d guide", min(m.guideOffset+max(1, height-1), len(m.compareGuideLines())), len(m.compareGuideLines()))
	case m.contentOpen:
		hints = []statusHint{{"Esc", "files"}, {"?", "guide"}}
		if m.content == nil {
			right = "Loading file"
		} else if m.content.note != "" {
			right = "Metadata"
			if m.contentOriginRow() >= 0 {
				hints = append(hints, statusHint{"l", "origin layer"})
			}
		} else {
			hints = append(hints, statusHint{"↑↓", "move"}, statusHint{"h/l", "scroll"}, statusHint{"PgUp/PgDn", "page"})
			right = fmt.Sprintf("%d/%d · col %d", min(m.contentCursor+1, len(m.content.rows)), len(m.content.rows), m.contentHOffset+1)
		}
	case m.overview:
		hints = append(hints, statusHint{"Enter", "investigate"}, statusHint{"↑↓", "area"}, statusHint{"Tab", "views"})
		count := 0
		if m.fileIndex != nil {
			count = len(m.fileIndex.directories)
		}
		right = fmt.Sprintf("%d/%d areas", min(m.directoryCursor+1, count), count)
	case m.fileView && m.fileFiltering:
		hints = []statusHint{{"Enter", "apply"}, {"Esc", "clear"}, {"Ctrl+C", "exit"}}
		right = "Editing path filter"
	case m.fileView:
		hints = append(hints, statusHint{"Enter", "details"}, statusHint{"/", "filter"}, statusHint{"s", "rank"}, statusHint{"m", "metadata"}, statusHint{"Tab", "views"})
		if m.fileDirectory != "" || m.fileFilter != "" {
			hints = append(hints, statusHint{"Esc", "all files"})
		}
		right = fmt.Sprintf("%d/%d files", min(m.fileCursor+1, m.fileCount()), m.fileCount())
	default:
		hints = append(hints, statusHint{"↑↓", "move"}, statusHint{"Tab", "views"}, statusHint{"g/G", "ends"})
		right = fmt.Sprintf("%d/%d pairs", min(m.cursor+1, len(m.result.LayerDiffs)), len(m.result.LayerDiffs))
	}
	if width < 40 {
		right = ""
		switch {
		case m.fileFiltering && !m.contentOpen:
			hints = []statusHint{{"Esc", "clear"}, {"Ctrl+C", "exit"}}
		case m.contentOpen || m.guideOpen:
			hints = []statusHint{{"Esc", "back"}, {"Ctrl+C", "exit"}}
		case m.result == nil:
			hints = []statusHint{{"q", "cancel"}, {"Ctrl+C", "cancel"}}
		default:
			hints = []statusHint{{"q", "exit"}, {"?", "guide"}}
		}
	}
	// Prefer fewer fully labeled actions over a dense row of unexplained keys.
	// This also keeps separators visible in compact comparison footers.
	budget := width - min(ansi.StringWidth(right), max(width*2/3, 0)) - 1
	for len(hints) > 2 {
		cells := 1 + (len(hints)-1)*3
		for _, h := range hints {
			cells += ansi.StringWidth(h.key) + 1 + ansi.StringWidth(h.desc)
		}
		if cells <= budget {
			break
		}
		drop := len(hints) - 1
		for drop >= 0 && (hints[drop].key == "q" || hints[drop].key == "?" || hints[drop].key == "Esc" || hints[drop].key == "Ctrl+C") {
			drop--
		}
		if drop < 0 {
			break
		}
		hints = append(hints[:drop:drop], hints[drop+1:]...)
	}
	return renderStatusLine(newThemeStyles(m.theme), hints, right, width)
}
