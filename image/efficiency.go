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
func EfficiencyFromAnalysis(a *Analysis) *EfficiencyResult {
	if a == nil || len(a.Layers) == 0 {
		return &EfficiencyResult{Score: 1.0}
	}
	return computeEfficiency(a.Layers, a.StackedTrees)
}

// occurrence records a single layer-i appearance of a path with the bytes
// it occupied at that snapshot.
type efficiencyOccurrence struct {
	layerIdx int
	size     int64
}

func computeEfficiency(layers []Layer, stacked []*FileTree) *EfficiencyResult {
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
		runs := pathRuns(path, stacked)
		var pathWaste int64
		var occurrenceCount int
		for _, run := range runs {
			occurrenceCount += len(run)
			if len(run) < 2 {
				continue
			}
			for _, occ := range run[:len(run)-1] {
				pathWaste += occ.size
			}
		}
		if pathWaste == 0 {
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

// pathRuns walks the stacked trees and groups occurrences of path into runs
// separated by Removed snapshots or absences (path missing from the snapshot,
// e.g. inside an opaque-whiteouted directory). Within a run, an occurrence is
// recorded only at snapshots where the path was Added or Modified — the layer
// that actually wrote new bytes. Unchanged carryover does not contribute.
func pathRuns(path string, stacked []*FileTree) [][]efficiencyOccurrence {
	var runs [][]efficiencyOccurrence
	var cur []efficiencyOccurrence

	flush := func() {
		if len(cur) > 0 {
			runs = append(runs, cur)
			cur = nil
		}
	}

	for i, tree := range stacked {
		if tree == nil || tree.Root == nil {
			flush()
			continue
		}
		node := findByPath(tree.Root, path)
		switch {
		case node == nil:
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

// findByPath returns the node at path within root, or nil. Path is the
// absolute Path stored on FileNode; descent uses Path-prefix on directories
// and exact match on leaves.
func findByPath(root *FileNode, path string) *FileNode {
	if root == nil {
		return nil
	}
	if root.Path == path {
		return root
	}
	for _, child := range root.Children {
		if child.Path == path {
			return child
		}
		if child.IsDir && isPathPrefix(child.Path, path) {
			if found := findByPath(child, path); found != nil {
				return found
			}
		}
	}
	return nil
}

// isPathPrefix returns true if prefix is a directory prefix of full. Both
// values come from FileNode.Path which uses forward slashes.
func isPathPrefix(prefix, full string) bool {
	if prefix == "" || prefix == "/" {
		return true
	}
	if len(full) <= len(prefix) {
		return false
	}
	if full[:len(prefix)] != prefix {
		return false
	}
	return full[len(prefix)] == '/'
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
