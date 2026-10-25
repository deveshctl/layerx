package tui

import (
	"strings"
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
		assert.Contains(t, header, "71.68 KB / 0B budget [OVER BUDGET]")
		assert.LessOrEqual(t, ansi.StringWidth(header), 80)
	}
}

func TestHeaderBudgetBadgeBoundariesAndNarrowLayouts(t *testing.T) {
	for _, width := range []int{60, 80, 120} {
		for _, size := range []int64{0, 1000, 1001} {
			m := NewModel(Config{ImageRef: "app:local", Platform: "linux/amd64", SizeBudget: sizebudget.Config{Max: "1KB"}})
			t.Cleanup(m.fetchCancel)
			m.width = width
			m.analysis = &image.Analysis{UncompressedSize: size}
			header := ansi.Strip(m.renderHeader())
			assert.Equal(t, size > 1000, strings.Contains(header, "[OVER BUDGET]"), header)
			assert.LessOrEqual(t, ansi.StringWidth(header), width)
		}
	}
}
