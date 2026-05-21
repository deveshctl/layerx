package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHelpLayoutColumns_SixSections_ThreeColumns(t *testing.T) {
	secs := defaultHelpSections()
	cols := helpLayoutColumns(secs)
	assert.Len(t, cols, 3)
	assert.Len(t, cols[0], 2)
	assert.Equal(t, "Layers", cols[0][1].title)
}

func TestOverlayHelp_WideTerminal_UsesHorizontalLayout(t *testing.T) {
	m := setupModel()
	m.width = 120
	m.showHelp = true
	out := m.overlayHelp()
	assert.Contains(t, out, "Layers")
	assert.Contains(t, out, "File Tree")
	assert.Contains(t, out, "Change = files in the image")
	// Both section titles appear on one screen without requiring only vertical stack order.
	assert.True(t, strings.Index(out, "Navigation") < strings.Index(out, "Wasted Files"))
}

func TestOverlayHelp_NarrowTerminal_StillHasLayersSection(t *testing.T) {
	m := setupModel()
	m.width = 80
	out := m.overlayHelp()
	assert.Contains(t, out, "Cycle size: Change")
}

func TestSizeModePanelSuffix(t *testing.T) {
	assert.Equal(t, "change", sizeModePanelSuffix(sizeColDelta))
	assert.Equal(t, "stored", sizeModePanelSuffix(sizeColBlob))
	assert.Equal(t, "stored+change", sizeModePanelSuffix(sizeColBoth))
}
