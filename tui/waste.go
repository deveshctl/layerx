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
	Path       string
	Wasted     int64
	LayerCount int
	IntroLayer int
}

func buildIntroIndex(root *image.FileNode) map[string]int {
	idx := make(map[string]int)
	if root == nil {
		return idx
	}
	var walk func(n *image.FileNode)
	walk = func(n *image.FileNode) {
		for _, c := range n.Children {
			if c.IsDir {
				walk(c)
			} else {
				idx[c.Path] = c.IntroducedInLayer
			}
		}
	}
	walk(root)
	return idx
}

func (m *model) openWaste() {
	m.wasteRows = nil
	m.wasteCursor = 0
	m.wasteExpanded = false

	if m.efficiency != nil && len(m.efficiency.WastedFiles) > 0 {
		var introIdx map[string]int
		if m.analysis != nil && len(m.analysis.StackedTrees) > 0 {
			last := m.analysis.StackedTrees[len(m.analysis.StackedTrees)-1]
			if last != nil {
				introIdx = buildIntroIndex(last.Root)
			}
		}
		if introIdx == nil {
			introIdx = map[string]int{}
		}

		capRows := min(len(m.efficiency.WastedFiles), wasteMaxRows)
		m.wasteRows = make([]wasteRow, 0, capRows)
		for i, wf := range m.efficiency.WastedFiles {
			if i >= wasteMaxRows {
				break
			}
			m.wasteRows = append(m.wasteRows, wasteRow{
				Path:       wf.Path,
				Wasted:     wf.TotalWasted,
				LayerCount: wf.LayerCount,
				IntroLayer: introIdx[wf.Path],
			})
		}
	}
	m.showWaste = true
}

func (m *model) closeWaste() {
	m.showWaste = false
	m.wasteRows = nil
	m.wasteCursor = 0
	m.wasteExpanded = false
}

func (m model) visibleWasteRows() []wasteRow {
	if !m.wasteExpanded && len(m.wasteRows) > wasteDefaultLimit {
		return m.wasteRows[:wasteDefaultLimit]
	}
	return m.wasteRows
}

func (m model) handleWasteOverlay(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Quit) {
		m.quitting = true
		return m, tea.Quit
	}
	rows := m.visibleWasteRows()
	switch {
	case key.Matches(msg, m.keys.Down):
		if m.wasteCursor < len(rows)-1 {
			m.wasteCursor++
		}
	case key.Matches(msg, m.keys.Up):
		if m.wasteCursor > 0 {
			m.wasteCursor--
		}
	case key.Matches(msg, m.keys.Top):
		m.wasteCursor = 0
	case key.Matches(msg, m.keys.Bottom):
		if len(rows) > 0 {
			m.wasteCursor = len(rows) - 1
		}
	case msg.Code == tea.KeyEnter:
		if len(rows) == 0 {
			return m, nil
		}
		return m.wasteJump(rows[m.wasteCursor])
	case msg.Text == "a":
		m.wasteExpanded = !m.wasteExpanded
		m.wasteCursor = 0
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
	m.filterActive = false
	m.filterQuery = ""

	layers := m.layers()
	if row.IntroLayer >= 0 && row.IntroLayer < len(layers) {
		m.layerCursor = row.IntroLayer
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
	if found < 0 {
		mp.treeCursor = 0
		status = "File not visible in current view"
	} else {
		mp.treeCursor = found
		status = fmt.Sprintf("Jumped → L%d %s", row.IntroLayer+1, row.Path)
	}
	mp.adjustTreeScroll()
	mp.statusMsg = status
	mp.closeWaste()
	return *mp, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return clearStatusMsg{} })
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
	boxWidth := min(m.width-4, 70)
	boxWidth = max(boxWidth, 30)
	innerWidth := boxWidth - 2

	rows := m.visibleWasteRows()
	totalAvailable := len(m.wasteRows)
	originalCount := 0
	if m.efficiency != nil {
		originalCount = len(m.efficiency.WastedFiles)
	}

	titleStyle := lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(statusDimColor)
	keyStyle := lipgloss.NewStyle().Foreground(statusKeyColor).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(fileNameColor)

	var lines []string
	lines = append(lines, "")

	if originalCount == 0 {
		empty := "No wasted files — efficiency 100%"
		pad := 0
		if innerWidth > len(empty) {
			pad = (innerWidth - len(empty)) / 2
		}
		bodyHeight := 5
		mid := bodyHeight / 2
		for i := range bodyHeight {
			if i == mid {
				lines = append(lines, strings.Repeat(" ", pad)+dimStyle.Render(empty))
			} else {
				lines = append(lines, "")
			}
		}
	} else {
		var header string
		switch {
		case m.wasteExpanded && totalAvailable < originalCount:
			header = fmt.Sprintf("All %d of %d wasted files (truncated) | %s wasted",
				totalAvailable, originalCount, image.FormatBytes(m.efficiency.WastedBytes))
		case m.wasteExpanded:
			header = fmt.Sprintf("All %d wasted files | %s wasted",
				totalAvailable, image.FormatBytes(m.efficiency.WastedBytes))
		default:
			limit := min(originalCount, wasteDefaultLimit)
			header = fmt.Sprintf("Top %d of %d wasted files | %s wasted",
				limit, originalCount, image.FormatBytes(m.efficiency.WastedBytes))
		}
		lines = append(lines, "  "+titleStyle.Render(header))
		lines = append(lines, "")

		for i, r := range rows {
			lines = append(lines, formatWasteRow(r, i == m.wasteCursor, innerWidth))
		}
	}

	lines = append(lines, "")

	var footer string
	switch {
	case m.copyConfirm:
		copied := lipgloss.NewStyle().Foreground(addedColor).Bold(true).Render("Copied!")
		pad := 0
		if innerWidth > lipgloss.Width(copied) {
			pad = (innerWidth - lipgloss.Width(copied)) / 2
		}
		footer = strings.Repeat(" ", pad) + copied
		lines = append(lines, footer)
	case originalCount == 0:
		footer = keyStyle.Render("Esc") + " " + descStyle.Render("close")
		lines = append(lines, "  "+footer)
	case m.wasteExpanded:
		footer = keyStyle.Render("Enter") + " " + descStyle.Render("jump") + dimStyle.Render(" │ ") +
			keyStyle.Render("a") + " " + descStyle.Render("collapse") + dimStyle.Render(" │ ") +
			keyStyle.Render("y") + " " + descStyle.Render("copy") + dimStyle.Render(" │ ") +
			keyStyle.Render("Esc") + " " + descStyle.Render("close")
		lines = append(lines, "  "+footer)
	default:
		footer = keyStyle.Render("Enter") + " " + descStyle.Render("jump") + dimStyle.Render(" │ ") +
			keyStyle.Render("a") + " " + descStyle.Render("expand") + dimStyle.Render(" │ ") +
			keyStyle.Render("y") + " " + descStyle.Render("copy") + dimStyle.Render(" │ ") +
			keyStyle.Render("Esc") + " " + descStyle.Render("close")
		lines = append(lines, "  "+footer)
	}
	lines = append(lines, "")

	body := strings.Join(lines, "\n")
	boxHeight := len(lines)
	if m.height > 2 && boxHeight > m.height-2 {
		boxHeight = m.height - 2
	}

	panel := renderPanel(body, "Wasted Files", true, innerWidth, boxHeight, false, false)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

func formatWasteRow(r wasteRow, selected bool, innerWidth int) string {
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
	layerStr := fmt.Sprintf("L%d", r.IntroLayer+1)
	countStr := fmt.Sprintf("x%d", r.LayerCount)

	wide := innerWidth-2 >= narrowMin
	var fixedW int
	if wide {
		fixedW = gutterW + wastedW + gap + countW + gap + layerW
	} else {
		fixedW = gutterW + wastedW + gap + layerW
	}

	pathW := innerWidth - fixedW - gap - 2
	pathW = max(pathW, minPath)

	path := truncateLeft(r.Path, pathW)
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
		return lipgloss.NewStyle().Foreground(selectedColor).Background(selectedBgColor).Bold(true).Render(line)
	}

	pathStyle := styleWithFg(fileNameColor)
	wastedStyle := styleWithFg(headerDimColor)
	layerStyle := styleWithFg(metaDimColor)
	countStyle := styleWithFg(metaDimColor)

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
	keep := width - 1
	runes := []rune(s)
	if keep >= len(runes) {
		return s
	}
	return "…" + string(runes[len(runes)-keep:])
}
