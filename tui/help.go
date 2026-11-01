package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// User-facing names for layer size column modes (internal names stay sizeCol*).
const (
	sizeModeLabelChange = "change"
	sizeModeLabelStored = "stored"
	sizeModeLabelBoth   = "stored+change"
)

func sizeModePanelSuffix(mode sizeColMode) string {
	switch mode {
	case sizeColBlob:
		return sizeModeLabelStored
	case sizeColBoth:
		return sizeModeLabelBoth
	default:
		return sizeModeLabelChange
	}
}

type helpEntry struct {
	key  string
	desc string
}

type helpSection struct {
	title   string
	entries []helpEntry
}

func defaultHelpSections() []helpSection {
	return []helpSection{
		{
			title: "Navigation",
			entries: []helpEntry{
				{"j / k", "Move cursor down / up"},
				{"g / G", "Jump to first / last item"},
				{"Ctrl+D/U", "Half page down / up"},
				{"Ctrl+F/B", "Page down / up (PgDn / PgUp)"},
				{"Tab", "Cycle layers and file panels"},
			},
		},
		{
			title: "Layers",
			entries: []helpEntry{
				{"S", "Cycle size: Change → Stored → both"},
				{"c", "Copy Dockerfile command"},
				{"A", "Toggle layer files + cumulative files"},
			},
		},
		{
			title: "File Tree",
			entries: []helpEntry{
				{"/", "Filter file paths"},
				{"Enter", "View file / expand or collapse folder"},
				{"h / l", "Collapse / expand folder, or move to parent / child"},
				{"m", "Toggle file metadata columns"},
				{"Backspace", "Clear confirmed filter"},
				{"d", "Show only changed files"},
				{"s", "List files by size (largest → smallest → tree)"},
				{"x", "Save file to current directory"},
				{"y", "Copy file path to clipboard"},
				{"Esc", "Clear filter"},
			},
		},
		{
			title: "File Viewer",
			entries: []helpEntry{
				{"j / k", "Scroll down / up"},
				{"h / l", "Scroll left / right (long lines)"},
				{"g / G", "Jump to top / bottom"},
				{"/", "Search in file"},
				{"n / N", "Next / previous match"},
				{"Y", "Copy file content to clipboard"},
				{"Esc", "Clear search, then close viewer"},
			},
		},
		{
			title: "Waste and cleanup",
			entries: []helpEntry{
				{"w", "Review layer waste and potential cleanup"},
				{"i", "Explain the selected file or cache"},
				{"Tab", "Switch between layer waste and potential cleanup"},
				{"Enter", "Jump to original layer and path"},
				{"a", "Expand top 20 to up to 500"},
				{"y", "Copy highlighted path"},
				{"Esc", "Close overlay"},
			},
		},
		{
			title: "General",
			entries: []helpEntry{
				{"?", "Toggle this help"},
				{"q / Ctrl+C", "Quit"},
			},
		},
	}
}

type helpStyles struct {
	title   lipgloss.Style
	section lipgloss.Style
	key     lipgloss.Style
	desc    lipgloss.Style
	dim     lipgloss.Style
	note    lipgloss.Style
}

func newHelpStyles(t Theme) helpStyles {
	return helpStyles{
		title:   lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		section: lipgloss.NewStyle().Foreground(t.Modified).Bold(true),
		key:     lipgloss.NewStyle().Foreground(t.StatusKey),
		desc:    lipgloss.NewStyle().Foreground(t.FileName),
		dim:     lipgloss.NewStyle().Foreground(t.StatusDim),
		note:    lipgloss.NewStyle().Foreground(t.StatusDim).Italic(true),
	}
}

const helpKeyWidth = 12

func padHelpKey(key string) string {
	w := lipgloss.Width(key)
	if w >= helpKeyWidth {
		return key
	}
	return key + strings.Repeat(" ", helpKeyWidth-w)
}

func renderHelpSection(sec helpSection, st helpStyles) string {
	var lines []string
	lines = append(lines, " "+st.section.Render(sec.title))
	for _, e := range sec.entries {
		line := " " + st.key.Render(padHelpKey(e.key)) + st.desc.Render(e.desc)
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func renderHelpSectionsVertical(sections []helpSection, st helpStyles) string {
	var blocks []string
	for i, sec := range sections {
		blocks = append(blocks, renderHelpSection(sec, st))
		if i < len(sections)-1 {
			blocks = append(blocks, "")
		}
	}
	return strings.Join(blocks, "\n")
}

func renderHelpColumn(sections []helpSection, st helpStyles) string {
	return renderHelpSectionsVertical(sections, st)
}

func helpLayoutColumns(sections []helpSection) [][]helpSection {
	switch len(sections) {
	case 0:
		return nil
	case 6:
		// Copy into a fixed-size array first. Indexing a [6]helpSection is
		// a compile-time-checked constant access, so gosec G602 (which
		// flags slice access without an upstream bounds check) sees no
		// taint here.
		var s [6]helpSection
		copy(s[:], sections)
		return [][]helpSection{
			{s[0], s[1]},
			{s[2], s[3]},
			{s[4], s[5]},
		}
	default:
		n := len(sections)
		per := (n + 2) / 3
		var cols [][]helpSection
		for i := 0; i < n; i += per {
			end := min(i+per, n)
			cols = append(cols, sections[i:end])
		}
		return cols
	}
}

const (
	helpMultiColumnMinWidth = 100
	helpColumnGap           = 2
)

func (m model) helpContent() string {
	st := newHelpStyles(m.theme)
	sections := defaultHelpSections()

	var body strings.Builder
	body.WriteString("\n")
	body.WriteString(" ")
	body.WriteString(st.title.Render("layerx — Keyboard Shortcuts"))
	body.WriteString("\n\n")

	cols := helpLayoutColumns(sections)
	if m.width >= helpMultiColumnMinWidth && len(cols) >= 2 && helpColumnsWidth(cols, st) <= m.width-6 {
		colBodies := make([]string, len(cols))
		maxH := 0
		for i, group := range cols {
			colBodies[i] = renderHelpColumn(group, st)
			if h := lipgloss.Height(colBodies[i]); h > maxH {
				maxH = h
			}
		}
		parts := make([]string, 0, len(colBodies)*2-1)
		for i, cb := range colBodies {
			if i > 0 {
				parts = append(parts, strings.Repeat(" ", helpColumnGap))
			}
			parts = append(parts, padHelpColumn(cb, maxH))
		}
		body.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, parts...))
	} else {
		body.WriteString(renderHelpSectionsVertical(sections, st))
	}

	body.WriteString("\n\n")
	body.WriteString(st.note.Render("Change = files in the image grew or shrank at this step.\n" +
		"Stored = bytes kept in this layer. Layer 1 is the first layer."))
	body.WriteString("\n")
	body.WriteString(st.dim.Render("Sort, filter, and changes-only mode flatten the file tree."))
	return ansi.Wrap(body.String(), max(m.width-6, 1), "")
}

func helpColumnsWidth(cols [][]helpSection, st helpStyles) int {
	width := max(len(cols)-1, 0) * helpColumnGap
	for _, col := range cols {
		width += lipgloss.Width(renderHelpColumn(col, st))
	}
	return width
}

const helpScrollHint = "↑/↓ scroll · PgUp/PgDn page · ?/Esc close"

func (m model) helpVisibleLines() int {
	footerHeight := lipgloss.Height(ansi.Wrap(helpScrollHint, max(m.width-6, 1), ""))
	return max(m.height-4-footerHeight, 1)
}

func (m model) helpMaxOffset() int {
	return max(lipgloss.Height(m.helpContent())-m.helpVisibleLines(), 0)
}

func (m model) overlayHelp() string {
	lines := strings.Split(m.helpContent(), "\n")
	height := min(m.helpVisibleLines(), len(lines))
	offset := min(m.helpOffset, max(len(lines)-height, 0))
	content := strings.Join(lines[offset:offset+height], "\n")
	footer := "? / Esc close"
	if len(lines) > height {
		footer = helpScrollHint
	}
	content += "\n\n" + newHelpStyles(m.theme).dim.Render(ansi.Wrap(footer, max(m.width-6, 1), ""))
	// Reserve the border and footer before sizing the scrollable body.
	popup := renderPanel(m.theme, content, "Help", true, max(m.width-6, 1),
		min(lipgloss.Height(content), max(m.height-2, 1)), offset > 0, offset+height < len(lines))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, popup)
}

func padHelpColumn(col string, height int) string {
	lines := strings.Split(col, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
