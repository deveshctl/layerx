package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	accentColor          = lipgloss.Color("#89B4FA")
	focusedBorderColor   = lipgloss.Color("#89B4FA")
	unfocusedBorderColor = lipgloss.Color("#45475A")

	selectedColor   = lipgloss.Color("#CDD6F4")
	selectedBgColor = lipgloss.Color("#313244")
	addedColor      = lipgloss.Color("#A6E3A1")
	modifiedColor   = lipgloss.Color("#F9E2AF")
	removedColor    = lipgloss.Color("#F38BA8")
	unchangedColor  = lipgloss.Color("#A6ADC8")

	separatorColor = lipgloss.Color("#313244")
	commandColor   = lipgloss.Color("#A6ADC8")
	statusKeyColor = lipgloss.Color("#89B4FA")
	statusDimColor = lipgloss.Color("#6C7086")
	statusBgColor  = lipgloss.Color("#181825")
	headerDimColor = lipgloss.Color("#9399B2")
	headerSepColor = lipgloss.Color("#313244")
	fileNameColor  = lipgloss.Color("#BAC2DE")

	metaDimColor   = lipgloss.Color("#6C7086")
	treeDimColor   = lipgloss.Color("#45475A")
	scrollDimColor = lipgloss.Color("#6C7086")

	searchHighlightBg = lipgloss.Color("#585B70")
	searchCurrentBg   = lipgloss.Color("#F9E2AF")
	searchCurrentFg   = lipgloss.Color("#1E1E2E")
)

// largeStepGrowthFraction is the threshold above which a positive Δfs
// is colored as a notable size increase. 0.10 = 10% of final live size.
const largeStepGrowthFraction = 0.10

func renderPanel(content, title string, focused bool, contentWidth, height int, hasAbove, hasBelow bool) string {
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

	titleLen := lipgloss.Width(title)
	fillCount := contentWidth - titleLen - 3
	if fillCount < 0 {
		fillCount = 0
	}
	topBorder := topLeft + borderFg.Render("─") + " " + titleRendered + " " + borderFg.Render(strings.Repeat("─", fillCount)) + topRight

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

		rightBorder := vLine
		if hasAbove && i == 0 {
			rightBorder = styleWithFg(scrollDimColor).Render("▴")
		} else if hasBelow && i == height-1 {
			rightBorder = styleWithFg(scrollDimColor).Render("▾")
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
