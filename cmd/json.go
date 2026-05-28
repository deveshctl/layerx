package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/deveshctl/layerx/image"
)

type jsonExport struct {
	ImageRef   string         `json:"imageRef"`
	TotalSize  int64          `json:"totalSize"`
	LayerCount int            `json:"layerCount"`
	Efficiency jsonEfficiency `json:"efficiency"`
	Layers     []jsonLayer    `json:"layers"`
}

type jsonEfficiency struct {
	Score       float64          `json:"score"`
	WastedBytes int64            `json:"wastedBytes"`
	WastedFiles []jsonWastedFile `json:"wastedFiles"`
}

type jsonWastedFile struct {
	Path        string `json:"path"`
	TotalWasted int64  `json:"totalWasted"`
	LayerCount  int    `json:"layerCount"`
}

type jsonLayer struct {
	Index    int        `json:"index"`
	ID       string     `json:"id"`
	Size     int64      `json:"size"`
	NetDelta int64      `json:"netDelta"`
	Command  string     `json:"command"`
	Files    []jsonFile `json:"files"`
}

type jsonFile struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	DiffType string `json:"diffType"`
}

func runJSONExport(ctx context.Context, imageRef, outputPath string, noCache bool) error {
	resolver, err := selectResolver(imageRef)
	if err != nil {
		return err
	}

	analysis, err := image.AnalyzeWithOptions(ctx, resolver, imageRef,
		image.AnalyzeOptions{NoCache: noCache})
	if err != nil {
		return err
	}

	return runJSONExportFromAnalysis(analysis, outputPath)
}

// runJSONExportFromAnalysis writes the analysis to outputPath without
// re-resolving the image. Used when CI has already produced an analysis we
// can reuse.
func runJSONExportFromAnalysis(analysis *image.Analysis, outputPath string) error {
	efficiency := image.EfficiencyFromAnalysis(analysis)

	export := buildJSONExport(analysis, efficiency)

	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling JSON: %w", err)
	}

	if err := writeJSONAtomic(outputPath, data); err != nil {
		return fmt.Errorf("writing %s: %w", outputPath, err)
	}

	fmt.Fprintf(os.Stderr, "Written to %s\n", outputPath)
	return nil
}

func buildJSONExport(analysis *image.Analysis, efficiency *image.EfficiencyResult) *jsonExport {
	export := &jsonExport{
		ImageRef:   analysis.ImageRef,
		TotalSize:  analysis.TotalSize,
		LayerCount: len(analysis.Layers),
		Efficiency: jsonEfficiency{
			Score:       efficiency.Score,
			WastedBytes: efficiency.WastedBytes,
		},
	}

	for _, wf := range efficiency.WastedFiles {
		export.Efficiency.WastedFiles = append(export.Efficiency.WastedFiles, jsonWastedFile{
			Path:        wf.Path,
			TotalWasted: wf.TotalWasted,
			LayerCount:  wf.LayerCount,
		})
	}
	if export.Efficiency.WastedFiles == nil {
		export.Efficiency.WastedFiles = []jsonWastedFile{}
	}

	for i, layer := range analysis.Layers {
		jl := jsonLayer{
			Index:    layer.Index,
			ID:       layer.ID,
			Size:     layer.Size,
			NetDelta: layer.NetDelta,
			Command:  layer.Command,
			Files:    []jsonFile{},
		}
		if i < len(analysis.StackedTrees) && analysis.StackedTrees[i] != nil && analysis.StackedTrees[i].Root != nil {
			collectFiles(analysis.StackedTrees[i].Root, &jl.Files)
		}
		export.Layers = append(export.Layers, jl)
	}

	return export
}

func collectFiles(node *image.FileNode, files *[]jsonFile) {
	for _, child := range node.Children {
		if child.IsDir {
			collectFiles(child, files)
		} else {
			*files = append(*files, jsonFile{
				Path:     child.Path,
				Size:     child.Size,
				DiffType: diffTypeString(child.DiffType),
			})
		}
	}
}

func diffTypeString(dt image.DiffType) string {
	switch dt {
	case image.Added:
		return "added"
	case image.Modified:
		return "modified"
	case image.Removed:
		return "removed"
	default:
		return "unchanged"
	}
}

func writeJSONAtomic(targetPath string, data []byte) error {
	dir := filepath.Dir(targetPath)
	f, err := os.CreateTemp(dir, ".layerx-json-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, targetPath); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
