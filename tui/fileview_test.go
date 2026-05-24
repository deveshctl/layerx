package tui

import (
	"strings"
	"testing"

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
	body := renderFileView(viewerParams{
		content: &image.FileContent{
			Path: "app.go",
			Data: []byte("package main\n"),
			Size: 13,
		},
		offset: 0,
		width:  80,
		height: 10,
	})
	assert.Contains(t, body, "\x1b[")
}

func TestRenderFileViewSearchDisablesSyntaxHighlighting(t *testing.T) {
	body := renderFileView(viewerParams{
		content: &image.FileContent{
			Path: "app.go",
			Data: []byte("package main\n"),
			Size: 13,
		},
		offset:      0,
		width:       80,
		height:      10,
		searchQuery: "main",
	})
	assert.NotContains(t, body, "\x1b[38;5;")
}
