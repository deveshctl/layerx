package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/image"
	"github.com/deveshctl/layerx/internal/sizebudget"
	"github.com/stretchr/testify/assert"
)

func TestCompareWorkspaceSelectionAcrossResize(t *testing.T) {
	diffs := make([]image.FileDiff, 80)
	for i := range diffs {
		diffs[i] = image.FileDiff{Path: fmt.Sprintf("/app/file-%03d", i), DiffType: image.Added, AfterSize: int64(i), SizeDelta: int64(i)}
	}
	result := image.CompareAnalysis(&image.Analysis{ImageRef: "old"}, &image.Analysis{ImageRef: "new"})
	result.FileDiffs = diffs
	index := buildCompareFileIndex(t.Context(), diffs, [2]image.CompareImage{})().(compareFileIndexMsg).index
	m := compareModel{result: result, fileIndex: index, fileView: true, theme: themeFor(""), keys: defaultKeys()}
	for _, size := range [][2]int{{180, 45}, {100, 24}, {80, 20}, {140, 35}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(compareModel)
		for _, key := range []rune{'G', 'k', 'g'} {
			next, _ = m.Update(tea.KeyPressMsg{Code: key})
			m = next.(compareModel)
			out := ansi.Strip(m.View().Content)
			assert.Contains(t, out, m.fileDiffAt(m.fileCursor).Path)
			assert.Len(t, strings.Split(out, "\n"), size[1])
			for _, line := range strings.Split(out, "\n") {
				assert.LessOrEqual(t, ansi.StringWidth(line), size[0])
			}
		}
	}
}

func TestCompareWorkspaceExplainsRegressionAndEscapesRefs(t *testing.T) {
	r := image.CompareAnalysis(&image.Analysis{ImageRef: "old\x1b[2J"}, &image.Analysis{ImageRef: "new\nline"})
	r.AfterEfficiency.WastedBytesDelta = 1024
	m := compareModel{overview: true, result: r, theme: themeFor(""), width: 140, height: 35}
	out := ansi.Strip(m.View().Content)
	assert.Contains(t, out, "Layer waste increased by")
	assert.Contains(t, out, "old\\x1b[2J")
	assert.Contains(t, out, "new\\nline")
	assert.NotContains(t, out, "\x1b[2J")
}

func TestCompareKeepsEvidenceAvailableAtSplitPaneWidths(t *testing.T) {
	r := &image.CompareResult{
		Before:     image.ImageSummary{ImageRef: "/long/common/directory/for/images/old.tar", Platform: "linux/amd64"},
		After:      image.ImageSummary{ImageRef: "/long/common/directory/for/images/new.tar", Platform: "linux/arm64"},
		LayerDiffs: []image.LayerDiff{{BeforeCommand: "COPY old app", AfterCommand: "COPY new app"}},
		WasteDiffs: []image.WasteDiff{{Path: "/build/artifact", AfterWasted: 1024, WastedDelta: 1024, AfterFirstLayer: 2}},
	}
	m := compareModel{result: r, overview: true, width: 80, height: 20, theme: themeFor(""), keys: defaultKeys(),
		checks: []sizebudget.Result{{Name: "image size"}, {Name: "image growth"}}}
	press := func(code rune) string {
		next, _ := m.Update(tea.KeyPressMsg{Code: code})
		m = next.(compareModel)
		return ansi.Strip(m.View().Content)
	}
	out := ansi.Strip(m.View().Content)
	for _, value := range []string{"old.tar", "new.tar", "linux/amd64", "linux/arm64", "FAIL image size", "FAIL image growth"} {
		assert.Contains(t, out, value)
	}
	assert.Contains(t, press('c'), "FAIL image growth")
	press(tea.KeyEscape)
	m.checkError = "select the same --platform"
	m.checks = nil
	out = press('c')
	assert.Contains(t, out, "select the same --platform")
	assert.NotContains(t, out, "No size budgets configured")
	press(tea.KeyEscape)
	out = press('w')
	assert.Contains(t, out, "/build/artifact")
	assert.Contains(t, out, "After #3")
	press(tea.KeyEscape)
	press('1')
	out = press(tea.KeyEnter)
	assert.Contains(t, out, "COPY old app")
	assert.Contains(t, out, "COPY new app")
}
