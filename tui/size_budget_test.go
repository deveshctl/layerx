package tui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/image"
	"github.com/deveshctl/layerx/internal/sizebudget"
	"github.com/stretchr/testify/assert"
)

func TestHeaderKeepsSizeBudgetAtNormalTerminalWidth(t *testing.T) {
	for _, platform := range []string{"", "linux/amd64"} {
		m := NewModel(Config{ImageRef: "layerx-feature-check:build", Platform: platform, SizeBudget: sizebudget.Config{Max: "0B"}})
		t.Cleanup(m.fetchCancel)
		m.width = 80
		m.analysis = &image.Analysis{UncompressedSize: 71680, Layers: []image.Layer{{}}}
		header := ansi.Strip(m.renderHeader())
		assert.Contains(t, header, "71.68 KB > 0B budget")
		assert.LessOrEqual(t, ansi.StringWidth(header), 80)
	}
}
