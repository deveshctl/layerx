package image

import (
	"archive/tar"
	"io"
	"path"
	"strings"
)

// ParseLayerTar reads a tar archive (a single layer) and builds a FileTree.
func ParseLayerTar(r io.Reader) (*FileTree, error) {
	tree := NewFileTree()
	tr := tar.NewReader(r)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		name := cleanTarPath(hdr.Name)
		if name == "" || name == "." {
			continue
		}

		isDir := hdr.Typeflag == tar.TypeDir
		size := hdr.Size
		if isDir {
			size = 0
		}

		insertNode(tree.Root, name, size, isDir)
	}

	return tree, nil
}

func cleanTarPath(p string) string {
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimPrefix(p, "./")
	return p
}

func insertNode(root *FileNode, fullPath string, size int64, isDir bool) {
	cleanPath := strings.TrimSuffix(fullPath, "/")
	parts := strings.Split(cleanPath, "/")

	current := root
	for i, part := range parts {
		if part == "" {
			continue
		}
		isLast := i == len(parts)-1
		existing := current.FindChild(part)

		if isLast {
			absPath := "/" + cleanPath
			if existing != nil {
				if isDir {
					existing.IsDir = true
				}
				if size > 0 {
					existing.Size = size
				}
				existing.Path = absPath
			} else {
				node := &FileNode{
					Name:  part,
					Path:  absPath,
					Size:  size,
					IsDir: isDir,
				}
				current.AddChild(node)
			}
		} else {
			if existing == nil {
				dirPath := "/" + path.Join(parts[:i+1]...)
				existing = &FileNode{
					Name:  part,
					Path:  dirPath,
					IsDir: true,
				}
				current.AddChild(existing)
			}
			current = existing
		}
	}
}
