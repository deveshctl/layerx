package image

import (
	"sort"
	"strings"
)

// CleanupCandidate is live cache content worth reviewing, not proven waste.
// Bytes are uncompressed file bytes, not a predicted registry download saving.
type CleanupCandidate struct {
	Path   string
	Bytes  int64
	Reason string
}

// CleanupCandidates groups recognized package/build caches in the final live
// filesystem. It never changes the layer-efficiency score or CI waste rules.
func CleanupCandidates(a *Analysis) []CleanupCandidate {
	if a == nil || len(a.Layers) == 0 {
		return nil
	}
	trees := a.StackedTrees
	if len(trees) != len(a.Layers) {
		trees = Stack(a.Layers)
	}
	last := trees[len(trees)-1]
	if last == nil || last.Root == nil {
		return nil
	}
	groups := make(map[string]CleanupCandidate)
	var walk func(*FileNode)
	walk = func(n *FileNode) {
		if n.DiffType == Removed || isWhiteoutName(n.Name) {
			return
		}
		if !n.IsDir && !n.IsHardlink && n.Mode.IsRegular() && n.Size > 0 {
			if root, reason := cleanupRoot(n.Path); root != "" {
				group := groups[root]
				group.Path, group.Reason = root, reason
				group.Bytes += n.Size
				groups[root] = group
			}
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(last.Root)
	out := make([]CleanupCandidate, 0, len(groups))
	for _, group := range groups {
		out = append(out, group)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bytes != out[j].Bytes {
			return out[i].Bytes > out[j].Bytes
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func cleanupRoot(path string) (string, string) {
	for _, cache := range []struct{ root, reason string }{
		{"/var/cache/apt/archives", "Downloaded apt packages"},
		{"/var/lib/apt/lists", "Downloaded apt package indexes"},
		{"/var/cache/apk", "Downloaded apk packages"},
	} {
		if strings.HasPrefix(path, cache.root+"/") {
			return cache.root, cache.reason
		}
	}
	// Only conventional user homes are recognized; a similarly named directory
	// inside an application is not enough evidence to classify its contents.
	home := ""
	if strings.HasPrefix(path, "/root/") {
		home = "/root"
	} else if strings.HasPrefix(path, "/home/") {
		user, _, ok := strings.Cut(strings.TrimPrefix(path, "/home/"), "/")
		if ok && user != "" {
			home = "/home/" + user
		}
	}
	if home == "" {
		return "", ""
	}
	for _, cache := range []struct{ suffix, reason string }{
		{"/.cache/pip", "pip download and wheel cache"},
		{"/.npm/_cacache", "npm content cache"},
		{"/.cache/yarn", "Yarn package cache"},
		{"/.cache/go-build", "Go build cache"},
	} {
		root := home + cache.suffix
		if strings.HasPrefix(path, root+"/") {
			return root, cache.reason
		}
	}
	return "", ""
}
