package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompareFileViewsKeepSelection(t *testing.T) {
	diffs := []image.FileDiff{
		{Path: "/a", DiffType: image.Added, SizeDelta: 2},
		{Path: "/b", DiffType: image.Modified, SizeDelta: -5, ChangeReason: "mode"},
		{Path: "/c", DiffType: image.Removed, SizeDelta: -1},
	}
	index := buildCompareFileIndex(t.Context(), diffs, [2]image.CompareImage{})().(compareFileIndexMsg).index
	require.NotNil(t, index)
	m := compareModel{result: &image.CompareResult{FileDiffs: diffs}, keys: defaultKeys(), theme: themeFor(""), width: 80, height: 10, fileView: true, fileIndex: index}
	m.updateFileKeys(tea.KeyPressMsg{Code: 'j'})
	assert.Equal(t, "/b", m.selectedPath)
	m.updateFileKeys(tea.KeyPressMsg{Code: 's'})
	assert.Equal(t, 2, m.fileCursor)
	assert.Equal(t, "/b", m.fileDiffAt(m.fileCursor).Path)
	m.updateFileKeys(tea.KeyPressMsg{Code: 's'})
	assert.Zero(t, m.fileCursor)
	assert.Equal(t, "/b", m.fileDiffAt(m.fileCursor).Path)
	m.updateFileKeys(tea.KeyPressMsg{Code: 'm'})
	assert.Contains(t, m.fileBrowserView().Content, "mode")
	m.updateFileKeys(tea.KeyPressMsg{Code: '?'})
	assert.Contains(t, m.fileBrowserView().Content, "inspects text")
	m.updateFileKeys(tea.KeyPressMsg{Code: 'G'})
	assert.Equal(t, 2, m.fileCursor)
	m.updateFileKeys(tea.KeyPressMsg{Code: 'g'})
	assert.Zero(t, m.fileCursor)
	next, _ := m.Update(tea.KeyPressMsg{Code: '1'})
	assert.False(t, next.(compareModel).fileView)
	next, _ = next.(compareModel).Update(tea.KeyPressMsg{Code: '2'})
	assert.True(t, next.(compareModel).fileView)
	m.width, m.height = 25, 6
	for _, line := range strings.Split(m.fileBrowserView().Content, "\n") {
		assert.LessOrEqual(t, ansi.StringWidth(line), 25)
	}
}

func TestCompareFileIndexCancellationAndEmpty(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.Nil(t, buildCompareFileIndex(ctx, []image.FileDiff{{Path: "/a"}}, [2]image.CompareImage{})().(compareFileIndexMsg).index)
	m := compareModel{result: &image.CompareResult{}, fileView: true, theme: themeFor(""), width: 80, height: 10}
	assert.Contains(t, m.View().Content, "No file changes")
}

func TestCompareFileIndexRetainsChangedMetadataOnly(t *testing.T) {
	tree := image.NewFileTree()
	changed := &image.FileNode{Name: "changed", Path: "/changed", UID: 7}
	tree.Root.AddChild(changed)
	tree.Root.AddChild(&image.FileNode{Name: "other", Path: "/other"})
	results := [2]image.CompareImage{{Analysis: &image.Analysis{StackedTrees: []*image.FileTree{tree}}}}
	diffs := []image.FileDiff{{Path: "/changed"}}
	index := buildCompareFileIndex(t.Context(), diffs, results)().(compareFileIndexMsg).index
	require.NotNil(t, index)
	assert.Same(t, changed, index.before["/changed"])
	assert.NotContains(t, index.before, "/other")
	assert.Empty(t, index.after)
	assert.Contains(t, compareFileMeta(changed), "uid:7")
	assert.Equal(t, "— absent", compareFileMeta(nil))
}

func TestCompareFileIndexArrivalKeepsSelectedPath(t *testing.T) {
	diffs := []image.FileDiff{{Path: "/a", SizeDelta: 5}, {Path: "/b", SizeDelta: -2}}
	m := compareModel{result: &image.CompareResult{FileDiffs: diffs}, keys: defaultKeys(), fileView: true, width: 80, height: 10}
	m.updateFileKeys(tea.KeyPressMsg{Code: 'j'})
	m.updateFileKeys(tea.KeyPressMsg{Code: 's'})
	index := buildCompareFileIndex(t.Context(), diffs, [2]image.CompareImage{})().(compareFileIndexMsg)
	next, _ := m.Update(index)
	m = next.(compareModel)
	assert.Equal(t, "/b", m.fileDiffAt(m.fileCursor).Path)
	assert.Equal(t, 1, m.fileCursor)
}

func TestCompareFilePathCannotAddTerminalLines(t *testing.T) {
	m := compareModel{result: &image.CompareResult{FileDiffs: []image.FileDiff{{Path: "/bad\nrow\x1b[2J"}}}, fileView: true, theme: themeFor(""), width: 80, height: 10}
	out := m.View().Content
	assert.NotContains(t, out, "\x1b[2J")
	assert.Contains(t, out, "\\n")
	assert.LessOrEqual(t, len(strings.Split(out, "\n")), 10)
}

func BenchmarkCompareFileIndex150k(b *testing.B) {
	diffs := make([]image.FileDiff, 150000)
	for i := range diffs {
		diffs[i] = image.FileDiff{Path: fmt.Sprintf("/dir/%06d", i), SizeDelta: int64(i%1000 - 500)}
	}
	b.ReportAllocs()
	for b.Loop() {
		msg := buildCompareFileIndex(b.Context(), diffs, [2]image.CompareImage{})().(compareFileIndexMsg)
		if msg.index == nil {
			b.Fatal("missing index")
		}
	}
}

var compareFileBenchmarkModel compareModel

func BenchmarkCompareFileView150k(b *testing.B) {
	const count = 150000
	diffs := make([]image.FileDiff, count)
	tree := image.NewFileTree()
	for i := range diffs {
		path := fmt.Sprintf("/dir/%06d", i)
		diffs[i] = image.FileDiff{Path: path, DiffType: image.Modified, SizeDelta: int64(i%1000 - 500)}
		tree.Root.Children = append(tree.Root.Children, &image.FileNode{Name: path[5:], Path: path})
	}
	results := [2]image.CompareImage{{Analysis: &image.Analysis{StackedTrees: []*image.FileTree{tree}}}}
	index := buildCompareFileIndex(b.Context(), diffs, results)().(compareFileIndexMsg).index
	if index == nil {
		b.Fatal("missing index")
	}
	compareFileBenchmarkModel = compareModel{result: &image.CompareResult{FileDiffs: diffs}, fileIndex: index, fileView: true, theme: themeFor(""), width: 100, height: 30}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = compareFileBenchmarkModel.View()
	}
}

func TestComparePathFilter(t *testing.T) {
	diffs := []image.FileDiff{{Path: "/etc/nginx.conf", SizeDelta: 1}, {Path: "/lib/libssl", SizeDelta: 4}, {Path: "/etc/hosts", SizeDelta: 2}}
	index := buildCompareFileIndex(t.Context(), diffs, [2]image.CompareImage{})().(compareFileIndexMsg).index
	m := compareModel{result: &image.CompareResult{FileDiffs: diffs}, fileIndex: index, keys: defaultKeys(), fileView: true}
	next, _ := m.Update(tea.KeyPressMsg{Code: '/'})
	m = next.(compareModel)
	next, _ = m.Update(tea.KeyPressMsg{Code: 'e', Text: "/etc"})
	m = next.(compareModel)
	assert.Equal(t, 2, m.fileCount())
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(compareModel)
	m.updateFileKeys(tea.KeyPressMsg{Code: 's'})
	assert.Equal(t, "/etc/hosts", m.fileDiffAt(0).Path)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(compareModel)
	assert.Equal(t, 3, m.fileCount())
	m.fileFilter = "no matches"
	m.rebuildFileFilter()
	m.adjustFileViewport()
	assert.Zero(t, m.fileCount())
	assert.NotPanics(t, func() { m.updateFileKeys(tea.KeyPressMsg{Code: 's'}) })
}

func TestCompareFilterPasteAndHelpViewport(t *testing.T) {
	diffs := make([]image.FileDiff, 20)
	for i := range diffs {
		diffs[i] = image.FileDiff{Path: fmt.Sprintf("/path/%02d", i)}
	}
	m := compareModel{result: &image.CompareResult{FileDiffs: diffs}, keys: defaultKeys(), theme: themeFor(""), fileView: true, width: 40, height: 10}
	m.fileIndex = buildCompareFileIndex(t.Context(), diffs, [2]image.CompareImage{})().(compareFileIndexMsg).index
	for range 5 {
		m.updateFileKeys(tea.KeyPressMsg{Code: 'j'})
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: '?'})
	m = next.(compareModel)
	assert.Contains(t, ansi.Strip(m.View().Content), "/path/05")
	next, _ = m.Update(tea.KeyPressMsg{Code: '/'})
	m = next.(compareModel)
	assert.Contains(t, ansi.Strip(m.View().Content), "/path/05")
	next, _ = m.Update(tea.PasteMsg{Content: "/path/19"})
	m = next.(compareModel)
	assert.Equal(t, 1, m.fileCount())
	assert.Equal(t, "/path/19", m.fileDiffAt(0).Path)
	m.fileFilter = "missing"
	m.rebuildFileFilter()
	assert.Contains(t, ansi.Strip(m.View().Content), "No matching paths")
	assert.Contains(t, ansi.Strip(m.View().Content), "q exit")
}
