package image

import (
	"io/fs"
	"time"
)

// SchemaVersion is the on-disk schema version for cached analysis files.
// Bump when the cacheEnvelope, cachedLayer, or cachedNode shape changes
// (or when the parse output produced by docker.go/parseLayers changes in a
// way that affects what we persist). Do NOT bump for stack.go or size.go
// changes — those run on every load and are not persisted.
const SchemaVersion = 1

// cacheEnvelope is the single top-level value written to disk per digest.
type cacheEnvelope struct {
	Digest        string
	SchemaVersion int
	CachedAt      time.Time
	Layers        []cachedLayer
}

// cachedLayer is the persisted shape of a Layer. Excludes NetDelta (derived).
type cachedLayer struct {
	Index   int
	ID      string
	Size    int64
	Command string
	Tree    *cachedNode // nil when the source Tree was nil
}

// cachedNode is the persisted shape of a FileNode. Excludes DiffType and
// IntroducedInLayer — both are recomputed by Stack() on load.
type cachedNode struct {
	Name     string
	Path     string
	Size     int64
	Mode     fs.FileMode
	UID      int
	GID      int
	IsDir    bool
	Children []*cachedNode
}

func toCachedLayers(layers []Layer) []cachedLayer {
	out := make([]cachedLayer, len(layers))
	for i, l := range layers {
		out[i] = cachedLayer{
			Index:   l.Index,
			ID:      l.ID,
			Size:    l.Size,
			Command: l.Command,
		}
		if l.Tree != nil {
			out[i].Tree = toCachedNode(l.Tree.Root)
		}
	}
	return out
}

func fromCachedLayers(dtos []cachedLayer) []Layer {
	out := make([]Layer, len(dtos))
	for i, d := range dtos {
		out[i] = Layer{
			Index:   d.Index,
			ID:      d.ID,
			Size:    d.Size,
			Command: d.Command,
		}
		if d.Tree != nil {
			out[i].Tree = &FileTree{Root: fromCachedNode(d.Tree)}
		}
	}
	return out
}

func toCachedNode(n *FileNode) *cachedNode {
	if n == nil {
		return nil
	}
	cn := &cachedNode{
		Name:  n.Name,
		Path:  n.Path,
		Size:  n.Size,
		Mode:  n.Mode,
		UID:   n.UID,
		GID:   n.GID,
		IsDir: n.IsDir,
	}
	for _, c := range n.Children {
		cn.Children = append(cn.Children, toCachedNode(c))
	}
	return cn
}

func fromCachedNode(c *cachedNode) *FileNode {
	if c == nil {
		return nil
	}
	n := &FileNode{
		Name:  c.Name,
		Path:  c.Path,
		Size:  c.Size,
		Mode:  c.Mode,
		UID:   c.UID,
		GID:   c.GID,
		IsDir: c.IsDir,
	}
	for _, child := range c.Children {
		n.Children = append(n.Children, fromCachedNode(child))
	}
	return n
}
