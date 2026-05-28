package image

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fileNode builds a non-directory FileNode with the metadata fields the
// comparison cares about. Path doubles as the unique key in the live-file
// map.
func fileNode(path string, size int64, mode fs.FileMode, uid, gid int, linkname string, hardlink bool) *FileNode {
	parts := splitLast(path)
	return &FileNode{
		Name:       parts,
		Path:       path,
		Size:       size,
		Mode:       mode,
		UID:        uid,
		GID:        gid,
		Linkname:   linkname,
		IsHardlink: hardlink,
	}
}

func splitLast(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

// analysisFromTree wraps a single layer's tree as a one-layer Analysis with
// pre-stacked trees. The stacked tree IS the input tree for tests that don't
// need real Stack() behavior.
func analysisFromTree(ref string, tree *FileTree) *Analysis {
	layer := Layer{Index: 0, Size: 100, Command: "RUN noop", Tree: tree}
	return &Analysis{
		ImageRef:     ref,
		Layers:       []Layer{layer},
		StackedTrees: []*FileTree{tree},
		TotalSize:    100,
	}
}

func TestCompareAnalysis_BothNil(t *testing.T) {
	assert.Nil(t, CompareAnalysis(nil, nil))
}

func TestCompareAnalysis_NilBefore_AllAdded(t *testing.T) {
	after := analysisFromTree("after", makeTree(
		makeFile("a", "/a", 10),
		makeFile("b", "/b", 20),
	))

	r := CompareAnalysis(nil, after)
	require.NotNil(t, r)
	assert.Equal(t, "", r.Before.ImageRef)
	assert.Equal(t, 0, r.Before.LayerCount)
	assert.Equal(t, "after", r.After.ImageRef)
	assert.Equal(t, 2, r.FileSummary.AddedCount)
	assert.Equal(t, 0, r.FileSummary.RemovedCount)
	assert.Equal(t, 0, r.FileSummary.ModifiedCount)
	assert.Equal(t, int64(30), r.FileSummary.BytesAdded)
	for _, d := range r.FileDiffs {
		assert.Equal(t, Added, d.DiffType)
		assert.Equal(t, "", d.ChangeReason)
	}
}

func TestCompareAnalysis_NilAfter_AllRemoved(t *testing.T) {
	before := analysisFromTree("before", makeTree(
		makeFile("a", "/a", 10),
		makeFile("b", "/b", 20),
	))

	r := CompareAnalysis(before, nil)
	require.NotNil(t, r)
	assert.Equal(t, "", r.After.ImageRef)
	assert.Equal(t, 2, r.FileSummary.RemovedCount)
	assert.Equal(t, int64(30), r.FileSummary.BytesRemoved)
	for _, d := range r.FileDiffs {
		assert.Equal(t, Removed, d.DiffType)
	}
}

func TestCompareAnalysis_Identical_NoChanges(t *testing.T) {
	tree := makeTree(
		makeFile("a", "/a", 10),
		makeDir("etc", "/etc",
			makeFile("conf", "/etc/conf", 50),
		),
	)
	a := analysisFromTree("img", tree)

	r := CompareAnalysis(a, a)
	require.NotNil(t, r)
	assert.Empty(t, r.FileDiffs)
	assert.Empty(t, r.WasteDiffs)
	assert.Empty(t, r.Warnings)
	assert.Equal(t, FileDiffSummary{}, r.FileSummary)
	assert.Equal(t, float64(0), r.AfterEfficiency.ScoreDelta)
	assert.Equal(t, int64(0), r.AfterEfficiency.WastedBytesDelta)
	assert.False(t, r.IsRegression())
}

func TestCompareAnalysis_FileAdded(t *testing.T) {
	before := analysisFromTree("b", makeTree(makeFile("a", "/a", 10)))
	after := analysisFromTree("a", makeTree(
		makeFile("a", "/a", 10),
		makeFile("b", "/b", 20),
	))

	r := CompareAnalysis(before, after)
	require.Len(t, r.FileDiffs, 1)
	d := r.FileDiffs[0]
	assert.Equal(t, "/b", d.Path)
	assert.Equal(t, Added, d.DiffType)
	assert.Equal(t, int64(0), d.BeforeSize)
	assert.Equal(t, int64(20), d.AfterSize)
	assert.Equal(t, int64(20), d.SizeDelta)
}

func TestCompareAnalysis_FileRemoved(t *testing.T) {
	before := analysisFromTree("b", makeTree(
		makeFile("a", "/a", 10),
		makeFile("b", "/b", 20),
	))
	after := analysisFromTree("a", makeTree(makeFile("a", "/a", 10)))

	r := CompareAnalysis(before, after)
	require.Len(t, r.FileDiffs, 1)
	d := r.FileDiffs[0]
	assert.Equal(t, "/b", d.Path)
	assert.Equal(t, Removed, d.DiffType)
	assert.Equal(t, int64(20), d.BeforeSize)
	assert.Equal(t, int64(0), d.AfterSize)
	assert.Equal(t, int64(-20), d.SizeDelta)
}

func TestCompareAnalysis_SizeOnlyModified(t *testing.T) {
	before := analysisFromTree("b", makeTree(makeFile("a", "/a", 10)))
	after := analysisFromTree("a", makeTree(makeFile("a", "/a", 25)))

	r := CompareAnalysis(before, after)
	require.Len(t, r.FileDiffs, 1)
	d := r.FileDiffs[0]
	assert.Equal(t, Modified, d.DiffType)
	assert.Equal(t, "size", d.ChangeReason)
	assert.Equal(t, int64(15), d.SizeDelta)
}

func TestCompareAnalysis_ModeOnlyModified(t *testing.T) {
	before := analysisFromTree("b", makeTree(
		fileNode("/bin/sh", 100, 0644, 0, 0, "", false),
	))
	after := analysisFromTree("a", makeTree(
		fileNode("/bin/sh", 100, 0755, 0, 0, "", false),
	))

	r := CompareAnalysis(before, after)
	require.Len(t, r.FileDiffs, 1)
	d := r.FileDiffs[0]
	assert.Equal(t, Modified, d.DiffType)
	assert.Equal(t, "mode", d.ChangeReason)
	assert.Equal(t, int64(0), d.SizeDelta)
}

func TestCompareAnalysis_MultiFieldModified_CanonicalOrder(t *testing.T) {
	before := analysisFromTree("b", makeTree(
		fileNode("/x", 100, 0644, 0, 0, "", false),
	))
	after := analysisFromTree("a", makeTree(
		fileNode("/x", 200, 0755, 0, 0, "", false),
	))

	r := CompareAnalysis(before, after)
	require.Len(t, r.FileDiffs, 1)
	assert.Equal(t, "size,mode", r.FileDiffs[0].ChangeReason)
}

func TestCompareAnalysis_AllFieldChanges(t *testing.T) {
	tests := []struct {
		name    string
		before  *FileNode
		after   *FileNode
		wantStr string
	}{
		{"uid", fileNode("/x", 1, 0644, 0, 0, "", false), fileNode("/x", 1, 0644, 1, 0, "", false), "uid"},
		{"gid", fileNode("/x", 1, 0644, 0, 0, "", false), fileNode("/x", 1, 0644, 0, 2, "", false), "gid"},
		{"linkname", fileNode("/x", 1, 0644, 0, 0, "/old", false), fileNode("/x", 1, 0644, 0, 0, "/new", false), "linkname"},
		{"hardlink", fileNode("/x", 1, 0644, 0, 0, "", false), fileNode("/x", 1, 0644, 0, 0, "", true), "hardlink"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := analysisFromTree("b", makeTree(tc.before))
			after := analysisFromTree("a", makeTree(tc.after))
			r := CompareAnalysis(before, after)
			require.Len(t, r.FileDiffs, 1)
			assert.Equal(t, tc.wantStr, r.FileDiffs[0].ChangeReason)
		})
	}
}

func TestCompareAnalysis_RemovedClonesIgnored(t *testing.T) {
	// Final stacked tree retains a Removed clone; CompareAnalysis must
	// treat that path as not-present.
	tombstone := makeFile("gone", "/gone", 50)
	tombstone.DiffType = Removed

	before := analysisFromTree("b", makeTree(makeFile("a", "/a", 10)))
	after := analysisFromTree("a", makeTree(
		makeFile("a", "/a", 10),
		tombstone,
	))

	r := CompareAnalysis(before, after)
	assert.Empty(t, r.FileDiffs, "Removed clone should not surface as a real file")
}

func TestCompareAnalysis_WhiteoutsIgnored(t *testing.T) {
	wh := makeFile(".wh.gone", "/.wh.gone", 0)
	opq := makeFile(".wh..wh..opq", "/.wh..wh..opq", 0)

	before := analysisFromTree("b", makeTree(makeFile("a", "/a", 10)))
	after := analysisFromTree("a", makeTree(
		makeFile("a", "/a", 10),
		wh,
		opq,
	))

	r := CompareAnalysis(before, after)
	assert.Empty(t, r.FileDiffs, "whiteout entries must not appear in FileDiffs")
}

func TestCompareAnalysis_DirectoriesIgnored(t *testing.T) {
	before := analysisFromTree("b", makeTree(
		makeDir("etc", "/etc", makeFile("a", "/etc/a", 10)),
	))
	after := analysisFromTree("a", makeTree(
		makeDir("etc", "/etc", makeFile("a", "/etc/a", 10)),
		makeDir("opt", "/opt"),
	))

	r := CompareAnalysis(before, after)
	assert.Empty(t, r.FileDiffs, "empty new directory must not appear in FileDiffs")
}

func TestCompareAnalysis_LayerCountDiffers_Warns(t *testing.T) {
	before := &Analysis{
		ImageRef: "b",
		Layers: []Layer{
			{Index: 0, Size: 10, Command: "FROM alpine"},
			{Index: 1, Size: 20, Command: "RUN apk add"},
		},
	}
	after := &Analysis{
		ImageRef: "a",
		Layers: []Layer{
			{Index: 0, Size: 10, Command: "FROM alpine"},
			{Index: 1, Size: 20, Command: "RUN apk add"},
			{Index: 2, Size: 30, Command: "COPY ./ /app"},
		},
	}

	r := CompareAnalysis(before, after)
	require.Len(t, r.LayerDiffs, 3)
	require.NotEmpty(t, r.Warnings)
	assert.Contains(t, r.Warnings[0], "layer count differs: before=2, after=3")
	// Trailing index has zeroed before counterpart.
	assert.Equal(t, int64(0), r.LayerDiffs[2].BeforeSize)
	assert.Equal(t, int64(30), r.LayerDiffs[2].AfterSize)
	assert.Equal(t, "", r.LayerDiffs[2].BeforeCommand)
	assert.False(t, r.LayerDiffs[2].CommandsMatch, "out-of-range slot must not report CommandsMatch true")
}

func TestCompareAnalysis_CommandsDiverge_SingleWarning(t *testing.T) {
	before := &Analysis{
		Layers: []Layer{
			{Index: 0, Command: "FROM alpine"},
			{Index: 1, Command: "RUN apk add"},
			{Index: 2, Command: "COPY old /app"},
			{Index: 3, Command: "CMD [\"app\"]"},
		},
	}
	after := &Analysis{
		Layers: []Layer{
			{Index: 0, Command: "FROM alpine"},
			{Index: 1, Command: "RUN apk add"},
			{Index: 2, Command: "COPY new /app"},
			{Index: 3, Command: "CMD [\"app\"]"},
		},
	}

	r := CompareAnalysis(before, after)
	require.Len(t, r.LayerDiffs, 4)
	assert.True(t, r.LayerDiffs[0].CommandsMatch)
	assert.True(t, r.LayerDiffs[1].CommandsMatch)
	assert.False(t, r.LayerDiffs[2].CommandsMatch)
	// Index 3 commands match but follow a divergence — CommandsMatch reports
	// per-row equality, not "alignment is still trustworthy".
	assert.True(t, r.LayerDiffs[3].CommandsMatch)

	// Exactly one command-divergence warning, naming the first diverging index.
	count := 0
	for _, w := range r.Warnings {
		if w == "layer 2 command differs (rebuild may have shifted layers; later indexes may be misaligned)" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestCompareAnalysis_WasteDiffsSorted(t *testing.T) {
	// Build minimal layers where Efficiency() will report different
	// per-path waste between before and after.
	//
	// before: /a appears in layers 0+1 (waste=10), /b appears 0+1 (waste=100), /c appears 0+1 (waste=5)
	// after:  /a appears 0+1+2 (waste=20), /b only in 0   (waste=0),   /c appears 0+1 (waste=5)
	//
	// Deltas: /a +10, /b -100, /c 0 -> dropped.
	// Sort by |delta| desc: /b (100), /a (10).
	before := &Analysis{
		Layers: []Layer{
			{Index: 0, Tree: makeTree(
				makeFile("a", "/a", 10),
				makeFile("b", "/b", 100),
				makeFile("c", "/c", 5),
			)},
			{Index: 1, Tree: makeTree(
				makeFile("a", "/a", 10),
				makeFile("b", "/b", 100),
				makeFile("c", "/c", 5),
			)},
		},
	}
	after := &Analysis{
		Layers: []Layer{
			{Index: 0, Tree: makeTree(
				makeFile("a", "/a", 10),
				makeFile("b", "/b", 100),
				makeFile("c", "/c", 5),
			)},
			{Index: 1, Tree: makeTree(
				makeFile("a", "/a", 10),
				makeFile("c", "/c", 5),
			)},
			{Index: 2, Tree: makeTree(
				makeFile("a", "/a", 10),
			)},
		},
	}

	r := CompareAnalysis(before, after)
	require.Len(t, r.WasteDiffs, 2)
	assert.Equal(t, "/b", r.WasteDiffs[0].Path)
	assert.Equal(t, int64(-100), r.WasteDiffs[0].WastedDelta)
	assert.Equal(t, "/a", r.WasteDiffs[1].Path)
	assert.Equal(t, int64(10), r.WasteDiffs[1].WastedDelta)
}

func TestCompareAnalysis_WasteDiff_PathTiebreak(t *testing.T) {
	// Two paths with equal |delta| -> sort by Path asc deterministically.
	before := &Analysis{
		Layers: []Layer{
			{Index: 0, Tree: makeTree(
				makeFile("y", "/y", 50),
				makeFile("z", "/z", 50),
			)},
		},
	}
	after := &Analysis{
		Layers: []Layer{
			{Index: 0, Tree: makeTree(
				makeFile("y", "/y", 50),
				makeFile("z", "/z", 50),
			)},
			{Index: 1, Tree: makeTree(
				makeFile("y", "/y", 50),
				makeFile("z", "/z", 50),
			)},
		},
	}

	for range 5 {
		r := CompareAnalysis(before, after)
		require.Len(t, r.WasteDiffs, 2)
		assert.Equal(t, "/y", r.WasteDiffs[0].Path)
		assert.Equal(t, "/z", r.WasteDiffs[1].Path)
	}
}

func TestCompareAnalysis_IsRegression(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var r *CompareResult
		assert.False(t, r.IsRegression())
	})

	t.Run("wasted up", func(t *testing.T) {
		r := &CompareResult{
			BeforeEfficiency: EfficiencySummary{Score: 1.0, WastedBytes: 0},
			AfterEfficiency:  EfficiencySummary{Score: 1.0, WastedBytes: 100},
		}
		assert.True(t, r.IsRegression())
	})

	t.Run("score down beyond epsilon", func(t *testing.T) {
		r := &CompareResult{
			BeforeEfficiency: EfficiencySummary{Score: 0.95, WastedBytes: 50},
			AfterEfficiency:  EfficiencySummary{Score: 0.80, WastedBytes: 50},
		}
		assert.True(t, r.IsRegression())
	})

	t.Run("score down within epsilon", func(t *testing.T) {
		r := &CompareResult{
			BeforeEfficiency: EfficiencySummary{Score: 0.9, WastedBytes: 0},
			AfterEfficiency:  EfficiencySummary{Score: 0.9 - 1e-12, WastedBytes: 0},
		}
		assert.False(t, r.IsRegression())
	})

	t.Run("both improved", func(t *testing.T) {
		r := &CompareResult{
			BeforeEfficiency: EfficiencySummary{Score: 0.7, WastedBytes: 200},
			AfterEfficiency:  EfficiencySummary{Score: 0.9, WastedBytes: 50},
		}
		assert.False(t, r.IsRegression())
	})
}

func TestCompareAnalysis_DoesNotMutateInputs(t *testing.T) {
	before := &Analysis{
		ImageRef: "before",
		Layers: []Layer{
			{Index: 0, Size: 10, Command: "FROM alpine", Tree: makeTree(
				makeFile("a", "/a", 10),
			)},
		},
		StackedTrees: []*FileTree{makeTree(makeFile("a", "/a", 10))},
		TotalSize:    10,
	}
	after := &Analysis{
		ImageRef: "after",
		Layers: []Layer{
			{Index: 0, Size: 25, Command: "FROM alpine", Tree: makeTree(
				makeFile("a", "/a", 25),
				makeFile("b", "/b", 5),
			)},
		},
		StackedTrees: []*FileTree{makeTree(
			makeFile("a", "/a", 25),
			makeFile("b", "/b", 5),
		)},
		TotalSize: 30,
	}

	beforeRef := before.ImageRef
	afterRef := after.ImageRef
	beforeLayerCount := len(before.Layers)
	afterLayerCount := len(after.Layers)
	beforeFiles := collectLiveFiles(before.StackedTrees[0])
	afterFiles := collectLiveFiles(after.StackedTrees[0])

	_ = CompareAnalysis(before, after)

	assert.Equal(t, beforeRef, before.ImageRef)
	assert.Equal(t, afterRef, after.ImageRef)
	assert.Len(t, before.Layers, beforeLayerCount)
	assert.Len(t, after.Layers, afterLayerCount)
	assert.Equal(t, beforeFiles, collectLiveFiles(before.StackedTrees[0]))
	assert.Equal(t, afterFiles, collectLiveFiles(after.StackedTrees[0]))
}
