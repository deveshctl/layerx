package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
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
	lines := highlightFileLines("pam_env.conf", src)
	if lines == nil {
		lines = highlightFileLines("main.go", []byte("package main\n"+string(src)))
	}
	require.NotNil(t, lines)

	const contentWidth = 80
	const height = 20
	body := strings.Join(lines[10:30], "\n")
	panel := renderPanel(body, "/etc/security/pam_env.conf", true, contentWidth, height, true, true)

	// Panel is contentWidth + left border + right border.
	require.LessOrEqual(t, maxVisibleLineWidth(panel), contentWidth+2)
}
