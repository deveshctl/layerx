package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompareDirectoryInvestigation(t *testing.T) {
	diffs := []image.FileDiff{
		{Path: "/etc/app.conf", BeforeSize: 10, AfterSize: 20, SizeDelta: 10},
		{Path: "/usr/lib/pkg/a.so", BeforeSize: 100, AfterSize: 300, SizeDelta: 200},
		{Path: "/usr/lib/pkg/b.so", BeforeSize: 50, AfterSize: 0, SizeDelta: -50},
	}
	index := buildCompareFileIndex(t.Context(), diffs, [2]image.CompareImage{})().(compareFileIndexMsg).index
	require.Len(t, index.directories, 2)
	assert.Equal(t, "/usr/lib", index.directories[0].path)
	assert.Equal(t, int64(150), index.directories[0].after-index.directories[0].before)
	m := compareModel{overview: true, result: &image.CompareResult{FileDiffs: diffs}, fileIndex: index, keys: defaultKeys(), width: 100, height: 24, theme: themeFor("")}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(compareModel)
	assert.True(t, m.fileView)
	assert.False(t, m.overview)
	assert.Equal(t, 2, m.fileCount())
	m.updateFileKeys(tea.KeyPressMsg{Code: 's'})
	assert.Equal(t, "/usr/lib/pkg/a.so", m.fileDiffAt(0).Path)
	m.updateFileKeys(tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, 3, m.fileCount())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.Nil(t, compareDirectories(ctx, diffs))
}

func TestCompareOverviewResizeAndGuide(t *testing.T) {
	for _, width := range []int{20, 40, 80, 140} {
		for _, height := range []int{5, 16, 24, 40} {
			m := compareModel{overview: true, result: image.CompareAnalysis(&image.Analysis{ImageRef: "old"}, &image.Analysis{ImageRef: "new"}), keys: defaultKeys(), theme: themeFor(""), width: width, height: height}
			for _, key := range []rune{'G', '?', 'q'} {
				next, _ := m.Update(tea.KeyPressMsg{Code: key})
				m = next.(compareModel)
				out := ansi.Strip(m.View().Content)
				assert.LessOrEqual(t, len(strings.Split(out, "\n")), height)
				for _, line := range strings.Split(out, "\n") {
					assert.LessOrEqual(t, ansi.StringWidth(line), width)
				}
			}
		}
	}
}

func TestCompareLargeFileDetailsWithoutExtraction(t *testing.T) {
	node := &image.FileNode{Size: compareContentMaxBytes + 1, UID: 1000}
	c, err := loadCompareContent(t.Context(), image.FileDiff{Path: "/app/server", DiffType: image.Added}, nil, node, [2]image.CompareInput{}, [2]image.CompareImage{})
	require.Empty(t, err)
	require.NotNil(t, c)
	assert.Contains(t, c.note, "5 MiB")
	assert.Contains(t, c.metadata, "uid:1000")
}

func TestCompareMetadataJumpUsesCorrectSide(t *testing.T) {
	m := compareModel{keys: defaultKeys(), contentOpen: true, fileView: true, content: &compareContent{path: "/bin/app", note: "binary"}, fileIndex: &compareFileIndex{before: map[string]*image.FileNode{"/bin/app": {IntroducedInLayer: 2}}}, result: &image.CompareResult{LayerDiffs: []image.LayerDiff{{BeforeIndex: 0, AfterIndex: 2}, {BeforeIndex: 2, AfterIndex: -1}}}, width: 120, height: 24}
	next, _ := m.Update(tea.KeyPressMsg{Code: 'l'})
	m = next.(compareModel)
	assert.False(t, m.contentOpen)
	assert.False(t, m.fileView)
	assert.Equal(t, 1, m.cursor)
}

func TestDirectoryLeadsAreBoundedAndRanked(t *testing.T) {
	var diffs []image.FileDiff
	for i := 0; i < 100; i++ {
		diffs = append(diffs, image.FileDiff{Path: "/usr/lib/pkg/file", SizeDelta: int64(i)})
	}
	groups := compareDirectories(t.Context(), diffs)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].files, 5)
	assert.Equal(t, int64(99), groups[0].files[0].SizeDelta)
}

func TestCompareDirectoryPath(t *testing.T) {
	for file, want := range map[string]string{"/file": "/", "/etc/app.conf": "/etc", "/usr/lib/pkg/a.so": "/usr/lib", "/usr/lib/a.so": "/usr/lib", "usr/lib/pkg/a.so": "usr/lib"} {
		assert.Equal(t, want, compareDirectoryPath(file))
	}
}

func TestCompareSeparatesHeaderPanelAndFooter(t *testing.T) {
	m := compareModel{overview: true, result: image.CompareAnalysis(&image.Analysis{}, &image.Analysis{}), fileIndex: &compareFileIndex{}, theme: themeFor(""), width: 120, height: 24}
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	require.Len(t, lines, 24)
	assert.Contains(t, lines[0], "layerx")
	assert.Contains(t, lines[5], "├")
	assert.Contains(t, lines[22], "╰")
	assert.Contains(t, lines[23], "q exit")
	assert.Contains(t, lines[23], "│")
	assert.Contains(t, lines[23], "0/0 areas")
}

func TestCompareGuideAllTextReachableInSmallTerminals(t *testing.T) {
	for _, width := range []int{40, 72, 80, 120} {
		m := compareModel{overview: true, result: &image.CompareResult{}, keys: defaultKeys(), theme: themeFor(""), width: width, height: 24}
		next, _ := m.Update(tea.KeyPressMsg{Code: '?'})
		m = next.(compareModel)
		var seen strings.Builder
		for range len(m.compareGuideLines()) {
			seen.WriteString(ansi.Strip(m.View().Content))
			next, _ = m.Update(tea.KeyPressMsg{Code: 'j'})
			m = next.(compareModel)
		}
		for _, line := range m.compareGuideLines() {
			assert.Contains(t, seen.String(), strings.TrimSpace(ansi.Strip(line)))
		}
		if len(m.compareGuideLines()) > 16 {
			assert.Greater(t, m.guideOffset, 0)
		}
		next, _ = m.Update(tea.KeyPressMsg{Code: 'g'})
		m = next.(compareModel)
		assert.Zero(t, m.guideOffset)
		next, _ = m.Update(tea.KeyPressMsg{Code: 'G'})
		m = next.(compareModel)
		assert.Contains(t, ansi.Strip(m.View().Content), "opportunity.")
		next, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 60})
		m = next.(compareModel)
		assert.Zero(t, m.guideOffset)
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		assert.False(t, next.(compareModel).guideOpen)
	}
}

func TestCompareCompactFooterKeepsLabelsAndSeparators(t *testing.T) {
	m := compareModel{result: &image.CompareResult{}, contentOpen: true, content: &compareContent{rows: []compareContentRow{{after: "text", afterLine: 1}}}, theme: themeFor(""), width: 72, height: 24}
	footer := ansi.Strip(m.compareFooter(70))
	assert.Contains(t, footer, "Esc files")
	assert.Contains(t, footer, "│")
	assert.Contains(t, footer, "? guide")
	assert.LessOrEqual(t, ansi.StringWidth(footer), 70)
	m.contentOpen = false
	m.fileView = true
	m.fileFiltering = true
	assert.Contains(t, ansi.Strip(m.compareFooter(38)), "Ctrl+C")
}

func TestComparisonUsesEveryBuiltInThemeAcrossViews(t *testing.T) {
	for _, name := range []string{"tokyo-night", "catppuccin-mocha", "kanagawa", "gruvbox-dark", "rose-pine", "dracula", "oxocarbon", "cyberdream"} {
		for _, size := range [][2]int{{40, 16}, {72, 24}, {80, 24}, {140, 36}} {
			theme := themeFor(name)
			result := image.CompareAnalysis(&image.Analysis{ImageRef: "before"}, &image.Analysis{ImageRef: "after"})
			for _, view := range []string{"overview", "layers", "files", "text", "metadata", "guide"} {
				m := compareModel{result: result, fileIndex: &compareFileIndex{}, keys: defaultKeys(), theme: theme, width: size[0], height: size[1]}
				m.overview = view == "overview"
				m.fileView = view == "files"
				m.guideOpen = view == "guide"
				if view == "text" || view == "metadata" {
					m.contentOpen = true
					m.content = &compareContent{path: "/app/file", rows: []compareContentRow{{before: "before", after: "after", beforeLine: 1, afterLine: 1}}}
				}
				if view == "metadata" {
					m.content.note = "Binary file"
					m.content.metadata = "After: 42 B"
				}
				out := m.View()
				assert.Equal(t, theme.MainBg, out.BackgroundColor, "theme=%s view=%s", name, view)
				assert.Contains(t, out.Content, renderBrand(theme))
				prefix := strings.Split(styleWithFg(theme.FileName).Render("BODY"), "BODY")[0]
				assert.Contains(t, out.Content, prefix, "body should use theme foreground: %s %s", name, view)
				for _, line := range strings.Split(out.Content, "\n") {
					assert.LessOrEqual(t, ansi.StringWidth(line), size[0])
				}
				assert.LessOrEqual(t, len(strings.Split(out.Content, "\n")), size[1])
				m.theme.MainBg = nil
				assert.Nil(t, m.View().BackgroundColor, "transparent theme=%s view=%s", name, view)
			}
		}
	}
}
