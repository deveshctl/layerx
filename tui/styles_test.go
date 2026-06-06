package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/deveshctl/layerx/theme"
)

func maxVisibleLineWidth(content string) int {
	maxW := 0
	for ln := range strings.SplitSeq(content, "\n") {
		if w := ansi.StringWidth(ln); w > maxW {
			maxW = w
		}
	}
	return maxW
}

func TestRenderPanel_SyntaxHighlightedContentFitsWidth(t *testing.T) {
	src := []byte(strings.Repeat("# comment line with some text\n", 40))
	lines := highlightFileLines("pam_env.conf", src)
	if lines == nil {
		lines = highlightFileLines("main.go", []byte("package main\n"+string(src)))
	}
	require.NotNil(t, lines)

	const contentWidth = 80
	const height = 20
	body := strings.Join(lines[10:30], "\n")
	styles := BuildStyles(theme.Default().Palette)
	panel := renderPanel(styles, body, "/etc/security/pam_env.conf", true, contentWidth, height, true, true)

	// Panel is contentWidth + left border + right border.
	require.LessOrEqual(t, maxVisibleLineWidth(panel), contentWidth+2)
}

// TestBuildStyles_AllThemes asserts BuildStyles runs without panic
// for every registered theme and that key fields are non-zero.
// This is the back-half of TestPaletteCompleteness in theme/: the
// theme/ test guards "every Palette token is filled"; this test
// guards "every Palette token actually flows into a Styles field".
func TestBuildStyles_AllThemes(t *testing.T) {
	zero := lipgloss.Style{}
	for _, th := range theme.All() {
		t.Run(string(th.Name), func(t *testing.T) {
			s := BuildStyles(th.Palette)
			// Spot-check fields that span every Palette category.
			// Equality vs the zero-value Style suffices: BuildStyles
			// always sets at least Foreground or Background, so a
			// zero-value field means the wiring forgot it.
			require.NotEqual(t, zero, s.Added, "Added unset")
			require.NotEqual(t, zero, s.Selected, "Selected unset")
			require.NotEqual(t, zero, s.StatusKey, "StatusKey unset")
			require.NotEqual(t, zero, s.HelpTitle, "HelpTitle unset")
			require.NotEqual(t, zero, s.LayerArrow, "LayerArrow unset")
			require.NotEqual(t, zero, s.WasteTitle, "WasteTitle unset")
		})
	}
}
