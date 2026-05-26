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
	lines := highlightFileLines("main.go", src)
	require.NotNil(t, lines)
	require.Len(t, lines, 3)
	assert.True(t, strings.Contains(lines[0], "\x1b["), "expected ANSI color codes in highlighted output")
}

func TestHighlightFileLinesUnknownExtension(t *testing.T) {
	src := []byte("#!/bin/sh\necho hello\n")
	lines := highlightFileLines("run.sh", src)
	require.NotNil(t, lines)
	assert.True(t, strings.Contains(lines[0], "\x1b["))
}

func TestRenderFileViewSyntaxHighlighting(t *testing.T) {
	src := []byte("package main\n")
	body := renderFileView(viewerParams{
		content: &image.FileContent{
			Path: "app.go",
			Data: src,
			Size: 13,
		},
		offset:           0,
		width:            80,
		height:           10,
		highlightedLines: highlightFileLines("app.go", src),
	})
	assert.Contains(t, body, "\x1b[")
}

func TestRenderFileView_ScrolledDoesNotExceedWidth(t *testing.T) {
	data := []byte(strings.Repeat("# comment line with some text\n", 74))
	body := renderFileView(viewerParams{
		content: &image.FileContent{
			Path: "/etc/security/pam_env.conf",
			Data: data,
			Size: int64(len(data)),
		},
		offset:           36,
		width:            120,
		height:           30,
		highlightedLines: highlightFileLines("/etc/security/pam_env.conf", data),
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
		content: &image.FileContent{
			Path: "app.go",
			Data: src,
			Size: 13,
		},
		offset:           0,
		width:            80,
		height:           10,
		searchQuery:      "main",
		highlightedLines: highlightFileLines("app.go", src),
	})
	assert.NotContains(t, body, "\x1b[38;5;")
}

func TestRenderFileView_TitleTruncation_WideChar(t *testing.T) {
	// 30 CJK characters → display width 60. Title budget is 40 columns
	// of cmd, so the rendered cmd segment must fit within 40 cols and
	// end in an ellipsis. Use ansi.Truncate's behavior as the contract.
	wideCmd := strings.Repeat("中", 30)
	body := renderFileView(viewerParams{
		content: &image.FileContent{
			Path: "/etc/hosts",
			Data: []byte("a\n"),
			Size: 2,
		},
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
		content: &image.FileContent{Path: "x.txt", Data: data, Size: int64(len(data))},
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
