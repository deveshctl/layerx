package tui

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/image"
	"github.com/deveshctl/layerx/internal/sizebudget"
)

const (
	compareContentMaxBytes     = 5 << 20 // 5 MiB per side, independent of MaxViewSize.
	compareContentMaxLines     = 1500
	compareContentMaxLineBytes = 4096
)

type compareContentRow struct {
	before, after         string
	beforeLine, afterLine int
}

type compareContent struct {
	path, note, metadata string
	rows                 []compareContentRow
	maxWidth             int
	maxNarrowWidth       int
}

type compareContentMsg struct {
	request int
	content *compareContent
	err     string
}

func (m compareModel) openContent() (tea.Model, tea.Cmd) {
	if m.fileCount() == 0 || m.fileIndex == nil || m.session == nil {
		return m, nil
	}
	d := m.fileDiffAt(m.fileCursor)
	m.contentOpen = true
	m.content, m.contentError = nil, ""
	m.contentCursor, m.contentOffset, m.contentHOffset = 0, 0, 0
	m.contentRequest++
	if m.contentCancel != nil {
		m.contentCancel()
	}
	ctx, cancel := context.WithCancel(m.session.ctx)
	m.contentCancel = cancel
	request := m.contentRequest
	inputs, results := m.session.inputs, m.session.results
	before, after := m.fileIndex.before[d.Path], m.fileIndex.after[d.Path]
	return m, func() tea.Msg {
		content, err := loadCompareContent(ctx, d, before, after, inputs, results)
		return compareContentMsg{request: request, content: content, err: err}
	}
}

func (m *compareModel) closeContent() {
	if m.contentCancel != nil {
		m.contentCancel()
		m.contentCancel = nil
	}
	m.contentOpen = false
	m.content = nil
	m.contentError = ""
	m.contentRequest++
}

func loadCompareContent(ctx context.Context, d image.FileDiff, before, after *image.FileNode, inputs [2]image.CompareInput, results [2]image.CompareImage) (*compareContent, string) {
	content := &compareContent{path: d.Path}
	content.metadata = compareInspectionMeta(before, results[0].Analysis, "Before") + "\n" + compareInspectionMeta(after, results[1].Analysis, "After")
	if (d.DiffType != image.Added && before == nil) || (d.DiffType != image.Removed && after == nil) {
		return nil, "file metadata is unavailable"
	}
	if d.DiffType == image.Modified && !strings.Contains(","+d.ChangeReason+",", ",size,") {
		content.note = "Metadata changed without a size change; content was not checked."
		return content, ""
	}
	for _, node := range []*image.FileNode{before, after} {
		if node != nil && (node.IsDir || node.IsHardlink || node.Linkname != "" || !node.Mode.IsRegular()) {
			content.note = "A link or non-regular file changed; inspect its metadata instead."
			return content, ""
		}
	}
	var data [2][]byte
	for side, node := range []*image.FileNode{before, after} {
		if node == nil {
			continue
		}
		if node.Size > compareContentMaxBytes {
			content.note = fmt.Sprintf("Image %c exceeds the 5 MiB text limit. Size and metadata are shown below.", 'A'+side)
			return content, ""
		}
		resolver := inputs[side].Resolver
		source, ok := resolver.(image.ExtractorSource)
		if !ok {
			return nil, fmt.Sprintf("Image %c: content extraction unavailable", 'A'+side)
		}
		extractor, ok := source.NewExtractor().(image.BoundedLayerExtractor)
		if !ok {
			return nil, fmt.Sprintf("Image %c: bounded extraction unavailable", 'A'+side)
		}
		pin := results[side].Digest
		if pin == "" {
			return nil, fmt.Sprintf("Image %c: analyzed image identity unavailable", 'A'+side)
		}
		current, err := resolver.ImageID(ctx, inputs[side].Ref)
		if err != nil || current != pin {
			return nil, fmt.Sprintf("Image %c: image changed since analysis", 'A'+side)
		}
		bytes, err := extractor.ExtractBoundedFromLayer(ctx, pin, d.Path, len(results[side].Analysis.Layers)-1, compareContentMaxBytes)
		if err != nil {
			return nil, fmt.Sprintf("Image %c: %v", 'A'+side, err)
		}
		if int64(len(bytes)) != node.Size {
			return nil, fmt.Sprintf("Image %c: extracted content is truncated or changed", 'A'+side)
		}
		current, err = resolver.ImageID(ctx, inputs[side].Ref)
		if err != nil || current != pin {
			return nil, fmt.Sprintf("Image %c: image changed during extraction", 'A'+side)
		}
		if err := validateCompareContent(bytes); err != nil {
			content.note = fmt.Sprintf("Image %c: %v. Size and metadata remain available below.", 'A'+side, err)
			return content, ""
		}
		data[side] = bytes
	}
	if err := ctx.Err(); err != nil {
		return nil, err.Error()
	}
	rows, err := compareContentDiff(data[0], data[1])
	if err != nil {
		content.note = "Text preview unavailable: " + err.Error() + ". Size and metadata remain available below."
		return content, ""
	}
	content.rows = rows
	return content, ""
}

func validateCompareContent(data []byte) error {
	if len(data) > compareContentMaxBytes {
		return fmt.Errorf("file exceeds 5 MiB")
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return fmt.Errorf("binary content cannot be shown")
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("invalid UTF-8 text")
	}
	if image.IsBinary(data) {
		return fmt.Errorf("binary content cannot be shown")
	}
	return nil
}

func compareContentLines(data []byte) ([]string, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if len(data) > compareContentMaxBytes {
		return nil, fmt.Errorf("file exceeds 5 MiB")
	}
	// Normalize CRLF and legacy CR endings for display, after the raw byte cap.
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lineCount, lineBytes := 1, 0
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if ch == '\n' {
			if i < len(text)-1 {
				lineCount++
			}
			lineBytes = 0
		} else {
			lineBytes++
		}
		if lineCount > compareContentMaxLines {
			return nil, fmt.Errorf("content has more than %d lines", compareContentMaxLines)
		}
		if lineBytes > compareContentMaxLineBytes {
			return nil, fmt.Errorf("content line exceeds %d bytes", compareContentMaxLineBytes)
		}
	}
	return strings.Split(text, "\n"), nil
}

// A bounded LCS aligns equal lines, including changes separated by unchanged
// context. The 1500-line cap bounds its uint16 table to under 4.5 MiB.
func compareContentDiff(before, after []byte) ([]compareContentRow, error) {
	a, err := compareContentLines(before)
	if err != nil {
		return nil, err
	}
	b, err := compareContentLines(after)
	if err != nil {
		return nil, err
	}
	width := len(b) + 1
	table := make([]uint16, (len(a)+1)*width)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			at := i*width + j
			if a[i] == b[j] {
				table[at] = table[(i+1)*width+j+1] + 1
			} else {
				table[at] = max(table[(i+1)*width+j], table[i*width+j+1])
			}
		}
	}
	rows := make([]compareContentRow, 0, len(a)+len(b))
	for i, j := 0, 0; i < len(a) || j < len(b); {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			rows = append(rows, compareContentRow{a[i], b[j], i + 1, j + 1})
			i++
			j++
		case i < len(a) && (j == len(b) || table[(i+1)*width+j] >= table[i*width+j+1]):
			rows = append(rows, compareContentRow{before: a[i], beforeLine: i + 1})
			i++
		default:
			rows = append(rows, compareContentRow{after: b[j], afterLine: j + 1})
			j++
		}
	}
	// Pair adjacent removed/added runs so replacements read across one row.
	aligned := make([]compareContentRow, 0, len(rows))
	for i := 0; i < len(rows); {
		if rows[i].beforeLine > 0 && rows[i].afterLine > 0 {
			aligned = append(aligned, rows[i])
			i++
			continue
		}
		var left, right []compareContentRow
		for i < len(rows) && (rows[i].beforeLine == 0 || rows[i].afterLine == 0) {
			if rows[i].beforeLine > 0 {
				left = append(left, rows[i])
			} else {
				right = append(right, rows[i])
			}
			i++
		}
		for j := 0; j < max(len(left), len(right)); j++ {
			var row compareContentRow
			if j < len(left) {
				row.before, row.beforeLine = left[j].before, left[j].beforeLine
			}
			if j < len(right) {
				row.after, row.afterLine = right[j].after, right[j].afterLine
			}
			aligned = append(aligned, row)
		}
	}
	return aligned, nil
}

func (m compareModel) contentPageSize() int {
	width, height := m.compareSize()
	if width < 80 {
		return max(1, height-4)
	}
	return max(1, height-5)
}

func (m compareModel) contentColumnWidth() int {
	width, _ := m.compareSize()
	return max(0, (width-5)/2)
}

func (m *compareModel) clampContentScroll() {
	if m.content == nil {
		return
	}
	if m.content.maxWidth == 0 {
		for _, row := range m.content.rows {
			m.content.maxWidth = max(m.content.maxWidth, ansi.StringWidth(compareTextLine(row.before)), ansi.StringWidth(compareTextLine(row.after)))
			m.content.maxNarrowWidth = max(m.content.maxNarrowWidth, ansi.StringWidth(compareNarrowText(row)))
		}
	}
	width, _ := m.compareSize()
	available := max(1, width-3)
	if width >= 80 {
		available = max(1, m.contentColumnWidth()-5)
	}
	maxWidth := m.content.maxWidth
	if width < 80 {
		maxWidth = max(maxWidth, m.content.maxNarrowWidth)
	}
	m.contentHOffset = min(m.contentHOffset, max(0, maxWidth-available))
}

func (m *compareModel) adjustContentViewport() {
	if m.content == nil {
		return
	}
	m.contentCursor = min(max(0, m.contentCursor), max(0, len(m.content.rows)-1))
	m.contentOffset = min(m.contentOffset, m.contentCursor)
	if m.contentCursor >= m.contentOffset+m.contentPageSize() {
		m.contentOffset = m.contentCursor - m.contentPageSize() + 1
	}
	m.contentOffset = max(0, m.contentOffset)
	m.clampContentScroll()
}

func (m *compareModel) updateContentKeys(msg tea.KeyPressMsg) {
	if m.content == nil {
		return
	}
	switch {
	case msg.Code == 'h' || msg.Code == tea.KeyLeft:
		m.contentHOffset = max(0, m.contentHOffset-8)
	case msg.Code == 'l' || msg.Code == tea.KeyRight:
		m.contentHOffset += 8
	case key.Matches(msg, m.keys.Down):
		m.contentCursor++
	case key.Matches(msg, m.keys.Up):
		m.contentCursor--
	case key.Matches(msg, m.keys.PageDown):
		m.contentCursor += m.contentPageSize()
	case key.Matches(msg, m.keys.PageUp):
		m.contentCursor -= m.contentPageSize()
	case key.Matches(msg, m.keys.HalfPageDown):
		m.contentCursor += max(1, m.contentPageSize()/2)
	case key.Matches(msg, m.keys.HalfPageUp):
		m.contentCursor -= max(1, m.contentPageSize()/2)
	case key.Matches(msg, m.keys.Top):
		m.contentCursor = 0
	case key.Matches(msg, m.keys.Bottom):
		m.contentCursor = len(m.content.rows) - 1
	}
	m.adjustContentViewport()
}

func (m compareModel) contentView() tea.View {
	lines := []string{"Content inspection", "Loading…", "Before − removed · After + added · ↑↓ move · PgUp/PgDn page"}
	if m.contentError != "" {
		lines[1] = strconv.Quote(m.contentError)
	}
	if c := m.content; c != nil {
		lines[0] = "File: " + strconv.Quote(c.path)
		lines[1] = "Up to 5 MiB per side; text lines are bounded."
		if c.note != "" {
			lines[1] = c.note
			lines[2] = "Size, permissions, ownership and provenance"
			lines = append(lines, strings.Split(c.metadata, "\n")...)
		}
		if c.note == "" {
			wide := m.width >= 80
			if wide {
				lines = append(lines, "  "+compareColumn("Image A", m.contentColumnWidth())+" │ Image B")
			}
			for i := m.contentOffset; i < min(len(c.rows), m.contentOffset+m.contentPageSize()); i++ {
				r := c.rows[i]
				marker := " "
				if i == m.contentCursor {
					marker = ">"
				}
				if wide {
					column := m.contentColumnWidth()
					left, right := "", ""
					if r.beforeLine > 0 {
						left = fmt.Sprintf("%4d %s", r.beforeLine, ansi.Cut(compareTextLine(r.before), m.contentHOffset, m.contentHOffset+compareContentMaxLineBytes*4))
					}
					if r.afterLine > 0 {
						right = fmt.Sprintf("%4d %s", r.afterLine, ansi.Cut(compareTextLine(r.after), m.contentHOffset, m.contentHOffset+compareContentMaxLineBytes*4))
					}
					left, right = compareColumn(left, column), compareColumn(right, column)
					if r.before != r.after || r.beforeLine == 0 || r.afterLine == 0 {
						left = styleWithFg(m.theme.Removed).Render(left)
						right = styleWithFg(m.theme.Added).Render(right)
					}
					lines = append(lines, marker+" "+left+" │ "+right)
				} else {
					prefix := "  "
					if r.beforeLine == 0 {
						prefix = "+ "
					} else if r.afterLine == 0 {
						prefix = "- "
					} else if r.before != r.after {
						prefix = "~ "
					}
					text := compareNarrowText(r)
					if r.beforeLine > 0 && r.afterLine > 0 && r.before != r.after {
						text = styleWithFg(m.theme.Removed).Render(compareTextLine(r.before)) + " → " + styleWithFg(m.theme.Added).Render(compareTextLine(r.after))
					}
					lines = append(lines, marker+prefix+ansi.Cut(text, m.contentHOffset, m.contentHOffset+compareContentMaxLineBytes*8))
				}
				at := len(lines) - 1
				style := styleWithFg(m.theme.Unchanged)
				if r.beforeLine == 0 {
					style = styleWithFg(m.theme.Added)
				} else if r.afterLine == 0 {
					style = styleWithFg(m.theme.Removed)
				}

				lines[at] = style.Render(lines[at])
			}
		}
	}
	footer := fmt.Sprintf("Esc/q back  h/l scroll  ↑↓ move  col %d", m.contentHOffset+1)
	if m.content != nil && m.content.note != "" {
		footer = "Esc/q back to files  ? guide"
		if m.contentOriginRow() >= 0 {
			footer += "  l origin layer"
		}
	}
	lines = append(lines, footer)
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(0, m.width), "…")
	}
	lines = fitCompareLines(lines, m.height)
	return m.finalCompareView(strings.Join(lines, "\n"))
}

func (m compareModel) contentOriginRow() int {
	if m.content == nil || m.fileIndex == nil || m.result == nil {
		return -1
	}
	n := m.fileIndex.after[m.content.path]
	before := n == nil
	if before {
		n = m.fileIndex.before[m.content.path]
	}
	if n == nil {
		return -1
	}
	for row, d := range m.result.LayerDiffs {
		if (!before && d.AfterIndex == n.IntroducedInLayer) || (before && d.BeforeIndex == n.IntroducedInLayer) {
			return row
		}
	}
	return -1
}

func compareInspectionMeta(n *image.FileNode, a *image.Analysis, label string) string {
	if n == nil {
		return label + ": absent"
	}
	text := fmt.Sprintf("%s: %s · %s", label, sizebudget.Format(n.Size), compareFileMeta(n))
	if n.Linkname != "" {
		text += " · target " + strconv.Quote(n.Linkname)
	}
	if a != nil && n.IntroducedInLayer >= 0 && n.IntroducedInLayer < len(a.Layers) {
		text += fmt.Sprintf("\n  Origin layer #%d: %s", n.IntroducedInLayer+1, strconv.Quote(a.Layers[n.IntroducedInLayer].Command))
	}
	return text
}

// Escape terminal controls without surrounding every ordinary text line in quotes.
func compareTextLine(text string) string {
	quoted := strconv.Quote(text)
	return quoted[1 : len(quoted)-1]
}

func compareNarrowText(row compareContentRow) string {
	if row.beforeLine > 0 && row.afterLine > 0 && row.before != row.after {
		return compareTextLine(row.before) + " → " + compareTextLine(row.after)
	}
	if row.afterLine > 0 {
		return compareTextLine(row.after)
	}
	return compareTextLine(row.before)
}
