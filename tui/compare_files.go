package tui

import (
	"context"
	"fmt"
	"image/color"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/image"
)

// File diffs are immutable after CompareAnalysis. Each permutation stores only
// indices; the path is the stable selection ID across all three views.
type compareFileIndex struct {
	order         [3][]int
	before, after map[string]*image.FileNode
}

type compareFileIndexMsg struct{ index *compareFileIndex }

func buildCompareFileIndex(ctx context.Context, diffs []image.FileDiff, results [2]image.CompareImage) tea.Cmd {
	return func() tea.Msg {
		if ctx.Err() != nil {
			return compareFileIndexMsg{}
		}
		index := &compareFileIndex{}
		wanted := make(map[string]struct{}, len(diffs))
		for _, d := range diffs {
			wanted[d.Path] = struct{}{}
		}
		index.before = compareFileNodes(ctx, results[0].Analysis, wanted)
		if ctx.Err() != nil {
			return compareFileIndexMsg{}
		}
		index.after = compareFileNodes(ctx, results[1].Analysis, wanted)
		for mode := range index.order {
			index.order[mode] = make([]int, len(diffs))
			for i := range diffs {
				index.order[mode][i] = i
			}
		}
		// CompareAnalysis supplies path order. For equal deltas, retain that order.
		sort.SliceStable(index.order[1], func(i, j int) bool {
			return diffs[index.order[1][i]].SizeDelta > diffs[index.order[1][j]].SizeDelta
		})
		if ctx.Err() != nil {
			return compareFileIndexMsg{}
		}
		sort.SliceStable(index.order[2], func(i, j int) bool {
			return diffs[index.order[2][i]].SizeDelta < diffs[index.order[2][j]].SizeDelta
		})
		if ctx.Err() != nil {
			return compareFileIndexMsg{}
		}
		return compareFileIndexMsg{index: index}
	}
}

func compareFileNodes(ctx context.Context, a *image.Analysis, wanted map[string]struct{}) map[string]*image.FileNode {
	nodes := make(map[string]*image.FileNode)
	if len(wanted) == 0 || a == nil || len(a.StackedTrees) == 0 {
		return nodes
	}
	tree := a.StackedTrees[len(a.StackedTrees)-1]
	if tree == nil || tree.Root == nil {
		return nodes
	}
	stack := []*image.FileNode{tree.Root}
	for len(stack) > 0 {
		if ctx.Err() != nil {
			return nil
		}
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.DiffType == image.Removed || strings.HasPrefix(n.Name, ".wh.") {
			continue
		}
		if n.IsDir {
			stack = append(stack, n.Children...)
			continue
		}
		if _, ok := wanted[n.Path]; ok {
			nodes[n.Path] = n
		}
	}
	return nodes
}

func (m compareModel) fileCount() int {
	if m.result == nil {
		return 0
	}
	if m.fileFilter != "" {
		return len(m.fileFiltered)
	}
	return len(m.result.FileDiffs)
}

func (m compareModel) filePageSize() int {
	extra := 0
	if m.fileFiltering || m.fileFilter != "" {
		extra++
	}
	if m.fileHelp {
		extra++
	}
	if m.fileIndex == nil {
		extra++
	}
	return max(1, m.height-6-extra)
}

func (m *compareModel) adjustFileViewport() {
	m.fileCursor = min(max(0, m.fileCursor), max(0, m.fileCount()-1))
	m.fileOffset = min(m.fileOffset, m.fileCursor)
	if m.fileCursor >= m.fileOffset+m.filePageSize() {
		m.fileOffset = m.fileCursor - m.filePageSize() + 1
	}
	m.fileOffset = max(0, m.fileOffset)
}

func (m compareModel) fileDiffAt(row int) image.FileDiff {
	index := row
	if m.fileFilter != "" {
		index = m.fileFiltered[row]
	} else if m.fileIndex != nil {
		index = m.fileIndex.order[m.fileSort][row]
	}
	return m.result.FileDiffs[index]
}

func (m *compareModel) updateFileKeys(msg tea.KeyPressMsg) {
	if msg.Code == '/' {
		m.fileFiltering = true
		m.adjustFileViewport()
		return
	}
	if msg.Code == tea.KeyEscape {
		m.fileFilter = ""
		m.rebuildFileFilter()
		m.adjustFileViewport()
		return
	}
	if key.Matches(msg, m.keys.Sort) {
		if m.fileCount() > 0 {
			m.selectedPath = m.fileDiffAt(m.fileCursor).Path
		}
		m.fileSort = (m.fileSort + 1) % 3
		m.rebuildFileFilter()
		if m.fileIndex != nil {
			for row := 0; row < m.fileCount(); row++ {
				if m.fileDiffAt(row).Path == m.selectedPath {
					m.fileCursor = row
					break
				}
			}
		}
	} else if msg.Code == 'm' {
		m.fileMetadata = !m.fileMetadata
	} else if key.Matches(msg, m.keys.Help) {
		m.fileHelp = !m.fileHelp
	} else {
		switch {
		case key.Matches(msg, m.keys.Down):
			m.fileCursor++
		case key.Matches(msg, m.keys.Up):
			m.fileCursor--
		case key.Matches(msg, m.keys.PageDown):
			m.fileCursor += m.filePageSize()
		case key.Matches(msg, m.keys.PageUp):
			m.fileCursor -= m.filePageSize()
		case key.Matches(msg, m.keys.HalfPageDown):
			m.fileCursor += max(1, m.filePageSize()/2)
		case key.Matches(msg, m.keys.HalfPageUp):
			m.fileCursor -= max(1, m.filePageSize()/2)
		case key.Matches(msg, m.keys.Top):
			m.fileCursor = 0
		case key.Matches(msg, m.keys.Bottom):
			m.fileCursor = m.fileCount() - 1
		}
	}
	m.adjustFileViewport()
	if m.fileCount() > 0 {
		m.selectedPath = m.fileDiffAt(m.fileCursor).Path
	}
}

func (m compareModel) fileBrowserView() tea.View {
	sortNames := [3]string{"path", "growth", "shrink"}
	sum := m.result.FileSummary
	lines := []string{
		"File changes",
		fmt.Sprintf("Added %d   Removed %d   Modified %d   +%s / -%s", sum.AddedCount, sum.RemovedCount, sum.ModifiedCount, image.FormatBytes(sum.BytesAdded), image.FormatBytes(sum.BytesRemoved)),
		"Sizes and metadata only; same-size content changes may be absent.",
		"Directory metadata is not compared.",
		fmt.Sprintf("Order: %s   Metadata: %t", sortNames[m.fileSort], m.fileMetadata),
	}
	if m.fileFiltering || m.fileFilter != "" {
		lines = append(lines, "Filter /"+strconv.Quote(m.fileFilter)+"  Enter apply; Esc clear")
	}
	if m.fileHelp {
		lines = append(lines, "Enter/v inspects text; Tab/1/2 switches views; s cycles path, growth, shrink; m shows metadata; ? closes help.")
	}
	if m.fileIndex == nil {
		lines = append(lines, "Building file views…")
	}
	if m.fileCount() == 0 {
		if m.fileFilter != "" {
			lines = append(lines, "No matching paths; Esc clears filter")
		} else {
			lines = append(lines, "No file changes")
		}
	}
	for row := m.fileOffset; row < min(m.fileCount(), m.fileOffset+m.filePageSize()); row++ {
		d := m.fileDiffAt(row)
		marker := " "
		if row == m.fileCursor {
			marker = ">"
		}
		kind := [4]string{"  same", "+ added", "~ changed", "- removed"}[d.DiffType]
		line := fmt.Sprintf("%s %s %s  ", marker, styleWithFg([4]color.Color{m.theme.Unchanged, m.theme.Added, m.theme.Modified, m.theme.Removed}[d.DiffType]).Render(compareColumn(kind, 9)), signedBytes(d.SizeDelta))
		if m.fileSort == 0 {
			line += "  " + strings.Repeat("  ", min(6, max(0, strings.Count(d.Path, "/")-1)))
		} else {
			line += "  "
		}
		line += strconv.Quote(d.Path)
		if m.fileMetadata {
			line += "  [" + d.ChangeReason + "]"
			if m.fileIndex != nil {
				line += "  " + compareFileMeta(m.fileIndex.before[d.Path]) + " → " + compareFileMeta(m.fileIndex.after[d.Path])
			}
		}
		if row == m.fileCursor {
			line = styleWithFg(m.theme.Selected).Background(m.theme.SelectedBg).Render(line)
		}
		lines = append(lines, line)
	}
	footer := "Enter content  / filter  q exit  ? help"
	if m.width >= 80 {
		footer = fmt.Sprintf("%d/%d  Enter content  / filter  s sort  m metadata  Tab views  ↑↓ move  ? help  q exit", min(m.fileCursor+1, m.fileCount()), m.fileCount())
	}
	lines = append(lines, footer)
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(0, m.width), "…")
	}
	lines = fitCompareLines(lines, m.height)
	return finalizeViewNoMouse(tea.NewView(strings.Join(lines, "\n")), m.theme.MainBg)
}

func compareFileMeta(n *image.FileNode) string {
	if n == nil {
		return "— absent"
	}
	return fmt.Sprintf("%s uid:%d gid:%d", n.Mode, n.UID, n.GID)
}

// Rebuild only on edits or sort/index changes, never while rendering or moving.
func (m *compareModel) rebuildFileFilter() {
	m.fileFiltered = m.fileFiltered[:0]
	if m.fileFilter == "" || m.result == nil {
		return
	}
	for row := range m.result.FileDiffs {
		index := row
		if m.fileIndex != nil {
			index = m.fileIndex.order[m.fileSort][row]
		}
		if strings.Contains(m.result.FileDiffs[index].Path, m.fileFilter) {
			m.fileFiltered = append(m.fileFiltered, index)
		}
	}
}
func (m *compareModel) updateFileFilter(msg tea.KeyPressMsg) {
	switch msg.Code {
	case tea.KeyEnter:
		m.fileFiltering = false
		m.adjustFileViewport()
		return
	case tea.KeyEscape:
		m.fileFiltering = false
		m.fileFilter = ""
	case tea.KeyBackspace:
		if len(m.fileFilter) > 0 {
			_, n := utf8.DecodeLastRuneInString(m.fileFilter)
			m.fileFilter = m.fileFilter[:len(m.fileFilter)-n]
		}
	default:
		m.fileFilter += msg.Text
	}
	m.rebuildFileFilter()
	m.fileCursor, m.fileOffset = 0, 0
}
