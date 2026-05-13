package image

import "strings"

// Stack takes layers with populated Trees and returns one FileTree per layer,
// each representing the cumulative filesystem state at that layer with DiffType
// set on every node to indicate what the current layer changed.
func Stack(layers []Layer) []*FileTree {
	result := make([]*FileTree, len(layers))
	var cumulative *FileNode

	for i, layer := range layers {
		if layer.Tree == nil || layer.Tree.Root == nil || len(layer.Tree.Root.Children) == 0 {
			if cumulative == nil {
				result[i] = NewFileTree()
			} else {
				stacked := cloneAsUnchanged(cumulative)
				result[i] = &FileTree{Root: stacked}
			}
			continue
		}

		if cumulative == nil {
			stacked := cloneAsAdded(layer.Tree.Root)
			result[i] = &FileTree{Root: stacked}
			cumulative = cloneStructure(stacked)
		} else {
			stacked := mergeLayer(cumulative, layer.Tree.Root)
			result[i] = &FileTree{Root: stacked}
			cumulative = cloneStructure(stacked)
		}
	}

	return result
}

func mergeLayer(cumulative, layerRoot *FileNode) *FileNode {
	merged := &FileNode{
		Name:  cumulative.Name,
		Path:  cumulative.Path,
		IsDir: cumulative.IsDir,
		Size:  cumulative.Size,
	}

	whiteouts, opaque := extractWhiteouts(layerRoot)

	if opaque {
		for _, child := range cumulative.Children {
			removed := cloneAsRemoved(child)
			merged.AddChild(removed)
		}
		for _, child := range layerRoot.Children {
			if isWhiteoutName(child.Name) {
				continue
			}
			added := cloneAsAdded(child)
			merged.AddChild(added)
		}
	} else {
		cumulativeNames := make(map[string]struct{})
		for _, child := range cumulative.Children {
			cumulativeNames[child.Name] = struct{}{}
		}

		for _, cChild := range cumulative.Children {
			if _, whited := whiteouts[cChild.Name]; whited {
				removed := cloneAsRemoved(cChild)
				merged.AddChild(removed)
				continue
			}

			lChild := layerRoot.FindChild(cChild.Name)
			if lChild == nil {
				unchanged := cloneAsUnchanged(cChild)
				merged.AddChild(unchanged)
				continue
			}

			if cChild.IsDir && lChild.IsDir {
				mergedChild := mergeLayer(cChild, lChild)
				merged.AddChild(mergedChild)
			} else {
				mod := &FileNode{
					Name:     cChild.Name,
					Path:     cChild.Path,
					Size:     lChild.Size,
					IsDir:    lChild.IsDir,
					DiffType: Modified,
				}
				if lChild.IsDir {
					for _, gc := range lChild.Children {
						if !isWhiteoutName(gc.Name) {
							mod.AddChild(cloneAsAdded(gc))
						}
					}
				}
				merged.AddChild(mod)
			}
		}

		for _, lChild := range layerRoot.Children {
			if isWhiteoutName(lChild.Name) {
				continue
			}
			if _, exists := cumulativeNames[lChild.Name]; exists {
				continue
			}
			added := cloneAsAdded(lChild)
			merged.AddChild(added)
		}
	}

	if merged.IsDir {
		if hasChangedChildren(merged) {
			merged.DiffType = Modified
		} else {
			merged.DiffType = Unchanged
		}
	}

	return merged
}

func extractWhiteouts(node *FileNode) (map[string]struct{}, bool) {
	whiteouts := make(map[string]struct{})
	opaque := false
	for _, child := range node.Children {
		if child.Name == ".wh..wh..opq" {
			opaque = true
		} else if target, ok := strings.CutPrefix(child.Name, ".wh."); ok {
			whiteouts[target] = struct{}{}
		}
	}
	return whiteouts, opaque
}

func cloneAsUnchanged(node *FileNode) *FileNode {
	clone := &FileNode{
		Name:     node.Name,
		Path:     node.Path,
		Size:     node.Size,
		IsDir:    node.IsDir,
		DiffType: Unchanged,
	}
	for _, child := range node.Children {
		clone.AddChild(cloneAsUnchanged(child))
	}
	return clone
}

func cloneAsRemoved(node *FileNode) *FileNode {
	clone := &FileNode{
		Name:     node.Name,
		Path:     node.Path,
		Size:     node.Size,
		IsDir:    node.IsDir,
		DiffType: Removed,
	}
	for _, child := range node.Children {
		clone.AddChild(cloneAsRemoved(child))
	}
	return clone
}

func cloneAsAdded(node *FileNode) *FileNode {
	clone := &FileNode{
		Name:     node.Name,
		Path:     node.Path,
		Size:     node.Size,
		IsDir:    node.IsDir,
		DiffType: Added,
	}
	for _, child := range node.Children {
		if isWhiteoutName(child.Name) {
			continue
		}
		clone.AddChild(cloneAsAdded(child))
	}
	return clone
}

// cloneStructure deep-clones a node for cumulative state tracking.
// Nodes marked Removed are excluded since they no longer exist in the filesystem.
func cloneStructure(node *FileNode) *FileNode {
	clone := &FileNode{
		Name:  node.Name,
		Path:  node.Path,
		Size:  node.Size,
		IsDir: node.IsDir,
	}
	for _, child := range node.Children {
		if child.DiffType == Removed {
			continue
		}
		clone.AddChild(cloneStructure(child))
	}
	return clone
}

func hasChangedChildren(node *FileNode) bool {
	for _, child := range node.Children {
		if child.DiffType != Unchanged {
			return true
		}
	}
	return false
}

func isWhiteoutName(name string) bool {
	return name == ".wh..wh..opq" || strings.HasPrefix(name, ".wh.")
}
