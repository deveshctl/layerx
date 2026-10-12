package tui

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
)

func TestFocusMarkerFollowsActivePane(t *testing.T) {
	m := setupModelTallTree(100)
	t.Cleanup(m.fetchCancel)
	m.width, m.height = 80, 24
	m.focus = focusLayers
	m.analysis.AggregatedTrees = image.BuildAggregatedTrees(m.analysis.Layers)
	m.aggregated = true
	for _, label := range []string{"Layers", "Layer files", "Cumulative", "Layers"} {
		out := ansi.Strip(m.View().Content)
		assert.Contains(t, out, "╭─ ▸ "+label)
		assert.Equal(t, 1, strings.Count(out, "╭─ ▸ "))
		m = send(m, keyPressSpecial(tea.KeyTab))
	}
	m.focus = focusTreeAgg
	m = send(m, tea.WindowSizeMsg{Width: 50, Height: 11})
	assert.Contains(t, ansi.Strip(m.View().Content), "╭─ ▸ Cumulative")
}

func TestInactiveTreeSelectionRetainsQuieterContext(t *testing.T) {
	m := setupModelTallTree(5)
	t.Cleanup(m.fetchCancel)
	files := m.displayTree()
	render := func(focused bool) string {
		out := renderFileTree(m.theme, m.styles, files, 2, 0, 80, 12, focused, false, "", true, false, nil, 0)
		borderColor := m.theme.UnfocusedBorder
		if focused {
			borderColor = m.theme.FocusedBorder
		}
		border := styleWithFg(borderColor).Render("│")
		for line := range strings.SplitSeq(out, "\n") {
			if strings.Contains(line, "file002") {
				return strings.TrimSuffix(strings.TrimPrefix(line, border), border)
			}
		}
		t.Fatal("retained selection disappeared")
		return ""
	}
	active, inactive := render(true), render(false)
	assert.Equal(t, ansi.Strip(active), ansi.Strip(inactive))
	assert.NotEqual(t, active, inactive)
	assert.Equal(t, 0, m.analysis.Layers[0].Index)
}

func TestPathsKeepFilenameAndDirectoryControls(t *testing.T) {
	m := setupModel()
	t.Cleanup(m.fetchCancel)
	for _, name := range []string{"settings.yaml", "配置.yaml", "e\u0301.conf"} {
		f := &image.FileNode{Name: name, Path: "/usr/local/share/company/service/production/" + name, Mode: 0644}
		original := f.Path
		for width := 20; width <= 90; width++ {
			for _, tree := range []bool{false, true} {
				for _, selected := range []bool{false, true} {
					out := formatFileNodeLine(m.theme, m.styles, f, selected, width, tree, nil, 0, "")
					assert.Contains(t, ansi.Strip(out), name, "width=%d tree=%t", width, tree)
					assert.LessOrEqual(t, lipgloss.Width(out), width)
					assert.True(t, utf8.ValidString(out))
				}
			}
		}
		assert.Equal(t, original, f.Path)
	}
	dir := &image.FileNode{Name: strings.Repeat("long-directory-", 6), Path: "/a/b/" + strings.Repeat("long-directory-", 6), IsDir: true}
	for _, collapsed := range []bool{false, true} {
		out := ansi.Strip(formatFileNodeLine(m.theme, m.styles, dir, false, 23, true, map[string]bool{dir.Path: collapsed}, 0, ""))
		glyph := "▾ "
		if collapsed {
			glyph = "▸ "
		}
		assert.Contains(t, out, glyph)
		assert.Contains(t, out, "/")
		assert.LessOrEqual(t, lipgloss.Width(out), 23)
	}
}

func TestViewerTitlePreservesFilenameAndOrigin(t *testing.T) {
	m := setupModel()
	t.Cleanup(m.fetchCancel)
	for _, width := range []int{50, 80, 120} {
		p := viewerParams{theme: m.theme, styles: m.styles, width: width, height: 12,
			content:     &image.FileContent{Path: "/usr/local/share/company/service/configuration/production/settings.yaml", Data: []byte("value"), Size: 5},
			originLayer: 0, currentLayer: 3, originCmd: "RUN echo first\n\necho second"}
		out := ansi.Strip(renderFileView(p))
		title := strings.Split(out, "\n")[0]
		assert.Contains(t, title, "settings.yaml")
		assert.Contains(t, title, "L1:")
		assert.LessOrEqual(t, lipgloss.Width(out), width)
		assert.Equal(t, 14, lipgloss.Height(out))
	}
}

func TestLayerRowsUseOneBasedNumbersWithoutChangingIndices(t *testing.T) {
	m := setupModel()
	t.Cleanup(m.fetchCancel)
	for _, index := range []int{0, 9, 99} {
		layer := image.Layer{Index: index, Command: "COPY app /app"}
		for _, selected := range []bool{false, true} {
			out := ansi.Strip(formatLayerLine(m.theme, m.styles, layer, selected, 60, sizeColDelta, 0))
			assert.Contains(t, out, fmt.Sprintf("#%d ", index+1))
			assert.Equal(t, index, layer.Index)
		}
	}
}

func TestReadyFooterRemainsAtBottomAcrossCommandsAndThemes(t *testing.T) {
	for _, theme := range []string{"tokyo-night", "catppuccin-mocha", "kanagawa", "gruvbox-dark", "rose-pine", "dracula", "oxocarbon", "cyberdream"} {
		for _, size := range [][2]int{{50, 11}, {80, 16}, {80, 24}, {120, 40}, {180, 50}} {
			for _, transparent := range []bool{false, true} {
				m := NewModel(Config{ImageRef: "app:local", Theme: theme, TransparentBg: transparent})
				m.state, m.analysis = stateReady, testAnalysis()
				m.width, m.height = size[0], size[1]
				for _, command := range []string{"", "COPY app /app", "RUN echo first\n\necho last", strings.Repeat("RUN echo 配置 ", 40)} {
					m.analysis.Layers[0].Command = command
					out := m.View().Content
					assert.Equal(t, m.height, lipgloss.Height(out), "theme=%s size=%v", theme, size)
					assert.LessOrEqual(t, lipgloss.Width(out), m.width)
					last := strings.Split(ansi.Strip(out), "\n")[m.height-1]
					assert.Contains(t, last, "? help")
				}
				m.fetchCancel()
			}
		}
	}
}
