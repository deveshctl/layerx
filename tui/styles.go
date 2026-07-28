package tui

import (
	"image/color"
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
	borderColor := t.UnfocusedBorder
	if focused {
		borderColor = t.FocusedBorder
	}

	borderFg := styleWithFg(borderColor)

	maxTitle := max(contentWidth-3, 0)
	if ansi.StringWidth(title) > maxTitle {
		title = ansi.Truncate(title, maxTitle, "…")
	}
	titleRendered := lipgloss.NewStyle().Foreground(borderColor).Bold(true).Render(title)

	topLeft := borderFg.Render("╭")
	topRight := borderFg.Render("╮")
	bottomLeft := borderFg.Render("╰")
	bottomRight := borderFg.Render("╯")
	vLine := borderFg.Render("│")

	titleLen := lipgloss.Width(title)
	fillCount := max(contentWidth-titleLen-3, 0)
	topBorder := topLeft + borderFg.Render("─") + " " + titleRendered + " " + borderFg.Render(strings.Repeat("─", fillCount)) + topRight

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
		sb.WriteString(strings.Repeat(" ", pad))

		rightBorder := vLine
		if hasAbove && i == 0 {
			rightBorder = styleWithFg(t.ScrollDim).Render("▴")
		} else if hasBelow && i == height-1 {
			rightBorder = styleWithFg(t.ScrollDim).Render("▾")
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

func styleWithFg(c color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(c)
}

// renderGradient interpolates linearly from `from` to `to` across the runes
// of text, rendering each rune in its own per-character colour. Single-rune
// strings get the start colour; the transition is spread evenly across longer
// strings. Used for the image-ref header to give it a premium swept look
// without any new dependencies — only stdlib color arithmetic.
func renderGradient(text string, from, to color.Color) string {
	runes := []rune(text)
	n := len(runes)
	if n == 0 {
		return ""
	}
	fr, fg, fb, _ := from.RGBA()
	tr, tg, tb, _ := to.RGBA()
	var sb strings.Builder
	for i, r := range runes {
		t := 0.0
		if n > 1 {
			t = float64(i) / float64(n-1)
		}
		c := color.NRGBA{
			R: uint8(float64(fr>>8) + (float64(tr>>8)-float64(fr>>8))*t),
			G: uint8(float64(fg>>8) + (float64(tg>>8)-float64(fg>>8))*t),
			B: uint8(float64(fb>>8) + (float64(tb>>8)-float64(fb>>8))*t),
			A: 255,
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(c).Render(string(r)))
	}
	return sb.String()
}

// renderDivider draws a full-width ├────┤ rule in the unfocused border colour.
// Used to separate the column header row from content rows inside panels,
// creating visual hierarchy without adding extra panels or margins.
func renderDivider(t Theme, width int) string {
	if width <= 2 {
		return styleWithFg(t.UnfocusedBorder).Render(strings.Repeat("─", max(width, 0)))
	}
	return styleWithFg(t.UnfocusedBorder).Render("├" + strings.Repeat("─", width-2) + "┤")
}
