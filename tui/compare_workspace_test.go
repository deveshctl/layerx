package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/image"
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
