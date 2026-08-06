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
	// Use exact binary-MB multiples so FormatBytes emits clean values.
	const mb = 1024 * 1024
	l := makeLayer(1, 5*mb, 3*mb, "RUN apt-get install foo")
	out := formatLayerLine(CatppuccinMocha(), themeStyles{}, l, false, 60, sizeColDelta, 30*mb)
	// Should contain the signed delta string somewhere.
	assert.Contains(t, out, "+3.0 MB")
	// Should NOT contain the blob value when in delta mode.
	assert.NotContains(t, out, "5.0 MB")
}

func TestFormatLayerLine_BlobMode_RegressionGuard(t *testing.T) {
	const mb = 1024 * 1024
	l := makeLayer(1, 5*mb, 3*mb, "RUN apt-get install foo")
	out := formatLayerLine(CatppuccinMocha(), themeStyles{}, l, false, 60, sizeColBlob, 30*mb)
	// Blob value should appear; signed delta should NOT.
	assert.Contains(t, out, "5.0 MB")
	assert.NotContains(t, out, "+3.0 MB")
}

func TestFormatLayerLine_BothMode_HasBothValues(t *testing.T) {
	const mb = 1024 * 1024
	l := makeLayer(1, 5*mb, 3*mb, "RUN apt-get install foo")
	out := formatLayerLine(CatppuccinMocha(), themeStyles{}, l, false, 80, sizeColBoth, 30*mb)
	assert.Contains(t, out, "5.0 MB")
	assert.Contains(t, out, "+3.0 MB")
}

func TestFormatLayerLine_NegativeDeltaRendersWithMinus(t *testing.T) {
	const mb = 1024 * 1024
	l := makeLayer(2, 1*mb, -2*mb, "RUN apt-get clean")
	out := formatLayerLine(CatppuccinMocha(), themeStyles{}, l, false, 60, sizeColDelta, 30*mb)
	assert.Contains(t, out, "-2.0 MB")
}

func TestRenderLayers_BothMode_FallsBackToDelta_OnNarrowPanel(t *testing.T) {
	const mb = 1024 * 1024
	layers := []image.Layer{
		makeLayer(0, 5*mb, 5*mb, "FROM alpine"),
		makeLayer(1, 200_000, -2*mb, "RUN clean"),
	}
	// width = 36 → contentWidth = 34, below the < 38 fallback threshold.
	out := renderLayers(CatppuccinMocha(), themeStyles{}, layers, 0, 0, 36, 10, true, sizeColBoth, 8*mb)
	// In delta-only fallback the blob value (5.0 MB) must be hidden;
	// only the signed delta column is rendered.
	assert.NotContains(t, out, " 5.0 MB", "blob value (unsigned) must not appear in delta-fallback")
	assert.Contains(t, out, "+5.0 MB")
}

func TestRenderLayers_FocusedTitle_ReflectsSizeMode(t *testing.T) {
	layers := []image.Layer{makeLayer(0, 1000, 1000, "FROM scratch")}
	out := renderLayers(CatppuccinMocha(), themeStyles{}, layers, 0, 0, 60, 5, true, sizeColDelta, 1000)
	assert.Contains(t, out, sizeModeLabelChange)

	out = renderLayers(CatppuccinMocha(), themeStyles{}, layers, 0, 0, 60, 5, true, sizeColBlob, 1000)
	assert.Contains(t, out, sizeModeLabelStored)
	assert.False(t, strings.Contains(out, sizeModeLabelChange), "stored mode title should not show change label")

	out = renderLayers(CatppuccinMocha(), themeStyles{}, layers, 0, 0, 60, 5, true, sizeColBoth, 1000)
	assert.Contains(t, out, sizeModeLabelBoth)
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

func TestWrapCommandLines_BreaksAtMidpointSpace(t *testing.T) {
	// width=10, midpoint=5. The space at index 5 IS the midpoint and
	// must qualify as a break point. The old `j > width/2` loop missed
	// j == width/2 and fell through to the hard cut at width.
	got := wrapCommandLines("abcde fghij", 10, 2)
	assert.Equal(t, "abcde", got[0])
	assert.Equal(t, "fghij", got[1])
}
