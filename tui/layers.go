package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/deveshctl/layerx/image"
)

func renderLayers(t Theme, st themeStyles, layers []image.Layer, cursor int, offset int, width, height int, focused bool, mode sizeColMode, finalLiveSize int64) string {
	contentWidth := width - 2
	listHeight := height

	end := min(offset+listHeight, len(layers))
	if offset > end {
		offset = end
	}
	visible := layers[offset:end]

	// Apply the both→delta fallback once per render: callers (including
	// tests) compute the panel width from leftPanelWidth(); we mirror that
	// threshold here without mutating the model.
	effMode := mode
	if effMode == sizeColBoth && contentWidth < 38 {
		effMode = sizeColDelta
	}

	var sb strings.Builder

	for i, layer := range visible {
		line := formatLayerLine(t, st, layer, offset+i == cursor, contentWidth, effMode, finalLiveSize)
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
	if focused {
		title += " · " + sizeModePanelSuffix(effMode)
	}

	hasAbove := offset > 0
	hasBelow := end < len(layers)

	content := sb.String()
	return renderPanel(t, content, title, focused, contentWidth, height, hasAbove, hasBelow)
}

func renderCommandBar(st themeStyles, cmd string, width int) string {
	maxLines := 3
	wrappedLines := wrapCommandLines(cmd, width-2, maxLines)

	// Determine the count of non-empty wrapped lines so we don't emit
	// trailing newlines for unfilled rows. wrapCommandLines preserves a
	// fixed-length return for caller-test stability; the fixup happens
	// here at render time.
	filled := 0
	for _, wl := range wrappedLines {
		if wl != "" {
			filled++
		}
	}

	prefix := st.accent.Bold(true).Render("▶ ")

	var sb strings.Builder
	for i := range filled {
		wl := wrappedLines[i]
		if i == 0 {
			styled := highlightInstruction(st, wl)
			sb.WriteString(prefix + styled)
		} else {
			sb.WriteString("  " + st.command.Render(wl))
		}
		if i < filled-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func highlightInstruction(st themeStyles, line string) string {
	parts := strings.SplitN(line, " ", 2)
	if len(parts) == 0 {
		return ""
	}
	instruction := st.accent.Bold(true).Render(parts[0])
	if len(parts) == 1 {
		return instruction
	}
	return instruction + " " + st.command.Render(parts[1])
}

func wrapCommandLines(cmd string, width int, maxLines int) []string {
	lines := make([]string, maxLines)
	runes := []rune(cmd)

	for i := range maxLines {
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
		for j := width - 1; j >= width/2; j-- {
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

// sizeColumnText returns the unstyled column text for the given mode.
// delta and blob modes produce a column of at least 7 chars; both
// produces at least 15 chars ("blob   Δfs" — 7 + 1 gap + 7). Width
// expands to fit unusually large values rather than byte-truncating.
func sizeColumnText(l image.Layer, mode sizeColMode) (string, int) {
	switch mode {
	case sizeColBlob:
		blob := image.FormatBytes(l.Size)
		w := max(len([]rune(blob)), 7)
		return padLeftRunes(blob, w), w
	case sizeColBoth:
		blob := image.FormatBytes(l.Size)
		delta := image.FormatSignedBytes(l.NetDelta)
		bw := max(len([]rune(blob)), 7)
		dw := max(len([]rune(delta)), 7)
		return padLeftRunes(blob, bw) + " " + padLeftRunes(delta, dw), bw + 1 + dw
	default:
		delta := image.FormatSignedBytes(l.NetDelta)
		w := max(len([]rune(delta)), 7)
		return padLeftRunes(delta, w), w
	}
}

// padLeftRunes pads s on the left with spaces to width measured in
// runes, never truncating. Used for size column values which we want
// to display in full even when they exceed the nominal 7-char budget.
func padLeftRunes(s string, width int) string {
	n := len([]rune(s))
	if n >= width {
		return s
	}
	return strings.Repeat(" ", width-n) + s
}

// deltaColor picks the color for a Δfs value given the final live size.
// Negative → green (cleanup); ≥ 10% of final → modified/accent (large
// growth); else → dim default.
func deltaColor(t Theme, delta int64, finalLiveSize int64) color.Color {
	if delta < 0 {
		return t.Added
	}
	if finalLiveSize > 0 && float64(delta) >= float64(finalLiveSize)*largeStepGrowthFraction {
		return t.Modified
	}
	return t.HeaderDim
}

func formatLayerLine(t Theme, st themeStyles, l image.Layer, selected bool, maxWidth int, mode sizeColMode, finalLiveSize int64) string {
	index := fmt.Sprintf("#%d", l.Index)

	indexWidth := max(len([]rune(index)), 3)

	sizeText, sizeWidth := sizeColumnText(l, mode)

	// 3 (leading: cursor + space) + indexWidth + 2 (gap) + sizeWidth + 2 (gap before cmd)
	fixedCols := 3 + indexWidth + 2 + sizeWidth + 2
	cmdSpace := maxWidth - fixedCols
	cmd := l.Command
	if cmdSpace <= 0 {
		cmd = ""
	} else if lipgloss.Width(cmd) > cmdSpace {
		if cmdSpace > 1 {
			cmd = ansi.Truncate(cmd, cmdSpace, "…")
		} else {
			cmd = ansi.Truncate(cmd, cmdSpace, "")
		}
	}

	indexPad := ""
	if len([]rune(index)) < indexWidth {
		indexPad = strings.Repeat(" ", indexWidth-len([]rune(index)))
	}

	if selected {
		cursor := st.accent.Render("▸")
		plain := " " + index + indexPad + "  " + sizeText + "  " + cmd
		if lipgloss.Width(plain) > maxWidth-1 {
			plain = ansi.Truncate(plain, maxWidth-1, "")
		}
		inner := cursor + st.selectedTreeBg.Render(plain)
		return inner
	}

	dimHash := st.metaDim.Render("#")
	numStr := fmt.Sprintf("%d", l.Index)
	numPad := ""
	if len([]rune(numStr))+1 < indexWidth {
		numPad = strings.Repeat(" ", indexWidth-len([]rune(numStr))-1)
	}

	sizeRendered := renderSizeColumn(t, st, l, mode, finalLiveSize)
	cmdRendered := st.command.Render(cmd)

	plain := "   " + dimHash + st.fileName.Render(numStr) + numPad + "  " + sizeRendered + "  " + cmdRendered

	lineWidth := lipgloss.Width(plain)
	if lineWidth > maxWidth {
		return ansi.Truncate(plain, maxWidth, "")
	}
	return plain
}

// renderSizeColumn produces the colored size column for the unselected
// row state. Selected rows use the inverted background and skip per-cell
// coloring to keep the highlight uniform.
func renderSizeColumn(t Theme, st themeStyles, l image.Layer, mode sizeColMode, finalLiveSize int64) string {
	switch mode {
	case sizeColBlob:
		blob := image.FormatBytes(l.Size)
		w := max(len([]rune(blob)), 7)
		return st.headerDim.Render(padLeftRunes(blob, w))
	case sizeColBoth:
		blob := image.FormatBytes(l.Size)
		delta := image.FormatSignedBytes(l.NetDelta)
		bw := max(len([]rune(blob)), 7)
		dw := max(len([]rune(delta)), 7)
		blobR := st.headerDim.Render(padLeftRunes(blob, bw))
		deltaR := styleWithFg(deltaColor(t, l.NetDelta, finalLiveSize)).Render(padLeftRunes(delta, dw))
		return blobR + " " + deltaR
	default:
		delta := image.FormatSignedBytes(l.NetDelta)
		w := max(len([]rune(delta)), 7)
		return styleWithFg(deltaColor(t, l.NetDelta, finalLiveSize)).Render(padLeftRunes(delta, w))
	}
}
