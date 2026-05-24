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

	max := 0
	for _, ln := range strings.Split(body, "\n") {
		if w := ansi.StringWidth(ln); w > max {
			max = w
		}
	}
	require.LessOrEqual(t, max, 120)
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
