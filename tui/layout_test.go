package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTreeSelectionRemainsVisibleAfterNavigationAndResize(t *testing.T) {
	for _, split := range []bool{false, true} {
		for _, filtered := range []bool{false, true} {
			for _, pane := range []focus{focusTree, focusTreeAgg} {
				if !split && pane == focusTreeAgg {
					continue
				}
				t.Run(fmt.Sprintf("split=%t/filter=%t/pane=%d", split, filtered, pane), func(t *testing.T) {
					m := setupModelTallTree(100)
					t.Cleanup(m.fetchCancel)
					m.analysis.AggregatedTrees = image.BuildAggregatedTrees(m.analysis.Layers)
					m.aggregated = split
					m.focus = pane
					if filtered {
						m.filterQuery = "file"
					}
					visible := func() {
						files := m.displayTreeFor(m.focus)
						name := files[m.treeCursorFor(m.focus)].Name
						rendered := ansi.Strip(m.renderRightPanel(m.width-m.leftPanelWidth()-1, m.height-8))
						assert.Contains(t, rendered, name, "focused file must be drawn, not just counted in the title")
					}
					for _, size := range [][2]int{{120, 40}, {80, 24}, {50, 11}, {60, 15}, {120, 40}} {
						m = send(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
						visible()
						m = send(m, keyPress('G'))
						visible()
						m = send(m, keyPress('g'))
						for range 35 {
							m = send(m, keyPress('j'))
							visible()
						}
						m = send(m, keyPressSpecial(tea.KeyPgDown))
						visible()
						m = send(m, keyPressSpecial(tea.KeyPgUp))
						visible()
					}
				})
			}
		}
	}
}

func TestReadyViewStaysWithinTerminal(t *testing.T) {
	for _, theme := range []string{"tokyo-night", "catppuccin-mocha", "kanagawa", "gruvbox-dark", "rose-pine", "dracula", "oxocarbon", "cyberdream"} {
		for _, size := range [][2]int{{50, 11}, {60, 15}, {80, 24}, {89, 24}, {90, 24}, {120, 40}, {180, 50}} {
			for _, state := range []string{"normal", "message", "split", "viewer"} {
				t.Run(fmt.Sprintf("%s/%dx%d/%s", theme, size[0], size[1], state), func(t *testing.T) {
					m := setupModelTallTree(40)
					t.Cleanup(m.fetchCancel)
					m.width, m.height = size[0], size[1]
					m.theme = themeFor(theme)
					m.styles = newThemeStyles(m.theme)
					m.platform = "linux/arm64/v8"
					m.renderedImageRef = renderGradient("registry.example.com/team/service/app@sha256:"+strings.Repeat("a", 64), m.theme.GradientStart, m.theme.GradientEnd)
					m.analysis.Layers[0].Command = "RUN echo " + strings.Repeat("生产配置 e\u0301 ", 50)
					m.efficiency = &image.EfficiencyResult{Score: .83, WastedBytes: 20_000_000}
					switch state {
					case "message":
						m.statusMsg = "Saved: " + strings.Repeat("production-config-", 20) + "settings.json"
					case "split":
						m.aggregated = true
						m.focus = focusTreeAgg
						m.analysis.AggregatedTrees = image.BuildAggregatedTrees(m.analysis.Layers)
						m.filterQuery = "file"
					case "viewer":
						m.viewState = viewReady
						openViewer(&m, &image.FileContent{Path: "/app/config.yaml", Data: []byte("server: localhost\n"), Size: 18})
						m.viewSearchActive = true
						m.viewSearchQuery = strings.Repeat("long query ", 20)
					}
					out := m.View().Content
					assert.LessOrEqual(t, lipgloss.Width(out), m.width)
					assert.LessOrEqual(t, lipgloss.Height(out), m.height)
					assert.Contains(t, ansi.Strip(m.renderHeader()), m.platform)
					if state != "viewer" {
						assert.Contains(t, ansi.Strip(m.renderStatusBar(m.displayTree())), "? help")
					}
				})
			}
		}
	}
}

func TestCompactSplitPreservesBothPaneSelections(t *testing.T) {
	m := setupModelTallTree(100)
	t.Cleanup(m.fetchCancel)
	m.analysis.AggregatedTrees = image.BuildAggregatedTrees(m.analysis.Layers)
	m.aggregated = true
	m.treeCursor = 24
	m.aggCursor = 76
	m.focus = focusTreeAgg
	m = send(m, tea.WindowSizeMsg{Width: 50, Height: 11})
	assert.Contains(t, ansi.Strip(m.renderStatusBar(m.displayTree())), "[compact]")
	assert.Contains(t, ansi.Strip(m.renderRightPanel(25, 3)), "file076")
	m = send(m, keyPressSpecial(tea.KeyTab)) // layers
	m = send(m, keyPressSpecial(tea.KeyTab)) // top tree
	assert.Contains(t, ansi.Strip(m.renderRightPanel(25, 3)), "file024")
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	assert.True(t, m.aggregated)
	assert.Equal(t, 24, m.treeCursor)
	assert.Equal(t, 76, m.aggCursor)
	assert.Contains(t, ansi.Strip(m.renderRightPanel(77, 32)), "Cumulative")
}

func TestSplitFocusChangeKeepsFilteredSelectionVisible(t *testing.T) {
	m := setupModelTallTree(100)
	t.Cleanup(m.fetchCancel)
	m.analysis.AggregatedTrees = image.BuildAggregatedTrees(m.analysis.Layers)
	m.aggregated = true
	m.filterQuery = "file"
	m.focus = focusLayers
	m.treeCursor, m.aggCursor = 50, 70
	m.clampCursors()
	for range 6 {
		m = send(m, keyPressSpecial(tea.KeyTab))
		if m.isTreeFocused() {
			files := m.displayTreeFor(m.focus)
			name := files[m.treeCursorFor(m.focus)].Name
			assert.Contains(t, ansi.Strip(m.renderRightPanel(77, 12)), name)
		}
	}
}

func TestLongQueryKeepsCaretAndTailVisible(t *testing.T) {
	m := setupModel()
	t.Cleanup(m.fetchCancel)
	query := strings.Repeat("目录/e\u0301/production/", 10) + "END"
	for _, width := range []int{12, 23, 50, 80, 120} {
		for _, viewer := range []bool{false, true} {
			var line string
			if viewer {
				line = renderViewerSearchBar(viewerParams{styles: m.styles}, query, true, 12, 0, width)
			} else {
				line = renderFilterBar(m.styles, true, query, 0, width)
			}
			assert.LessOrEqual(t, lipgloss.Width(line), width)
			assert.Contains(t, ansi.Strip(line), "END█")
		}
	}
	m.filterActive = true
	m.filterQuery = query
	m = send(m, keyPress('Z'))
	assert.Equal(t, query+"Z", m.filterQuery)
	assert.Contains(t, ansi.Strip(renderFilterBar(m.styles, true, m.filterQuery, 0, 30)), "ENDZ█")
}

func TestCommandWrappingHonoursDisplayWidth(t *testing.T) {
	for _, cmd := range []string{"RUN echo " + strings.Repeat("生产配置", 120), "RUN echo " + strings.Repeat("e\u0301👩‍💻 ", 120), strings.Repeat("x", 500)} {
		for _, width := range []int{2, 12, 48, 78} {
			lines := wrapCommandLines(cmd, width, 3)
			require.Len(t, lines, 3)
			for _, line := range lines {
				assert.LessOrEqual(t, ansi.StringWidth(line), width)
			}
			assert.True(t, strings.HasSuffix(lines[2], "…"), "truncated command must be marked")
		}
	}
}

func TestCommandBarPreservesInternalBlankLines(t *testing.T) {
	m := setupModel()
	t.Cleanup(m.fetchCancel)
	for _, tc := range []struct{ name, command, want string }{
		{"single line", "RUN echo first", "▶ RUN echo first"},
		{"blank middle", "RUN echo first\n\necho FINALSTEP", "▶ RUN echo first\n  \n  echo FINALSTEP"},
		{"blank first", "\nRUN echo FINALSTEP", "▶ \n  RUN echo FINALSTEP"},
		{"truncated after blanks", "RUN echo first\n\n\necho FINALSTEP", "▶ RUN echo first\n  \n  …"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := renderCommandBar(m.styles, tc.command, 80)
			assert.Equal(t, tc.want, ansi.Strip(out))
			assert.LessOrEqual(t, lipgloss.Height(out), 3)
			assert.LessOrEqual(t, lipgloss.Width(out), 80)
		})
	}
}

func TestFilterTransitionsKeepSelectionVisible(t *testing.T) {
	for _, pane := range []struct {
		name  string
		split bool
		focus focus
	}{{"single", false, focusTree}, {"split top", true, focusTree}, {"split bottom", true, focusTreeAgg}} {
		for _, height := range []int{11, 15, 16, 17, 24, 40} {
			for _, exitKey := range []rune{tea.KeyEnter, tea.KeyBackspace} {
				t.Run(fmt.Sprintf("%s/height=%d/exit=%d", pane.name, height, exitKey), func(t *testing.T) {
					m := setupModelTallTree(100)
					t.Cleanup(m.fetchCancel)
					m.analysis.AggregatedTrees = image.BuildAggregatedTrees(m.analysis.Layers)
					m.aggregated, m.focus = pane.split, pane.focus
					m = send(m, tea.WindowSizeMsg{Width: 80, Height: 40})
					m = send(m, keyPress('G'))
					visible := func() {
						assert.Equal(t, 99, m.treeCursorFor(pane.focus))
						assert.Contains(t, ansi.Strip(m.renderRightPanel(51, m.height-8)), "file099")
					}
					visible()
					m = send(m, keyPress('/'))
					assert.True(t, m.filterActive)
					visible()
					m = send(m, tea.WindowSizeMsg{Width: 80, Height: height})
					visible()
					m = send(m, keyPressSpecial(exitKey))
					assert.False(t, m.filterActive)
					assert.Empty(t, m.filterQuery)
					visible()
				})
			}
		}
	}
}

func TestLongErrorFitsAndCanBeScrolledToEnd(t *testing.T) {
	for _, size := range [][2]int{{50, 11}, {60, 15}, {80, 24}} {
		m := setupModel()
		t.Cleanup(m.fetchCancel)
		m.width, m.height = size[0], size[1]
		m.state = stateError
		m.errMsg = strings.Repeat("proxy authentication rejected request; ", 40) + "ENDDIAGNOSTIC"
		out := m.View().Content
		assert.LessOrEqual(t, lipgloss.Width(out), m.width)
		assert.LessOrEqual(t, lipgloss.Height(out), m.height)
		assert.Contains(t, ansi.Strip(out), "q/Esc exit")
		m = send(m, keyPress('G'))
		assert.Positive(t, m.errorOffset)
		assert.Contains(t, ansi.Strip(m.View().Content), "ENDDIAGNOSTIC")
		m = send(m, keyPressSpecial(tea.KeyPgUp))
		assert.Less(t, m.errorOffset, m.errorMaxOffset())
		m = send(m, tea.WindowSizeMsg{Width: 120, Height: 100})
		assert.Zero(t, m.errorOffset)
		m = send(m, keyPressSpecial(tea.KeyEscape))
		assert.True(t, m.quitting)
	}
}
