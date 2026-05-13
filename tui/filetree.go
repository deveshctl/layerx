package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/deveshpharswan/layerx/image"
)

func renderFileTree(files []*image.FileNode, cursor, offset int, width, height int, focused bool) string {
	contentWidth := width - 2
	contentHeight := height

	var sb strings.Builder

	if len(files) == 0 {
		msg := "(no filesystem changes)"
		pad := ""
		if contentWidth > len(msg) {
			pad = strings.Repeat(" ", (contentWidth-len(msg))/2)
		}
		midpoint := contentHeight / 2
		for i := 0; i < contentHeight; i++ {
			if i == midpoint {
				sb.WriteString(pad + styleWithFg(unchangedColor).Render(msg))
			}
			if i < contentHeight-1 {
				sb.WriteString("\n")
			}
		}
	} else {
		end := offset + contentHeight
		if end > len(files) {
			end = len(files)
		}
		visible := files[offset:end]

		for i, f := range visible {
			line := formatFileNodeLine(f, offset+i == cursor, contentWidth)
			sb.WriteString(line)
			if i < contentHeight-1 {
				sb.WriteString("\n")
			}
		}

		rendered := len(visible)
		for i := rendered; i < contentHeight; i++ {
			sb.WriteString("\n")
		}
	}

	title := "FILE TREE"
	if len(files) > 0 {
		title = fmt.Sprintf("FILE TREE [%d/%d]", cursor+1, len(files))
	}

	content := sb.String()
	return renderPanel(content, title, focused, contentWidth, contentHeight)
}

func formatFileNodeLine(f *image.FileNode, selected bool, maxWidth int) string {
	indent := strings.Repeat("  ", nodeIndent(f))

	prefixWidth := 2
	rightPart := ""
	if !f.IsDir && f.Size > 0 {
		rightPart = image.FormatBytes(f.Size)
	}
	rightLen := len(rightPart)

	nameSpace := maxWidth - prefixWidth - len(indent) - 1 - rightLen
	name := f.Name
	if f.IsDir {
		name += "/"
	}
	if nameSpace < 4 {
		nameSpace = 4
	}
	nameRunes := []rune(name)
	if len(nameRunes) > nameSpace {
		name = string(nameRunes[:nameSpace-2]) + ".."
	}

	var prefix string
	var prefixChar string
	if selected {
		prefixChar = ">"
	} else if f.DiffType != image.Unchanged {
		switch f.DiffType {
		case image.Added:
			prefixChar = "+"
		case image.Modified:
			prefixChar = "~"
		case image.Removed:
			prefixChar = "-"
		default:
			prefixChar = " "
		}
	}

	if prefixChar != "" {
		prefix = prefixChar + " "
	} else {
		prefix = "  "
	}

	leftPart := prefix + indent + name
	leftLen := prefixWidth + len(indent) + len([]rune(name))
	gap := maxWidth - leftLen - rightLen
	if gap < 1 {
		gap = 1
	}

	fullLine := leftPart + strings.Repeat(" ", gap) + rightPart

	if selected {
		return lipgloss.NewStyle().Foreground(selectedColor).Background(selectedBgColor).Render(fullLine)
	}

	switch f.DiffType {
	case image.Added:
		return styleWithFg(addedColor).Render(fullLine)
	case image.Modified:
		return styleWithFg(modifiedColor).Render(fullLine)
	case image.Removed:
		return styleWithFg(removedColor).Render(fullLine)
	default:
		return styleWithFg(fileNameColor).Render(fullLine)
	}
}
