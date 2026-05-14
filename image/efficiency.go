package image

import "sort"

// WastedFile records a file path that appears redundantly across multiple layers.
type WastedFile struct {
	Path        string
	TotalWasted int64
	LayerCount  int
}

// EfficiencyResult holds the efficiency analysis output.
type EfficiencyResult struct {
	Score       float64
	WastedBytes int64
	WastedFiles []WastedFile
}

// Efficiency computes how much space is wasted by files appearing in multiple layers.
// A file at the same path in N layers means N-1 occurrences are redundant overwrites.
func Efficiency(layers []Layer) *EfficiencyResult {
	if len(layers) == 0 {
		return &EfficiencyResult{Score: 1.0}
	}

	type occurrence struct {
		layerIdx int
		size     int64
	}

	pathOccurrences := make(map[string][]occurrence)
	var totalFileBytes int64

	for i, layer := range layers {
		if layer.Tree == nil || layer.Tree.Root == nil {
			continue
		}
		walkFiles(layer.Tree.Root, func(path string, size int64) {
			pathOccurrences[path] = append(pathOccurrences[path], occurrence{layerIdx: i, size: size})
			totalFileBytes += size
		})
	}

	var wastedBytes int64
	var wastedFiles []WastedFile

	for path, occs := range pathOccurrences {
		if len(occs) < 2 {
			continue
		}
		var waste int64
		for _, occ := range occs[1:] {
			waste += occ.size
		}
		wastedBytes += waste
		wastedFiles = append(wastedFiles, WastedFile{
			Path:        path,
			TotalWasted: waste,
			LayerCount:  len(occs),
		})
	}

	sort.Slice(wastedFiles, func(i, j int) bool {
		return wastedFiles[i].TotalWasted > wastedFiles[j].TotalWasted
	})

	score := 1.0
	if totalFileBytes > 0 {
		score = 1.0 - float64(wastedBytes)/float64(totalFileBytes)
	}
	if score < 0 {
		score = 0
	}

	return &EfficiencyResult{
		Score:       score,
		WastedBytes: wastedBytes,
		WastedFiles: wastedFiles,
	}
}

// walkFiles recursively visits all non-directory file nodes.
func walkFiles(node *FileNode, fn func(path string, size int64)) {
	for _, child := range node.Children {
		if child.IsDir {
			walkFiles(child, fn)
		} else {
			fn(child.Path, child.Size)
		}
	}
}
