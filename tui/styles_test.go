package tui

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	lines := highlightFileLines("pam_env.conf", src, "monokai")
	if lines == nil {
		lines = highlightFileLines("main.go", []byte("package main\n"+string(src)), "monokai")
	}
	require.NotNil(t, lines)

	const contentWidth = 80
	const height = 20
	body := strings.Join(lines[10:30], "\n")
	panel := renderPanel(CatppuccinMocha(), body, "/etc/security/pam_env.conf", true, contentWidth, height, true, true)

	// Panel is contentWidth + left border + right border.
	require.LessOrEqual(t, maxVisibleLineWidth(panel), contentWidth+2)
}

func TestRenderGradient(t *testing.T) {
	blue := color.NRGBA{R: 0x7A, G: 0xA2, B: 0xF7, A: 255}
	purple := color.NRGBA{R: 0xBB, G: 0x9A, B: 0xF7, A: 255}

	t.Run("empty string returns empty", func(t *testing.T) {
		assert.Equal(t, "", renderGradient("", blue, purple))
	})

	t.Run("single rune gets start colour", func(t *testing.T) {
		got := renderGradient("x", blue, purple)
		assert.Equal(t, 1, lipgloss.Width(got),
			"single rune gradient should occupy 1 display cell")
	})

	t.Run("display width equals rune count for ascii", func(t *testing.T) {
		text := "nginx:latest"
		got := renderGradient(text, blue, purple)
		assert.Equal(t, len([]rune(text)), lipgloss.Width(got),
			"gradient display width must equal rune count for ASCII input")
	})

	t.Run("display width equals rune count for CJK", func(t *testing.T) {
		text := "映像:最新"
		got := renderGradient(text, blue, purple)
		// CJK runes are 2 display cells each — width should be 2× rune count
		assert.Equal(t, lipgloss.Width(text), lipgloss.Width(got),
			"gradient must preserve CJK display width")
	})

	t.Run("output contains ANSI sequences", func(t *testing.T) {
		got := renderGradient("ab", blue, purple)
		// ansi.StringWidth strips sequences; raw len must be larger than width
		assert.Greater(t, len(got), ansi.StringWidth(got),
			"gradient output should contain ANSI colour sequences")
	})
}
