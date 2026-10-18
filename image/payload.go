package image

import "strings"

// payloadRef identifies the layer that stored a particular copy of a path.
// A later hardlink can keep that copy live after its original name is gone.
type payloadRef struct {
	path  string
	layer int
}

func survivingPayloads(indices []map[string]*FileNode) map[payloadRef]int64 {
	live := make(map[payloadRef]int64)
	if len(indices) == 0 {
		return live
	}
	type resolution struct {
		ref payloadRef
		ok  bool
	}
	memo := make(map[payloadRef]resolution)
	resolving := make(map[payloadRef]bool)
	var resolve func(string, int) (payloadRef, bool)
	resolve = func(path string, at int) (payloadRef, bool) {
		query := payloadRef{path, at}
		if cached, ok := memo[query]; ok {
			return cached.ref, cached.ok
		}
		if resolving[query] {
			return payloadRef{}, false
		}
		resolving[query] = true
		defer delete(resolving, query)
		for i := at; i >= 0; i-- {
			node := indices[i][path]
			if node == nil || node.DiffType == Removed || node.IsDir {
				break
			}
			if node.DiffType != Added && node.DiffType != Modified {
				continue
			}
			if node.IsHardlink {
				target := "/" + strings.TrimPrefix(node.Linkname, "/")
				ref, ok := resolve(target, i)
				memo[query] = resolution{ref, ok}
				return ref, ok
			}
			ref := payloadRef{path, i}
			memo[query] = resolution{ref, true}
			return ref, true
		}
		memo[query] = resolution{}
		return payloadRef{}, false
	}
	last := len(indices) - 1
	for path, node := range indices[last] {
		if node.IsDir || node.DiffType == Removed {
			continue
		}
		if ref, ok := resolve(path, last); ok {
			live[ref] = indices[ref.layer][ref.path].Size
		}
	}
	return live
}
