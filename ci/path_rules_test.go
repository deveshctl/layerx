package ci

import (
	"strings"
	"testing"

	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeLayerTree builds a minimal Layer with a per-layer FileTree containing
// the given file paths. Each path becomes one Added FileNode. Used to set up
// EvalContext.Layers for path-rule tests.
func makeLayerTree(index int, id string, addedPaths, removedPaths []string) image.Layer {
	tree := image.NewFileTree()
	for _, p := range addedPaths {
		tree.Root.AddChild(&image.FileNode{
			Name:     p,
			Path:     p,
			DiffType: image.Added,
		})
	}
	for _, p := range removedPaths {
		tree.Root.AddChild(&image.FileNode{
			Name:     p,
			Path:     p,
			DiffType: image.Removed,
		})
	}
	return image.Layer{
		Index: index,
		ID:    id,
		Tree:  tree,
	}
}

// makeRawLayerTree mirrors what `image.ParseLayerTar` produces in
// production: every entry — including overlay whiteout markers like
// `.wh.<name>` and `.wh..wh..opq` — lands as a regular FileNode with the
// default `DiffType=Unchanged`. `Removed` status is only assigned later
// in stack.go / compare.go against stacked or comparison trees, neither
// of which BlockPathRule consumes. Tests that need to verify the rule's
// whiteout-skip contract against production-shaped trees must use this.
func makeRawLayerTree(index int, id string, paths []string) image.Layer {
	tree := image.NewFileTree()
	for _, p := range paths {
		// Use the basename as Name so the whiteout predicate (which
		// matches on Name, not Path) sees the marker correctly.
		name := p
		if i := strings.LastIndex(p, "/"); i >= 0 {
			name = p[i+1:]
		}
		tree.Root.AddChild(&image.FileNode{
			Name: name,
			Path: p,
		})
	}
	return image.Layer{Index: index, ID: id, Tree: tree}
}

func TestBlockPathRule_Match(t *testing.T) {
	r := BlockPathRule{ID: "block", Patterns: []string{"/tmp/**"}}
	ctx := EvalContext{
		Layers: []image.Layer{
			makeLayerTree(0, "abc123", []string{"/tmp/foo"}, nil),
		},
	}
	results := r.Evaluate(ctx)
	require.Len(t, results, 1)
	assert.False(t, results[0].Passed)
	assert.Equal(t, "block", results[0].Name)
	assert.Contains(t, results[0].Detail, "layer 0")
	assert.Equal(t, "/tmp/foo", results[0].Actual)
}

func TestBlockPathRule_NoMatch(t *testing.T) {
	r := BlockPathRule{ID: "block", Patterns: []string{"/tmp/**"}}
	ctx := EvalContext{
		Layers: []image.Layer{
			makeLayerTree(0, "abc", []string{"/etc/passwd"}, nil),
		},
	}
	results := r.Evaluate(ctx)
	require.Len(t, results, 1, "no-match must still emit one PASS result")
	assert.True(t, results[0].Passed)
	assert.Contains(t, results[0].Threshold, "1 patterns")
}

// Spec §6.3: a layer adding /secret then a later layer whiteouting it must
// still trip the rule. The blob lives in the lower layer's tar regardless
// of the deletion.
func TestBlockPathRule_WhiteoutBypass(t *testing.T) {
	r := BlockPathRule{ID: "block", Patterns: []string{"/secret"}}
	ctx := EvalContext{
		Layers: []image.Layer{
			makeLayerTree(0, "lay0", []string{"/secret"}, nil), // wrote it
			makeLayerTree(1, "lay1", nil, nil),                 // unrelated
			makeLayerTree(2, "lay2", nil, []string{"/secret"}), // whiteout
		},
	}
	results := r.Evaluate(ctx)
	require.Len(t, results, 1)
	assert.False(t, results[0].Passed)
	assert.Contains(t, results[0].Detail, "layer 0", "must report the layer that wrote the secret, not the whiteout layer")
}

// .wh..wh..opq is the opaque-whiteout marker. It must not appear in
// findings even when the user pattern would otherwise match it (e.g.
// `**` matches everything). Uses a production-shape tree: `ParseLayerTar`
// inserts the marker as a regular FileNode (DiffType=Unchanged), so the
// rule must filter by name, not by DiffType.
func TestBlockPathRule_OpaqueWhiteoutNotMatched(t *testing.T) {
	r := BlockPathRule{ID: "block", Patterns: []string{"/some/**"}}
	ctx := EvalContext{
		Layers: []image.Layer{
			makeRawLayerTree(0, "lay0", []string{"/some/.wh..wh..opq"}),
		},
	}
	results := r.Evaluate(ctx)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed,
		"opaque whiteout marker must be skipped even when the user pattern matches its path")
}

// `.wh.<name>` per-file tombstones produced by `apt-get clean` and
// similar must not surface as user-visible findings — the underlying
// blob lives in whichever lower layer first wrote it, and that layer's
// node (named `<name>`, no `.wh.` prefix) is what the rule should report.
//
// Production shape: ParseLayerTar inserts the tombstone as a regular
// FileNode with DiffType=Unchanged. Pre-fix, BlockPathRule's
// DiffType==Removed skip never fired and a pattern like `/var/cache/**`
// would surface `/var/cache/.wh.foo` as a "blocked path".
func TestBlockPathRule_PerFileWhiteoutNotMatched(t *testing.T) {
	r := BlockPathRule{ID: "block", Patterns: []string{"/var/cache/**"}}
	ctx := EvalContext{
		Layers: []image.Layer{
			makeRawLayerTree(0, "lay0", []string{"/var/cache/.wh.foo"}),
		},
	}
	results := r.Evaluate(ctx)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed,
		"per-file whiteout tombstone must be skipped; the actual blob's layer surfaces it instead")
}

// Spec §6.2: tar-relative paths (no leading slash) must match the same
// patterns as absolute paths after normalization.
func TestBlockPathRule_LeadingSlashNormalization(t *testing.T) {
	r := BlockPathRule{ID: "block", Patterns: []string{"/tmp/**"}}
	ctx := EvalContext{
		Layers: []image.Layer{
			makeLayerTree(0, "lay0", []string{"tmp/x"}, nil), // no leading slash
		},
	}
	results := r.Evaluate(ctx)
	require.Len(t, results, 1)
	assert.False(t, results[0].Passed, "tar-relative path 'tmp/x' should match pattern '/tmp/**'")
}

// Spec §6.8: doublestar v4 lets `**` match zero segments, so /foo matches /foo/**.
func TestBlockPathRule_DoublestarZeroSegments(t *testing.T) {
	r := BlockPathRule{ID: "block", Patterns: []string{"/foo/**"}}
	ctx := EvalContext{
		Layers: []image.Layer{
			makeLayerTree(0, "lay0", []string{"/foo"}, nil),
		},
	}
	results := r.Evaluate(ctx)
	require.Len(t, results, 1)
	assert.False(t, results[0].Passed, "doublestar v4: ** matches zero segments")
}

// One path matched by multiple patterns should produce one finding, not N.
func TestBlockPathRule_MultiPatternSinglePathOneFinding(t *testing.T) {
	r := BlockPathRule{ID: "block", Patterns: []string{"/tmp/**", "**/x"}}
	ctx := EvalContext{
		Layers: []image.Layer{
			makeLayerTree(0, "lay0", []string{"/tmp/x"}, nil),
		},
	}
	results := r.Evaluate(ctx)
	failures := 0
	for _, r := range results {
		if !r.Passed {
			failures++
		}
	}
	assert.Equal(t, 1, failures, "multi-pattern match on one path must produce exactly one failure")
}

// Empty pattern list: rule should pass with a "0 patterns" detail.
func TestBlockPathRule_EmptyPatterns(t *testing.T) {
	r := BlockPathRule{ID: "block", Patterns: nil}
	ctx := EvalContext{
		Layers: []image.Layer{
			makeLayerTree(0, "lay0", []string{"/anything"}, nil),
		},
	}
	results := r.Evaluate(ctx)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)
	assert.Contains(t, results[0].Threshold, "0 patterns")
}

func TestDenyWastePathRule_Match(t *testing.T) {
	r := DenyWastePathRule{ID: "deny-waste", Patterns: []string{"**/*.pyc"}}
	ctx := EvalContext{
		Efficiency: &image.EfficiencyResult{
			WastedFiles: []image.WastedFile{
				{Path: "/usr/lib/python/foo.pyc", TotalWasted: 100, LayerCount: 2},
				{Path: "/etc/hostname", TotalWasted: 50, LayerCount: 2},
			},
		},
	}
	results := r.Evaluate(ctx)
	failures := 0
	for _, r := range results {
		if !r.Passed {
			failures++
			assert.Equal(t, "/usr/lib/python/foo.pyc", r.Actual,
				"Actual must hold the bare path; layer/byte context lives in Detail")
			assert.Contains(t, r.Detail, "2 layers")
		}
	}
	assert.Equal(t, 1, failures, "only the .pyc should match")
}

func TestDenyWastePathRule_NoMatch(t *testing.T) {
	r := DenyWastePathRule{ID: "deny-waste", Patterns: []string{"**/*.pyc"}}
	ctx := EvalContext{
		Efficiency: &image.EfficiencyResult{
			WastedFiles: []image.WastedFile{
				{Path: "/etc/hostname", TotalWasted: 50, LayerCount: 2},
			},
		},
	}
	results := r.Evaluate(ctx)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)
}

func TestDenyWastePathRule_LayerCountInDetail(t *testing.T) {
	r := DenyWastePathRule{ID: "deny-waste", Patterns: []string{"**/*.log"}}
	ctx := EvalContext{
		Efficiency: &image.EfficiencyResult{
			WastedFiles: []image.WastedFile{
				{Path: "/var/log/big.log", TotalWasted: 10485760, LayerCount: 4},
			},
		},
	}
	results := r.Evaluate(ctx)
	require.Len(t, results, 1)
	assert.False(t, results[0].Passed)
	// Actual is the bare path; layer count and byte size live in Detail.
	assert.Equal(t, "/var/log/big.log", results[0].Actual)
	assert.Contains(t, results[0].Detail, "4 layers")
	assert.True(t,
		strings.Contains(results[0].Detail, "10.0 MB") || strings.Contains(results[0].Detail, "10 MB"),
		"detail %q should contain the human-readable size", results[0].Detail)
}

func TestMaxLayerCountRule_Boundary(t *testing.T) {
	cases := []struct {
		name       string
		layerCount int
		max        int
		wantPass   bool
	}{
		{"under_threshold", 2, 3, true},
		{"at_threshold", 3, 3, true},
		{"over_threshold", 4, 3, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := MaxLayerCountRule{ID: "dedupe-cap", MaxCount: tc.max}
			ctx := EvalContext{
				Efficiency: &image.EfficiencyResult{
					WastedFiles: []image.WastedFile{
						{Path: "/x", TotalWasted: 10, LayerCount: tc.layerCount},
					},
				},
			}
			results := r.Evaluate(ctx)
			require.NotEmpty(t, results)
			anyFail := false
			for _, r := range results {
				if !r.Passed {
					anyFail = true
				}
			}
			if tc.wantPass {
				assert.False(t, anyFail, "%d layers vs max %d should pass", tc.layerCount, tc.max)
			} else {
				assert.True(t, anyFail, "%d layers vs max %d should fail", tc.layerCount, tc.max)
			}
		})
	}
}

// Defensive: even though config rejects MaxCount: 0, the rule itself must
// not divide by zero or fire for every wasted file when given zero.
func TestMaxLayerCountRule_DisabledZero(t *testing.T) {
	r := MaxLayerCountRule{ID: "dedupe-cap", MaxCount: 0}
	ctx := EvalContext{
		Efficiency: &image.EfficiencyResult{
			WastedFiles: []image.WastedFile{
				{Path: "/x", TotalWasted: 10, LayerCount: 99},
			},
		},
	}
	results := r.Evaluate(ctx)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed, "MaxCount: 0 means disabled — never fire")
}
