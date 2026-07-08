package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/deveshctl/layerx/image"
)

type viewerParams struct {
	content      *image.FileContent
	offset       int
	hOffset      int
	cursorCol    int
	width        int
	height       int
	loading      bool
	spinnerFrame int
	originLayer  int
	originCmd    string
	currentLayer int
	searchQuery  string
	searchMatches [][2]int
	searchCursor int
	searchActive bool
	// highlightedLines is the cached chroma output for content.Data, or nil if
	// highlighting hasn't been computed yet or is unavailable. When non-nil and
	// no search is active, it is used directly to skip per-frame chroma work.
	highlightedLines []string
}

func renderFileView(p viewerParams) string {
	contentWidth := p.width - 2
	contentHeight := p.height

	if p.loading {
		frame := spinnerFrames[p.spinnerFrame%len(spinnerFrames)]
		msg := frame + " Extracting file…"
		body := lipgloss.Place(contentWidth, contentHeight, lipgloss.Center, lipgloss.Center, msg)
		return renderPanel(body, "File Viewer", true, contentWidth, p.height, false, false)
	}

	if p.content == nil {
		return renderPanel("", "File Viewer", true, contentWidth, p.height, false, false)
	}

	title := p.content.Path
	if p.originLayer != p.currentLayer && p.originCmd != "" {
		cmd := p.originCmd
		if lipgloss.Width(cmd) > 40 {
			cmd = ansi.Truncate(cmd, 40, "…")
		}
		title = fmt.Sprintf("%s  ← L%d: %s", p.content.Path, p.originLayer, cmd)
	}

	if p.content.Binary {
		msg := fmt.Sprintf("Binary file (%s) — cannot display", image.FormatBytes(p.content.Size))
		hint := "Press Esc to return"
		body := lipgloss.Place(contentWidth, contentHeight, lipgloss.Center, lipgloss.Center,
			styleWithFg(removedColor).Render(msg)+"\n\n"+styleWithFg(statusDimColor).Render(hint))
		return renderPanel(body, title, true, contentWidth, p.height, false, false)
	}

	if len(p.content.Data) == 0 {
		msg := "Empty file (0 bytes)"
		hint := "Press Esc to return"
		body := lipgloss.Place(contentWidth, contentHeight, lipgloss.Center, lipgloss.Center,
			styleWithFg(unchangedColor).Render(msg)+"\n\n"+styleWithFg(statusDimColor).Render(hint))
		return renderPanel(body, title, true, contentWidth, p.height, false, false)
	}

	lines := splitFileLines(p.content.Data)
	if lines == nil {
		lines = []string{""}
	}
	syntaxHighlight := p.searchQuery == ""
	if syntaxHighlight {
		if p.highlightedLines != nil {
			lines = p.highlightedLines
		} else {
			syntaxHighlight = false
		}
	}

	viewHeight := contentHeight
	if p.content.Truncated {
		viewHeight--
	}
	showSearchBar := p.searchActive || p.searchQuery != ""
	if showSearchBar {
		viewHeight--
	}

	totalLines := len(lines)
	gutterDigits := len(fmt.Sprintf("%d", totalLines)) + 1

	var sb strings.Builder
	end := min(p.offset+viewHeight, totalLines)
	if p.offset > totalLines {
		p.offset = totalLines
	}
	if p.offset > end {
		p.offset = end
	}
	visible := lines[p.offset:end]

	for i, line := range visible {
		lineNum := p.offset + i + 1
		lineIdx := p.offset + i
		gutter := styleWithFg(metaDimColor).Render(fmt.Sprintf("%*d ", gutterDigits, lineNum))
		gutterW := ansi.StringWidth(gutter)

		maxLineWidth := max(contentWidth-gutterW, 1)
		lineContent := renderViewerLine(line, lineIdx, p.searchQuery, p.searchMatches, p.searchCursor, syntaxHighlight)

		// Cursor sits on the first visible line. Overlay it before truncation
		// so the cursor cell is preserved (or correctly clipped) by the same
		// horizontal-scroll path as the content beneath it. Off-screen cursor
		// positions still adjust hOffset in scrollViewLeft/Right, so by the
		// time we render the cursor is always within the visible window.
		if i == 0 {
			lineContent = overlayCursor(lineContent, p.cursorCol)
		}

		// Horizontal scroll: keep the styled output intact, then trim from
		// the left by display columns. ansi.TruncateLeft is grapheme-aware
		// and preserves ANSI escapes, so chroma colors and search-match
		// backgrounds carry through. The leading "«" only renders when the
		// cut actually drops content; if the line is shorter than hOffset,
		// TruncateLeft returns "" and the row is rendered blank.
		if p.hOffset > 0 {
			lineContent = ansi.TruncateLeft(lineContent, p.hOffset, "«")
		}
		if ansi.StringWidth(lineContent) > maxLineWidth {
			lineContent = ansi.Truncate(lineContent, maxLineWidth, "…")
		}

		fullLine := gutter + lineContent
		if w := ansi.StringWidth(fullLine); w > contentWidth {
			lineContent = ansi.Truncate(lineContent, contentWidth-gutterW, "…")
			fullLine = gutter + lineContent
		}
		sb.WriteString(fullLine)
		if i < len(visible)-1 {
			sb.WriteString("\n")
		}
	}

	rendered := len(visible)
	for i := rendered; i < viewHeight; i++ {
		sb.WriteString("\n")
	}

	if showSearchBar {
		sb.WriteString("\n")
		sb.WriteString(renderViewerSearchBar(p.searchQuery, p.searchActive, len(p.searchMatches), p.searchCursor, contentWidth))
	}

	if p.content.Truncated {
		notice := fmt.Sprintf("  File truncated at 1 MB (total: %s)", image.FormatBytes(p.content.Size))
		sb.WriteString("\n")
		sb.WriteString(styleWithFg(modifiedColor).Render(notice))
	}

	hasAbove := p.offset > 0
	hasBelow := end < totalLines

	return renderPanel(sb.String(), title, true, contentWidth, p.height, hasAbove, hasBelow)
}

// overlayCursor paints a reverse-video block at display column `col` of a
// styled line. ansi.Cut is grapheme- and escape-aware, so slicing the line
// into [pre | cell | post] keeps chroma colors and search highlights intact.
// When the cursor is past the end of the rendered line (cursor on a short
// line below a longer one), the line is padded with spaces so the block
// still renders at the requested column rather than collapsing onto EOL.
func overlayCursor(line string, col int) string {
	if col < 0 {
		return line
	}
	w := ansi.StringWidth(line)
	if col >= w {
		// Past EOL: pad with spaces, then place the block. The padding
		// fills any gap; the cell itself is one space rendered reversed.
		pad := strings.Repeat(" ", col-w)
		return line + pad + lipgloss.NewStyle().Reverse(true).Render(" ")
	}
	pre := ansi.Cut(line, 0, col)
	cell := ansi.Cut(line, col, col+1)
	post := ansi.Cut(line, col+1, w)
	if cell == "" {
		cell = " "
	}
	return pre + lipgloss.NewStyle().Reverse(true).Render(cell) + post
}

func renderViewerLine(line string, lineIdx int, query string, matches [][2]int, matchCursor int, syntaxHighlight bool) string {
	if query == "" || len(matches) == 0 {
		if syntaxHighlight {
			return line
		}
		return styleWithFg(fileNameColor).Render(line)
	}

	lineRunes := []rune(line)
	lowerLineRunes := []rune(strings.ToLower(line))
	queryRunes := []rune(strings.ToLower(query))
	queryLen := len(queryRunes)

	currentOccurrence := -1
	if matchCursor < len(matches) && matches[matchCursor][0] == lineIdx {
		// Count how many matches on this line precede the current one.
		count := 0
		for _, m := range matches {
			if m[0] != lineIdx {
				if m[0] > lineIdx {
					break
				}
				continue
			}
			if m[1] == matches[matchCursor][1] {
				currentOccurrence = count
				break
			}
			count++
		}
	}

	type segment struct {
		text    string
		current bool
		match   bool
	}

	var segments []segment
	pos := 0
	occurrence := 0
	for pos <= len(lowerLineRunes)-queryLen {
		idx := -1
		for i := pos; i <= len(lowerLineRunes)-queryLen; i++ {
			if string(lowerLineRunes[i:i+queryLen]) == string(queryRunes) {
				idx = i
				break
			}
		}
		if idx < 0 {
			break
		}
		matchEnd := idx + queryLen

		if idx > pos {
			segments = append(segments, segment{text: string(lineRunes[pos:idx])})
		}

		isCurrent := occurrence == currentOccurrence
		segments = append(segments, segment{text: string(lineRunes[idx:matchEnd]), match: true, current: isCurrent})
		pos = matchEnd
		occurrence++
	}
	if pos < len(lineRunes) {
		segments = append(segments, segment{text: string(lineRunes[pos:])})
	}
	if len(segments) == 0 {
		return styleWithFg(fileNameColor).Render(line)
	}

	var sb strings.Builder
	for _, seg := range segments {
		if seg.current {
			sb.WriteString(lipgloss.NewStyle().Foreground(searchCurrentFg).Background(searchCurrentBg).Render(seg.text))
		} else if seg.match {
			sb.WriteString(lipgloss.NewStyle().Foreground(fileNameColor).Background(searchHighlightBg).Render(seg.text))
		} else {
			sb.WriteString(styleWithFg(fileNameColor).Render(seg.text))
		}
	}
	return sb.String()
}

func renderViewerSearchBar(query string, active bool, matchCount, cursor, maxWidth int) string {
	prefix := styleWithFg(accentColor).Render("/ ")
	if active {
		cursorChar := styleWithFg(selectedColor).Render("█")
		queryStr := styleWithFg(selectedColor).Render(query)
		line := prefix + queryStr + cursorChar
		if matchCount > 0 {
			counter := styleWithFg(statusDimColor).Render(fmt.Sprintf("  (%d/%d)", cursor+1, matchCount))
			line += counter
		} else if query != "" {
			line += styleWithFg(statusDimColor).Render("  (no matches)")
		}
		return line
	}
	queryStr := styleWithFg(selectedColor).Render(query)
	var counter string
	if matchCount > 0 {
		counter = styleWithFg(statusDimColor).Render(fmt.Sprintf("  (%d/%d)", cursor+1, matchCount))
	} else {
		counter = styleWithFg(statusDimColor).Render("  (no matches)")
	}
	hint := styleWithFg(unchangedColor).Render("  [Esc clear]")
	line := prefix + queryStr + counter + hint
	if lipgloss.Width(line) > maxWidth {
		line = prefix + queryStr + counter
	}
	return line
}

func fileViewLineCount(content *image.FileContent) int {
	if content == nil || content.Binary || len(content.Data) == 0 {
		return 0
	}
	lines := splitFileLines(content.Data)
	if len(lines) == 0 {
		// Content was just a single trailing newline — semantically one empty line.
		return 1
	}
	return len(lines)
}

// splitFileLines normalizes CRLF→LF, strips bare CRs, drops a single
// trailing newline, and splits the content into rendered viewer lines.
// Shared by the renderer and search match indexing so both agree on line
// count and indices. Bare CR (Mac classic line endings, mixed-source
// files) would otherwise reach the terminal and reset the cursor mid-row.
func splitFileLines(data []byte) []string {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
