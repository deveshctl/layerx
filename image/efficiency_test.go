package image

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	assert.Equal(t, int64(100), result.WastedBytes)
	expectedScore := 1.0 - 100.0/300.0
	assert.InDelta(t, expectedScore, result.Score, 0.001)
	assert.Len(t, result.WastedFiles, 1)
	assert.Equal(t, "/etc/config", result.WastedFiles[0].Path)
	assert.Equal(t, int64(100), result.WastedFiles[0].TotalWasted)
	assert.Equal(t, 2, result.WastedFiles[0].LayerCount)
}

func TestEfficiency_ThreeLayers_FileInAll(t *testing.T) {
	layers := []Layer{
		{Index: 0, Size: 1000, Tree: makeTree(makeFile("app", "/app", 1000))},
		{Index: 1, Size: 1500, Tree: makeTree(makeFile("app", "/app", 1500))},
		{Index: 2, Size: 2000, Tree: makeTree(makeFile("app", "/app", 2000))},
	}
	result := Efficiency(layers)
	assert.Equal(t, int64(2500), result.WastedBytes)
	expectedScore := 1.0 - 2500.0/4500.0
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
	// /big waste = 1000 (layer 0 copy), /small waste = 10 (layer 0 copy)
	assert.Equal(t, "/big", result.WastedFiles[0].Path)
	assert.Equal(t, "/small", result.WastedFiles[1].Path)
}

func TestEfficiency_StableOrderOnEqualWaste(t *testing.T) {
	// Three files with identical size. Each appears in two layers, so each
	// produces identical TotalWasted. Without the Path tiebreaker, sort.Slice
	// (pdqsort) reorders runs non-deterministically — 50 runs catches it.
	build := func() []Layer {
		return []Layer{
			{Index: 0, Size: 300, Tree: makeTree(
				makeFile("c", "/c", 100),
				makeFile("a", "/a", 100),
				makeFile("b", "/b", 100),
			)},
			{Index: 1, Size: 300, Tree: makeTree(
				makeFile("c", "/c", 100),
				makeFile("a", "/a", 100),
				makeFile("b", "/b", 100),
			)},
		}
	}

	for range 50 {
		result := Efficiency(build())
		require.Len(t, result.WastedFiles, 3)
		assert.Equal(t, "/a", result.WastedFiles[0].Path)
		assert.Equal(t, "/b", result.WastedFiles[1].Path)
		assert.Equal(t, "/c", result.WastedFiles[2].Path)
	}
}

// install_clean_reinstall: a file is added, deleted via whiteout, and a
// different file appears at the same path. The two distinct files live in
// separate runs and neither is wasted. Pre-Round-8 the algorithm counted
// both copies as a duplicate occurrence of the same path and flagged the
// first as waste — the canonical apt-get install + apt-get clean +
// apt-get install bug.
func TestEfficiency_InstallCleanReinstall_NoWaste(t *testing.T) {
	layers := []Layer{
		{Index: 0, Size: 100, Tree: makeTree(
			makeDir("var", "/var",
				makeFile("x", "/var/x", 100),
			),
		)},
		{Index: 1, Size: 0, Tree: makeTree(
			makeDir("var", "/var",
				makeFile(".wh.x", "/var/.wh.x", 0),
			),
		)},
		{Index: 2, Size: 60, Tree: makeTree(
			makeDir("var", "/var",
				makeFile("x", "/var/x", 60),
			),
		)},
	}
	result := Efficiency(layers)
	assert.Equal(t, int64(0), result.WastedBytes,
		"a deletion between two writes means the first copy was properly cleaned up — not wasted")
	assert.Empty(t, result.WastedFiles)
}

// duplicate_in_same_run: a file is added, then modified in the next layer
// without any deletion in between. The first copy is shadowed by the second
// — that is wasted bytes.
func TestEfficiency_DuplicateInSameRun_IsWasted(t *testing.T) {
	layers := []Layer{
		{Index: 0, Size: 100, Tree: makeTree(
			makeDir("etc", "/etc",
				makeFile("x", "/etc/x", 100),
			),
		)},
		{Index: 1, Size: 100, Tree: makeTree(
			makeDir("etc", "/etc",
				makeFile("x", "/etc/x", 100),
			),
		)},
	}
	result := Efficiency(layers)
	assert.Equal(t, int64(100), result.WastedBytes)
	require.Len(t, result.WastedFiles, 1)
	assert.Equal(t, "/etc/x", result.WastedFiles[0].Path)
	assert.Equal(t, int64(100), result.WastedFiles[0].TotalWasted)
}

// install -> modify -> clean -> reinstall: only the install->modify pair
// inside the first run contributes waste; the post-clean reinstall is a
// fresh run with one occurrence (no waste).
func TestEfficiency_InstallModifyCleanReinstall_OnlyFirstRunWasted(t *testing.T) {
	layers := []Layer{
		{Index: 0, Size: 100, Tree: makeTree(
			makeDir("etc", "/etc",
				makeFile("x", "/etc/x", 100),
			),
		)},
		{Index: 1, Size: 80, Tree: makeTree(
			makeDir("etc", "/etc",
				makeFile("x", "/etc/x", 80),
			),
		)},
		{Index: 2, Size: 0, Tree: makeTree(
			makeDir("etc", "/etc",
				makeFile(".wh.x", "/etc/.wh.x", 0),
			),
		)},
		{Index: 3, Size: 60, Tree: makeTree(
			makeDir("etc", "/etc",
				makeFile("x", "/etc/x", 60),
			),
		)},
	}
	result := Efficiency(layers)
	assert.Equal(t, int64(100), result.WastedBytes,
		"first run has occurrences (0,100)+(1,80); only the size-100 layer-0 copy is shadowed; the post-clean reinstall starts a new run")
}

// Opaque whiteout breaks a run just like an explicit per-file whiteout.
func TestEfficiency_OpaqueWhiteout_BreaksRun(t *testing.T) {
	layers := []Layer{
		{Index: 0, Size: 100, Tree: makeTree(
			makeDir("var", "/var",
				makeDir("cache", "/var/cache",
					makeFile("x", "/var/cache/x", 100),
				),
			),
		)},
		{Index: 1, Size: 0, Tree: makeTree(
			makeDir("var", "/var",
				makeDir("cache", "/var/cache",
					makeFile(".wh..wh..opq", "/var/cache/.wh..wh..opq", 0),
				),
			),
		)},
		{Index: 2, Size: 60, Tree: makeTree(
			makeDir("var", "/var",
				makeDir("cache", "/var/cache",
					makeFile("x", "/var/cache/x", 60),
				),
			),
		)},
	}
	result := Efficiency(layers)
	assert.Equal(t, int64(0), result.WastedBytes,
		"opaque whiteout should reset run; the post-opaque copy is fresh")
}

// regular_file_replaced_by_hardlink: a file is added as real bytes in layer 0
// and replaced by a hardlink at the same path in layer 1. The original 1KB
// still ships in layer 0's tar even though the live filesystem now points
// elsewhere — those bytes are dead weight and must be charged as waste.
// Pre-fix indexTree skipped the hardlink at snapshot 1, so pathRuns flushed
// the run as a single occurrence and the 1KB silently vanished from the
// total.
func TestEfficiency_RegularFileReplacedByHardlink_IsWasted(t *testing.T) {
	hardlink := makeFile("foo", "/foo", 0)
	hardlink.IsHardlink = true
	hardlink.Linkname = "/bar"

	layers := []Layer{
		{Index: 0, Size: 1000, Tree: makeTree(
			makeFile("foo", "/foo", 1000),
			makeFile("bar", "/bar", 1000),
		)},
		{Index: 1, Size: 0, Tree: makeTree(hardlink)},
	}
	result := Efficiency(layers)
	assert.Equal(t, int64(1000), result.WastedBytes,
		"layer 0's 1KB at /foo is dead weight once layer 1 replaces it with a hardlink")
	// liveBytes = /bar (1000); /foo at the final snapshot is a hardlink
	// (size 0 in tar). totalBytes = 1000 live + 1000 wasted = 2000.
	assert.Equal(t, 0.5, result.Score, "score must reflect 1KB waste against 2KB total")
	require.Len(t, result.WastedFiles, 1)
	assert.Equal(t, "/foo", result.WastedFiles[0].Path)
	assert.Equal(t, int64(1000), result.WastedFiles[0].TotalWasted)
	// LayerCount counts byte-contributing occurrences only — the size=0
	// hardlink replacement extends the run but does not contribute bytes.
	assert.Equal(t, 1, result.WastedFiles[0].LayerCount,
		"only layer 0 contributed bytes; the layer-1 hardlink is size 0")
}

// EfficiencyFromAnalysis must agree with Efficiency on the same inputs and
// avoid the redundant Stack call.
func TestEfficiencyFromAnalysis_MatchesEfficiency(t *testing.T) {
	layers := []Layer{
		{Index: 0, Size: 100, Tree: makeTree(
			makeFile("x", "/x", 100),
		)},
		{Index: 1, Size: 200, Tree: makeTree(
			makeFile("x", "/x", 200),
		)},
	}
	stacked := Stack(layers)
	a := &Analysis{Layers: layers, StackedTrees: stacked}

	fromAnalysis := EfficiencyFromAnalysis(a)
	fromLayers := Efficiency(layers)

	assert.Equal(t, fromLayers.WastedBytes, fromAnalysis.WastedBytes)
	assert.InDelta(t, fromLayers.Score, fromAnalysis.Score, 0.0001)
	assert.Equal(t, fromLayers.WastedFiles, fromAnalysis.WastedFiles)
}

// TestEfficiency_Golden pins every EfficiencyResult field for one fixed
// multi-layer fixture, with each expected number derived below. Unlike the
// focused tests above (each isolating one behaviour), this locks the whole
// result — WastedBytes, Score, and the ordered WastedFiles slice — so a
// refactor that silently changes the denominator (e.g. reverting to raw
// cumulative bytes) or the waste accounting fails loudly here.
//
// Fixture (three layers):
//
//	L0: /bin/app = 1000, /lib/shared.so = 500
//	L1: /bin/app = 1200 (rewrite, same run), /etc/config = 300 (new)
//	L2: /bin/app = 1500 (rewrite, same run), /etc/config = 300 (rewrite)
//
// Per-path runs and waste. Stack classifies a path present in both the
// cumulative tree and the new layer as Modified regardless of whether its
// bytes changed — there is no identical-size shortcut for files — so a file
// re-emitted at the same size still opens a fresh Added/Modified occurrence
// that pathRuns records:
//   - /bin/app: one run [(L0,1000),(L1,1200),(L2,1500)]. All but the last
//     occurrence are shadowed → waste = 1000 + 1200 = 2200; 3 byte-contributing
//     occurrences.
//   - /lib/shared.so: single occurrence → no waste.
//   - /etc/config: L1 Added (300), L2 Modified (300) → run [(L1,300),(L2,300)];
//     the shadowed L1 copy → waste = 300; 2 occurrences.
//
// Totals:
//   - WastedBytes = 2200 + 300 = 2500.
//   - liveBytes (final stacked tree) = /bin/app 1500 + /lib/shared.so 500 +
//     /etc/config 300 = 2300.
//   - totalBytes = 2300 + 2500 = 4800.
//   - Score = 1 - 2500/4800 = 0.47916….
//   - WastedFiles = two entries, sorted by TotalWasted desc: /bin/app (2200)
//     then /etc/config (300); /lib/shared.so is omitted (zero waste).
func TestEfficiency_Golden(t *testing.T) {
	layers := []Layer{
		{Index: 0, Size: 1500, Tree: makeTree(
			makeDir("bin", "/bin", makeFile("app", "/bin/app", 1000)),
			makeDir("lib", "/lib", makeFile("shared.so", "/lib/shared.so", 500)),
		)},
		{Index: 1, Size: 1500, Tree: makeTree(
			makeDir("bin", "/bin", makeFile("app", "/bin/app", 1200)),
			makeDir("etc", "/etc", makeFile("config", "/etc/config", 300)),
		)},
		{Index: 2, Size: 1800, Tree: makeTree(
			makeDir("bin", "/bin", makeFile("app", "/bin/app", 1500)),
			makeDir("etc", "/etc", makeFile("config", "/etc/config", 300)),
		)},
	}

	result := Efficiency(layers)

	assert.Equal(t, int64(2500), result.WastedBytes)
	assert.InDelta(t, 1.0-2500.0/4800.0, result.Score, 1e-9)

	require.Len(t, result.WastedFiles, 2)
	assert.Equal(t, "/bin/app", result.WastedFiles[0].Path)
	assert.Equal(t, int64(2200), result.WastedFiles[0].TotalWasted)
	assert.Equal(t, 3, result.WastedFiles[0].LayerCount)
	assert.Equal(t, "/etc/config", result.WastedFiles[1].Path)
	assert.Equal(t, int64(300), result.WastedFiles[1].TotalWasted)
	assert.Equal(t, 2, result.WastedFiles[1].LayerCount)
}
