package image

import (
	"archive/tar"
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStack_RecreatedDirectories(t *testing.T) {
	for _, opaque := range []bool{false, true} {
		name := "directory whiteout"
		if opaque {
			name = "opaque whiteout"
		}
		t.Run(name, func(t *testing.T) {
			base := makeTree(makeDir("cache", "/cache",
				makeDir("sub", "/cache/sub", makeFile("old", "/cache/sub/old", 100))))
			next := makeTree(makeDir("cache", "/cache",
				makeDir("sub", "/cache/sub", makeFile("new", "/cache/sub/new", 50))))
			if opaque {
				next.Root.FindChild("cache").AddChild(makeFile(".wh..wh..opq", "/cache/.wh..wh..opq", 0))
			} else {
				next.Root.AddChild(makeFile(".wh.cache", "/.wh.cache", 0))
			}
			layers := []Layer{{Tree: base}, {Tree: next}, {}}
			stacked := Stack(layers)
			aggregated := BuildAggregatedTrees(layers)
			for view, trees := range map[string][]*FileTree{"layer": stacked, "cumulative": aggregated} {
				t.Run(view, func(t *testing.T) {
					cache := trees[1].Root.FindChild("cache")
					require.NotNil(t, cache)
					require.Len(t, cache.Children, 1)
					sub := cache.FindChild("sub")
					require.NotNil(t, sub)
					require.Len(t, sub.Children, 2)
					old := sub.FindChild("old")
					require.NotNil(t, old)
					assert.Equal(t, Removed, old.DiffType)
					assert.Equal(t, 0, old.IntroducedInLayer)
					fresh := sub.FindChild("new")
					require.NotNil(t, fresh)
					assert.Equal(t, Added, fresh.DiffType)
					assert.Equal(t, 1, fresh.IntroducedInLayer)
				})
			}
			assert.Nil(t, stacked[2].Root.FindChild("cache").FindChild("sub").FindChild("old"))
			old := aggregated[2].Root.FindChild("cache").FindChild("sub").FindChild("old")
			require.NotNil(t, old)
			assert.Equal(t, Removed, old.DiffType)
			efficiency := EfficiencyFromAnalysis(&Analysis{Layers: layers, StackedTrees: stacked})
			assert.Equal(t, int64(100), efficiency.WastedBytes)
			assert.InDelta(t, 1.0/3, efficiency.Score, 0.0001)
		})
	}
}

func TestStack_RecreatedPathOrigin(t *testing.T) {
	for _, directory := range []bool{false, true} {
		name := "file"
		original, recreated := makeFile("x", "/x", 100), makeFile("x", "/x", 200)
		if directory {
			name = "directory"
			original = makeDir("x", "/x", makeFile("old", "/x/old", 100))
			recreated = makeDir("x", "/x", makeFile("new", "/x/new", 200))
		}
		t.Run(name, func(t *testing.T) {
			layers := []Layer{
				{Tree: makeTree(original)},
				{Tree: makeTree(makeFile(".wh.x", "/.wh.x", 0))},
				{Tree: makeTree(recreated)},
			}
			for view, trees := range map[string][]*FileTree{"layer": Stack(layers), "cumulative": BuildAggregatedTrees(layers)} {
				t.Run(view, func(t *testing.T) {
					x := trees[2].Root.FindChild("x")
					require.NotNil(t, x)
					assert.Equal(t, Added, x.DiffType)
					assert.Equal(t, 2, x.IntroducedInLayer)
					if directory {
						assert.Equal(t, Added, x.FindChild("new").DiffType)
					}
				})
			}
		})
	}
}

func TestStack_DirectoryWhiteoutRecreationTarOrder(t *testing.T) {
	parse := func(headers ...*tar.Header) ([]byte, *FileTree) {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		for _, h := range headers {
			require.NoError(t, tw.WriteHeader(h))
			_, err := tw.Write(make([]byte, h.Size))
			require.NoError(t, err)
		}
		require.NoError(t, tw.Close())
		tree, err := ParseLayerTar(bytes.NewReader(buf.Bytes()))
		require.NoError(t, err)
		return buf.Bytes(), tree
	}
	_, base := parse(&tar.Header{Name: "cache/", Typeflag: tar.TypeDir, Mode: 0755},
		&tar.Header{Name: "cache/old", Typeflag: tar.TypeReg, Mode: 0644, Size: 100})
	for _, order := range []string{"whiteout first", "whiteout last"} {
		t.Run(order, func(t *testing.T) {
			entries := []*tar.Header{
				{Name: "cache/", Typeflag: tar.TypeDir, Mode: 0755},
				{Name: "cache/new", Typeflag: tar.TypeReg, Mode: 0644, Size: 50},
			}
			marker := &tar.Header{Name: ".wh.cache", Typeflag: tar.TypeReg, Mode: 0644}
			if order == "whiteout first" {
				entries = append([]*tar.Header{marker}, entries...)
			} else {
				entries = append(entries, marker)
			}
			data, next := parse(entries...)
			_, found, err := findFileInLayer(data, "cache/old")
			assert.False(t, found)
			assert.True(t, errors.Is(err, errWhiteoutStop))
			result := Efficiency([]Layer{{Tree: base}, {Tree: next}})
			assert.Equal(t, int64(100), result.WastedBytes)
			assert.InDelta(t, 1.0/3, result.Score, 0.0001)
		})
	}
}
