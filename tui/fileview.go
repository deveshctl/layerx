package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/deveshpharswan/layerx/image"
)

func renderFileView(content *image.FileContent, offset, width, height int, loading bool, spinnerFrame int) string {
	contentWidth := width - 2
	contentHeight := height

	if loading {
		frame := spinnerFrames[spinnerFrame%len(spinnerFrames)]
		msg := frame + " Extracting file..."
		body := lipgloss.Place(contentWidth, contentHeight, lipgloss.Center, lipgloss.Center, msg)
		return renderPanel(body, "FILE VIEWER", true, contentWidth, height)
	}

	if content == nil {
		return renderPanel("", "FILE VIEWER", true, contentWidth, height)
	}

	title := content.Path

	if content.Binary {
		msg := fmt.Sprintf("Binary file (%s) — cannot display", image.FormatBytes(content.Size))
		hint := "Press Esc to return"
		body := lipgloss.Place(contentWidth, contentHeight, lipgloss.Center, lipgloss.Center,
			styleWithFg(removedColor).Render(msg)+"\n\n"+styleWithFg(statusDimColor).Render(hint))
		return renderPanel(body, title, true, contentWidth, height)
	}

	if len(content.Data) == 0 {
		msg := "Empty file (0 bytes)"
		hint := "Press Esc to return"
		body := lipgloss.Place(contentWidth, contentHeight, lipgloss.Center, lipgloss.Center,
			styleWithFg(unchangedColor).Render(msg)+"\n\n"+styleWithFg(statusDimColor).Render(hint))
		return renderPanel(body, title, true, contentWidth, height)
	}

	lines := strings.Split(string(content.Data), "\n")

	viewHeight := contentHeight
	if content.Truncated {
		viewHeight--
	}

	totalLines := len(lines)
	gutterWidth := len(fmt.Sprintf("%d", totalLines)) + 1

	var sb strings.Builder
	end := offset + viewHeight
	if end > totalLines {
		end = totalLines
	}
	if offset > totalLines {
		offset = totalLines
	}
	if offset > end {
		offset = end
	}
	visible := lines[offset:end]

	for i, line := range visible {
		lineNum := offset + i + 1
		gutter := styleWithFg(unchangedColor).Render(fmt.Sprintf("%*d ", gutterWidth, lineNum))

		maxLineWidth := contentWidth - gutterWidth - 1
		if maxLineWidth < 1 {
			maxLineWidth = 1
		}
		if len([]rune(line)) > maxLineWidth {
			line = string([]rune(line)[:maxLineWidth-1]) + "…"
		}

		sb.WriteString(gutter + styleWithFg(fileNameColor).Render(line))
		if i < len(visible)-1 {
			sb.WriteString("\n")
		}
	}

	rendered := len(visible)
	for i := rendered; i < viewHeight; i++ {
		sb.WriteString("\n")
	}

	if content.Truncated {
		notice := fmt.Sprintf("  File truncated at 1 MB (total: %s)", image.FormatBytes(content.Size))
		sb.WriteString("\n")
		sb.WriteString(styleWithFg(modifiedColor).Render(notice))
	}

	return renderPanel(sb.String(), title, true, contentWidth, height)
}

func fileViewLineCount(content *image.FileContent) int {
	if content == nil || content.Binary || len(content.Data) == 0 {
		return 0
	}
	return strings.Count(string(content.Data), "\n") + 1
}
