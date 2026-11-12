package image

import (
	"context"
	"strings"
)

// payloadRef identifies the layer that stored a particular copy of a path.
// A later hardlink can keep that copy live after its original name is gone.
type payloadRef struct {
	path  string
	layer int
}

func survivingPayloads(indices []map[string]*FileNode) map[payloadRef]int64 {
	if len(indices) == 0 {
		return make(map[payloadRef]int64)
	}
	return resolveLivePayloads(context.Background(), len(indices)-1, indices[len(indices)-1], func(path string, at int) (int, *FileNode) {
		for i := at; i >= 0; i-- {
			node := indices[i][path]
			if node == nil || node.DiffType == Removed || node.IsDir {
				break
			}
			if node.DiffType == Added || node.DiffType == Modified {
				return i, node
			}
		}
		return 0, nil
	})
}

func resolveLivePayloads(ctx context.Context, last int, final map[string]*FileNode, findWrite func(string, int) (int, *FileNode)) map[payloadRef]int64 {
	live := make(map[payloadRef]int64)
	type resolution struct {
		ref  payloadRef
		size int64
		ok   bool
	}
	memo := make(map[payloadRef]resolution)
	resolving := make(map[payloadRef]bool)
	var resolve func(string, int) resolution
	resolve = func(path string, at int) resolution {
		if ctx.Err() != nil {
			return resolution{}
		}
		query := payloadRef{path, at}
		if cached, ok := memo[query]; ok {
			return cached
		}
		if resolving[query] {
			return resolution{}
		}
		resolving[query] = true
		defer delete(resolving, query)
		i, node := findWrite(path, at)
		var result resolution
		if node != nil {
			if node.IsHardlink {
				target := "/" + strings.TrimPrefix(node.Linkname, "/")
				result = resolve(target, i)
			} else {
				result = resolution{payloadRef{path, i}, node.Size, true}
			}
		}
		memo[query] = result
		return result
	}
	for path, node := range final {
		if ctx.Err() != nil {
			break
		}
		if node.IsDir || node.DiffType == Removed {
			continue
		}
		if result := resolve(path, last); result.ok {
			live[result.ref] = result.size
		}
	}
	return live
}
