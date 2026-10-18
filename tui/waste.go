package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/deveshctl/layerx/image"
)

const (
	wasteDefaultLimit = 20
	wasteMaxRows      = 500
)

type wasteRow struct {
	Path          string
	Wasted        int64
	LayerCount    int
	IntroLayer    int
	CleanupReason string
}

type wasteDetailState struct {
	row         wasteRow
	explanation image.WasteExplanation
	cursor      int
}

func (m *model) openWaste() {
	m.wasteRows = nil
	m.wasteCursor = 0
	m.wasteOffset = 0
	m.wasteExpanded = false
	m.wasteShowCleanup = false
	m.wasteDetail = nil
	m.cleanupRows = nil
	for _, candidate := range image.CleanupCandidates(m.analysis) {
		m.cleanupRows = append(m.cleanupRows, wasteRow{
			Path: candidate.Path, Wasted: candidate.Bytes,
			IntroLayer: len(m.layers()) - 1, CleanupReason: candidate.Reason,
		})
	}

	if m.efficiency != nil && len(m.efficiency.WastedFiles) > 0 {
		capRows := min(len(m.efficiency.WastedFiles), wasteMaxRows)
		m.wasteRows = make([]wasteRow, 0, capRows)
		for i, wf := range m.efficiency.WastedFiles {
			if i >= wasteMaxRows {
				break
			}
			intro := wf.FirstWastedLayer
			m.wasteRows = append(m.wasteRows, wasteRow{
				Path:       wf.Path,
				Wasted:     wf.TotalWasted,
				LayerCount: wf.LayerCount,
				IntroLayer: intro,
			})
		}
	}
	m.showWaste = true
}

func (m *model) closeWaste() {
	m.showWaste = false
	m.wasteRows = nil
	m.wasteCursor = 0
	m.wasteOffset = 0
	m.wasteExpanded = false
	m.cleanupRows = nil
	m.wasteShowCleanup = false
	m.wasteDetail = nil
}

func (m model) allWasteRows() []wasteRow {
	if m.wasteShowCleanup {
		return m.cleanupRows
	}
	return m.wasteRows
}

func (m model) visibleWasteRows() []wasteRow {
	rows := m.allWasteRows()
	if !m.wasteExpanded && len(rows) > wasteDefaultLimit {
		return rows[:wasteDefaultLimit]
	}
	return rows
}

// Leave room for the summary, persistent key hints, panel borders and margins.
func (m model) wasteVisibleHeight() int {
	h := m.height - 10
	if h < 1 {
		return 1
	}
	return h
}

func (m *model) adjustWasteScroll(visibleHeight int) {
	if visibleHeight <= 0 {
		return
	}
	if m.wasteCursor < m.wasteOffset {
		m.wasteOffset = m.wasteCursor
	}
	if m.wasteCursor >= m.wasteOffset+visibleHeight {
		m.wasteOffset = m.wasteCursor - visibleHeight + 1
	}
}

func (m model) handleWasteOverlay(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Quit) {
		m.quitting = true
		m.cancelInflight()
		return m, tea.Quit
	}
	if m.wasteDetail != nil {
		return m.handleWasteDetail(msg)
	}
	rows := m.visibleWasteRows()
	allRows := m.allWasteRows()
	visibleHeight := m.wasteVisibleHeight()
	switch {
	case key.Matches(msg, m.keys.Down):
		if m.wasteCursor < len(rows)-1 {
			m.wasteCursor++
			m.adjustWasteScroll(visibleHeight)
		} else if !m.wasteExpanded && len(allRows) > wasteDefaultLimit {
			m.wasteExpanded = true
			m.wasteCursor++
			m.adjustWasteScroll(visibleHeight)
		}
	case key.Matches(msg, m.keys.Up):
		if m.wasteCursor > 0 {
			m.wasteCursor--
			m.adjustWasteScroll(visibleHeight)
		}
	case key.Matches(msg, m.keys.Top):
		m.wasteCursor = 0
		m.wasteOffset = 0
	case key.Matches(msg, m.keys.Bottom):
		if len(allRows) > 0 {
			if !m.wasteExpanded && len(allRows) > wasteDefaultLimit {
				m.wasteExpanded = true
			}
			m.wasteCursor = len(allRows) - 1
			m.adjustWasteScroll(visibleHeight)
		}
	case msg.Code == tea.KeyEnter:
		if len(rows) == 0 {
			return m, nil
		}
		return m.wasteJump(rows[m.wasteCursor])
	case msg.Text == "i":
		if len(rows) > 0 {
			row := rows[m.wasteCursor]
			m.wasteDetail = &wasteDetailState{row: row}
			if row.CleanupReason == "" {
				m.wasteDetail.explanation = image.ExplainWaste(m.analysis, row.Path)
			}
		}
	case key.Matches(msg, m.keys.Switch):
		m.wasteShowCleanup = !m.wasteShowCleanup
		m.wasteCursor, m.wasteOffset = 0, 0
		m.wasteExpanded = false
	case msg.Text == "a" || msg.Text == "A":
		m.wasteExpanded = !m.wasteExpanded
		m.wasteCursor = 0
		m.wasteOffset = 0
	case key.Matches(msg, m.keys.CopyPath):
		if len(rows) == 0 {
			return m, nil
		}
		m.copyConfirm = true
		return m, tea.Batch(
			tea.SetClipboard(rows[m.wasteCursor].Path),
			tea.Tick(2*time.Second, func(time.Time) tea.Msg { return clearCopyMsg{} }),
		)
	}
	return m, nil
}

func (m model) wasteJump(row wasteRow) (tea.Model, tea.Cmd) {
	layers := m.layers()
	jumped := false
	if row.IntroLayer >= 0 && row.IntroLayer < len(layers) {
		m.filterActive = false
		m.filterQuery = ""
		m.layerCursor = row.IntroLayer
		m.focus = focusTree
		jumped = true
	}
	mp := &m
	mp.resetTreeForLayerChange()

	files := mp.displayTree()
	found := indexOfPath(files, row.Path)
	if found < 0 && mp.diffOnly {
		mp.diffOnly = false
		files = mp.displayTree()
		found = indexOfPath(files, row.Path)
	}

	var status string
	switch {
	case !jumped:
		mp.treeCursor = 0
		status = fmt.Sprintf("Intro layer unknown for %s", row.Path)
	case found < 0:
		mp.treeCursor = 0
		status = "File not visible in current view"
	default:
		mp.treeCursor = found
		status = fmt.Sprintf("Jumped → L%d %s", row.IntroLayer+1, row.Path)
	}
	mp.adjustTreeScroll()
	mp.setStatus(status)
	mp.closeWaste()
	return *mp, mp.scheduleStatusClear(2 * time.Second)
}

func indexOfPath(files []*image.FileNode, path string) int {
	for i, f := range files {
		if f.Path == path {
			return i
		}
	}
	return -1
}

func (m model) renderWasteOverlay() string {
	if m.wasteDetail != nil {
		return m.renderWasteDetail()
	}
	innerWidth := max(min(m.width-6, 68), 0)
	rows := m.visibleWasteRows()
	visibleCount := len(m.allWasteRows())
	originalCount := visibleCount
	if !m.wasteShowCleanup && m.efficiency != nil {
		originalCount = len(m.efficiency.WastedFiles)
	}
	position := 0
	if visibleCount > 0 {
		position = m.wasteCursor + 1
	}
	title := fmt.Sprintf("Wasted Files %d/%d", position, visibleCount)
	var total int64
	for _, row := range m.allWasteRows() {
		total += row.Wasted
	}
	if !m.wasteShowCleanup && m.efficiency != nil {
		total = m.efficiency.WastedBytes
	}
	header := fmt.Sprintf("%s layer waste across %d files", image.FormatBytes(total), originalCount)
	if m.wasteShowCleanup {
		title = fmt.Sprintf("Cleanup candidates %d/%d", position, visibleCount)
		header = image.FormatBytes(total) + " live cache bytes; review before removing"
	}
	if originalCount > visibleCount {
		title += fmt.Sprintf(" (capped, %d total)", originalCount)
	}
	dim := lipgloss.NewStyle().Foreground(m.theme.StatusDim)
	lines := []string{m.styles.accent.Render(header)}
	if len(rows) == 0 {
		empty := "No layer waste detected"
		if m.wasteShowCleanup {
			empty = "No recognized cache candidates"
		}
		lines = append(lines, empty, dim.Render("Layer efficiency is not a size assessment."))
	} else {
		start := min(m.wasteOffset, len(rows))
		end := min(start+m.wasteVisibleHeight(), len(rows))
		for i := start; i < end; i++ {
			lines = append(lines, formatWasteRow(m.theme, rows[i], i == m.wasteCursor, innerWidth))
		}
	}
	footer := "Enter jump · i explain · y copy · a expand"
	if m.wasteExpanded {
		footer = "Enter jump · i explain · y copy · a collapse"
	}
	if len(rows) == 0 {
		footer = ""
	}
	if m.copyConfirm {
		footer = m.styles.added.Render("Copied!")
	}
	lines = append(lines, footer,
		dim.Render(fmt.Sprintf("Tab waste/cache (%d) · Esc close", len(m.cleanupRows))))
	panel := renderPanel(m.theme, strings.Join(lines, "\n"), title, true,
		innerWidth, min(len(lines), max(m.height-4, 0)), m.wasteOffset > 0,
		m.wasteOffset+m.wasteVisibleHeight() < len(rows))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

func formatWasteRow(t Theme, r wasteRow, selected bool, innerWidth int) string {
	const (
		gutterW   = 2
		wastedW   = 9
		layerW    = 5
		countW    = 5
		gap       = 2
		minPath   = 12
		narrowMin = 60
	)

	wastedStr := image.FormatBytes(r.Wasted)
	var layerStr string
	if r.IntroLayer < 0 {
		layerStr = "L?"
	} else {
		layerStr = fmt.Sprintf("L%d", r.IntroLayer+1)
	}
	countStr := fmt.Sprintf("x%d", r.LayerCount)
	if r.CleanupReason != "" {
		countStr = "cache"
	}

	wide := innerWidth-2 >= narrowMin
	var fixedW int
	if wide {
		fixedW = gutterW + wastedW + gap + countW + gap + layerW
	} else {
		fixedW = gutterW + wastedW + gap + layerW
	}

	pathW := innerWidth - fixedW - gap - 2
	pathW = max(pathW, minPath)

	path := truncateMid(r.Path, pathW)
	pathPad := max(pathW-lipgloss.Width(path), 0)

	gutter := "  "
	if selected {
		gutter = "> "
	}

	if selected {
		var line string
		if wide {
			line = gutter + path + strings.Repeat(" ", pathPad) +
				strings.Repeat(" ", gap) + padLeft(wastedStr, wastedW) +
				strings.Repeat(" ", gap) + padLeft(countStr, countW) +
				strings.Repeat(" ", gap) + padLeft(layerStr, layerW)
		} else {
			line = gutter + path + strings.Repeat(" ", pathPad) +
				strings.Repeat(" ", gap) + padLeft(wastedStr, wastedW) +
				strings.Repeat(" ", gap) + padLeft(layerStr, layerW)
		}
		return lipgloss.NewStyle().Foreground(t.Selected).Background(t.SelectedBg).Bold(true).Render(line)
	}

	pathStyle := styleWithFg(t.FileName)
	wastedStyle := styleWithFg(t.HeaderDim)
	layerStyle := styleWithFg(t.MetaDim)
	countStyle := styleWithFg(t.MetaDim)

	var b strings.Builder
	b.WriteString(gutter)
	b.WriteString(pathStyle.Render(path))
	b.WriteString(strings.Repeat(" ", pathPad))
	b.WriteString(strings.Repeat(" ", gap))
	b.WriteString(wastedStyle.Render(padLeft(wastedStr, wastedW)))
	if wide {
		b.WriteString(strings.Repeat(" ", gap))
		b.WriteString(countStyle.Render(padLeft(countStr, countW)))
	}
	b.WriteString(strings.Repeat(" ", gap))
	b.WriteString(layerStyle.Render(padLeft(layerStr, layerW)))
	return b.String()
}

// truncateLeft trims the left side of s so it fits within width display
// columns, prefixing the result with "…" when truncation actually occurs.
//
// Display columns, not rune count: a CJK ideograph or wide emoji occupies
// two cells, so taking "the last N runes" of a string can overflow the
// column budget by up to N. Peel runes off the right while summing each
// rune's lipgloss.Width contribution and stop when adding another would
// blow the budget. Without this, a Japanese / Chinese / emoji file path
// rendered in the waste panel would push the surrounding columns out of
// alignment and corrupt the table layout.
func truncateLeft(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return ansi.Truncate(s, width, "")
	}
	keep := width - 1 // one cell reserved for the "…" prefix
	runes := []rune(s)
	used := 0
	startIdx := len(runes)
	for i := len(runes) - 1; i >= 0; i-- {
		w := lipgloss.Width(string(runes[i]))
		if used+w > keep {
			break
		}
		used += w
		startIdx = i
	}
	return "…" + string(runes[startIdx:])
}

// truncateMid keeps the leading path prefix and the filename, replacing the
// middle with "…" so the most useful parts (root context + filename) stay
// visible. Falls back to truncateLeft when the filename alone exceeds width.
func truncateMid(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	// width <= 2: use bare truncation with no ellipsis, matching ansi.Truncate
	// semantics. truncateLeft emits "…"+char at width==2; the difference is
	// intentional — mid-truncation needs both ends and can't spare the cell.
	// In practice pathW is bounded to minPath (12) before this is called, so
	// this branch is a safety net rather than a production path.
	if width <= 2 {
		return ansi.Truncate(s, width, "")
	}

	// Split on the last path separator to isolate the filename.
	sep := strings.LastIndexByte(s, '/')
	if sep < 0 {
		return truncateLeft(s, width)
	}
	filename := s[sep+1:]
	prefix := s[:sep+1]

	filenameW := lipgloss.Width(filename)
	// "…" + filename needs to fit; if not, fall back to left-truncation.
	if filenameW+1 >= width {
		return truncateLeft(s, width)
	}

	// Budget remaining columns for as much of the prefix as will fit.
	// Reserve 1 for "…" between prefix and filename.
	prefixBudget := width - filenameW - 1
	truncatedPrefix := ansi.Truncate(prefix, prefixBudget, "")
	return truncatedPrefix + "…" + filename
}
