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
			stacked := cloneAsAdded(layer.Tree.Root, i)
			result[i] = &FileTree{Root: stacked}
			cumulative = cloneStructure(stacked)
		} else {
			stacked := mergeLayer(cumulative, layer.Tree.Root, i)
			result[i] = &FileTree{Root: stacked}
			cumulative = cloneStructure(stacked)
		}
	}

	return result
}

func mergeLayer(cumulative, layerRoot *FileNode, layerIdx int) *FileNode {
	merged := &FileNode{
		Name:              cumulative.Name,
		Path:              cumulative.Path,
		Linkname:          cumulative.Linkname,
		IsDir:             cumulative.IsDir,
		IsHardlink:        cumulative.IsHardlink,
		Size:              cumulative.Size,
		Mode:              cumulative.Mode,
		UID:               cumulative.UID,
		GID:               cumulative.GID,
		IntroducedInLayer: cumulative.IntroducedInLayer,
	}

	metadataChanged := cumulative.Mode != layerRoot.Mode ||
		cumulative.UID != layerRoot.UID ||
		cumulative.GID != layerRoot.GID
	if metadataChanged {
		merged.Mode = layerRoot.Mode
		merged.UID = layerRoot.UID
		merged.GID = layerRoot.GID
		merged.IntroducedInLayer = layerIdx
	}

	whiteouts, opaque := extractWhiteouts(layerRoot)

	if opaque {
		// Opaque whiteout semantics: wipe directory contents, then apply the
		// new layer's children. If a name in cumulative is reintroduced by
		// this layer, skip the Removed clone — emitting both clones leaves
		// duplicate-Name children that confuse FindChild and double-count
		// the path in efficiency calculations.
		layerNames := make(map[string]struct{})
		for _, child := range layerRoot.Children {
			if !isWhiteoutName(child.Name) {
				layerNames[child.Name] = struct{}{}
			}
		}
		for _, child := range cumulative.Children {
			if _, reintroduced := layerNames[child.Name]; reintroduced {
				continue
			}
			removed := cloneAsRemoved(child)
			merged.AddChild(removed)
		}
		for _, child := range layerRoot.Children {
			if isWhiteoutName(child.Name) {
				continue
			}
			added := cloneAsAdded(child, layerIdx)
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
				mergedChild := mergeLayer(cChild, lChild, layerIdx)
				merged.AddChild(mergedChild)
			} else {
				if cChild.IsDir && !lChild.IsDir {
					// The path used to be a directory and is now a regular file.
					// Emit each old child as Removed against `merged` so the stacked
					// tree retains visibility into the deleted subtree. Path-level
					// consumers (efficiency, JSON export) read by Path; the structural
					// flattening is harmless to them and avoids duplicate-Name
					// children that would confuse FindChild and the TUI tree view.
					for _, gc := range cChild.Children {
						merged.AddChild(cloneAsRemoved(gc))
					}
				}
				mod := &FileNode{
					Name:              cChild.Name,
					Path:              cChild.Path,
					Linkname:          lChild.Linkname,
					Size:              lChild.Size,
					IsDir:             lChild.IsDir,
					IsHardlink:        lChild.IsHardlink,
					DiffType:          Modified,
					Mode:              lChild.Mode,
					UID:               lChild.UID,
					GID:               lChild.GID,
					IntroducedInLayer: cChild.IntroducedInLayer,
				}
				if lChild.IsDir {
					for _, gc := range lChild.Children {
						if !isWhiteoutName(gc.Name) {
							mod.AddChild(cloneAsAdded(gc, layerIdx))
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
			added := cloneAsAdded(lChild, layerIdx)
			merged.AddChild(added)
		}
	}

	if merged.IsDir {
		if metadataChanged || hasChangedChildren(merged) {
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
			continue
		}
		// Reserved control entries (".wh..wh.*") are not regular whiteouts.
		// Treating one as a whiteout against ".wh..*" produces a phantom
		// removal entry. Mirrors regularWhiteoutTarget in extractor.go.
		if strings.HasPrefix(child.Name, ".wh..wh.") {
			continue
		}
		if target, ok := strings.CutPrefix(child.Name, ".wh."); ok {
			whiteouts[target] = struct{}{}
		}
	}
	return whiteouts, opaque
}

func cloneAsUnchanged(node *FileNode) *FileNode {
	clone := &FileNode{
		Name:              node.Name,
		Path:              node.Path,
		Linkname:          node.Linkname,
		Size:              node.Size,
		IsDir:             node.IsDir,
		IsHardlink:        node.IsHardlink,
		DiffType:          Unchanged,
		Mode:              node.Mode,
		UID:               node.UID,
		GID:               node.GID,
		IntroducedInLayer: node.IntroducedInLayer,
	}
	for _, child := range node.Children {
		clone.AddChild(cloneAsUnchanged(child))
	}
	return clone
}

func cloneAsRemoved(node *FileNode) *FileNode {
	clone := &FileNode{
		Name:              node.Name,
		Path:              node.Path,
		Linkname:          node.Linkname,
		Size:              node.Size,
		IsDir:             node.IsDir,
		IsHardlink:        node.IsHardlink,
		DiffType:          Removed,
		Mode:              node.Mode,
		UID:               node.UID,
		GID:               node.GID,
		IntroducedInLayer: node.IntroducedInLayer,
	}
	for _, child := range node.Children {
		clone.AddChild(cloneAsRemoved(child))
	}
	return clone
}

func cloneAsAdded(node *FileNode, layerIdx int) *FileNode {
	clone := &FileNode{
		Name:              node.Name,
		Path:              node.Path,
		Linkname:          node.Linkname,
		Size:              node.Size,
		IsDir:             node.IsDir,
		IsHardlink:        node.IsHardlink,
		DiffType:          Added,
		Mode:              node.Mode,
		UID:               node.UID,
		GID:               node.GID,
		IntroducedInLayer: layerIdx,
	}
	for _, child := range node.Children {
		if isWhiteoutName(child.Name) {
			continue
		}
		clone.AddChild(cloneAsAdded(child, layerIdx))
	}
	return clone
}

// cloneStructure deep-clones a node for cumulative state tracking.
// Nodes marked Removed are excluded since they no longer exist in the filesystem.
func cloneStructure(node *FileNode) *FileNode {
	clone := &FileNode{
		Name:              node.Name,
		Path:              node.Path,
		Linkname:          node.Linkname,
		Size:              node.Size,
		IsDir:             node.IsDir,
		IsHardlink:        node.IsHardlink,
		Mode:              node.Mode,
		UID:               node.UID,
		GID:               node.GID,
		IntroducedInLayer: node.IntroducedInLayer,
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
	return IsWhiteoutName(name)
}

// IsWhiteoutName reports whether name is an overlay whiteout marker:
// either an opaque-directory marker (`.wh..wh..opq`) or a per-file
// tombstone (`.wh.<name>`). Exported so consumers outside image/ — such
// as ci/path-rules — can identify these markers in raw per-layer trees,
// where tar entries land as regular FileNodes (DiffType=Unchanged) until
// stack.go interprets them.
func IsWhiteoutName(name string) bool {
	return name == ".wh..wh..opq" || strings.HasPrefix(name, ".wh.")
}
