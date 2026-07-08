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
