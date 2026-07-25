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

func renderFileTree(t Theme, files []*image.FileNode, cursor, offset int, width, height int, focused bool, filterActive bool, filterQuery string, treeMode bool, aggregated bool, collapsed map[string]bool, currentLayer int) string {
	contentWidth := width - 2

	body, hasAbove, hasBelow := renderTreeBody(treePaneInput{
		theme:        t,
		files:        files,
		cursor:       cursor,
		offset:       offset,
		contentWidth: contentWidth,
		bodyHeight:   height,
		showFilterBar: filterActive || filterQuery != "",
		filterActive: filterActive,
		filterQuery:  filterQuery,
		treeMode:     treeMode,
		collapsed:    collapsed,
		currentLayer: currentLayer,
		showHeader:   true,
	})

	title := "Current Layer Contents"
	if aggregated {
		title = "Aggregated Layer Contents"
	}
	if len(files) > 0 {
		title = fmt.Sprintf("%s %d/%d", title, cursor+1, len(files))
	} else if filterQuery != "" {
		title = title + " 0/0"
	}

	return renderPanel(t, body, title, focused, contentWidth, height, hasAbove, hasBelow)
}

type treePaneInput struct {
	theme         Theme
	files         []*image.FileNode
	cursor        int
	offset        int
	contentWidth  int
	bodyHeight    int
	showFilterBar bool
	filterActive  bool
	filterQuery   string
	treeMode      bool
	collapsed     map[string]bool
	currentLayer  int
	// showHeader prepends the column header row when true. The split renderer
	// shows it on every sub-pane so the metadata columns line up regardless
	// of which pane has focus.
	showHeader bool
	// emptyMsg overrides the default "(no filesystem changes)" placeholder
	// shown when files is empty. The aggregated sub-pane uses
	// "(no entries at this layer)" because the cumulative tree at L0 with
	// nothing yet would otherwise misleadingly read as "no changes".
	emptyMsg string
}

func renderTreeBody(in treePaneInput) (body string, hasAbove, hasBelow bool) {
	contentHeight := in.bodyHeight
	if in.showFilterBar {
		contentHeight--
	}
	if in.showHeader {
		contentHeight--
	}
	if contentHeight < 0 {
		contentHeight = 0
	}

	var sb strings.Builder

	if in.showHeader {
		sb.WriteString(renderTreeHeader(in.theme, in.contentWidth))
		sb.WriteString("\n")
	}

	if len(in.files) == 0 {
		msg := in.emptyMsg
		if msg == "" {
			msg = "(no filesystem changes)"
		}
		if in.filterQuery != "" {
			msg = "(no matches)"
		}
		pad := ""
		if in.contentWidth > len(msg) {
			pad = strings.Repeat(" ", (in.contentWidth-len(msg))/2)
		}
		midpoint := contentHeight / 2
		for i := 0; i < contentHeight; i++ {
			if i == midpoint {
				sb.WriteString(pad)
				sb.WriteString(styleWithFg(in.theme.Unchanged).Render(msg))
			}
			if i < contentHeight-1 {
				sb.WriteString("\n")
			}
		}
	} else {
		if in.offset > len(in.files) {
			in.offset = len(in.files)
		}
		end := max(min(in.offset+contentHeight, len(in.files)), in.offset)
		visible := in.files[in.offset:end]

		for i, f := range visible {
			line := formatFileNodeLine(in.theme, f, in.offset+i == in.cursor, in.contentWidth, in.treeMode, in.collapsed, in.currentLayer, in.filterQuery)
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

	if in.showFilterBar {
		sb.WriteString("\n")
		sb.WriteString(renderFilterBar(in.theme, in.filterActive, in.filterQuery, len(in.files), in.contentWidth))
	}

	hasAbove = in.offset > 0
	end := min(in.offset+contentHeight, len(in.files))
	hasBelow = end < len(in.files)
	if in.showFilterBar {
		// The filter bar occupies the panel's bottom border row, so a ▾
		// indicator there would collide with the bar. The title's match
		// counter signals the rest already.
		hasBelow = false
	}
	return sb.String(), hasAbove, hasBelow
}

type splitTreeInput struct {
	theme         Theme
	width, height int
	currentLayer  int
	treeMode      bool

	// Top sub-pane: per-layer Δ from StackedTrees.
	topFiles    []*image.FileNode
	topCursor   int
	topOffset   int
	topFocused  bool
	topCollapsed map[string]bool

	// Bottom sub-pane: cumulative provenance from AggregatedTrees.
	botFiles    []*image.FileNode
	botCursor   int
	botOffset   int
	botFocused  bool
	botCollapsed map[string]bool

	// Filter is shared across panes (same query applies to both); but the
	// filter bar is drawn under whichever pane has focus so the user sees
	// what they're typing without doubling the chrome.
	filterActive bool
	filterQuery  string
}

// renderSplitFileTree paints the file-tree panel in split mode: the top
// half shows the per-layer Δ tree (StackedTrees), the bottom half shows
// the cumulative tree (AggregatedTrees). The two halves are separated by
// a labelled divider so the eye reads top→bottom as "Δ" → "cumulative".
//
// The outer panel border is one box; the focused-state border colour
// follows whichever sub-pane currently owns focus, but only the
// active-pane content displays the bright accent cursor. The split
// renderer takes the place of two independent renderFileTree calls
// stacked by JoinVertical because a single shared border keeps the
// horizontal alignment with the layers panel and the file viewer
// pixel-perfect.
func renderSplitFileTree(in splitTreeInput) string {
	contentWidth := in.width - 2
	// in.height is the number of content rows the outer panel exposes
	// (renderPanel adds the two border rows on top of it). Match the
	// signature of renderFileTree, which is passed the same value.
	totalContent := max(in.height, 4)

	topRows, botRows := splitPanelRows(totalContent)

	topBody, topAbove, topBelow := renderTreeBody(treePaneInput{
		theme:         in.theme,
		files:         in.topFiles,
		cursor:        in.topCursor,
		offset:        in.topOffset,
		contentWidth:  contentWidth,
		bodyHeight:    topRows,
		showFilterBar: (in.filterActive || in.filterQuery != "") && in.topFocused,
		filterActive:  in.filterActive,
		filterQuery:   in.filterQuery,
		treeMode:      in.treeMode,
		collapsed:     in.topCollapsed,
		currentLayer:  in.currentLayer,
		showHeader:    true,
		emptyMsg:      "(no changes in this layer)",
	})

	botBody, botAbove, botBelow := renderTreeBody(treePaneInput{
		theme:         in.theme,
		files:         in.botFiles,
		cursor:        in.botCursor,
		offset:        in.botOffset,
		contentWidth:  contentWidth,
		bodyHeight:    botRows,
		showFilterBar: (in.filterActive || in.filterQuery != "") && in.botFocused,
		filterActive:  in.filterActive,
		filterQuery:   in.filterQuery,
		treeMode:      in.treeMode,
		collapsed:     in.botCollapsed,
		currentLayer:  in.currentLayer,
		showHeader:    true,
		emptyMsg:      "(no entries at this layer)",
	})

	divider := renderSplitDivider(in.theme, in.botFocused, contentWidth, in.botFiles, in.botCursor)

	body := topBody + "\n" + divider + "\n" + botBody

	title := buildSplitTitle(in)

	focused := in.topFocused || in.botFocused
	hasAbove := topAbove || botAbove
	hasBelow := topBelow || botBelow
	return renderPanel(in.theme, body, title, focused, contentWidth, in.height, hasAbove, hasBelow)
}

// renderSplitDivider draws the horizontal separator between the two
// sub-panes. The bottom pane's section label sits in the divider with a
// match-count and a focus-weight background when that pane has focus.
// This places the "▾ Cumulative" affordance on a row that would otherwise
// be wasted whitespace.
func renderSplitDivider(t Theme, botFocused bool, contentWidth int, botFiles []*image.FileNode, botCursor int) string {
	label := " ▾ Cumulative "
	if botFocused && len(botFiles) > 0 {
		label = fmt.Sprintf(" ▾ Cumulative %d/%d ", botCursor+1, len(botFiles))
	} else if len(botFiles) > 0 {
		label = fmt.Sprintf(" ▾ Cumulative · %d items ", len(botFiles))
	}

	labelStyle := lipgloss.NewStyle().Foreground(t.Unchanged)
	lineStyle := lipgloss.NewStyle().Foreground(t.Separator)
	if botFocused {
		labelStyle = lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
		lineStyle = lipgloss.NewStyle().Foreground(t.Accent)
	}

	rendered := labelStyle.Render(label)
	labelW := lipgloss.Width(rendered)
	rest := max(contentWidth-labelW, 0)
	return rendered + lineStyle.Render(strings.Repeat("─", rest))
}

// buildSplitTitle assembles "Layer Δ N/M  ·  Cumulative" with the focused
// half emphasised. The bottom-pane match counter is duplicated in the
// divider for visibility, but the title's purpose is to advertise the
// split and let the user see at a glance which view has focus.
func buildSplitTitle(in splitTreeInput) string {
	topPart := "Layer Δ"
	if len(in.topFiles) > 0 {
		topPart = fmt.Sprintf("Layer Δ %d/%d", in.topCursor+1, len(in.topFiles))
	} else if in.filterQuery != "" {
		topPart += " 0/0"
	}
	botPart := "Cumulative"
	if len(in.botFiles) > 0 {
		botPart = fmt.Sprintf("Cumulative %d/%d", in.botCursor+1, len(in.botFiles))
	} else if in.filterQuery != "" {
		botPart += " 0/0"
	}
	if in.topFocused {
		return topPart + "  ·  " + botPart
	}
	if in.botFocused {
		return topPart + "  ·  " + botPart
	}
	return topPart + "  ·  " + botPart
}

func renderTreeHeader(t Theme, maxWidth int) string {
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

	if lipgloss.Width(header) > maxWidth {
		header = ansi.Truncate(header, maxWidth, "")
	}
	return styleWithFg(t.MetaDim).Render(header)
}

func renderFilterBar(t Theme, active bool, query string, matchCount int, maxWidth int) string {
	if active {
		prefix := styleWithFg(t.Accent).Render("/ ")
		cursor := query + "█"
		return prefix + cursor
	}
	prefix := styleWithFg(t.Accent).Render("/ ")
	queryStr := styleWithFg(t.Selected).Render(query)
	matches := styleWithFg(t.StatusDim).Render(fmt.Sprintf("  (%d matches)", matchCount))
	hint := styleWithFg(t.Unchanged).Render("  [⌫ clear]")

	line := prefix + queryStr + matches + hint
	lineWidth := lipgloss.Width(line)
	if lineWidth > maxWidth {
		line = prefix + queryStr + matches
	}
	return line
}

func formatFileNodeLine(t Theme, f *image.FileNode, selected bool, maxWidth int, treeMode bool, collapsed map[string]bool, currentLayer int, filterQuery string) string {
	perms := image.FormatMode(f.Mode)
	uidGid := fmt.Sprintf("%d:%d", f.UID, f.GID)
	flat := !treeMode
	size := formatSizeForNode(f, flat)

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
	availableForName := nameSpace - lipgloss.Width(originSuffix)
	if availableForName < 4 {
		availableForName = 4
		originSuffix = ""
		showOrigin = false
	}
	wasTruncated := lipgloss.Width(fullName) > availableForName
	if wasTruncated {
		fullName = ansi.Truncate(fullName, availableForName, "…")
	}

	nameWidth := lipgloss.Width(fullName) + lipgloss.Width(originSuffix)
	namePad := max(nameSpace-nameWidth, 0)

	var diffGlyph string
	switch f.DiffType {
	case image.Added:
		diffGlyph = styleWithFg(t.Added).Render("+ ")
	case image.Modified:
		diffGlyph = styleWithFg(t.Modified).Render("~ ")
	case image.Removed:
		diffGlyph = styleWithFg(t.Removed).Render("- ")
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
		return lipgloss.NewStyle().Foreground(t.Selected).Background(t.SelectedBg).Render(fullLine)
	}

	var metaCols string
	if showPerms {
		permStr := styleWithFg(t.MetaDim).Render(padRight(perms, permCol))
		uidStr := styleWithFg(t.MetaDim).Render(padRight(uidGid, uidGidCol))
		sizeStr := styleWithFg(t.HeaderDim).Render(padLeft(size, sizeCol))
		gap := strings.Repeat(" ", colGap)
		metaCols = permStr + gap + uidStr + gap + sizeStr + gap
	} else if showSize {
		sizeStr := styleWithFg(t.HeaderDim).Render(padLeft(size, sizeCol))
		metaCols = sizeStr + strings.Repeat(" ", colGap)
	}

	var nameRendered string
	prefixRuneLen := len([]rune(treePrefix))
	fullNameRuneLen := len([]rune(fullName))

	if flat || (wasTruncated && prefixRuneLen >= fullNameRuneLen) {
		nameRendered = renderNameWithHighlight(t, fullName, filterQuery, diffColorForNode(t, f))
	} else {
		fullRunes := []rune(fullName)
		var nameOnly string
		if prefixRuneLen < len(fullRunes) {
			nameOnly = string(fullRunes[prefixRuneLen:])
		}
		treePrefixRendered := styleWithFg(t.TreeDim).Render(treePrefix)
		nameOnlyRendered := renderNameWithHighlight(t, nameOnly, filterQuery, diffColorForNode(t, f))
		nameRendered = treePrefixRendered + nameOnlyRendered
	}

	var originRendered string
	if showOrigin {
		originRendered = styleWithFg(t.MetaDim).Render(originSuffix)
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
	if n.DiffType == image.Removed {
		return 0
	}
	if !n.IsDir {
		return n.Size
	}
	var total int64
	var walk func(*image.FileNode)
	walk = func(node *image.FileNode) {
		if node.DiffType == image.Removed {
			return
		}
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

func diffColorForNode(t Theme, f *image.FileNode) color.Color {
	switch f.DiffType {
	case image.Added:
		return t.Added
	case image.Modified:
		return t.Modified
	case image.Removed:
		return t.Removed
	default:
		return t.FileName
	}
}

func renderNameWithHighlight(t Theme, name, query string, fg color.Color) string {
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
	highlight := lipgloss.NewStyle().Foreground(fg).Background(t.SearchHighlightBg)
	return normal.Render(before) + highlight.Render(match) + normal.Render(after)
}
