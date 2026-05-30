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

// .wh..wh..opq is the opaque-whiteout marker; it must not match user
// patterns like "**/.git/**" by accident. The node is present in the
// per-layer tree as a regular file node, but its name is unique.
func TestBlockPathRule_OpaqueWhiteoutNotMatched(t *testing.T) {
	r := BlockPathRule{ID: "block", Patterns: []string{"**/.git/**"}}
	ctx := EvalContext{
		Layers: []image.Layer{
			makeLayerTree(0, "lay0", []string{"/some/.wh..wh..opq"}, nil),
		},
	}
	results := r.Evaluate(ctx)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed, "opaque whiteout filename should not trip user patterns")
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
			assert.Contains(t, r.Actual, "/usr/lib/python/foo.pyc")
			assert.Contains(t, r.Actual, "2 layers")
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
	// Actual should mention layer count and the byte size in human form.
	assert.Contains(t, results[0].Actual, "4 layers")
	assert.True(t,
		strings.Contains(results[0].Actual, "10.0 MB") || strings.Contains(results[0].Actual, "10 MB"),
		"actual %q should contain the human-readable size", results[0].Actual)
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
