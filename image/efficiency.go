package image

import "sort"

// WastedFile records a file path that appears redundantly across multiple layers.
type WastedFile struct {
	Path        string
	TotalWasted int64
	LayerCount  int
}

// EfficiencyResult holds the efficiency analysis output.
type EfficiencyResult struct {
	Score       float64
	WastedBytes int64
	WastedFiles []WastedFile
}

// Efficiency computes how much space is wasted by files appearing in multiple
// layers. It stacks layers internally; prefer EfficiencyFromAnalysis when the
// caller already has stacked trees.
//
// A file at the same path counted across runs separated by deletion (whiteout
// or opaque whiteout) is NOT considered wasted: the deleted copy was properly
// cleaned up before the new copy appeared. Within a single run (no deletion
// in between), all but the last occurrence are wasted.
func Efficiency(layers []Layer) *EfficiencyResult {
	if len(layers) == 0 {
		return &EfficiencyResult{Score: 1.0}
	}
	stacked := Stack(layers)
	return computeEfficiency(layers, stacked)
}

// EfficiencyFromAnalysis is the preferred entry point when the caller already
// has stacked trees (which Analysis carries). Avoids re-running Stack.
//
// Falls back to recomputing Stack(layers) if the carried StackedTrees are
// missing or out of sync with the layer count — defensive against an
// Analysis that was built or mutated outside AnalyzeWithOptions (e.g. by a
// test, or by a future caller that constructs an Analysis directly). The
// efficiency computation requires len(stacked) == len(layers) to align
// snapshot indices; without this guard a divergent input would produce a
// silently wrong score.
func EfficiencyFromAnalysis(a *Analysis) *EfficiencyResult {
	if a == nil || len(a.Layers) == 0 {
		return &EfficiencyResult{Score: 1.0}
	}
	stacked := a.StackedTrees
	if len(stacked) != len(a.Layers) {
		stacked = Stack(a.Layers)
	}
	return computeEfficiency(a.Layers, stacked)
}

// occurrence records a single layer-i appearance of a path with the bytes
// it occupied at that snapshot.
type efficiencyOccurrence struct {
	layerIdx int
	size     int64
}

func computeEfficiency(layers []Layer, stacked []*FileTree) *EfficiencyResult {
	// Build a path→FileNode index per stacked snapshot once. pathRuns then does
	// O(1) lookups instead of recursing through the tree once per (path,
	// snapshot) pair, which restored the analysis from quadratic to linear in
	// total file count for layered images with many shared paths.
	indices := make([]map[string]*FileNode, len(stacked))
	for i, tree := range stacked {
		if tree == nil || tree.Root == nil {
			continue
		}
		idx := make(map[string]*FileNode)
		indexTree(tree.Root, idx)
		indices[i] = idx
	}

	// Collect every path that ever appeared across the raw layers, regardless
	// of whether it was later removed. The stacked-tree walk decides occupancy
	// per snapshot.
	paths := make(map[string]struct{})
	for _, layer := range layers {
		if layer.Tree == nil || layer.Tree.Root == nil {
			continue
		}
		walkFiles(layer.Tree.Root, func(path string, _ int64) {
			paths[path] = struct{}{}
		})
	}

	var wastedBytes int64
	var wastedFiles []WastedFile

	for path := range paths {
		runs := pathRuns(path, indices)
		var pathWaste int64
		var occurrenceCount int
		for _, run := range runs {
			for _, occ := range run {
				// LayerCount documents "how many layers contributed bytes".
				// Zero-size occurrences (hardlink replacements that extend a
				// run only to keep the earlier real-file bytes chargeable)
				// are not byte-contributors and must not inflate the count.
				if occ.size > 0 {
					occurrenceCount++
				}
			}
			if len(run) < 2 {
				continue
			}
			for _, occ := range run[:len(run)-1] {
				pathWaste += occ.size
			}
		}
		if pathWaste == 0 {
			// Path appears in multiple layers but every duplicate has size 0
			// (typical for symlinks, empty marker files, or directory entries
			// that show up as files in some tar emitters). They contribute
			// nothing to wasted bytes; surfacing them in the "top wasted
			// files" list would only clutter the output.
			continue
		}
		wastedBytes += pathWaste
		wastedFiles = append(wastedFiles, WastedFile{
			Path:        path,
			TotalWasted: pathWaste,
			LayerCount:  occurrenceCount,
		})
	}

	sort.Slice(wastedFiles, func(i, j int) bool {
		if wastedFiles[i].TotalWasted != wastedFiles[j].TotalWasted {
			return wastedFiles[i].TotalWasted > wastedFiles[j].TotalWasted
		}
		return wastedFiles[i].Path < wastedFiles[j].Path
	})

	// Denominator: bytes of live files in the FINAL stacked tree plus the
	// wasted bytes we just accumulated. This avoids double-counting unchanged
	// files (which would show up as N copies in raw layers) while still
	// charging the user for redundant rewrites.
	var liveBytes int64
	if n := len(stacked); n > 0 && stacked[n-1] != nil && stacked[n-1].Root != nil {
		walkLiveFiles(stacked[n-1].Root, func(_ string, size int64) {
			liveBytes += size
		})
	}

	totalBytes := liveBytes + wastedBytes
	score := 1.0
	if totalBytes > 0 {
		score = 1.0 - float64(wastedBytes)/float64(totalBytes)
	}
	if score < 0 {
		score = 0
	}

	return &EfficiencyResult{
		Score:       score,
		WastedBytes: wastedBytes,
		WastedFiles: wastedFiles,
	}
}

// indexTree populates idx with every non-whiteout leaf or directory reachable
// from root, keyed by FileNode.Path. The map is consumed by pathRuns for O(1)
// per-path lookups across stacked snapshots.
//
// Hardlinks are admitted at every DiffType. They carry size 0 in the tar, so
// they contribute nothing to byte-accounting on their own — but a hardlink
// snapshot at the same path as a prior real-file occurrence keeps the run
// going so the prior bytes are charged as waste. pathRuns flushes only on
// Removed or absence, so a Modified or Added hardlink (which is what Stack
// emits when a hardlink replaces or reintroduces a real file at the same
// path) extends the run past the prior real occurrence; computeEfficiency
// then charges run[:len-1] as waste. The previous behaviour — skipping all
// hardlinks unconditionally — caused regular-file → hardlink replacements
// to silently vanish from the waste total.
func indexTree(node *FileNode, idx map[string]*FileNode) {
	for _, child := range node.Children {
		if isWhiteoutName(child.Name) {
			continue
		}
		if child.Path != "" {
			idx[child.Path] = child
		}
		if child.IsDir {
			indexTree(child, idx)
		}
	}
}

// pathRuns walks the per-snapshot path indices and groups occurrences of path
// into runs separated by Removed snapshots or absences (path missing from the
// snapshot, e.g. inside an opaque-whiteouted directory). Within a run, an
// occurrence is recorded only at snapshots where the path was Added or
// Modified — the layer that actually wrote new bytes. Unchanged carryover does
// not contribute.
func pathRuns(path string, indices []map[string]*FileNode) [][]efficiencyOccurrence {
	var runs [][]efficiencyOccurrence
	var cur []efficiencyOccurrence

	flush := func() {
		if len(cur) > 0 {
			runs = append(runs, cur)
			cur = nil
		}
	}

	for i, idx := range indices {
		if idx == nil {
			flush()
			continue
		}
		node, ok := idx[path]
		switch {
		case !ok:
			flush()
		case node.DiffType == Removed:
			flush()
		case node.DiffType == Added || node.DiffType == Modified:
			cur = append(cur, efficiencyOccurrence{layerIdx: i, size: node.Size})
		}
		// Unchanged carryover: skip (no new bytes; don't flush).
	}
	flush()
	return runs
}

// walkLiveFiles is walkFiles but skips Removed leaves so the denominator
// reflects only files actually present in the snapshot.
func walkLiveFiles(node *FileNode, fn func(path string, size int64)) {
	for _, child := range node.Children {
		if isWhiteoutName(child.Name) {
			continue
		}
		if child.DiffType == Removed {
			continue
		}
		if child.IsDir {
			walkLiveFiles(child, fn)
		} else if !child.IsHardlink {
			fn(child.Path, child.Size)
		}
	}
}

// walkFiles recursively visits all non-directory file nodes. Hardlinks are
// skipped because their content lives at the link target — counting them
// would double-count bytes (size 0 in tar) and inflate the file count
// without adding real data.
func walkFiles(node *FileNode, fn func(path string, size int64)) {
	for _, child := range node.Children {
		if isWhiteoutName(child.Name) {
			continue
		}
		if child.IsDir {
			walkFiles(child, fn)
		} else if !child.IsHardlink {
			fn(child.Path, child.Size)
		}
	}
}
