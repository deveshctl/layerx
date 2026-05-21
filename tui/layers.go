package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/deveshctl/layerx/image"
)

func renderLayers(layers []image.Layer, cursor int, offset int, width, height int, focused bool) string {
	contentWidth := width - 2
	listHeight := height

	end := offset + listHeight
	if end > len(layers) {
		end = len(layers)
	}
	if offset > end {
		offset = end
	}
	visible := layers[offset:end]

	var sb strings.Builder

	for i, layer := range visible {
		line := formatLayerLine(layer, offset+i == cursor, contentWidth)
		sb.WriteString(line)
		if i < len(visible)-1 {
			sb.WriteString("\n")
		}
	}

	rendered := len(visible)
	for i := rendered; i < listHeight; i++ {
		if i > 0 || rendered > 0 {
			sb.WriteString("\n")
		}
	}

	title := "Layers"
	if len(layers) > 0 {
		title = fmt.Sprintf("Layers %d/%d", cursor+1, len(layers))
	}

	hasAbove := offset > 0
	hasBelow := end < len(layers)

	content := sb.String()
	return renderPanel(content, title, focused, contentWidth, height, hasAbove, hasBelow)
}

func renderCommandBar(cmd string, width int) string {
	maxLines := 3
	wrappedLines := wrapCommandLines(cmd, width-2, maxLines)

	prefix := lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render("▶ ")

	var sb strings.Builder
	for i, wl := range wrappedLines {
		if i == 0 {
			styled := highlightInstruction(wl)
			sb.WriteString(prefix + styled)
		} else {
			sb.WriteString("  " + styleWithFg(commandColor).Render(wl))
		}
		if i < maxLines-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func highlightInstruction(line string) string {
	parts := strings.SplitN(line, " ", 2)
	if len(parts) == 0 {
		return ""
	}
	instruction := lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render(parts[0])
	if len(parts) == 1 {
		return instruction
	}
	return instruction + " " + styleWithFg(commandColor).Render(parts[1])
}

func wrapCommandLines(cmd string, width int, maxLines int) []string {
	lines := make([]string, maxLines)
	runes := []rune(cmd)

	for i := 0; i < maxLines; i++ {
		if len(runes) == 0 {
			break
		}
		if len(runes) <= width {
			lines[i] = string(runes)
			break
		}
		if i == maxLines-1 {
			lines[i] = string(runes[:width-1]) + "…"
			break
		}
		breakAt := width
		for j := width - 1; j > width/2; j-- {
			if runes[j] == ' ' {
				breakAt = j
				break
			}
		}
		lines[i] = string(runes[:breakAt])
		runes = []rune(strings.TrimLeft(string(runes[breakAt:]), " "))
	}

	return lines
}

func formatLayerLine(l image.Layer, selected bool, maxWidth int) string {
	index := fmt.Sprintf("#%d", l.Index)
	size := image.FormatBytes(l.Size)

	indexWidth := len([]rune(index))
	if indexWidth < 3 {
		indexWidth = 3
	}
	sizeWidth := len([]rune(size))
	if sizeWidth < 7 {
		sizeWidth = 7
	}

	// 3 (leading: cursor + space) + indexWidth + 2 (gap) + sizeWidth + 2 (gap before cmd)
	fixedCols := 3 + indexWidth + 2 + sizeWidth + 2
	cmdSpace := maxWidth - fixedCols
	cmd := l.Command
	cmdRunes := []rune(cmd)
	if cmdSpace <= 0 {
		cmd = ""
	} else if len(cmdRunes) > cmdSpace {
		if cmdSpace > 1 {
			cmd = string(cmdRunes[:cmdSpace-1]) + "…"
		} else {
			cmd = string(cmdRunes[:cmdSpace])
		}
	}

	indexPad := ""
	if len([]rune(index)) < indexWidth {
		indexPad = strings.Repeat(" ", indexWidth-len([]rune(index)))
	}
	sizePad := ""
	if len([]rune(size)) < sizeWidth {
		sizePad = strings.Repeat(" ", sizeWidth-len([]rune(size)))
	}

	if selected {
		cursor := lipgloss.NewStyle().Foreground(accentColor).Render("▸")
		plain := " " + index + indexPad + "  " + sizePad + size + "  " + cmd
		plainRunes := []rune(plain)
		if len(plainRunes) > maxWidth-1 {
			plain = string(plainRunes[:maxWidth-1])
		}
		inner := cursor + lipgloss.NewStyle().Foreground(selectedColor).Background(selectedBgColor).Render(plain)
		return inner
	}

	dimHash := styleWithFg(metaDimColor).Render("#")
	numStr := fmt.Sprintf("%d", l.Index)
	numPad := ""
	if len([]rune(numStr))+1 < indexWidth {
		numPad = strings.Repeat(" ", indexWidth-len([]rune(numStr))-1)
	}
	sizeRendered := styleWithFg(headerDimColor).Render(sizePad + size)
	cmdRendered := styleWithFg(commandColor).Render(cmd)

	plain := "   " + dimHash + styleWithFg(fileNameColor).Render(numStr) + numPad + "  " + sizeRendered + "  " + cmdRendered

	lineWidth := lipgloss.Width(plain)
	if lineWidth > maxWidth {
		return ansi.Truncate(plain, maxWidth, "")
	}
	return plain
}
