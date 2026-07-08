package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
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
				{"Tab", "Switch panel (layers ↔ tree)"},
			},
		},
		{
			title: "Layers",
			entries: []helpEntry{
				{"S", "Cycle size: Change → Stored → both"},
				{"c", "Copy Dockerfile command"},
				{"A", "Toggle split aggregated view"},
			},
		},
		{
			title: "File Tree",
			entries: []helpEntry{
				{"/", "Search files (substring filter)"},
				{"Enter", "View file / expand or collapse folder"},
				{"Backspace", "Clear active filter"},
				{"d", "Show only changed files"},
				{"s", "Sort by size (↓ → ↑ → off)"},
				{"x", "Save file to current directory"},
				{"y", "Copy file path to clipboard"},
				{"Esc", "Clear filter / close viewer"},
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
				{"Esc", "Return to file tree"},
			},
		},
		{
			title: "Wasted Files",
			entries: []helpEntry{
				{"w", "Open wasted-files overlay"},
				{"Enter", "Jump to introducing layer + path"},
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

func newHelpStyles() helpStyles {
	return helpStyles{
		title:   lipgloss.NewStyle().Foreground(accentColor).Bold(true),
		section: lipgloss.NewStyle().Foreground(modifiedColor).Bold(true),
		key:     lipgloss.NewStyle().Foreground(statusKeyColor),
		desc:    lipgloss.NewStyle().Foreground(fileNameColor),
		dim:     lipgloss.NewStyle().Foreground(statusDimColor),
		note:    lipgloss.NewStyle().Foreground(statusDimColor).Italic(true),
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

func (m model) overlayHelp() string {
	st := newHelpStyles()
	sections := defaultHelpSections()

	var body strings.Builder
	body.WriteString("\n")
	body.WriteString(" ")
	body.WriteString(st.title.Render("layerx — Keyboard Shortcuts"))
	body.WriteString("\n\n")

	cols := helpLayoutColumns(sections)
	if m.width >= helpMultiColumnMinWidth && len(cols) >= 2 {
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
	body.WriteString(" ")
	body.WriteString(st.note.Render(
		"Change = files in the image grew or shrank at this step. " +
			"Stored = bytes Docker keeps for this layer. " +
			"Layer 0 Change is the size after the first layer.",
	))
	body.WriteString("\n")
	body.WriteString(" ")
	body.WriteString(st.dim.Render("(Sort, filter, and diff-only flatten the file tree.)"))
	body.WriteString("\n\n")
	body.WriteString(" ")
	body.WriteString(st.dim.Render("Press ? or Esc to close"))
	body.WriteString("\n")

	content := body.String()
	boxWidth := max(lipgloss.Width(content)+4, 56)
	if maxBox := m.width - 4; maxBox > 0 && boxWidth > maxBox {
		boxWidth = maxBox
	}
	boxHeight := lipgloss.Height(content) + 2

	popup := renderPanel(content, "Help", true, boxWidth, boxHeight, false, false)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, popup)
}

func padHelpColumn(col string, height int) string {
	lines := strings.Split(col, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
