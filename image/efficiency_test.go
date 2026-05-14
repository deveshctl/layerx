package image

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEfficiency_NilLayers(t *testing.T) {
	result := Efficiency(nil)
	assert.Equal(t, 1.0, result.Score)
	assert.Equal(t, int64(0), result.WastedBytes)
	assert.Empty(t, result.WastedFiles)
}

func TestEfficiency_SingleLayer(t *testing.T) {
	layers := []Layer{{
		Index: 0,
		Size:  1000,
		Tree: makeTree(
			makeFile("a.txt", "/a.txt", 500),
			makeFile("b.txt", "/b.txt", 500),
		),
	}}
	result := Efficiency(layers)
	assert.Equal(t, 1.0, result.Score)
	assert.Equal(t, int64(0), result.WastedBytes)
	assert.Empty(t, result.WastedFiles)
}

func TestEfficiency_TwoLayers_NoOverlap(t *testing.T) {
	layers := []Layer{
		{Index: 0, Size: 500, Tree: makeTree(makeFile("a.txt", "/a.txt", 500))},
		{Index: 1, Size: 600, Tree: makeTree(makeFile("b.txt", "/b.txt", 600))},
	}
	result := Efficiency(layers)
	assert.Equal(t, 1.0, result.Score)
	assert.Equal(t, int64(0), result.WastedBytes)
	assert.Empty(t, result.WastedFiles)
}

func TestEfficiency_TwoLayers_SameFile(t *testing.T) {
	layers := []Layer{
		{Index: 0, Size: 100, Tree: makeTree(makeFile("config", "/etc/config", 100))},
		{Index: 1, Size: 200, Tree: makeTree(makeFile("config", "/etc/config", 200))},
	}
	result := Efficiency(layers)
	assert.Equal(t, int64(200), result.WastedBytes)
	expectedScore := 1.0 - 200.0/300.0
	assert.InDelta(t, expectedScore, result.Score, 0.001)
	assert.Len(t, result.WastedFiles, 1)
	assert.Equal(t, "/etc/config", result.WastedFiles[0].Path)
	assert.Equal(t, int64(200), result.WastedFiles[0].TotalWasted)
	assert.Equal(t, 2, result.WastedFiles[0].LayerCount)
}

func TestEfficiency_ThreeLayers_FileInAll(t *testing.T) {
	layers := []Layer{
		{Index: 0, Size: 1000, Tree: makeTree(makeFile("app", "/app", 1000))},
		{Index: 1, Size: 1500, Tree: makeTree(makeFile("app", "/app", 1500))},
		{Index: 2, Size: 2000, Tree: makeTree(makeFile("app", "/app", 2000))},
	}
	result := Efficiency(layers)
	assert.Equal(t, int64(3500), result.WastedBytes)
	expectedScore := 1.0 - 3500.0/4500.0
	assert.InDelta(t, expectedScore, result.Score, 0.001)
	assert.Len(t, result.WastedFiles, 1)
	assert.Equal(t, 3, result.WastedFiles[0].LayerCount)
}

func TestEfficiency_DirectoriesNotCounted(t *testing.T) {
	layers := []Layer{
		{Index: 0, Size: 500, Tree: makeTree(
			makeDir("etc", "/etc", makeFile("passwd", "/etc/passwd", 100)),
		)},
		{Index: 1, Size: 500, Tree: makeTree(
			makeDir("etc", "/etc", makeFile("shadow", "/etc/shadow", 200)),
		)},
	}
	result := Efficiency(layers)
	assert.Equal(t, 1.0, result.Score)
	assert.Equal(t, int64(0), result.WastedBytes)
}

func TestEfficiency_NilTree(t *testing.T) {
	layers := []Layer{
		{Index: 0, Size: 500, Tree: nil},
		{Index: 1, Size: 500, Tree: makeTree(makeFile("a.txt", "/a.txt", 200))},
	}
	result := Efficiency(layers)
	assert.Equal(t, 1.0, result.Score)
	assert.Equal(t, int64(0), result.WastedBytes)
}

func TestEfficiency_WastedFilesSortedBySize(t *testing.T) {
	layers := []Layer{
		{Index: 0, Size: 1000, Tree: makeTree(
			makeFile("small", "/small", 10),
			makeFile("big", "/big", 1000),
		)},
		{Index: 1, Size: 1000, Tree: makeTree(
			makeFile("small", "/small", 20),
			makeFile("big", "/big", 900),
		)},
	}
	result := Efficiency(layers)
	assert.Len(t, result.WastedFiles, 2)
	assert.Equal(t, "/big", result.WastedFiles[0].Path)
	assert.Equal(t, "/small", result.WastedFiles[1].Path)
}
