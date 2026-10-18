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
	title := fmt.Sprintf("Layer waste %d/%d", position, visibleCount)
	var waste, cleanup int64
	if m.efficiency != nil {
		waste = m.efficiency.WastedBytes
	}
	for _, row := range m.cleanupRows {
		cleanup += row.Wasted
	}
	description := "Old file copies still stored in the image."
	if m.wasteShowCleanup {
		title = fmt.Sprintf("Potential cleanup %d/%d", position, visibleCount)
		description = "Cache files to review before removing."
	}
	if originalCount > visibleCount {
		title += fmt.Sprintf(" (capped, %d total)", originalCount)
	}
	dim := lipgloss.NewStyle().Foreground(m.theme.StatusDim)
	lines := []string{
		m.styles.accent.Render("Layer waste:       " + image.FormatBytes(waste)),
		m.styles.accent.Render("Potential cleanup: " + image.FormatBytes(cleanup)),
		dim.Render(description),
	}
	if len(rows) == 0 {
		empty := "No layer waste detected"
		if m.wasteShowCleanup {
			empty = "No known cache folders found."
		}
		lines = append(lines, empty, dim.Render("Live files may still offer cleanup."))
	} else {
		start := min(m.wasteOffset, len(rows))
		end := min(start+m.wasteVisibleHeight(), len(rows))
		for i := start; i < end; i++ {
			lines = append(lines, formatWasteRow(m.theme, rows[i], i == m.wasteCursor, innerWidth))
		}
	}
	if len(rows) > 0 {
		action := "i Why wasted? · Enter Go to layer"
		if m.wasteShowCleanup {
			action = "i Details · Enter View files"
		}
		if m.copyConfirm {
			action = m.styles.added.Render("Copied!")
		}
		options := "↑/↓ Select · y Copy path"
		if len(m.allWasteRows()) > wasteDefaultLimit {
			if m.wasteExpanded {
				options += " · a Show fewer"
			} else {
				options += " · a Show more"
			}
		}
		lines = append(lines, action, dim.Render(options))
	}
	nextView := "Tab Review cleanup · Esc Close"
	if m.wasteShowCleanup {
		nextView = "Tab View layer waste · Esc Close"
	}
	lines = append(lines, dim.Render(nextView))
	panel := renderPanel(m.theme, strings.Join(lines, "\n"), title, true,
		innerWidth, min(len(lines), max(m.height-4, 0)), m.wasteOffset > 0,
		m.wasteOffset+m.wasteVisibleHeight() < len(rows))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

func formatWasteRow(t Theme, r wasteRow, selected bool, innerWidth int) string {
	const sizeWidth = 9
	layer := ""
	if r.CleanupReason == "" {
		layer = "Layer ?"
		if r.IntroLayer >= 0 {
			layer = fmt.Sprintf("Layer %d", r.IntroLayer+1)
		}
	}
	fixed := 2 + 2 + sizeWidth
	if layer != "" {
		fixed += 2 + lipgloss.Width(layer)
	}
	pathWidth := max(innerWidth-fixed, 0)
	path := truncateMid(r.Path, pathWidth)
	line := "  " + path + strings.Repeat(" ", max(pathWidth-lipgloss.Width(path), 0)) +
		"  " + padLeft(image.FormatBytes(r.Wasted), sizeWidth)
	if layer != "" {
		line += "  " + layer
	}
	if selected {
		line = ">" + line[1:]
		return lipgloss.NewStyle().Foreground(t.Selected).Background(t.SelectedBg).Bold(true).Render(line)
	}
	return styleWithFg(t.FileName).Render(line)
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
