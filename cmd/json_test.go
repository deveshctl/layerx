package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
			{Index: 0, ID: "aabbccddee11", Size: 60000, NetDelta: 60000, Command: "/bin/sh -c apt-get update"},
			{Index: 1, ID: "ff0011223344", Size: 40000, NetDelta: -5000, Command: "/bin/sh -c #(nop) CMD"},
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
	assert.Equal(t, int64(60000), export.Layers[0].NetDelta)
	assert.Equal(t, int64(-5000), export.Layers[1].NetDelta)
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

// TestJSONExport_SchemaRoundTrip locks the public JSON schema by marshalling
// a populated export to bytes and unmarshalling it back into a struct that
// names every consumer-facing field literally. A future tag rename or
// misplaced omitempty would break consumers; this test catches it.
func TestJSONExport_SchemaRoundTrip(t *testing.T) {
	analysis := &image.Analysis{
		ImageRef:     "nginx:latest",
		TotalSize:    100,
		StackedTrees: []*image.FileTree{image.NewFileTree()},
		Layers: []image.Layer{
			{Index: 0, ID: "abc", Size: 100, NetDelta: 100, Command: "FROM"},
		},
	}
	efficiency := &image.EfficiencyResult{
		Score:       0.9,
		WastedBytes: 10,
		WastedFiles: []image.WastedFile{{Path: "/x", TotalWasted: 10, LayerCount: 2}},
	}

	data, err := json.Marshal(buildJSONExport(analysis, efficiency))
	require.NoError(t, err)

	var schema struct {
		SchemaVersion string `json:"schemaVersion"`
		ImageRef      string `json:"imageRef"`
		TotalSize     int64  `json:"totalSize"`
		LayerCount    int    `json:"layerCount"`
		Efficiency    struct {
			Score       float64 `json:"score"`
			WastedBytes int64   `json:"wastedBytes"`
			WastedFiles []struct {
				Path        string `json:"path"`
				TotalWasted int64  `json:"totalWasted"`
				LayerCount  int    `json:"layerCount"`
			} `json:"wastedFiles"`
		} `json:"efficiency"`
		Layers []struct {
			Index    int    `json:"index"`
			ID       string `json:"id"`
			Size     int64  `json:"size"`
			NetDelta int64  `json:"netDelta"`
			Command  string `json:"command"`
			Files    []struct {
				Path     string `json:"path"`
				Size     int64  `json:"size"`
				DiffType string `json:"diffType"`
			} `json:"files"`
		} `json:"layers"`
	}
	require.NoError(t, json.Unmarshal(data, &schema))

	assert.Equal(t, jsonSchemaVersion, schema.SchemaVersion,
		"schemaVersion is part of the locked schema; if jsonSchemaVersion changes, this test must be updated to match")
	assert.Equal(t, "nginx:latest", schema.ImageRef)
	assert.Equal(t, int64(100), schema.TotalSize)
	assert.Equal(t, 1, schema.LayerCount)
	assert.Equal(t, 0.9, schema.Efficiency.Score)
	assert.Equal(t, int64(10), schema.Efficiency.WastedBytes)
	require.Len(t, schema.Efficiency.WastedFiles, 1)
	assert.Equal(t, "/x", schema.Efficiency.WastedFiles[0].Path)
	require.Len(t, schema.Layers, 1)
	assert.Equal(t, "abc", schema.Layers[0].ID)
	assert.Equal(t, int64(100), schema.Layers[0].NetDelta)
}

func TestWriteJSONAtomic_OverwritesAndCleansTmp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.json")
	require.NoError(t, os.WriteFile(target, []byte(`{"prev":true}`), 0644))

	require.NoError(t, writeJSONAtomic(target, []byte("new")))

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, []byte("new"), got)

	leftovers, err := filepath.Glob(filepath.Join(dir, ".layerx-json-*.tmp"))
	require.NoError(t, err)
	assert.Empty(t, leftovers, "tmp files must be cleaned up after success")
}

func TestWriteJSONAtomic_RenameFailureCleansUpTmp(t *testing.T) {
	// Force os.Rename to fail by making the target path a non-empty directory.
	// On every supported OS, renaming a file onto a non-empty directory fails.
	dir := t.TempDir()
	target := filepath.Join(dir, "out.json")
	require.NoError(t, os.Mkdir(target, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "blocker"), []byte("x"), 0o644))

	err := writeJSONAtomic(target, []byte(`{"x":1}`))
	require.Error(t, err)

	leftovers, err := filepath.Glob(filepath.Join(dir, ".layerx-json-*.tmp"))
	require.NoError(t, err)
	assert.Empty(t, leftovers, "tmp file must be cleaned up after rename failure")
}

func TestWriteJSONAtomic_ConcurrentRunsDontCollide(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.json")

	t.Run("group", func(t *testing.T) {
		t.Run("writer-a", func(t *testing.T) {
			t.Parallel()
			for range 20 {
				require.NoError(t, writeJSONAtomic(target, []byte(`{"who":"a"}`)))
			}
		})
		t.Run("writer-b", func(t *testing.T) {
			t.Parallel()
			for range 20 {
				require.NoError(t, writeJSONAtomic(target, []byte(`{"who":"b"}`)))
			}
		})
	})

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	// Last-writer-wins on rename: the surviving file must be valid JSON from one of the writers.
	var payload map[string]string
	require.NoError(t, json.Unmarshal(got, &payload))
	assert.Contains(t, []string{"a", "b"}, payload["who"])

	leftovers, err := filepath.Glob(filepath.Join(dir, ".layerx-json-*.tmp"))
	require.NoError(t, err)
	assert.Empty(t, leftovers, "no tmp files must remain after concurrent writes")
}

// TestJSON_SchemaVersionFirstField pins two contracts simultaneously:
//  1. The exported JSON contains "schemaVersion": "1.0.1".
//  2. SchemaVersion is the FIRST field in the output (encoding/json
//     preserves struct declaration order).
//
// (2) is what lets downstream tools `head -n2 out.json | grep schemaVersion`
// without parsing.
func TestJSON_SchemaVersionFirstField(t *testing.T) {
	analysis := &image.Analysis{
		ImageRef:     "test:latest",
		Layers:       []image.Layer{{Index: 0, ID: "abc", Size: 100}},
		StackedTrees: []*image.FileTree{image.NewFileTree()},
		TotalSize:    100,
	}
	efficiency := &image.EfficiencyResult{Score: 1.0}
	export := buildJSONExport(analysis, efficiency)

	require.Equal(t, "1.0.1", export.SchemaVersion)

	data, err := json.MarshalIndent(export, "", "  ")
	require.NoError(t, err)
	out := string(data)
	assert.Contains(t, out, `"schemaVersion": "1.0.1"`)

	// Field-order pin: schemaVersion appears before imageRef.
	svIdx := strings.Index(out, "schemaVersion")
	irIdx := strings.Index(out, "imageRef")
	require.Greater(t, svIdx, -1, "schemaVersion must be present")
	require.Greater(t, irIdx, svIdx, "schemaVersion must appear before imageRef")
}

// TestRunJSONExport_ContextCancelled verifies cancellation propagates out of
// runJSONExport AND that no partial output file is written.
func TestRunJSONExport_ContextCancelled(t *testing.T) {
	withFakeResolver(t, cancelResolver())

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "out.json")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runJSONExport(ctx, "fake:img", outPath, false)
	}()

	cancel()
	err := <-done

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled), "err = %v, want chain containing context.Canceled", err)

	_, statErr := os.Stat(outPath)
	assert.True(t, os.IsNotExist(statErr), "no output file must exist when resolve was cancelled (statErr = %v)", statErr)
}
