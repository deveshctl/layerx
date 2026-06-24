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
	// Default: behave as if the image is not local yet. AnalyzeWithOptions
	// then skips the cache lookup and falls through to ResolveWithProgress.
	// Tests that exercise the cache must set imageID explicitly.
	return "", assert.AnError
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
	assert.Len(t, result.AggregatedTrees, 2, "AggregatedTrees length must match Layers")
	assert.NotNil(t, result.AggregatedTrees[0])
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

func TestAnalyze_CacheHit_SkipsResolve(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("LAYERX_CACHE_DIR", cacheRoot)

	layers := []Layer{
		{Index: 0, ID: "aa", Size: 100, Command: "FROM x",
			Tree: makeTree(makeFile("a", "/a", 50)),
		},
	}
	resolver := &mockResolver{layers: layers, imageID: "sha256:cafebabe"}

	// First call: miss -> resolve -> save.
	r1, err := AnalyzeWithOptions(context.Background(), resolver, "img:1", AnalyzeOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, resolver.resolveCalls)
	require.NotNil(t, r1)
	assert.Equal(t, int64(100), r1.TotalSize)

	// Second call: same digest -> hit -> no resolve.
	r2, err := AnalyzeWithOptions(context.Background(), resolver, "img:1", AnalyzeOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, resolver.resolveCalls, "cache hit must not call ResolveWithProgress")
	require.NotNil(t, r2)
	assert.Equal(t, "img:1", r2.ImageRef)
	require.Len(t, r2.StackedTrees, 1)
}

func TestAnalyze_NoCache_AlwaysResolves(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("LAYERX_CACHE_DIR", cacheRoot)

	layers := []Layer{{Index: 0, ID: "aa", Size: 1, Tree: makeTree(makeFile("x", "/x", 1))}}
	resolver := &mockResolver{layers: layers, imageID: "sha256:1111"}

	_, err := AnalyzeWithOptions(context.Background(), resolver, "img", AnalyzeOptions{NoCache: true})
	require.NoError(t, err)
	_, err = AnalyzeWithOptions(context.Background(), resolver, "img", AnalyzeOptions{NoCache: true})
	require.NoError(t, err)
	assert.Equal(t, 2, resolver.resolveCalls, "NoCache must always resolve")

	// But the cache MUST still have been written.
	cached, ok, err := loadCache(cacheRoot, "sha256:1111")
	require.NoError(t, err)
	require.True(t, ok, "NoCache still writes cache after a successful resolve")
	require.Len(t, cached, 1)
}

func TestAnalyze_DifferentTagSameDigest_HitsCache(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("LAYERX_CACHE_DIR", cacheRoot)

	layers := []Layer{{Index: 0, ID: "aa", Size: 1, Tree: makeTree(makeFile("x", "/x", 1))}}
	resolver := &mockResolver{layers: layers, imageID: "sha256:samedigest"}

	r1, err := AnalyzeWithOptions(context.Background(), resolver, "myapp:latest", AnalyzeOptions{})
	require.NoError(t, err)
	assert.Equal(t, "myapp:latest", r1.ImageRef)

	r2, err := AnalyzeWithOptions(context.Background(), resolver, "myapp:dev", AnalyzeOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, resolver.resolveCalls, "same digest under a different tag is still a hit")
	assert.Equal(t, "myapp:dev", r2.ImageRef, "ImageRef comes from current arg, not envelope")
}

func TestAnalyze_ImageIDError_FallsBackToColdResolve(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("LAYERX_CACHE_DIR", cacheRoot)

	layers := []Layer{{Index: 0, ID: "aa", Size: 1, Tree: makeTree(makeFile("x", "/x", 1))}}
	resolver := &mockResolver{
		layers:     layers,
		imageIDErr: assert.AnError,
	}

	r, err := AnalyzeWithOptions(context.Background(), resolver, "img", AnalyzeOptions{})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, 1, resolver.resolveCalls, "ImageID error -> cold resolve still works")
}

func TestAnalyze_CacheHit_EmitsPhaseCacheLoad(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("LAYERX_CACHE_DIR", cacheRoot)

	layers := []Layer{{Index: 0, ID: "aa", Size: 1, Tree: makeTree(makeFile("x", "/x", 1))}}
	resolver := &mockResolver{layers: layers, imageID: "sha256:probe"}

	// Prime the cache.
	_, err := AnalyzeWithOptions(context.Background(), resolver, "img", AnalyzeOptions{})
	require.NoError(t, err)

	// Second call: drain progress and look for PhaseCacheLoad.
	progress := make(chan ProgressEvent, 8)
	r, err := AnalyzeWithOptions(context.Background(), resolver, "img",
		AnalyzeOptions{Progress: progress})
	require.NoError(t, err)
	require.NotNil(t, r)

	close(progress)
	saw := false
	for ev := range progress {
		if ev.Phase == PhaseCacheLoad {
			saw = true
		}
	}
	assert.True(t, saw, "cache hit must emit PhaseCacheLoad")
}
