package image

// DiffType indicates how a file changed relative to previous layers.
type DiffType int

const (
	Unchanged DiffType = iota
	Added
	Modified
	Removed
)

// FileTree represents the filesystem state of one or more stacked layers.
type FileTree struct {
	Root *FileNode
}

// FileNode represents a single file or directory in a layer's filesystem.
type FileNode struct {
	Path     string
	Size     int64
	DiffType DiffType
	Children []*FileNode
	IsDir    bool
}
