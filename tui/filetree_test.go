package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

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
	out := renderFileTree(files, 0, 0, 60, 10, true, false, "f", false, false, nil, 0)
	if strings.Contains(out, "▾") {
		t.Fatalf("expected no ▾ when filter bar occupies last row; got panel:\n%s", out)
	}
}

func TestRenderFileTree_NoFilterRetainsBelowIndicator(t *testing.T) {
	files := fakeFiles(50)
	out := renderFileTree(files, 0, 0, 60, 10, true, false, "", false, false, nil, 0)
	if !strings.Contains(out, "▾") {
		t.Fatalf("expected ▾ when overflow exists and filter is not active; got panel:\n%s", out)
	}
}

func TestRenderFileTree_TitleSingleLayerMode(t *testing.T) {
	files := fakeFiles(3)
	out := renderFileTree(files, 0, 0, 60, 10, true, false, "", false, false, nil, 0)
	if !strings.Contains(out, "Current Layer Contents") {
		t.Fatalf("expected title to contain 'Current Layer Contents' when aggregated=false; got:\n%s", out)
	}
	if strings.Contains(out, "Aggregated Layer Contents") {
		t.Fatalf("aggregated title leaked into single-layer mode")
	}
}

func TestRenderFileTree_TitleAggregatedMode(t *testing.T) {
	files := fakeFiles(3)
	out := renderFileTree(files, 0, 0, 60, 10, true, false, "", false, true, nil, 0)
	if !strings.Contains(out, "Aggregated Layer Contents") {
		t.Fatalf("expected title to contain 'Aggregated Layer Contents' when aggregated=true; got:\n%s", out)
	}
}

func TestPadRight_DisplayWidthMeasurement(t *testing.T) {
	cases := []struct {
		name string
		in   string
		w    int
	}{
		{"ascii_short", "abc", 8},
		{"ascii_exact", "abcdefgh", 8},
		{"accented_latin", "café", 8},
		{"cjk", "日本語", 10}, // each rune renders at width 2 → 6 cells, 4 spaces pad
		{"emoji", "hi👋", 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := padRight(tc.in, tc.w)
			if w := lipgloss.Width(got); w != tc.w {
				t.Fatalf("padRight(%q, %d): width=%d, want %d (output=%q)",
					tc.in, tc.w, w, tc.w, got)
			}
		})
	}
}

func TestPadLeft_DisplayWidthMeasurement(t *testing.T) {
	cases := []struct {
		name string
		in   string
		w    int
	}{
		{"ascii_short", "abc", 8},
		{"ascii_exact", "abcdefgh", 8},
		{"accented_latin", "café", 8},
		{"cjk", "日本語", 10},
		{"emoji", "👋hi", 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := padLeft(tc.in, tc.w)
			if w := lipgloss.Width(got); w != tc.w {
				t.Fatalf("padLeft(%q, %d): width=%d, want %d (output=%q)",
					tc.in, tc.w, w, tc.w, got)
			}
		})
	}
}

func TestPadRight_TruncatesOverWidth(t *testing.T) {
	// Overlong ASCII must clip to exactly the requested width.
	got := padRight("abcdefghij", 5)
	if w := lipgloss.Width(got); w != 5 {
		t.Fatalf("expected width 5, got %d (%q)", w, got)
	}
}

func TestNodeEffectiveSize_RemovedFileReturnsZero(t *testing.T) {
	n := &image.FileNode{
		Name: "gone", Path: "/gone", Size: 1024,
		DiffType: image.Removed,
	}
	if got := nodeEffectiveSize(n); got != 0 {
		t.Fatalf("Removed leaf: got %d, want 0", got)
	}
}

func TestNodeEffectiveSize_DirExcludesRemovedChildren(t *testing.T) {
	kept := &image.FileNode{Name: "kept", Path: "/etc/kept", Size: 100}
	removed := &image.FileNode{
		Name: "gone", Path: "/etc/gone", Size: 900,
		DiffType: image.Removed,
	}
	dir := &image.FileNode{
		Name: "etc", Path: "/etc", IsDir: true,
		Children: []*image.FileNode{kept, removed},
	}
	if got := nodeEffectiveSize(dir); got != 100 {
		t.Fatalf("dir with Removed child: got %d, want 100", got)
	}
}

func TestNodeEffectiveSize_RemovedDirShortCircuits(t *testing.T) {
	child := &image.FileNode{Name: "passwd", Path: "/etc/passwd", Size: 500}
	dir := &image.FileNode{
		Name: "etc", Path: "/etc", IsDir: true,
		DiffType: image.Removed,
		Children: []*image.FileNode{child},
	}
	if got := nodeEffectiveSize(dir); got != 0 {
		t.Fatalf("Removed dir: got %d, want 0 (subtree must not be summed)", got)
	}
}

func TestNodeEffectiveSize_LiveDiffTypesStillCounted(t *testing.T) {
	added := &image.FileNode{Name: "a", Path: "/d/a", Size: 10, DiffType: image.Added}
	modified := &image.FileNode{Name: "m", Path: "/d/m", Size: 20, DiffType: image.Modified}
	unchanged := &image.FileNode{Name: "u", Path: "/d/u", Size: 30, DiffType: image.Unchanged}
	dir := &image.FileNode{
		Name: "d", Path: "/d", IsDir: true,
		Children: []*image.FileNode{added, modified, unchanged},
	}
	if got := nodeEffectiveSize(dir); got != 60 {
		t.Fatalf("live-diff dir: got %d, want 60", got)
	}
}
