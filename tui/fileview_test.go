package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHighlightFileLinesGoSource(t *testing.T) {
	src := []byte("package main\n\nfunc main() {}\n")
	lines := highlightFileLines("main.go", src, "monokai")
	require.NotNil(t, lines)
	require.Len(t, lines, 3)
	assert.True(t, strings.Contains(lines[0], "\x1b["), "expected ANSI color codes in highlighted output")
}

func TestHighlightFileLinesUnknownExtension(t *testing.T) {
	src := []byte("#!/bin/sh\necho hello\n")
	lines := highlightFileLines("run.sh", src, "monokai")
	require.NotNil(t, lines)
	assert.True(t, strings.Contains(lines[0], "\x1b["))
}

func TestRenderFileViewSyntaxHighlighting(t *testing.T) {
	src := []byte("package main\n")
	body := renderFileView(viewerParams{
		theme: CatppuccinMocha(),
		content: &image.FileContent{
			Path: "app.go",
			Data: src,
			Size: 13,
		},
		lines:            splitFileLines(src),
		offset:           0,
		width:            80,
		height:           10,
		highlightedLines: highlightFileLines("app.go", src, "monokai"),
	})
	assert.Contains(t, body, "\x1b[")
}

func TestRenderFileView_ScrolledDoesNotExceedWidth(t *testing.T) {
	data := []byte(strings.Repeat("# comment line with some text\n", 74))
	body := renderFileView(viewerParams{
		theme: CatppuccinMocha(),
		content: &image.FileContent{
			Path: "/etc/security/pam_env.conf",
			Data: data,
			Size: int64(len(data)),
		},
		lines:            splitFileLines(data),
		offset:           36,
		width:            120,
		height:           30,
		highlightedLines: highlightFileLines("/etc/security/pam_env.conf", data, "monokai"),
	})

	maxW := 0
	for ln := range strings.SplitSeq(body, "\n") {
		if w := ansi.StringWidth(ln); w > maxW {
			maxW = w
		}
	}
	require.LessOrEqual(t, maxW, 120)
}

func TestRenderFileViewSearchDisablesSyntaxHighlighting(t *testing.T) {
	src := []byte("package main\n")
	body := renderFileView(viewerParams{
		theme: CatppuccinMocha(),
		content: &image.FileContent{
			Path: "app.go",
			Data: src,
			Size: 13,
		},
		lines:            splitFileLines(src),
		offset:           0,
		width:            80,
		height:           10,
		searchQuery:      "main",
		highlightedLines: highlightFileLines("app.go", src, "monokai"),
	})
	assert.NotContains(t, body, "\x1b[38;5;")
}

func TestRenderFileView_TitleTruncation_WideChar(t *testing.T) {
	// 30 CJK characters → display width 60. Title budget is 40 columns
	// of cmd, so the rendered cmd segment must fit within 40 cols and
	// end in an ellipsis. Use ansi.Truncate's behavior as the contract.
	wideCmd := strings.Repeat("中", 30)
	body := renderFileView(viewerParams{
		theme: CatppuccinMocha(),
		content: &image.FileContent{
			Path: "/etc/hosts",
			Data: []byte("a\n"),
			Size: 2,
		},
		lines:        splitFileLines([]byte("a\n")),
		originLayer:  1,
		originCmd:    wideCmd,
		currentLayer: 2,
		offset:       0,
		width:        120,
		height:       10,
	})

	// Sanity: no rendered line exceeds the panel width.
	for ln := range strings.SplitSeq(body, "\n") {
		require.LessOrEqual(t, ansi.StringWidth(ln), 120, "panel width must not be exceeded")
	}
	// The title contains the ellipsis (proof of truncation, not raw cmd).
	assert.Contains(t, body, "…", "wide-char title must be truncated with an ellipsis")
}

func TestFileViewLineCount_TrailingNewline(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want int
	}{
		{"trailing newline (terminator)", []byte("a\nb\n"), 2},
		{"no trailing newline", []byte("a\nb"), 2},
		{"single newline", []byte("\n"), 1},
		{"single line", []byte("hello"), 1},
		{"empty", []byte{}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			content := &image.FileContent{Data: tc.data, Size: int64(len(tc.data))}
			assert.Equal(t, tc.want, fileViewLineCount(content))
		})
	}
}

func TestRenderFileView_TrailingNewlineLineCount(t *testing.T) {
	// Both rendered output (gutter rows) and fileViewLineCount must report 2
	// lines for "a\nb\n" — trailing newline is a terminator, not a separator.
	data := []byte("a\nb\n")
	body := renderFileView(viewerParams{
		theme:   CatppuccinMocha(),
		content: &image.FileContent{Path: "x.txt", Data: data, Size: int64(len(data))},
		lines:   splitFileLines(data),
		offset:  0,
		width:   80,
		height:  20,
	})

	// Gutter prefix uses width = len("2") + 1 = 2 chars padded right;
	// the simplest invariant is "exactly two non-empty content lines."
	// Search for the gutter prefixes using width-2 padding:
	// "%*d " with totalLines=2 → "%2d " → " 1 " and " 2 ".
	assert.Equal(t, 1, strings.Count(body, " 1 "), "exactly one '1' gutter row")
	assert.Equal(t, 1, strings.Count(body, " 2 "), "exactly one '2' gutter row")
	assert.Equal(t, 0, strings.Count(body, " 3 "), "no phantom '3' row from trailing \\n")
}

func TestSplitFileLines_CRLFAndTrailingNewline(t *testing.T) {
	// CRLF must produce the same line count as LF, with no \r leaking into
	// rendered lines. Trailing newline is a terminator, not a separator.
	tests := []struct {
		name string
		data []byte
		want []string
	}{
		{"crlf two lines no trailing", []byte("a\r\nb"), []string{"a", "b"}},
		{"crlf two lines trailing", []byte("a\r\nb\r\n"), []string{"a", "b"}},
		{"lf two lines trailing", []byte("a\nb\n"), []string{"a", "b"}},
		{"empty", []byte{}, nil},
		{"single newline", []byte("\n"), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := splitFileLines(tc.data)
			assert.Equal(t, tc.want, got)
			for _, line := range got {
				assert.False(t, strings.Contains(line, "\r"), "no CR should leak into rendered lines")
			}
		})
	}
}

func TestFileViewLineCount_CRLFMatchesLF(t *testing.T) {
	lf := &image.FileContent{Data: []byte("a\nb\nc\n")}
	crlf := &image.FileContent{Data: []byte("a\r\nb\r\nc\r\n")}
	assert.Equal(t, fileViewLineCount(lf), fileViewLineCount(crlf))
}

// hOffset > 0 must shift the visible window right and prefix the cut with
// "«" so users can tell the line continues to the left. Otherwise a search
// match scrolled into view looks like the start of the line.
func TestRenderFileView_HOffset_ShowsLeftMarker(t *testing.T) {
	prefix := strings.Repeat("a", 100)
	data := []byte(prefix + "MARKER suffix")
	body := renderFileView(viewerParams{
		theme:   CatppuccinMocha(),
		content: &image.FileContent{Path: "/long.txt", Data: data, Size: int64(len(data))},
		lines:   splitFileLines(data),
		offset:  0,
		hOffset: 80,
		width:   80,
		height:  10,
	})
	assert.Contains(t, body, "«", "left-cut marker must signal that text continues off-screen left")
	assert.Contains(t, body, "MARKER", "the off-screen content must now be visible")
}

func TestRenderFileView_HOffsetZero_NoLeftMarker(t *testing.T) {
	data := []byte("short line\n")
	body := renderFileView(viewerParams{
		theme:   CatppuccinMocha(),
		content: &image.FileContent{Path: "/x.txt", Data: data, Size: int64(len(data))},
		lines:   splitFileLines(data),
		offset:  0,
		hOffset: 0,
		width:   80,
		height:  10,
	})
	assert.NotContains(t, body, "«", "no marker when hOffset is zero")
}

// Even with horizontal scrolling, no rendered line may exceed the panel
// width. Without that guarantee the right border tears.
func TestRenderFileView_HOffset_StillRespectsWidth(t *testing.T) {
	data := []byte(strings.Repeat("x", 500) + "\n")
	body := renderFileView(viewerParams{
		theme:   CatppuccinMocha(),
		content: &image.FileContent{Path: "/x.txt", Data: data, Size: int64(len(data))},
		lines:   splitFileLines(data),
		offset:  0,
		hOffset: 50,
		width:   80,
		height:  10,
	})
	for ln := range strings.SplitSeq(body, "\n") {
		require.LessOrEqual(t, ansi.StringWidth(ln), 80, "panel width must not be exceeded")
	}
}

// When the search match is on a long line, the rendered output must show
// the match text. Before the fix the match was clipped by the right-side
// truncate and the user saw only "Match 1/1" with no visible highlight.
func TestRenderFileView_LongLineMatchVisibleAfterScroll(t *testing.T) {
	prefix := strings.Repeat("a", 200)
	data := []byte(prefix + "needle and rest")
	matches := [][2]int{{0, 200}}
	body := renderFileView(viewerParams{
		theme:         CatppuccinMocha(),
		content:       &image.FileContent{Path: "/long.txt", Data: data, Size: int64(len(data))},
		lines:         splitFileLines(data),
		offset:        0,
		hOffset:       170, // chosen so column 200 falls within an 80-col view
		width:         80,
		height:        10,
		searchQuery:   "needle",
		searchMatches: matches,
		searchCursor:  0,
	})
	assert.Contains(t, body, "needle", "match text must reach the rendered output")
}

// hOffset must compose with chroma-highlighted lines too: when the user
// presses 'l' on a long source line, the chroma colors before the cut
// should not bleed into broken ANSI escape sequences. ansi.TruncateLeft is
// supposed to be escape-aware; this test guards that property.
func TestRenderFileView_HOffset_PreservesChromaOutput(t *testing.T) {
	src := []byte("package main\nfunc main() { var x = " + strings.Repeat("y", 200) + " }\n")
	highlighted := highlightFileLines("app.go", src, "monokai")
	body := renderFileView(viewerParams{
		theme:            CatppuccinMocha(),
		content:          &image.FileContent{Path: "app.go", Data: src, Size: int64(len(src))},
		lines:            splitFileLines(src),
		offset:           0,
		hOffset:          50,
		width:            80,
		height:           10,
		highlightedLines: highlighted,
	})
	// No torn escape sequences: every CSI must be terminated.
	assert.NotContains(t, body, "\x1b[\x1b[", "consecutive escapes signal a torn sequence")
	for ln := range strings.SplitSeq(body, "\n") {
		require.LessOrEqual(t, ansi.StringWidth(ln), 80)
	}
}
