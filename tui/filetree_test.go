package tui

import (
	"strings"
	"testing"

	"github.com/deveshctl/layerx/image"
)

// fakeFiles builds a flat slice of n FileNodes, all matching the substring "f"
// so that the filter test still has overflow content after filtering.
func fakeFiles(n int) []*image.FileNode {
	files := make([]*image.FileNode, 0, n)
	for range n {
		files = append(files, &image.FileNode{
			Name: "f", Path: "/f", Size: 1,
		})
	}
	return files
}

func TestRenderFileTree_FilterActiveSuppressesBelowIndicator(t *testing.T) {
	files := fakeFiles(50)
	// filterQuery="f" with filterActive=false still shows the filter bar
	// (the bar persists until the query is cleared). The filter bar
	// occupies the panel's last row, where renderPanel would otherwise
	// paint the ▾ scroll indicator.
	out := renderFileTree(files, 0, 0, 60, 10, true, false, "f", false, nil, 0)
	if strings.Contains(out, "▾") {
		t.Fatalf("expected no ▾ when filter bar occupies last row; got panel:\n%s", out)
	}
}

func TestRenderFileTree_NoFilterRetainsBelowIndicator(t *testing.T) {
	files := fakeFiles(50)
	out := renderFileTree(files, 0, 0, 60, 10, true, false, "", false, nil, 0)
	if !strings.Contains(out, "▾") {
		t.Fatalf("expected ▾ when overflow exists and filter is not active; got panel:\n%s", out)
	}
}
