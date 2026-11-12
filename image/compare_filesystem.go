package image

import (
	"context"
	"sort"
)

type fileHistoryEvent struct {
	layer int
	node  *FileNode
}

// compareFilesystem retains only the final snapshot and the per-path writes
// and deletions needed to account for waste and surviving hardlinks.
func compareFilesystem(ctx context.Context, layers []Layer) (*FileTree, *EfficiencyResult, error) {
	history := make(map[string][]fileHistoryEvent)
	previous, current := make(map[string]*FileNode), make(map[string]*FileNode)
	var final *FileTree
	var previousBytes int64
	err := stackEach(ctx, layers, func(layer int, tree *FileTree) {
		final = tree
		liveBytes := TreeLiveFileBytes(tree)
		layers[layer].NetDelta = liveBytes - previousBytes
		previousBytes = liveBytes

		clear(current)
		indexTree(tree.Root, current)
		for path, node := range current {
			if ctx.Err() != nil {
				return
			}
			old := previous[path]
			if node.DiffType == Removed {
				continue
			}
			if node.DiffType == Unchanged && old != nil && old.DiffType != Removed && old.IsDir == node.IsDir {
				continue
			}
			if node.IsDir {
				// Keeping a directory pointer would also retain its old subtree.
				node = &FileNode{IsDir: true, DiffType: node.DiffType, Size: node.Size}
			}
			history[path] = append(history[path], fileHistoryEvent{layer, node})
		}
		for path, old := range previous {
			if ctx.Err() != nil {
				return
			}
			node := current[path]
			if old.DiffType != Removed && (node == nil || node.DiffType == Removed) {
				history[path] = append(history[path], fileHistoryEvent{layer: layer})
			}
		}
		previous, current = current, previous
	})
	if err != nil {
		return nil, nil, err
	}
	live := resolveLivePayloads(ctx, len(layers)-1, previous, func(path string, at int) (int, *FileNode) {
		events := history[path]
		index := sort.Search(len(events), func(i int) bool { return events[i].layer > at }) - 1
		if index >= 0 {
			event := events[index]
			if node := event.node; node != nil && !node.IsDir && node.DiffType != Unchanged {
				return event.layer, node
			}
		}
		return 0, nil
	})
	efficiency := scoreEfficiency(ctx, layers, live, func(path string) []pathRun {
		var runs []pathRun
		var occurrences []efficiencyOccurrence
		for _, event := range history[path] {
			if node := event.node; node == nil {
				if len(occurrences) > 0 {
					runs = append(runs, pathRun{occ: occurrences, endedInDeletion: true, endLayer: event.layer})
					occurrences = nil
				}
			} else if node.DiffType != Unchanged {
				occurrences = append(occurrences, efficiencyOccurrence{event.layer, node.Size, node.DiffType})
			}
		}
		if len(occurrences) > 0 {
			runs = append(runs, pathRun{occ: occurrences, endLayer: len(layers)})
		}
		return runs
	})
	return final, efficiency, ctx.Err()
}
