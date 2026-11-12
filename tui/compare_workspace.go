package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/internal/sizebudget"
)

// The wide workspace keeps evidence beside the selection. Compact terminals
// retain the single-column browser and the same navigation bindings.
func (m compareModel) usesCompareWorkspace() bool {
	return m.result != nil && m.width >= 80 && m.height >= 20
}

func (m compareModel) workspaceLoadingView() tea.View {
	quiet := styleWithFg(m.theme.MetaDim)
	lines := []string{renderBrand(m.theme) + quiet.Render("   /   Image comparison"), "", m.workspaceTitle("Preparing comparison"), quiet.Render("Resolving images and analyzing layer files."), ""}
	for side, input := range m.session.inputs {
		label := []string{"Before", "After"}[side]
		marker := string("|/-\\"[m.frame%4])
		if m.progress[side].Ready {
			marker = "✓"
		}
		lines = append(lines, m.workspaceTitle(label)+"  "+compareTextLine(input.Ref), "  "+styleWithFg(m.theme.Accent).Render(marker)+"  "+compareProgressText(m.progress[side]))
		if warning := m.progress[side].Warning; warning != "" {
			lines = append(lines, quiet.Render(compareTextLine(warning)))
		}
		lines = append(lines, "")
	}
	if m.progress[0].Ready && m.progress[1].Ready {
		lines = append(lines, quiet.Render("Comparing final files and building the report…"))
	}
	return m.workspaceFinish(lines)
}

func (m compareModel) workspaceView() tea.View {
	w := m.width - 4
	r := m.result
	quiet := styleWithFg(m.theme.MetaDim)
	strong := styleWithFg(m.theme.FileName).Bold(true)
	refs := [2]string{r.Before.ImageRef, r.After.ImageRef}
	if m.session != nil {
		refs = [2]string{m.session.inputs[0].Ref, m.session.inputs[1].Ref}
	}
	lines := []string{
		renderBrand(m.theme) + quiet.Render("   /   Image comparison"),
		"",
		strong.Render(ansi.Truncate(compareTextLine(refs[0]), (w-7)/2, "…")) + quiet.Render("   →   ") + strong.Render(ansi.Truncate(compareTextLine(refs[1]), (w-7)/2, "…")),
		m.compareStatus(m.comparisonVerdict()) + "   " + m.workspaceReason(),
		"",
	}
	cell := (w - 6) / 3
	labels := []string{"Image size · uncompressed", "Layer waste", "Layer efficiency"}
	values := []string{
		m.compareDelta(r.After.UncompressedSize - r.Before.UncompressedSize),
		m.compareDelta(r.AfterEfficiency.WastedBytesDelta),
		fmt.Sprintf("%.1f%%", r.AfterEfficiency.Score*100),
	}
	if r.AfterEfficiency.Score < 1 && r.AfterEfficiency.Score >= 0.9995 {
		values[2] = "~100%"
	}
	beforeAfter := []string{
		sizebudget.Format(r.Before.UncompressedSize) + " → " + sizebudget.Format(r.After.UncompressedSize),
		sizebudget.Format(r.BeforeEfficiency.WastedBytes) + " → " + sizebudget.Format(r.AfterEfficiency.WastedBytes),
		fmt.Sprintf("%+.1f percentage points", r.AfterEfficiency.ScoreDelta*100),
	}
	if delta := r.AfterEfficiency.ScoreDelta * 100; delta != 0 && delta > -0.05 && delta < 0.05 {
		beforeAfter[2] = "+<0.1 percentage points"
		if delta < 0 {
			beforeAfter[2] = "−<0.1 percentage points"
		}
	}
	if !m.contentOpen && !m.guideOpen {
		if m.height < 28 {
			lines = append(lines, "Size "+values[0]+"   Waste "+values[1]+"   Efficiency "+values[2])
		} else {
			for row := 0; row < 3; row++ {
				parts := make([]string, 3)
				for i := range parts {
					text := quiet.Render(labels[i])
					if row == 1 {
						text = strong.Render(values[i])
					} else if row == 2 {
						text = quiet.Render(beforeAfter[i])
					}
					parts[i] = compareColumn(text, cell)
				}
				lines = append(lines, strings.Join(parts, "   "))
			}
		}
		lines = append(lines, "")
	}
	tabs := []string{"3  Overview", "1  Layers", "2  Files"}
	active := 1
	if m.overview {
		active = 0
	}
	if m.fileView {
		active = 2
	}
	for i := range tabs {
		if i == active {
			tabs[i] = styleWithFg(m.theme.Selected).Background(m.theme.SelectedBg).Bold(true).Render(" " + tabs[i] + " ")
		} else {
			tabs[i] = quiet.Render(" " + tabs[i] + " ")
		}
	}
	if m.contentOpen {
		tabs = []string{quiet.Render("Files / ") + strong.Render("File details") + quiet.Render("   Esc back to files")}
	}
	if m.guideOpen {
		tabs = []string{strong.Render("Comparison guide") + quiet.Render("   Esc back")}
	}
	lines = append(lines, strings.Join(tabs, "   "), quiet.Render(strings.Repeat("─", w)))
	// Use the existing page budgets so selection stays visible across resizing.
	inner := m
	inner.width, inner.height, inner.inFrame = w, m.height-len(lines)-2, true
	var body []string
	switch {
	case m.guideOpen || m.contentOpen:
		v := inner.compareBodyView()
		body = strings.Split(v.Content, "\n")
		if len(body) > 0 {
			body = body[:len(body)-1]
		}
	case m.overview:
		body = inner.workspaceOverview(m.overviewPageSize())
	case m.fileView:
		body = inner.workspaceFiles(m.filePageSize())
	default:
		body = inner.workspaceLayers(m.pageSize())
	}
	room := max(1, m.height-len(lines)-2)
	lines = append(lines, body[:min(len(body), room)]...)
	return m.workspaceFinish(lines)
}

func (m compareModel) workspaceFinish(lines []string) tea.View {
	w := m.width - 4
	lines = lines[:min(len(lines), m.height-2)]
	for len(lines) < m.height-2 {
		lines = append(lines, "")
	}
	lines = append(lines, styleWithFg(m.theme.MetaDim).Render(strings.Repeat("─", w)), m.compareFooter(w))
	for i := range lines {
		lines[i] = "  " + compareColumn(lines[i], w) + "  "
	}
	return m.finalCompareView(strings.Join(lines, "\n"))
}

func (m compareModel) workspaceReason() string {
	if m.checkError != "" {
		return "Size checks could not be completed"
	}
	for _, c := range m.checks {
		if !c.Passed {
			return compareTextLine(c.Name) + " exceeds its budget"
		}
	}
	if m.result.AfterEfficiency.WastedBytesDelta > 0 {
		return "Layer waste increased by " + sizebudget.Format(m.result.AfterEfficiency.WastedBytesDelta)
	}
	if m.result.IsRegression() {
		return "Layer efficiency decreased"
	}
	return "No efficiency regression or failed size budgets"
}

func (m compareModel) workspaceColumns(left, right []string) []string {
	if m.width < 96 {
		return left
	}
	leftWidth := (m.width - 5) * 3 / 5
	rightWidth := m.width - leftWidth - 5
	rows := max(len(left), len(right))
	out := make([]string, rows)
	for i := range out {
		a, b := "", ""
		if i < len(left) {
			a = left[i]
		}
		if i < len(right) {
			b = right[i]
		}
		out[i] = compareColumn(a, leftWidth) + styleWithFg(m.theme.UnfocusedBorder).Render("  │  ") + compareColumn(b, rightWidth)
	}
	return out
}

func (m compareModel) workspaceTitle(text string) string {
	return styleWithFg(m.theme.FileName).Bold(true).Render(text)
}

func (m compareModel) workspaceSelected(text string, selected bool, width int) string {
	if selected {
		return styleWithFg(m.theme.Selected).Background(m.theme.SelectedBg).Render(compareColumn("▸ "+ansi.Strip(text), width))
	}
	return "  " + text
}

func (m compareModel) workspaceOverview(page int) []string {
	s := m.result.FileSummary
	left := []string{m.workspaceTitle("Where the files changed"), styleWithFg(m.theme.MetaDim).Render(fmt.Sprintf("%d added   %d modified   %d removed", s.AddedCount, s.ModifiedCount, s.RemovedCount)), styleWithFg(m.theme.MetaDim).Render("Size and metadata · Same-size edits may be absent")}
	right := []string{m.workspaceTitle("Comparison checks"), ""}
	for _, c := range m.checks {
		status := "PASS"
		if !c.Passed {
			status = "FAIL"
		}
		right = append(right, m.compareStatus(status)+"  "+compareTextLine(c.Name), "  "+sizebudget.Format(c.ActualBytes)+" / "+sizebudget.Format(c.LimitBytes), "")
	}
	if len(m.checks) == 0 {
		right = append(right, "No size budgets configured.", "Efficiency and waste checks are active.", "")
	}
	if m.width < 96 {
		for _, c := range m.checks {
			status := "PASS"
			if !c.Passed {
				status = "FAIL"
			}
			left[1] = m.compareStatus(status) + " " + compareTextLine(c.Name) + " " + sizebudget.Format(c.ActualBytes) + " / " + sizebudget.Format(c.LimitBytes)
			if !c.Passed {
				break
			}
		}
	}
	if m.fileIndex == nil {
		return append(left, "Indexing changes…")
	}
	dirs := m.fileIndex.directories
	if len(dirs) == 0 {
		left = append(left, "No file size or metadata changes.")
	}
	for i := m.directoryOffset; i < min(len(dirs), m.directoryOffset+page); i++ {
		d := dirs[i]
		bar := ""
		if len(dirs) > 0 && compareMagnitude(dirs[0].after-dirs[0].before) > 0 {
			n := int(12 * float64(compareMagnitude(d.after-d.before)) / float64(compareMagnitude(dirs[0].after-dirs[0].before)))
			bar = styleWithFg(m.theme.Accent).Render(strings.Repeat("━", n))
		}
		line := compareColumn(m.compareDelta(d.after-d.before), 12) + " " + compareColumn(bar, 12) + "  " + compareTextLine(d.path)
		rowWidth := (m.width - 5) * 3 / 5
		if m.width < 96 {
			rowWidth = m.width
		}
		left = append(left, m.workspaceSelected(line, i == m.directoryCursor, rowWidth))
	}
	if len(dirs) > 0 {
		d := dirs[m.directoryCursor]
		countLabel := "changed files"
		if d.count == 1 {
			countLabel = "changed file"
		}
		right = append(right, m.workspaceTitle(compareTextLine(d.path)), fmt.Sprintf("%d %s", d.count, countLabel), "", "Largest changes")
		for _, f := range d.files {
			pathWidth := m.width - (m.width-5)*3/5 - 19
			right = append(right, compareColumn(m.compareDelta(f.SizeDelta), 12)+" "+truncateMid(compareTextLine(f.Path), max(1, pathWidth)))
		}
		right = append(right, "", styleWithFg(m.theme.Accent).Render("Enter  Investigate this directory"))
	}
	return m.workspaceColumns(left, right)
}

func (m compareModel) workspaceFiles(page int) []string {
	sortNames := [4]string{"Path", "Largest growth", "Largest reduction", "Largest changes"}
	title := "Changed files"
	if m.fileDirectory != "" {
		title = compareTextLine(m.fileDirectory)
	}
	left := []string{m.workspaceTitle(title), styleWithFg(m.theme.MetaDim).Render(sortNames[m.fileSort] + "   ·   s change ranking"), ""}
	if m.fileFiltering || m.fileFilter != "" {
		left[1] = "/ " + compareTextLine(m.fileFilter) + "   Enter apply · Esc clear"
	}
	if m.fileIndex == nil {
		left[2] = "Indexing file details…"
	}
	width := (m.width - 5) * 3 / 5
	if m.width < 96 {
		width = m.width
	}
	for i := m.fileOffset; i < min(m.fileCount(), m.fileOffset+page); i++ {
		d := m.fileDiffAt(i)
		kind := [4]string{" ", "+", "~", "−"}[d.DiffType]
		line := kind + " " + compareColumn(m.compareDelta(d.SizeDelta), 12) + " " + truncateMid(compareTextLine(d.Path), max(1, width-18))
		left = append(left, m.workspaceSelected(line, i == m.fileCursor, width))
	}
	if m.width < 96 {
		if m.fileCount() == 0 {
			left = append(left, "No matching file changes.", "Esc clears the directory and filter.")
		}
		return left
	}
	right := []string{m.workspaceTitle("File inspector"), styleWithFg(m.theme.MetaDim).Render("Enter details · d text diff"), ""}
	if m.fileCount() == 0 {
		left = append(left, "No matching file changes.", "Esc clears the directory and filter.")
	} else {
		d := m.fileDiffAt(m.fileCursor)
		rightWidth := m.width - width - 5
		if m.height < 16 {
			right = append(right, truncateMid(compareTextLine(d.Path), rightWidth))
		} else {
			right = append(right, strings.Split(ansi.Wrap(compareTextLine(d.Path), rightWidth, "/"), "\n")...)
		}
		right = append(right, "", "Before   "+sizebudget.Format(d.BeforeSize), "After    "+sizebudget.Format(d.AfterSize), "Change   "+m.compareDelta(d.SizeDelta), "")
		if d.ChangeReason != "" {
			right = append(right, "Changed: "+compareTextLine(d.ChangeReason), "")
		}
		if m.fileMetadata && m.fileIndex != nil {
			right = append(right, "Before  "+compareFileMeta(m.fileIndex.before[d.Path]), "After   "+compareFileMeta(m.fileIndex.after[d.Path]), "")
		}
		if m.fileIndex != nil {
			node := m.fileIndex.after[d.Path]
			before := node == nil
			if before {
				node = m.fileIndex.before[d.Path]
			}
			if node != nil {
				for _, layer := range m.result.LayerDiffs {
					index, command := layer.AfterIndex, layer.AfterCommand
					if before {
						index, command = layer.BeforeIndex, layer.BeforeCommand
					}
					if index == node.IntroducedInLayer {
						right = append(right, m.workspaceTitle(fmt.Sprintf("Origin · layer %d", index+1)))
						right = append(right, workspaceCommand(command, rightWidth, 4)...)
						right = append(right, "")
						break
					}
				}
			}
		}
		right = append(right, styleWithFg(m.theme.MetaDim).Render("Details open without reading content."))
	}
	return m.workspaceColumns(left, right)
}

func (m compareModel) workspaceLayers(page int) []string {
	width := (m.width - 5) * 3 / 5
	if m.width < 96 {
		width = m.width
	}
	left := []string{m.workspaceTitle("Build steps"), styleWithFg(m.theme.MetaDim).Render("Before → After  Match           Uncompressed change"), ""}
	right := []string{m.workspaceTitle("Selected build step"), ""}
	for i := m.offset; i < min(len(m.result.LayerDiffs), m.offset+page); i++ {
		d := m.result.LayerDiffs[i]
		pair := compareLayerNumber(d.BeforeIndex) + " → " + compareLayerNumber(d.AfterIndex)
		line := compareColumn(pair, 16) + compareColumn(compareMatch(d.Match), 16) + m.compareDelta(d.SizeDelta)
		left = append(left, m.workspaceSelected(line, i == m.cursor, width))
	}
	if m.width < 96 {
		return left
	}
	if len(m.result.LayerDiffs) > 0 {
		d := m.result.LayerDiffs[m.cursor]
		rightWidth := m.width - width - 5
		for i, command := range []string{d.BeforeCommand, d.AfterCommand} {
			label := []string{"Before", "After"}[i]
			if command == "" {
				command = "Absent"
			}
			right = append(right, m.workspaceTitle(label))
			right = append(right, workspaceCommand(command, rightWidth, max(2, (m.height-8)/2))...)
			right = append(right, "")
		}
		right = append(right, "Filesystem change", signedBytes(d.BeforeNetDelta)+" → "+signedBytes(d.AfterNetDelta))
	}
	return m.workspaceColumns(left, right)
}

func workspaceCommand(command string, width, rows int) []string {
	lines := strings.Split(ansi.Wrap(compareTextLine(command), width, " "), "\n")
	if len(lines) > rows {
		lines = lines[:rows]
		lines[rows-1] = ansi.Truncate(lines[rows-1], max(0, width-1), "") + "…"
	}
	return lines
}
