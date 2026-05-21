package tui

import "github.com/deveshctl/layerx/image"

// visibleTree returns a depth-first list of nodes respecting collapsed directories.
// collapsed[path]==true hides that directory's descendants. Missing keys mean expanded.
func visibleTree(root *image.FileNode, collapsed map[string]bool) []*image.FileNode {
	if root == nil {
		return nil
	}
	var result []*image.FileNode
	var walk func(node *image.FileNode)
	walk = func(node *image.FileNode) {
		for _, child := range node.Children {
			result = append(result, child)
			if child.IsDir && !collapsed[child.Path] {
				walk(child)
			}
		}
	}
	walk(root)
	return result
}

func isCollapsed(collapsed map[string]bool, path string) bool {
	return collapsed != nil && collapsed[path]
}

func toggleCollapsed(collapsed map[string]bool, path string) map[string]bool {
	if collapsed == nil {
		collapsed = make(map[string]bool)
	}
	if collapsed[path] {
		delete(collapsed, path)
	} else {
		collapsed[path] = true
	}
	return collapsed
}
