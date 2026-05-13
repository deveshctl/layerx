package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	accentColor          = lipgloss.Color("#7C9EFF")
	focusedBorderColor   = lipgloss.Color("#7C9EFF")
	unfocusedBorderColor = lipgloss.Color("#3a3a50")

	selectedColor   = lipgloss.Color("#FFFFFF")
	selectedBgColor = lipgloss.Color("#1e2545")
	addedColor      = lipgloss.Color("#50FA7B")
	modifiedColor   = lipgloss.Color("#F1FA8C")
	removedColor    = lipgloss.Color("#FF5555")
	unchangedColor  = lipgloss.Color("#6272A4")

	separatorColor = lipgloss.Color("#3a3a50")
	commandColor   = lipgloss.Color("#999999")
	statusKeyColor = lipgloss.Color("#7C9EFF")
	statusDimColor = lipgloss.Color("#777777")
	statusBgColor  = lipgloss.Color("#1e1e2e")
	headerDimColor = lipgloss.Color("#777777")
	headerSepColor = lipgloss.Color("#444444")
	fileNameColor  = lipgloss.Color("#BBBBBB")
)

func renderPanel(content, title string, focused bool, contentWidth, height int) string {
	borderColor := unfocusedBorderColor
	if focused {
		borderColor = focusedBorderColor
	}

	borderFg := styleWithFg(borderColor)
	titleRendered := lipgloss.NewStyle().Foreground(borderColor).Bold(true).Render(title)

	topLeft := borderFg.Render("╭")
	topRight := borderFg.Render("╮")
	bottomLeft := borderFg.Render("╰")
	bottomRight := borderFg.Render("╯")
	vLine := borderFg.Render("│")

	titleLen := len(title)
	fillCount := contentWidth - titleLen - 2
	if fillCount < 0 {
		fillCount = 0
	}
	topBorder := topLeft + " " + titleRendered + " " + borderFg.Render(strings.Repeat("─", fillCount)) + topRight

	bottomBorder := bottomLeft + borderFg.Render(strings.Repeat("─", contentWidth)) + bottomRight

	lines := strings.Split(content, "\n")
	var sb strings.Builder
	sb.WriteString(topBorder)
	sb.WriteString("\n")

	for i := 0; i < height; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		lineWidth := lipgloss.Width(line)
		if lineWidth > contentWidth {
			line = ansi.Truncate(line, contentWidth, "")
			lineWidth = lipgloss.Width(line)
		}
		pad := contentWidth - lineWidth
		if pad < 0 {
			pad = 0
		}
		sb.WriteString(vLine)
		sb.WriteString(line)
		sb.WriteString(strings.Repeat(" ", pad))
		sb.WriteString(vLine)
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
