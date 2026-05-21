package image

import "context"

// Analysis holds the complete result of inspecting an image.
type Analysis struct {
	ImageRef     string
	Layers       []Layer
	StackedTrees []*FileTree
	TotalSize    int64
}

// Analyze resolves an image and computes stacked file trees.
func Analyze(ctx context.Context, resolver Resolver, imageRef string) (*Analysis, error) {
	return AnalyzeWithProgress(ctx, resolver, imageRef, nil)
}

// AnalyzeWithProgress resolves an image with progress reporting and computes stacked file trees.
func AnalyzeWithProgress(ctx context.Context, resolver Resolver, imageRef string, progress chan<- ProgressEvent) (*Analysis, error) {
	layers, err := resolver.ResolveWithProgress(ctx, imageRef, progress)
	if err != nil {
		return nil, err
	}

	stacked := Stack(layers)
	assignNetDeltas(layers, stacked)

	var totalSize int64
	for _, l := range layers {
		totalSize += l.Size
	}

	return &Analysis{
		ImageRef:     imageRef,
		Layers:       layers,
		StackedTrees: stacked,
		TotalSize:    totalSize,
	}, nil
}
