package tui

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/image"
	"github.com/deveshctl/layerx/internal/sizebudget"
)

type compareDirectory struct {
	path                 string
	before, after, count int64
	files                []image.FileDiff
}

// Build once with the file index, never scan all changes during rendering.
func compareDirectories(ctx context.Context, diffs []image.FileDiff) []compareDirectory {
	groups := make(map[string]*compareDirectory)
	for _, d := range diffs {
		if ctx.Err() != nil {
			return nil
		}
		dir := compareDirectoryPath(d.Path)
		g := groups[dir]
		if g == nil {
			g = &compareDirectory{path: dir}
			groups[dir] = g
		}
		g.before += d.BeforeSize
		g.after += d.AfterSize
		g.count++
		// Keep only five investigation leads per area, with bounded insertion work.
		pos := len(g.files)
		for pos > 0 && compareMagnitude(d.SizeDelta) > compareMagnitude(g.files[pos-1].SizeDelta) {
			pos--
		}
		if pos < 5 {
			if len(g.files) < 5 {
				g.files = append(g.files, image.FileDiff{})
			}
			copy(g.files[pos+1:], g.files[pos:len(g.files)-1])
			g.files[pos] = d
		}
	}
	out := make([]compareDirectory, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}

	sort.Slice(out, func(i, j int) bool {
		a, b := compareMagnitude(out[i].after-out[i].before), compareMagnitude(out[j].after-out[j].before)
		if a == b {
			return out[i].path < out[j].path
		}
		return a > b
	})
	return out
}

func (m compareModel) overviewPageSize() int {
	_, h := m.compareSize()
	extra := 0
	if m.checkError != "" {
		extra = 1
	}
	base := 11
	if h < 16 {
		base = 5
	}
	return max(1, h-base-len(m.checks)-extra)
}

func (m *compareModel) adjustOverview() {
	count := 0
	if m.fileIndex != nil {
		count = len(m.fileIndex.directories)
	}
	m.directoryCursor = min(max(0, m.directoryCursor), max(0, count-1))
	m.directoryOffset = min(m.directoryOffset, m.directoryCursor)
	if m.directoryCursor >= m.directoryOffset+m.overviewPageSize() {
		m.directoryOffset = m.directoryCursor - m.overviewPageSize() + 1
	}
}

func (m compareModel) overviewView() tea.View {
	r := m.result
	s := r.FileSummary
	lines := []string{
		styleWithFg(m.theme.Accent).Bold(true).Render("What changed"),
		fmt.Sprintf("Layer tar bytes  %s → %s  %s", sizebudget.Format(r.Before.UncompressedSize), sizebudget.Format(r.After.UncompressedSize), m.compareDelta(r.After.UncompressedSize-r.Before.UncompressedSize)),
		fmt.Sprintf("Layer waste      %s → %s  %s", sizebudget.Format(r.BeforeEfficiency.WastedBytes), sizebudget.Format(r.AfterEfficiency.WastedBytes), m.compareDelta(r.AfterEfficiency.WastedBytesDelta)),
		fmt.Sprintf("Changed files    +%d added   −%d removed   ~%d metadata/size changes", s.AddedCount, s.RemovedCount, s.ModifiedCount),
	}
	if m.height < 16 {
		lines = []string{
			fmt.Sprintf("Layer bytes %s → %s (%s)", sizebudget.Format(r.Before.UncompressedSize), sizebudget.Format(r.After.UncompressedSize), m.compareDelta(r.After.UncompressedSize-r.Before.UncompressedSize)),
			fmt.Sprintf("Files +%d −%d ~%d · waste %s", s.AddedCount, s.RemovedCount, s.ModifiedCount, m.compareDelta(r.AfterEfficiency.WastedBytesDelta)),
		}
	}
	for _, c := range m.checks {
		status := "PASS"
		if !c.Passed {
			status = "FAIL"
		}
		lines = append(lines, fmt.Sprintf("%s %s: %s / %s", m.compareStatus(status), c.Name, sizebudget.Format(c.ActualBytes), sizebudget.Format(c.LimitBytes)))
	}
	if m.checkError != "" {
		lines = append(lines, "Size check: "+strconv.Quote(m.checkError))
	}
	if m.height < 16 {
		lines = append(lines, styleWithFg(m.theme.Accent).Bold(true).Render("Areas · Enter investigates changed files"))
	} else {
		lines = append(lines, "", styleWithFg(m.theme.Accent).Bold(true).Render("Where to investigate"), "Directories ranked by net file-byte change; Enter opens their changed files.", "These are changed-file totals, not total directory sizes.")
	}
	if m.fileIndex == nil {
		lines = append(lines, "Indexing directory changes…")
	} else if len(m.fileIndex.directories) == 0 {
		lines = append(lines, "No size or metadata changes detected.")
	} else {
		details := []string{}
		leftWidth := m.width
		if m.width >= 110 {
			leftWidth = m.width/2 - 2
			d := m.fileIndex.directories[m.directoryCursor]
			details = append(details, styleWithFg(m.theme.Accent).Bold(true).Render("Selected area · "+strconv.Quote(d.path)),
				fmt.Sprintf("Changed-file bytes: %s → %s", sizebudget.Format(d.before), sizebudget.Format(d.after)), "", "Largest file changes")
			for _, f := range d.files {
				details = append(details, compareColumn(m.compareDelta(f.SizeDelta), 12)+" "+compareImageRef(f.Path, m.width-leftWidth-16))
			}
			details = append(details, "", "Enter opens all changes in this area.")
		}
		count := min(m.overviewPageSize(), max(len(m.fileIndex.directories)-m.directoryOffset, len(details)))
		for row := 0; row < count; row++ {
			i := m.directoryOffset + row
			line := ""
			if i < len(m.fileIndex.directories) {
				d := m.fileIndex.directories[i]
				marker := " "
				if i == m.directoryCursor {
					marker = styleWithFg(m.theme.Accent).Bold(true).Render("›")
				}
				delta := m.compareDelta(d.after - d.before)
				delta = strings.Repeat(" ", max(0, 12-ansi.StringWidth(delta))) + delta
				line = fmt.Sprintf("%s %s  %5d files  %s", marker, delta, d.count, strconv.Quote(d.path))
			}
			if m.width >= 110 {
				right := ""
				if row < len(details) {
					right = details[row]
				}
				line = compareColumn(line, leftWidth) + styleWithFg(m.theme.UnfocusedBorder).Render(" │ ") + right
			}
			lines = append(lines, line)
		}
	}
	if m.height >= 16 {
		lines = append(lines, "")
	}
	lines = append(lines, "Content is not fully scanned: same-size changes may be absent. Shared layer IDs identify identical layers.")
	lines = append(lines, "q exit  ? guide  ↑↓ directory  Enter files  1 layers  2 all files")
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(0, m.width), "…")
	}
	return finalizeViewNoMouse(tea.NewView(strings.Join(fitCompareLines(lines, m.height), "\n")), m.theme.MainBg)
}

func compareMagnitude(n int64) uint64 {
	if n < 0 {
		return uint64(-(n + 1)) + 1 // #nosec G115 -- n < 0 guarantees -(n+1) is nonnegative and fits int64.
	}
	return uint64(n)
}

func (m compareModel) compareGuideView() tea.View {
	lines := []string{"Your comparison guide", "", "3 Overview  Find directory growth, waste and failed budgets.", "Enter on a directory narrows the file list; Esc clears that scope.", "2 Files     s ranks growth/shrink/path; / searches; m shows metadata.", "Enter inspects text or shows metadata for binary, large and linked files.", "1 Layers    Compare build steps and their stored-byte changes.", "Shared DiffID = identical layer; command match? = best-effort pairing.", "", "↑↓ / j k move · PgUp/PgDn page · Ctrl+u/d half page · g/G ends", "Text: h/l scroll horizontally; Esc returns to files.", "Metadata: l opens the origin layer; Esc returns to files.", "", "Limits: size/metadata comparison does not detect every same-size content change.", "Layer waste measures overwritten/deleted bytes, not every cleanup opportunity.", "", "Esc / ? / q back to comparison"}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(0, m.width), "…")
	}
	return finalizeViewNoMouse(tea.NewView(strings.Join(fitCompareLines(lines, m.height), "\n")), m.theme.MainBg)
}

func compareDirectoryPath(file string) string {
	dir := path.Dir(file)
	start := 0
	if strings.HasPrefix(dir, "/") {
		start = 1
	}
	first := strings.IndexByte(dir[start:], '/')
	if first < 0 {
		return dir
	}
	from := start + first + 1
	if next := strings.IndexByte(dir[from:], '/'); next >= 0 {
		return dir[:from+next]
	}
	return dir
}
