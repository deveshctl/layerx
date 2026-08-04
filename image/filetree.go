package image

import "io/fs"

type DiffType int

const (
	Unchanged DiffType = iota
	Added
	Modified
	Removed
)

type FileTree struct {
	Root *FileNode
}

type FileNode struct {
	Name              string
	Path              string
	Linkname          string
	Size              int64
	// EffectiveSize is the sum of Size for all non-removed file descendants
	// (or Size itself for non-directory nodes). Populated once by
	// computeEffectiveSizes after the tree is fully built; used by the TUI
	// sort path to avoid an O(N²) subtree walk per sort invocation.
	EffectiveSize     int64
	Mode              fs.FileMode
	UID               int
	GID               int
	DiffType          DiffType
	IntroducedInLayer int
	Children          []*FileNode
	IsDir             bool
	IsHardlink        bool
}

func NewFileTree() *FileTree {
	return &FileTree{
		Root: &FileNode{Name: "/", Path: "/", IsDir: true},
	}
}

func (n *FileNode) FindChild(name string) *FileNode {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (n *FileNode) AddChild(child *FileNode) {
	n.Children = append(n.Children, child)
}

func (n *FileNode) RemoveChild(name string) bool {
	for i, c := range n.Children {
		if c.Name == name {
			n.Children = append(n.Children[:i], n.Children[i+1:]...)
			return true
		}
	}
	return false
}

func (t *FileTree) Walk(fn func(*FileNode)) {
	var walk func(*FileNode)
	walk = func(n *FileNode) {
		fn(n)
		for _, c := range n.Children {
			walk(c)
		}
	}
	if t.Root != nil {
		walk(t.Root)
	}
}

// computeEffectiveSizes populates EffectiveSize on every node in the tree via
// a single post-order DFS. For files it copies Size; for directories it sums
// the EffectiveSize of non-removed children. Called once per tree after it is
// fully built so the TUI sort path reads a field rather than re-walking the
// subtree on every sort invocation.
func (t *FileTree) computeEffectiveSizes() {
	if t.Root == nil {
		return
	}
	var walk func(*FileNode) int64
	walk = func(n *FileNode) int64 {
		if n.DiffType == Removed {
			n.EffectiveSize = 0
			return 0
		}
		if !n.IsDir {
			n.EffectiveSize = n.Size
			return n.Size
		}
		var total int64
		for _, c := range n.Children {
			total += walk(c)
		}
		n.EffectiveSize = total
		return total
	}
	walk(t.Root)
}
