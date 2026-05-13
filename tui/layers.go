package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/deveshpharswan/layerx/image"
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

	title := "LAYERS"
	if len(layers) > 0 {
		title = fmt.Sprintf("LAYERS [%d/%d]", cursor+1, len(layers))
	}

	content := sb.String()
	return renderPanel(content, title, focused, contentWidth, height)
}

func renderCommandBar(cmd string, width int) string {
	maxLines := 3
	wrappedLines := wrapCommandLines(cmd, width, maxLines)
	var sb strings.Builder
	for i, wl := range wrappedLines {
		sb.WriteString(styleWithFg(commandColor).Render(wl))
		if i < maxLines-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
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
			lines[i] = string(runes[:width-2]) + ".."
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

	// 2 (leading) + indexWidth + 1 (sep) + sizeWidth + 2 (sep before cmd)
	fixedCols := 2 + indexWidth + 1 + sizeWidth + 2
	cmdSpace := maxWidth - fixedCols
	cmd := l.Command
	cmdRunes := []rune(cmd)
	if cmdSpace <= 0 {
		cmd = ""
	} else if len(cmdRunes) > cmdSpace {
		if cmdSpace > 2 {
			cmd = string(cmdRunes[:cmdSpace-2]) + ".."
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
	plain := "  " + index + indexPad + " " + sizePad + size + "  " + cmd

	// Hard-clamp to maxWidth to prevent any overflow.
	plainRunes := []rune(plain)
	if len(plainRunes) > maxWidth {
		plain = string(plainRunes[:maxWidth])
	}

	if selected {
		runes := []rune(plain)
		inner := "> " + string(runes[2:])
		return lipgloss.NewStyle().Foreground(selectedColor).Background(selectedBgColor).Render(inner)
	}
	return plain
}
