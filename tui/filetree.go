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

	// Reserve a line for the column header.
	contentHeight--

	var sb strings.Builder

	// Column header
	sb.WriteString(renderTreeHeader(contentWidth))
	sb.WriteString("\n")

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

func renderTreeHeader(maxWidth int) string {
	const permCol = 10
	const uidGidCol = 8
	const sizeCol = 8
	const colGap = 2

	perms := padRight("Permission", permCol)
	uidGid := padRight("UID:GID", uidGidCol)
	size := padLeft("Size", sizeCol)
	tree := "Filetree"

	header := perms + strings.Repeat(" ", colGap) + uidGid + strings.Repeat(" ", colGap) + size + strings.Repeat(" ", colGap) + tree
	if len(header) > maxWidth {
		header = header[:maxWidth]
	}
	return styleWithFg(headerDimColor).Render(header)
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
	// Column layout: "Permissions  UID:GID  Size  TreePrefix Name"
	// Permissions: 10 chars fixed
	// UID:GID: variable (typically 3-7 chars)
	// Size: right-aligned, up to 8 chars
	// TreePrefix + Name: remainder

	perms := image.FormatMode(f.Mode)
	uidGid := fmt.Sprintf("%d:%d", f.UID, f.GID)
	size := formatSizeForNode(f, flat)

	// Fixed column widths
	const permCol = 10
	const uidGidCol = 8
	const sizeCol = 8
	const colGap = 2

	// Build metadata columns
	permStr := padRight(perms, permCol)
	uidStr := padRight(uidGid, uidGidCol)
	sizeStr := padLeft(size, sizeCol)

	metaCols := permStr + strings.Repeat(" ", colGap) + uidStr + strings.Repeat(" ", colGap) + sizeStr + strings.Repeat(" ", colGap)
	metaWidth := permCol + uidGidCol + sizeCol + colGap*3

	// Name portion with tree prefix
	nameSpace := maxWidth - metaWidth
	if nameSpace < 4 {
		nameSpace = 4
	}

	var displayName string
	var treePrefix string
	if flat {
		displayName = f.Path
		if len(displayName) > 1 && displayName[0] == '/' {
			displayName = displayName[1:]
		}
		treePrefix = ""
	} else {
		displayName = f.Name
		if f.IsDir {
			displayName += "/"
		}
		depth := nodeIndent(f)
		treePrefix = buildTreePrefix(depth)
	}

	fullName := treePrefix + displayName
	nameRunes := []rune(fullName)
	if len(nameRunes) > nameSpace {
		fullName = string(nameRunes[:nameSpace-1]) + "…"
	}

	// Pad name to fill remaining width
	nameWidth := len([]rune(fullName))
	namePad := nameSpace - nameWidth
	if namePad < 0 {
		namePad = 0
	}

	fullLine := metaCols + fullName + strings.Repeat(" ", namePad)

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

func formatSizeForNode(f *image.FileNode, flat bool) string {
	if !f.IsDir {
		if f.Size > 0 {
			return image.FormatBytes(f.Size)
		}
		return "0 B"
	}
	if flat {
		sz := nodeEffectiveSize(f)
		if sz > 0 {
			return image.FormatBytes(sz)
		}
	}
	return ""
}

func buildTreePrefix(depth int) string {
	if depth == 0 {
		return "├── "
	}
	var sb strings.Builder
	for i := 0; i < depth; i++ {
		sb.WriteString("│   ")
	}
	sb.WriteString("├── ")
	return sb.String()
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

func padLeft(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return strings.Repeat(" ", width-len(s)) + s
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
