package image

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockResolver struct {
	layers       []Layer
	err          error
	imageID      string
	imageIDErr   error
	resolveCalls int
}

func (m *mockResolver) Resolve(_ context.Context, _ string) ([]Layer, error) {
	m.resolveCalls++
	return m.layers, m.err
}

func (m *mockResolver) ResolveWithProgress(_ context.Context, _ string, _ chan<- ProgressEvent) ([]Layer, error) {
	m.resolveCalls++
	return m.layers, m.err
}

func (m *mockResolver) Inspect(_ context.Context, _ string) (*ImageMeta, error) {
	var total int64
	for _, l := range m.layers {
		total += l.Size
	}
	return &ImageMeta{Size: total}, m.err
}

func (m *mockResolver) ImageID(_ context.Context, _ string) (string, error) {
	if m.imageIDErr != nil {
		return "", m.imageIDErr
	}
	if m.imageID != "" {
		return m.imageID, nil
	}
	return "sha256:deadbeef", nil
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

func TestAnalyze_NetDelta_SingleLayer(t *testing.T) {
	layers := []Layer{
		{
			Index: 0, ID: "aabb", Size: 1000,
			Tree: makeTree(
				makeFile("README", "/README", 800),
			),
		},
	}
	result, err := Analyze(context.Background(), &mockResolver{layers: layers}, "test:latest")
	require.NoError(t, err)

	assert.Equal(t, int64(800), result.Layers[0].NetDelta)
	assert.Equal(t, TreeLiveFileBytes(result.StackedTrees[0]), result.Layers[0].NetDelta)
}

func TestAnalyze_NetDelta_TwoLayersNoOverlap(t *testing.T) {
	layers := []Layer{
		{Index: 0, Tree: makeTree(makeFile("a", "/a", 100))},
		{Index: 1, Tree: makeTree(makeFile("b", "/b", 250))},
	}
	result, err := Analyze(context.Background(), &mockResolver{layers: layers}, "test")
	require.NoError(t, err)

	assert.Equal(t, int64(100), result.Layers[0].NetDelta)
	assert.Equal(t, int64(250), result.Layers[1].NetDelta)
}

func TestAnalyze_NetDelta_OverwriteSameSize(t *testing.T) {
	layers := []Layer{
		{Index: 0, Tree: makeTree(makeFile("config", "/config", 500))},
		{Index: 1, Tree: makeTree(makeFile("config", "/config", 500))},
	}
	result, err := Analyze(context.Background(), &mockResolver{layers: layers}, "test")
	require.NoError(t, err)

	assert.Equal(t, int64(500), result.Layers[0].NetDelta)
	assert.Equal(t, int64(0), result.Layers[1].NetDelta)
}

func TestAnalyze_NetDelta_WhiteoutNegative(t *testing.T) {
	layers := []Layer{
		{Index: 0, Tree: makeTree(
			makeDir("var", "/var",
				makeFile("cache", "/var/cache", 1000),
			),
		)},
		{Index: 1, Tree: makeTree(
			makeDir("var", "/var",
				makeFile(".wh.cache", "/var/.wh.cache", 0),
			),
		)},
	}
	result, err := Analyze(context.Background(), &mockResolver{layers: layers}, "test")
	require.NoError(t, err)

	assert.Equal(t, int64(1000), result.Layers[0].NetDelta)
	assert.Equal(t, int64(-1000), result.Layers[1].NetDelta)
}

func TestAnalyze_NetDelta_EmptyLayer(t *testing.T) {
	layers := []Layer{
		{Index: 0, Tree: makeTree(makeFile("a", "/a", 100))},
		{Index: 1, Tree: makeTree()},
	}
	result, err := Analyze(context.Background(), &mockResolver{layers: layers}, "test")
	require.NoError(t, err)

	assert.Equal(t, int64(100), result.Layers[0].NetDelta)
	assert.Equal(t, int64(0), result.Layers[1].NetDelta)
}

func TestAnalyze_NetDelta_OpaqueWhiteoutNegative(t *testing.T) {
	layers := []Layer{
		{Index: 0, Tree: makeTree(
			makeDir("var", "/var",
				makeDir("cache", "/var/cache",
					makeFile("a", "/var/cache/a", 600),
					makeFile("b", "/var/cache/b", 400),
				),
			),
		)},
		{Index: 1, Tree: makeTree(
			makeDir("var", "/var",
				makeDir("cache", "/var/cache",
					makeFile(".wh..wh..opq", "/var/cache/.wh..wh..opq", 0),
				),
			),
		)},
	}
	result, err := Analyze(context.Background(), &mockResolver{layers: layers}, "test")
	require.NoError(t, err)

	assert.Equal(t, int64(1000), result.Layers[0].NetDelta)
	assert.Equal(t, int64(-1000), result.Layers[1].NetDelta, "opaque whiteout removes all dir contents")
}

func TestAnalyze_NetDelta_SumEqualsFinalLiveSize(t *testing.T) {
	layers := []Layer{
		{Index: 0, Tree: makeTree(
			makeFile("a", "/a", 300),
			makeFile("b", "/b", 200),
		)},
		{Index: 1, Tree: makeTree(
			makeFile("c", "/c", 150),
			makeFile(".wh.b", "/.wh.b", 0),
		)},
		{Index: 2, Tree: makeTree(
			makeFile("a", "/a", 500),
		)},
	}
	result, err := Analyze(context.Background(), &mockResolver{layers: layers}, "test")
	require.NoError(t, err)

	var sum int64
	for _, l := range result.Layers {
		sum += l.NetDelta
	}
	final := TreeLiveFileBytes(result.StackedTrees[len(result.StackedTrees)-1])
	assert.Equal(t, final, sum)
}
