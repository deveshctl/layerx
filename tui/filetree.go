package tui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/deveshpharswan/layerx/image"
)

func renderFileTree(files []*image.FileNode, cursor, offset int, width, height int, focused bool, filterActive bool, filterQuery string, sm sortMode) string {
	contentWidth := width - 2
	contentHeight := height

	// Reserve a line for the filter bar whenever a filter is active or has a query.
	showFilterBar := filterActive || filterQuery != ""
	if showFilterBar {
		contentHeight--
	}

	var sb strings.Builder

	if len(files) == 0 {
		msg := "(no filesystem changes)"
		if filterQuery != "" {
			msg = "(no matches)"
		}
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
			line := formatFileNodeLine(f, offset+i == cursor, contentWidth, sm != sortNone)
			sb.WriteString(line)
			if i < len(visible)-1 {
				sb.WriteString("\n")
			}
		}

		rendered := len(visible)
		for i := rendered; i < contentHeight; i++ {
			sb.WriteString("\n")
		}
	}

	if showFilterBar {
		sb.WriteString("\n")
		sb.WriteString(renderFilterBar(filterActive, filterQuery, len(files), contentWidth))
	}

	title := "FILE TREE"
	if len(files) > 0 {
		title = fmt.Sprintf("FILE TREE [%d/%d]", cursor+1, len(files))
	} else if filterQuery != "" {
		title = "FILE TREE [0/0]"
	}

	content := sb.String()
	return renderPanel(content, title, focused, contentWidth, height)
}

func renderFilterBar(active bool, query string, matchCount int, maxWidth int) string {
	if active {
		prefix := styleWithFg(accentColor).Render("/ ")
		cursor := query + "█"
		return prefix + cursor
	}
	// Persistent read-only indicator when filter is set but input closed.
	prefix := styleWithFg(accentColor).Render("/ ")
	queryStr := styleWithFg(selectedColor).Render(query)
	matches := styleWithFg(statusDimColor).Render(fmt.Sprintf("  (%d matches)", matchCount))
	hint := styleWithFg(unchangedColor).Render("  [Enter clear]")

	line := prefix + queryStr + matches + hint
	lineWidth := lipgloss.Width(line)
	if lineWidth > maxWidth {
		line = prefix + queryStr + matches
	}
	return line
}

func formatFileNodeLine(f *image.FileNode, selected bool, maxWidth int, flat bool) string {
	indentLevel := 0
	displayName := f.Name
	if flat {
		displayName = f.Path
		if len(displayName) > 1 && displayName[0] == '/' {
			displayName = displayName[1:]
		}
	} else {
		indentLevel = nodeIndent(f)
	}
	indent := strings.Repeat("  ", indentLevel)

	if f.IsDir && !flat {
		displayName += "/"
	}

	prefixWidth := 2
	rightPart := ""
	if !f.IsDir && f.Size > 0 {
		rightPart = image.FormatBytes(f.Size)
	} else if f.IsDir && flat {
		sz := nodeEffectiveSize(f)
		if sz > 0 {
			rightPart = image.FormatBytes(sz)
		}
	}
	rightLen := len(rightPart)

	nameSpace := maxWidth - prefixWidth - len(indent) - 1 - rightLen
	if nameSpace < 4 {
		nameSpace = 4
	}
	nameRunes := []rune(displayName)
	if len(nameRunes) > nameSpace {
		displayName = string(nameRunes[:nameSpace-2]) + ".."
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

	leftPart := prefix + indent + displayName
	leftLen := prefixWidth + len(indent) + len([]rune(displayName))
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

func applyDiffFilter(files []*image.FileNode) []*image.FileNode {
	var result []*image.FileNode
	for _, f := range files {
		if f.DiffType != image.Unchanged {
			result = append(result, f)
		}
	}
	return result
}

func applySubstringFilter(files []*image.FileNode, query string) []*image.FileNode {
	if query == "" {
		return files
	}
	lower := strings.ToLower(query)
	var result []*image.FileNode
	for _, f := range files {
		if strings.Contains(strings.ToLower(f.Path), lower) {
			result = append(result, f)
		}
	}
	return result
}

func applySortBySize(files []*image.FileNode, mode sortMode) []*image.FileNode {
	if mode == sortNone {
		return files
	}
	sorted := make([]*image.FileNode, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool {
		si := nodeEffectiveSize(sorted[i])
		sj := nodeEffectiveSize(sorted[j])
		if mode == sortDesc {
			return si > sj
		}
		return si < sj
	})
	return sorted
}

func nodeEffectiveSize(n *image.FileNode) int64 {
	if !n.IsDir {
		return n.Size
	}
	var total int64
	var walk func(*image.FileNode)
	walk = func(node *image.FileNode) {
		if !node.IsDir {
			total += node.Size
		}
		for _, c := range node.Children {
			walk(c)
		}
	}
	walk(n)
	return total
}
