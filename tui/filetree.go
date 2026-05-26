package tui

import (
	"fmt"
	"image/color"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/deveshctl/layerx/image"
)

func renderFileTree(files []*image.FileNode, cursor, offset int, width, height int, focused bool, filterActive bool, filterQuery string, treeMode bool, collapsed map[string]bool, currentLayer int) string {
	contentWidth := width - 2
	contentHeight := height

	showFilterBar := filterActive || filterQuery != ""
	if showFilterBar {
		contentHeight--
	}

	contentHeight--

	var sb strings.Builder

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
		end := min(offset+contentHeight, len(files))
		visible := files[offset:end]

		for i, f := range visible {
			line := formatFileNodeLine(f, offset+i == cursor, contentWidth, treeMode, collapsed, currentLayer, filterQuery)
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

	title := "File Tree"
	if len(files) > 0 {
		title = fmt.Sprintf("File Tree %d/%d", cursor+1, len(files))
	} else if filterQuery != "" {
		title = "File Tree 0/0"
	}

	hasAbove := offset > 0
	end := min(offset+contentHeight, len(files))
	hasBelow := end < len(files)

	// renderPanel paints the ▾ scroll indicator on the right border at row
	// height-1. When the filter bar is shown it occupies that row, so the
	// ▾ would overwrite the filter-bar border. Suppress it; the title's
	// match-count and natural cursor advancement already signal "more below"
	// once the user starts navigating.
	if showFilterBar {
		hasBelow = false
	}

	content := sb.String()
	return renderPanel(content, title, focused, contentWidth, height, hasAbove, hasBelow)
}

func renderTreeHeader(maxWidth int) string {
	const permCol = 10
	const uidGidCol = 8
	const sizeCol = 8
	const colGap = 2
	const diffGlyphCol = 2
	const minNameSpace = 8

	fullMeta := diffGlyphCol + permCol + uidGidCol + sizeCol + colGap*3
	sizeMeta := diffGlyphCol + sizeCol + colGap
	showPerms := maxWidth >= fullMeta+minNameSpace
	showSize := maxWidth >= sizeMeta+minNameSpace

	var header string
	if showPerms {
		perms := padRight("Permission", permCol)
		uidGid := padRight("UID:GID", uidGidCol)
		size := padLeft("Size", sizeCol)
		header = "  " + perms + strings.Repeat(" ", colGap) + uidGid + strings.Repeat(" ", colGap) + size + strings.Repeat(" ", colGap) + "Filetree"
	} else if showSize {
		size := padLeft("Size", sizeCol)
		header = "  " + size + strings.Repeat(" ", colGap) + "Filetree"
	} else {
		header = "  Filetree"
	}

	if len(header) > maxWidth {
		header = header[:maxWidth]
	}
	return styleWithFg(metaDimColor).Render(header)
}

func renderFilterBar(active bool, query string, matchCount int, maxWidth int) string {
	if active {
		prefix := styleWithFg(accentColor).Render("/ ")
		cursor := query + "█"
		return prefix + cursor
	}
	prefix := styleWithFg(accentColor).Render("/ ")
	queryStr := styleWithFg(selectedColor).Render(query)
	matches := styleWithFg(statusDimColor).Render(fmt.Sprintf("  (%d matches)", matchCount))
	hint := styleWithFg(unchangedColor).Render("  [⌫ clear]")

	line := prefix + queryStr + matches + hint
	lineWidth := lipgloss.Width(line)
	if lineWidth > maxWidth {
		line = prefix + queryStr + matches
	}
	return line
}

func formatFileNodeLine(f *image.FileNode, selected bool, maxWidth int, treeMode bool, collapsed map[string]bool, currentLayer int, filterQuery string) string {
	perms := image.FormatMode(f.Mode)
	uidGid := fmt.Sprintf("%d:%d", f.UID, f.GID)
	flat := !treeMode
	size := formatSizeForNode(f, flat)

	const permCol = 10
	const uidGidCol = 8
	const sizeCol = 8
	const colGap = 2
	const diffGlyphCol = 2

	// Determine which metadata columns fit alongside a minimum 8-char filename.
	const minNameSpace = 8
	fullMeta := diffGlyphCol + permCol + uidGidCol + sizeCol + colGap*3
	sizeMeta := diffGlyphCol + sizeCol + colGap
	showPerms := maxWidth >= fullMeta+minNameSpace
	showSize := maxWidth >= sizeMeta+minNameSpace

	var metaWidth int
	if showPerms {
		metaWidth = fullMeta
	} else if showSize {
		metaWidth = sizeMeta
	} else {
		metaWidth = diffGlyphCol
	}

	nameSpace := max(maxWidth-metaWidth, 4)

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
			if treeMode {
				glyph := "▾ "
				if isCollapsed(collapsed, f.Path) {
					glyph = "▸ "
				}
				displayName = glyph + displayName + "/"
			} else {
				displayName += "/"
			}
		}
		depth := nodeIndent(f)
		treePrefix = buildTreePrefix(depth)
	}

	var originSuffix string
	showOrigin := !f.IsDir && f.DiffType != image.Added && f.IntroducedInLayer != currentLayer
	if showOrigin {
		originSuffix = fmt.Sprintf(" (L%d)", f.IntroducedInLayer)
	}

	fullName := treePrefix + displayName
	nameRunes := []rune(fullName)
	availableForName := nameSpace - len([]rune(originSuffix))
	if availableForName < 4 {
		availableForName = 4
		originSuffix = ""
		showOrigin = false
	}
	if len(nameRunes) > availableForName {
		fullName = string(nameRunes[:availableForName-1]) + "…"
	}

	nameWidth := len([]rune(fullName)) + len([]rune(originSuffix))
	namePad := max(nameSpace-nameWidth, 0)

	var diffGlyph string
	switch f.DiffType {
	case image.Added:
		diffGlyph = styleWithFg(addedColor).Render("+ ")
	case image.Modified:
		diffGlyph = styleWithFg(modifiedColor).Render("~ ")
	case image.Removed:
		diffGlyph = styleWithFg(removedColor).Render("- ")
	default:
		diffGlyph = "  "
	}

	if selected {
		var selGlyph string
		switch f.DiffType {
		case image.Added:
			selGlyph = "+ "
		case image.Modified:
			selGlyph = "~ "
		case image.Removed:
			selGlyph = "- "
		default:
			selGlyph = "  "
		}
		var metaCols string
		if showPerms {
			permStr := padRight(perms, permCol)
			uidStr := padRight(uidGid, uidGidCol)
			sizeStr := padLeft(size, sizeCol)
			metaCols = permStr + strings.Repeat(" ", colGap) + uidStr + strings.Repeat(" ", colGap) + sizeStr + strings.Repeat(" ", colGap)
		} else if showSize {
			sizeStr := padLeft(size, sizeCol)
			metaCols = sizeStr + strings.Repeat(" ", colGap)
		}
		fullLine := selGlyph + metaCols + fullName + originSuffix + strings.Repeat(" ", namePad)
		return lipgloss.NewStyle().Foreground(selectedColor).Background(selectedBgColor).Render(fullLine)
	}

	var metaCols string
	if showPerms {
		permStr := styleWithFg(metaDimColor).Render(padRight(perms, permCol))
		uidStr := styleWithFg(metaDimColor).Render(padRight(uidGid, uidGidCol))
		sizeStr := styleWithFg(headerDimColor).Render(padLeft(size, sizeCol))
		gap := strings.Repeat(" ", colGap)
		metaCols = permStr + gap + uidStr + gap + sizeStr + gap
	} else if showSize {
		sizeStr := styleWithFg(headerDimColor).Render(padLeft(size, sizeCol))
		metaCols = sizeStr + strings.Repeat(" ", colGap)
	}

	var nameRendered string
	prefixRuneLen := len([]rune(treePrefix))
	fullNameRuneLen := len([]rune(fullName))
	wasTruncated := len(nameRunes) > availableForName

	if flat || (wasTruncated && prefixRuneLen >= fullNameRuneLen) {
		nameRendered = renderNameWithHighlight(fullName, filterQuery, diffColorForNode(f))
	} else {
		fullRunes := []rune(fullName)
		var nameOnly string
		if prefixRuneLen < len(fullRunes) {
			nameOnly = string(fullRunes[prefixRuneLen:])
		}
		treePrefixRendered := styleWithFg(treeDimColor).Render(treePrefix)
		nameOnlyRendered := renderNameWithHighlight(nameOnly, filterQuery, diffColorForNode(f))
		nameRendered = treePrefixRendered + nameOnlyRendered
	}

	var originRendered string
	if showOrigin {
		originRendered = styleWithFg(metaDimColor).Render(originSuffix)
	}

	nameRenderedWidth := lipgloss.Width(nameRendered) + lipgloss.Width(originRendered)
	diffGlyphWidth := lipgloss.Width(diffGlyph)
	actualNamePad := max(nameSpace-nameRenderedWidth+diffGlyphWidth-2, 0)

	fullLine := diffGlyph + metaCols + nameRendered + originRendered + strings.Repeat(" ", actualNamePad)

	return fullLine
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
		return ""
	}
	return strings.Repeat("  ", depth)
}

// padRight measures the string by display width (lipgloss.Width — handles
// CJK, emoji, ANSI sequences) rather than byte length, so non-ASCII content
// pads correctly. Truncation uses ansi.Truncate which respects rune
// boundaries; byte-slicing here would emit half-runes and corrupt the
// terminal renderer.
func padRight(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return ansi.Truncate(s, width, "")
	}
	return s + strings.Repeat(" ", width-w)
}

func padLeft(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return ansi.Truncate(s, width, "")
	}
	return strings.Repeat(" ", width-w) + s
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
	sizes := make(map[*image.FileNode]int64, len(files))
	for _, f := range files {
		sizes[f] = nodeEffectiveSize(f)
	}
	sorted := make([]*image.FileNode, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool {
		si, sj := sizes[sorted[i]], sizes[sorted[j]]
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

func diffColorForNode(f *image.FileNode) color.Color {
	switch f.DiffType {
	case image.Added:
		return addedColor
	case image.Modified:
		return modifiedColor
	case image.Removed:
		return removedColor
	default:
		return fileNameColor
	}
}

func renderNameWithHighlight(name, query string, fg color.Color) string {
	if query == "" || name == "" {
		return styleWithFg(fg).Render(name)
	}
	lowerName := strings.ToLower(name)
	lowerQuery := strings.ToLower(query)
	if !strings.Contains(lowerName, lowerQuery) {
		return styleWithFg(fg).Render(name)
	}
	runes := []rune(name)
	lowerRunes := []rune(lowerName)
	queryRunes := []rune(lowerQuery)
	runeIdx := -1
	for i := 0; i <= len(lowerRunes)-len(queryRunes); i++ {
		if string(lowerRunes[i:i+len(queryRunes)]) == string(queryRunes) {
			runeIdx = i
			break
		}
	}
	if runeIdx < 0 {
		return styleWithFg(fg).Render(name)
	}
	before := string(runes[:runeIdx])
	match := string(runes[runeIdx : runeIdx+len(queryRunes)])
	after := string(runes[runeIdx+len(queryRunes):])

	normal := styleWithFg(fg)
	highlight := lipgloss.NewStyle().Foreground(fg).Background(searchHighlightBg)
	return normal.Render(before) + highlight.Render(match) + normal.Render(after)
}
