package image

import "fmt"

// WasteEvent describes a write or deletion at a zero-based layer index.
// WastedBytes charges payload to the layer that stored it, never the whiteout.
type WasteEvent struct {
	Layer       int
	Command     string
	Change      DiffType
	Size        int64
	WastedBytes int64
	Reason      string
}

type WasteExplanation struct {
	Path        string
	TotalWasted int64
	Events      []WasteEvent
}

// ExplainWaste reconstructs one path's history from the same snapshots and
// attribution rules as EfficiencyFromAnalysis. It does not read file contents.
func ExplainWaste(a *Analysis, path string) WasteExplanation {
	out := WasteExplanation{Path: path}
	if a == nil || len(a.Layers) == 0 {
		return out
	}
	trees := a.StackedTrees
	if len(trees) != len(a.Layers) {
		trees = Stack(a.Layers)
	}
	indices := make([]map[string]*FileNode, len(trees))
	for i, tree := range trees {
		indices[i] = make(map[string]*FileNode)
		if tree != nil && tree.Root != nil {
			indexTree(tree.Root, indices[i])
		}
	}
	live := survivingPayloads(indices)
	for _, run := range pathRuns(path, indices) {
		charged := chargedOccurrences(path, run, live)
		chargedLayers := make(map[int]int64)
		for _, occ := range charged {
			chargedLayers[occ.layerIdx] = occ.size
		}
		for j, occ := range run.occ {
			event := WasteEvent{
				Layer: occ.layerIdx, Command: a.Layers[occ.layerIdx].Command,
				Change: occ.change, Size: occ.size, Reason: "Present in final filesystem",
			}
			if bytes := chargedLayers[occ.layerIdx]; bytes > 0 {
				event.WastedBytes = bytes
				if j+1 < len(run.occ) {
					event.Reason = fmt.Sprintf("Overwritten in L%d", run.occ[j+1].layerIdx+1)
				} else {
					event.Reason = fmt.Sprintf("Deleted in L%d", run.endLayer+1)
				}
			} else if run.endedInDeletion || j+1 < len(run.occ) {
				event.Reason = "Retained through a surviving hardlink"
			}
			out.TotalWasted += event.WastedBytes
			out.Events = append(out.Events, event)
		}
		if run.endedInDeletion {
			out.Events = append(out.Events, WasteEvent{
				Layer: run.endLayer, Command: a.Layers[run.endLayer].Command,
				Change: Removed, Reason: "Deletion does not reclaim earlier layer bytes",
			})
		}
	}
	return out
}
