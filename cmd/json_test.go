package cmd

import (
	"testing"

	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildJSONExport_BasicStructure(t *testing.T) {
	analysis := &image.Analysis{
		ImageRef:     "nginx:latest",
		TotalSize:    100000,
		StackedTrees: []*image.FileTree{image.NewFileTree(), image.NewFileTree()},
		Layers: []image.Layer{
			{Index: 0, ID: "aabbccddee11", Size: 60000, Command: "/bin/sh -c apt-get update"},
			{Index: 1, ID: "ff0011223344", Size: 40000, Command: "/bin/sh -c #(nop) CMD"},
		},
	}
	efficiency := &image.EfficiencyResult{
		Score:       0.95,
		WastedBytes: 5000,
		WastedFiles: []image.WastedFile{
			{Path: "/tmp/cache.tar", TotalWasted: 5000, LayerCount: 2},
		},
	}

	export := buildJSONExport(analysis, efficiency)

	assert.Equal(t, "nginx:latest", export.ImageRef)
	assert.Equal(t, int64(100000), export.TotalSize)
	assert.Equal(t, 2, export.LayerCount)
	assert.Equal(t, 0.95, export.Efficiency.Score)
	assert.Equal(t, int64(5000), export.Efficiency.WastedBytes)
	require.Len(t, export.Efficiency.WastedFiles, 1)
	assert.Equal(t, "/tmp/cache.tar", export.Efficiency.WastedFiles[0].Path)
	require.Len(t, export.Layers, 2)
	assert.Equal(t, "aabbccddee11", export.Layers[0].ID)
	assert.Equal(t, int64(60000), export.Layers[0].Size)
}

func TestBuildJSONExport_EmptyWastedFiles(t *testing.T) {
	analysis := &image.Analysis{
		ImageRef:     "alpine:latest",
		TotalSize:    5000,
		StackedTrees: []*image.FileTree{image.NewFileTree()},
		Layers:       []image.Layer{{Index: 0, ID: "abcdef123456", Size: 5000}},
	}
	efficiency := &image.EfficiencyResult{
		Score:       1.0,
		WastedBytes: 0,
		WastedFiles: nil,
	}

	export := buildJSONExport(analysis, efficiency)

	assert.NotNil(t, export.Efficiency.WastedFiles)
	assert.Len(t, export.Efficiency.WastedFiles, 0)
}

func TestBuildJSONExport_WithFileTrees(t *testing.T) {
	stacked := image.NewFileTree()
	stacked.Root.AddChild(&image.FileNode{
		Name: "etc", Path: "/etc", IsDir: true,
		Children: []*image.FileNode{
			{Name: "hostname", Path: "/etc/hostname", Size: 12, DiffType: image.Added},
		},
	})
	stacked.Root.AddChild(&image.FileNode{
		Name: "app.bin", Path: "/app.bin", Size: 4096, DiffType: image.Modified,
	})

	analysis := &image.Analysis{
		ImageRef:     "myapp:v1",
		TotalSize:    4108,
		StackedTrees: []*image.FileTree{stacked},
		Layers: []image.Layer{
			{Index: 0, ID: "layer0id1234", Size: 4108},
		},
	}
	efficiency := &image.EfficiencyResult{Score: 1.0, WastedFiles: nil}

	export := buildJSONExport(analysis, efficiency)

	require.Len(t, export.Layers, 1)
	files := export.Layers[0].Files
	require.Len(t, files, 2)

	assert.Equal(t, "/etc/hostname", files[0].Path)
	assert.Equal(t, int64(12), files[0].Size)
	assert.Equal(t, "added", files[0].DiffType)

	assert.Equal(t, "/app.bin", files[1].Path)
	assert.Equal(t, int64(4096), files[1].Size)
	assert.Equal(t, "modified", files[1].DiffType)
}

func TestDiffTypeString(t *testing.T) {
	assert.Equal(t, "added", diffTypeString(image.Added))
	assert.Equal(t, "modified", diffTypeString(image.Modified))
	assert.Equal(t, "removed", diffTypeString(image.Removed))
	assert.Equal(t, "unchanged", diffTypeString(image.Unchanged))
}

func TestBuildJSONExport_NoLayers(t *testing.T) {
	analysis := &image.Analysis{
		ImageRef:     "empty:latest",
		TotalSize:    0,
		StackedTrees: []*image.FileTree{},
		Layers:       []image.Layer{},
	}
	efficiency := &image.EfficiencyResult{Score: 1.0, WastedFiles: nil}

	export := buildJSONExport(analysis, efficiency)

	assert.Equal(t, 0, export.LayerCount)
	assert.Nil(t, export.Layers)
}
