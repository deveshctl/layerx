package tui

import (
	"strings"
	"testing"

	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
)

func makeLayer(idx int, size, delta int64, cmd string) image.Layer {
	return image.Layer{
		Index:    idx,
		ID:       "abcd1234",
		Size:     size,
		NetDelta: delta,
		Command:  cmd,
	}
}

func TestFormatLayerLine_DeltaMode_HasSignedColumn(t *testing.T) {
	l := makeLayer(1, 5_000_000, 3_000_000, "RUN apt-get install foo")
	out := formatLayerLine(l, false, 60, sizeColDelta, 30_000_000)
	// Should contain the signed delta string somewhere.
	assert.Contains(t, out, "+3.0 MB")
	// Should NOT contain the blob value when in delta mode.
	assert.NotContains(t, out, "5.0 MB")
}

func TestFormatLayerLine_BlobMode_RegressionGuard(t *testing.T) {
	l := makeLayer(1, 5_000_000, 3_000_000, "RUN apt-get install foo")
	out := formatLayerLine(l, false, 60, sizeColBlob, 30_000_000)
	// Blob value should appear; signed delta should NOT.
	assert.Contains(t, out, "4.8 MB")
	assert.NotContains(t, out, "+3.0 MB")
}

func TestFormatLayerLine_BothMode_HasBothValues(t *testing.T) {
	l := makeLayer(1, 5_000_000, 3_000_000, "RUN apt-get install foo")
	out := formatLayerLine(l, false, 80, sizeColBoth, 30_000_000)
	assert.Contains(t, out, "4.8 MB")
	assert.Contains(t, out, "+3.0 MB")
}

func TestFormatLayerLine_NegativeDeltaRendersWithMinus(t *testing.T) {
	l := makeLayer(2, 1_000_000, -2_000_000, "RUN apt-get clean")
	out := formatLayerLine(l, false, 60, sizeColDelta, 30_000_000)
	assert.Contains(t, out, "-2.0 MB")
}

func TestRenderLayers_BothMode_FallsBackToDelta_OnNarrowPanel(t *testing.T) {
	layers := []image.Layer{
		makeLayer(0, 5_000_000, 5_000_000, "FROM alpine"),
		makeLayer(1, 200_000, -2_000_000, "RUN clean"),
	}
	// width = 36 → contentWidth = 34, below the < 38 fallback threshold.
	out := renderLayers(layers, 0, 0, 36, 10, true, sizeColBoth, 8_000_000)
	// In delta-only fallback the blob value (5.0 MB / 4.8 MB) must be hidden.
	assert.NotContains(t, out, "4.8 MB")
	assert.Contains(t, out, "+5.0 MB")
}

func TestRenderLayers_FocusedTitle_ReflectsSizeMode(t *testing.T) {
	layers := []image.Layer{makeLayer(0, 1000, 1000, "FROM scratch")}
	out := renderLayers(layers, 0, 0, 60, 5, true, sizeColDelta, 1000)
	assert.Contains(t, out, "Δfs")

	out = renderLayers(layers, 0, 0, 60, 5, true, sizeColBlob, 1000)
	assert.Contains(t, out, "blob")
	assert.False(t, strings.Contains(out, "Δfs"), "blob mode title should not contain Δfs")

	out = renderLayers(layers, 0, 0, 60, 5, true, sizeColBoth, 1000)
	assert.Contains(t, out, "blob+Δfs")
}

// --- S key behavior ---------------------------------------------------------

func TestSizeColumnKey_CyclesInLayersFocus(t *testing.T) {
	m := setupModel()
	assert.Equal(t, sizeColDelta, m.sizeMode)

	m = send(m, keyPress('S'))
	assert.Equal(t, sizeColBlob, m.sizeMode)

	m = send(m, keyPress('S'))
	assert.Equal(t, sizeColBoth, m.sizeMode)

	m = send(m, keyPress('S'))
	assert.Equal(t, sizeColDelta, m.sizeMode)
}

func TestSizeColumnKey_NoOpInTreeFocus(t *testing.T) {
	m := setupModel()
	m.focus = focusTree
	before := m.sizeMode
	m = send(m, keyPress('S'))
	assert.Equal(t, before, m.sizeMode)
}

func TestSizeColumnKey_NoOpInViewerFocus(t *testing.T) {
	m := setupModel()
	m.viewState = viewReady
	before := m.sizeMode
	m = send(m, keyPress('S'))
	assert.Equal(t, before, m.sizeMode)
}

func TestFinalLiveSize_SumsNetDeltas(t *testing.T) {
	m := setupModel()
	m.analysis.Layers[0].NetDelta = 100
	m.analysis.Layers[1].NetDelta = 50
	m.analysis.Layers[2].NetDelta = -20
	assert.Equal(t, int64(130), m.finalLiveSize())
}
