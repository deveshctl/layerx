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
	m.contentCursor, m.contentOffset = 0, 0
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
	content.metadata = "A " + compareFileMeta(before) + " → B " + compareFileMeta(after)
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
			return nil, fmt.Sprintf("Image %c: file exceeds 5 MiB", 'A'+side)
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
			return nil, fmt.Sprintf("Image %c: %v", 'A'+side, err)
		}
		data[side] = bytes
	}
	if err := ctx.Err(); err != nil {
		return nil, err.Error()
	}
	rows, err := compareContentDiff(data[0], data[1])
	if err != nil {
		return nil, err.Error()
	}
	content.rows = rows
	return content, ""
}

func validateCompareContent(data []byte) error {
	if len(data) > compareContentMaxBytes {
		return fmt.Errorf("file exceeds 5 MiB")
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("invalid UTF-8 text")
	}
	if image.IsBinary(data) || bytes.IndexByte(data, 0) >= 0 {
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
	lineCount, lineBytes := 1, 0
	for _, ch := range data {
		if ch == '\n' {
			lineCount++
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
	return strings.Split(string(data), "\n"), nil
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
	return rows, nil
}

func (m compareModel) contentPageSize() int { return max(1, m.height-5) }

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
}

func (m *compareModel) updateContentKeys(msg tea.KeyPressMsg) {
	if m.content == nil {
		return
	}
	switch {
	case key.Matches(msg, m.keys.Down):
		m.contentCursor++
	case key.Matches(msg, m.keys.Up):
		m.contentCursor--
	case key.Matches(msg, m.keys.PageDown):
		m.contentCursor += m.contentPageSize()
	case key.Matches(msg, m.keys.PageUp):
		m.contentCursor -= m.contentPageSize()
	case key.Matches(msg, m.keys.Top):
		m.contentCursor = 0
	case key.Matches(msg, m.keys.Bottom):
		m.contentCursor = len(m.content.rows) - 1
	}
	m.adjustContentViewport()
}

func (m compareModel) contentView() tea.View {
	lines := []string{"Content inspection", "Loading…", "Esc closes; q closes; ↑↓ and PgUp/PgDn move together"}
	if m.contentError != "" {
		lines[1] = strconv.Quote(m.contentError)
	}
	if c := m.content; c != nil {
		lines[0] = "Content: " + strconv.Quote(c.path)
		lines[1] = "Up to 5 MiB per side; text lines are bounded."
		if c.note != "" {
			lines[1] = c.note
			lines = append(lines, c.metadata)
		}
		if c.note == "" {
			wide := m.width >= 80
			if wide {
				lines = append(lines, "  Image A                                      │ Image B")
			}
			for i := m.contentOffset; i < min(len(c.rows), m.contentOffset+m.contentPageSize()); i++ {
				r := c.rows[i]
				marker := " "
				if i == m.contentCursor {
					marker = ">"
				}
				if wide {
					column := max(12, (m.width-16)/2)
					left, right := "", ""
					if r.beforeLine > 0 {
						left = fmt.Sprintf("%4d %s", r.beforeLine, strconv.Quote(r.before))
					}
					if r.afterLine > 0 {
						right = fmt.Sprintf("%4d %s", r.afterLine, strconv.Quote(r.after))
					}
					lines = append(lines, fmt.Sprintf("%s %-*s │ %-*s", marker, column, ansi.Truncate(left, column, "…"), column, ansi.Truncate(right, column, "…")))
				} else {
					if r.beforeLine > 0 && r.afterLine == 0 {
						lines = append(lines, marker+"- "+strconv.Quote(r.before))
					}
					if r.afterLine > 0 {
						prefix := "+ "
						if r.beforeLine > 0 {
							prefix = "  "
						}
						lines = append(lines, marker+prefix+strconv.Quote(r.after))
					}
				}
			}
		}
	}
	lines = append(lines, "Esc return to files")
	for i := range lines {
		lines[i] = ansi.Truncate(ansi.Strip(lines[i]), max(0, m.width), "…")
	}
	lines = lines[:min(len(lines), max(0, m.height))]
	return finalizeViewNoMouse(tea.NewView(strings.Join(lines, "\n")), m.theme.MainBg)
}
