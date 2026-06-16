package cmd

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/require"
)

// updateGolden flips on with `go test ./cmd -update` to regenerate golden
// files after an intentional schema change. Without it, tests assert byte-
// for-byte equality and fail on any drift — which is the point: the JSON
// export shape is a public contract that downstream consumers parse, and
// silent additions or reorderings break them. The flag follows the
// stdlib-`go` convention used throughout the standard library.
var updateGolden = flag.Bool("update", false, "update golden test files in cmd/testdata/golden")

// goldenAssert compares got against the file at testdata/golden/<name>.
// With -update, writes got and skips the equality check so the same run
// regenerates the entire golden corpus.
func goldenAssert(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, got, 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "golden file missing — run `go test ./cmd -update`")
	if !bytes.Equal(got, want) {
		t.Fatalf("golden mismatch for %s\n--- want ---\n%s\n--- got ---\n%s\n(run `go test ./cmd -update` if intentional)", name, want, got)
	}
}

// TestJSONExport_Golden locks the on-disk shape of `layerx --json` for a
// canonical fixture. Existing TestJSONExport_SchemaRoundTrip catches a
// dropped field; this test catches the full surface — field order, indent,
// trailing newline, every leaf key name. Together they fence in the
// contract from both sides.
func TestJSONExport_Golden(t *testing.T) {
	analysis := &image.Analysis{
		ImageRef:     "alpine:3",
		TotalSize:    1024,
		StackedTrees: []*image.FileTree{image.NewFileTree()},
		Layers: []image.Layer{
			{Index: 0, ID: "sha256:abc", Size: 1024, NetDelta: 1024, Command: "ADD rootfs"},
		},
	}
	efficiency := &image.EfficiencyResult{
		Score:       1.0,
		WastedBytes: 0,
	}

	data, err := json.MarshalIndent(buildJSONExport(analysis, efficiency), "", "  ")
	require.NoError(t, err)

	goldenAssert(t, "json-export-minimal.json", data)
}
