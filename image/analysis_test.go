package image

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockResolver struct {
	layers []Layer
	err    error
}

func (m *mockResolver) Resolve(_ context.Context, _ string) ([]Layer, error) {
	return m.layers, m.err
}

func (m *mockResolver) ResolveWithProgress(_ context.Context, _ string, _ chan<- ProgressEvent) ([]Layer, error) {
	return m.layers, m.err
}

func (m *mockResolver) Inspect(_ context.Context, _ string) (*ImageMeta, error) {
	var total int64
	for _, l := range m.layers {
		total += l.Size
	}
	return &ImageMeta{Size: total}, m.err
}

func TestAnalyze_Success(t *testing.T) {
	layers := []Layer{
		{
			Index: 0, ID: "aabb", Size: 1000,
			Tree: makeTree(
				makeDir("bin", "/bin",
					makeFile("sh", "/bin/sh", 800),
				),
			),
		},
		{
			Index: 1, ID: "ccdd", Size: 500,
			Tree: makeTree(
				makeDir("etc", "/etc",
					makeFile("passwd", "/etc/passwd", 400),
				),
			),
		},
	}

	resolver := &mockResolver{layers: layers}
	result, err := Analyze(context.Background(), resolver, "test:latest")
	require.NoError(t, err)

	assert.Equal(t, "test:latest", result.ImageRef)
	assert.Len(t, result.Layers, 2)
	assert.Len(t, result.StackedTrees, 2)
	assert.Equal(t, int64(1500), result.TotalSize)

	bin := result.StackedTrees[0].Root.FindChild("bin")
	require.NotNil(t, bin)
	assert.Equal(t, Added, bin.DiffType)
}

func TestAnalyze_ResolverError(t *testing.T) {
	resolver := &mockResolver{err: &ErrDaemonNotRunning{Cause: assert.AnError}}
	result, err := Analyze(context.Background(), resolver, "test:latest")

	assert.Nil(t, result)
	assert.Error(t, err)
	var daemonErr *ErrDaemonNotRunning
	assert.ErrorAs(t, err, &daemonErr)
}

func TestAnalyze_EmptyImage(t *testing.T) {
	resolver := &mockResolver{layers: []Layer{}}
	result, err := Analyze(context.Background(), resolver, "scratch:latest")
	require.NoError(t, err)

	assert.Equal(t, "scratch:latest", result.ImageRef)
	assert.Empty(t, result.Layers)
	assert.Empty(t, result.StackedTrees)
	assert.Equal(t, int64(0), result.TotalSize)
}
