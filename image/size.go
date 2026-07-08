package image

func TreeLiveFileBytes(tree *FileTree) int64 {
	if tree == nil || tree.Root == nil {
		return 0
	}
	var total int64
	var walk func(n *FileNode)
	walk = func(n *FileNode) {
		if n.DiffType == Removed {
			return
		}
		if !n.IsDir {
			total += n.Size
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(tree.Root)
	return total
}

// assignNetDeltas writes layers[i].NetDelta from the stacked trees.
// Δfs[0] = TreeLiveFileBytes(stacked[0]) — the full live size after the
// first layer, not 0; this convention is documented on Layer.NetDelta.
// Δfs[i] = TreeLiveFileBytes(stacked[i]) − TreeLiveFileBytes(stacked[i−1])
// Each TreeLiveFileBytes is computed once and reused as the prior value.
func assignNetDeltas(layers []Layer, stacked []*FileTree) {
	if len(layers) == 0 || len(stacked) == 0 {
		return
	}
	n := min(len(layers), len(stacked))
	var prev int64
	for i := range n {
		curr := TreeLiveFileBytes(stacked[i])
		if i == 0 {
			layers[i].NetDelta = curr
		} else {
			layers[i].NetDelta = curr - prev
		}
		prev = curr
	}
}
