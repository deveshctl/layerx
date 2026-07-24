package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// largeStepGrowthFraction is the threshold above which a positive Δfs
// is colored as a notable size increase. 0.10 = 10% of final live size.
const largeStepGrowthFraction = 0.10

func renderPanel(t Theme, content, title string, focused bool, contentWidth, height int, hasAbove, hasBelow bool) string {
	// Defensive: callers can compute negative widths/heights when the
	// terminal is unusually small (m.width-2 with m.width=1, etc.).
	// strings.Repeat panics on negative counts; clamp here so every code
	// path that reaches the panel renderer is safe.
	if contentWidth < 0 {
		contentWidth = 0
	}
	if height < 0 {
		height = 0
	}
	borderColor := t.BorderBlur
	if focused {
		borderColor = t.BorderFocus
	}

	// Every glyph the panel emits (borders, corners, title, scroll arrows)
	// carries PanelBg so the panel body is a full-bleed fill with no
	// terminal-default background showing through at the edges.
	borderFg := panelText(t, borderColor)
	panelBg := lipgloss.NewStyle().Background(t.PanelBg)

	maxTitle := max(contentWidth-3, 0)
	if ansi.StringWidth(title) > maxTitle {
		title = ansi.Truncate(title, maxTitle, "…")
	}
	titleRendered := panelText(t, borderColor).Bold(true).Render(title)

	topLeft := borderFg.Render("╭")
	topRight := borderFg.Render("╮")
	bottomLeft := borderFg.Render("╰")
	bottomRight := borderFg.Render("╯")
	vLine := borderFg.Render("│")

	titleLen := lipgloss.Width(title)
	fillCount := max(contentWidth-titleLen-3, 0)
	topBorder := topLeft + borderFg.Render("─") + borderFg.Render(" ") + titleRendered + borderFg.Render(" ") + borderFg.Render(strings.Repeat("─", fillCount)) + topRight

	bottomBorder := bottomLeft + borderFg.Render(strings.Repeat("─", contentWidth)) + bottomRight

	lines := strings.Split(content, "\n")
	var sb strings.Builder
	sb.WriteString(topBorder)
	sb.WriteString("\n")

	for i := range height {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		lineWidth := ansi.StringWidth(line)
		if lineWidth > contentWidth {
			line = ansi.Truncate(line, contentWidth, "")
			lineWidth = ansi.StringWidth(line)
		}
		pad := max(contentWidth-lineWidth, 0)

		sb.WriteString(vLine)
		sb.WriteString(line)
		// Right-pad with PanelBg-backed spaces so the fill reaches the right
		// border. Plain spaces here would show the terminal background and
		// leave a bleed gap at every short line's end.
		sb.WriteString(panelBg.Render(strings.Repeat(" ", pad)))

		rightBorder := vLine
		if hasAbove && i == 0 {
			rightBorder = panelText(t, t.TextDim2).Render("▴")
		} else if hasBelow && i == height-1 {
			rightBorder = panelText(t, t.TextDim2).Render("▾")
		}
		sb.WriteString(rightBorder)

		if i < height-1 {
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(bottomBorder)

	return sb.String()
}
