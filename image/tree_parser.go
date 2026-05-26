package image

import (
	"archive/tar"
	"io"
	"io/fs"
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

		switch hdr.Typeflag {
		case tar.TypeReg, tar.TypeDir, tar.TypeSymlink, tar.TypeLink, tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
			// supported
		default:
			// Skip TypeXGlobalHeader, TypeXHeader, TypeGNULongName, TypeGNULongLink, etc.
			// Go's tar reader merges most extended-header forms transparently; the
			// remaining ones (notably XGlobalHeader) would otherwise appear as
			// phantom directory entries.
			continue
		}

		isDir := hdr.Typeflag == tar.TypeDir
		isHardlink := hdr.Typeflag == tar.TypeLink
		size := hdr.Size
		if isDir {
			size = 0
		}

		mode := hdr.FileInfo().Mode()
		uid := hdr.Uid
		gid := hdr.Gid

		insertNode(tree.Root, name, size, isDir, isHardlink, hdr.Linkname, mode, uid, gid)
	}

	return tree, nil
}

func cleanTarPath(p string) string {
	// Normalize backslashes to forward slashes for the rare case of an image
	// built on Windows that lands a tar entry like "etc\\foo". Tar archives
	// are nominally forward-slash, but Windows-native build tooling has
	// historically emitted backslashes; normalize once so downstream tree
	// insertion treats segments correctly.
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimPrefix(p, "./")
	if p == "" {
		return ""
	}
	cleaned := path.Clean(p)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return cleaned
}

func insertNode(root *FileNode, fullPath string, size int64, isDir, isHardlink bool, linkname string, mode fs.FileMode, uid, gid int) {
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
					existing.Size = 0
				} else {
					existing.IsDir = false
					existing.Size = size
					existing.Children = nil
				}
				existing.Path = absPath
				existing.Mode = mode
				existing.UID = uid
				existing.GID = gid
				existing.IsHardlink = isHardlink
				existing.Linkname = linkname
			} else {
				node := &FileNode{
					Name:       part,
					Path:       absPath,
					Size:       size,
					IsDir:      isDir,
					IsHardlink: isHardlink,
					Linkname:   linkname,
					Mode:       mode,
					UID:        uid,
					GID:        gid,
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
					Mode:  fs.ModeDir | 0755,
				}
				current.AddChild(existing)
			} else if !existing.IsDir {
				// A prior tar entry inserted this path as a regular file, but a
				// later entry treats it as a directory. Promote the node so the
				// AddChild below doesn't graft children onto a non-directory.
				existing.IsDir = true
				existing.Size = 0
				existing.Children = nil
				existing.Linkname = ""
				existing.IsHardlink = false
				existing.Mode = fs.ModeDir | 0755
			}
			current = existing
		}
	}
}
