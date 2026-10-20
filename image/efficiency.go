package image

import "sort"

type WastedFile struct {
	Path        string
	TotalWasted int64
	LayerCount  int
	// FirstWastedLayer is the zero-based layer storing the earliest wasted
	// copy. This navigation metadata is not part of the JSON export schema.
	FirstWastedLayer int `json:"-"`
}

type EfficiencyResult struct {
	Score       float64
	WastedBytes int64
	WastedFiles []WastedFile
}

// Efficiency computes how much space is wasted by files appearing in multiple
// layers. It stacks layers internally; prefer EfficiencyFromAnalysis when the
// caller already has stacked trees.
//
// A stored copy is wasted when no final path or hardlink retains that specific
// payload. Overwrites and deletions do not reclaim bytes stored in older layers.
// This measures layer efficiency, not whether live files are needed at runtime.
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

type efficiencyOccurrence struct {
	layerIdx int
	size     int64
	change   DiffType
}

// pathRun is one contiguous span of a path's Added/Modified occurrences.
// endedInDeletion records why the span closed: true when a whiteout, opaque
// whiteout, or absence removed the path, false when the name remains live.
// A surviving alias can retain a payload from either kind of run.
type pathRun struct {
	occ             []efficiencyOccurrence
	endedInDeletion bool
	endLayer        int
}

func computeEfficiency(layers []Layer, stacked []*FileTree) *EfficiencyResult {
	// Build a path→FileNode index per stacked snapshot once. pathRuns then does
	// O(1) lookups instead of recursing through the tree once per (path,
	// snapshot) pair, which keeps the analysis linear in total file count for
	// layered images with many shared paths.
	indices := make([]map[string]*FileNode, len(stacked))
	for i, tree := range stacked {
		if tree == nil || tree.Root == nil {
			continue
		}
		idx := make(map[string]*FileNode)
		indexTree(tree.Root, idx)
		indices[i] = idx
	}

	// Track stored payload identity, not just a hardlink's target pathname:
	// an alias can retain an older copy after that name is rewritten/deleted.
	livePayloads := survivingPayloads(indices)

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
		firstWastedLayer := -1
		for _, run := range runs {
			charged := chargedOccurrences(path, run, livePayloads)
			if len(charged) == 0 {
				continue
			}
			for _, occ := range charged {
				pathWaste += occ.size
				if occ.size > 0 && firstWastedLayer < 0 {
					firstWastedLayer = occ.layerIdx
				}
			}
			// LayerCount counts byte-contributing occurrences in runs with
			// charged payload. For deleted runs, every payload is charged. For
			// live runs, the surviving last copy is excluded from charged but
			// is still a real byte-contributor visible to the user, so include
			// all non-zero occurrences in the run (not just the charged slice).
			// Runs with no charged payload are skipped above, so separate live
			// reinstalls, even after empty writes, do not inflate the count.
			for _, occ := range run.occ {
				if occ.size > 0 {
					occurrenceCount++
				}
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
			Path:             path,
			TotalWasted:      pathWaste,
			LayerCount:       occurrenceCount,
			FirstWastedLayer: firstWastedLayer,
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
	for _, size := range livePayloads {
		liveBytes += size
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
// then charges copies that no final name retains. Skipping hardlinks would
// lose both replacements and surviving payload references.
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
//
// Each run records whether it ended in a deletion. A run flushed by a Removed
// node, an absence, or a nil index is marked endedInDeletion. Reaching the end
// leaves the name live. Payload survival is resolved separately, including aliases.
func pathRuns(path string, indices []map[string]*FileNode) []pathRun {
	var runs []pathRun
	var cur []efficiencyOccurrence

	flush := func(deleted bool, layer int) {
		if len(cur) > 0 {
			runs = append(runs, pathRun{occ: cur, endedInDeletion: deleted, endLayer: layer})
			cur = nil
		}
	}

	for i, idx := range indices {
		if idx == nil {
			flush(true, i)
			continue
		}
		node, ok := idx[path]
		switch {
		case !ok:
			flush(true, i)
		case node.DiffType == Removed:
			flush(true, i)
		case node.DiffType == Added || node.DiffType == Modified:
			cur = append(cur, efficiencyOccurrence{layerIdx: i, size: node.Size, change: node.DiffType})
		}
		// Unchanged carryover: skip (no new bytes; don't flush).
	}
	flush(false, len(indices))
	return runs
}

// Shared by the score and the per-file explanation so attribution cannot
// silently diverge from the waste total used by CI.
func chargedOccurrences(path string, run pathRun, live map[payloadRef]int64) []efficiencyOccurrence {
	var charged []efficiencyOccurrence
	for _, occ := range run.occ {
		if _, survives := live[payloadRef{path, occ.layerIdx}]; occ.size > 0 && !survives {
			charged = append(charged, occ)
		}
	}
	return charged
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
