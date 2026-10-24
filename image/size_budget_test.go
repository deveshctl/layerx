package image

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizedLayerSizeAcrossCompressionAndCache(t *testing.T) {
	raw := buildSimpleLayerTar(t, map[string][]byte{"app/data": bytes.Repeat([]byte("A"), 1<<20)})
	config := []byte(`{"os":"linux","architecture":"amd64","history":[{"created_by":"COPY data /app/data"}]}`)
	configPath := fmt.Sprintf("blobs/sha256/%x", sha256.Sum256(config))
	var reference *Analysis
	root := t.TempDir()
	for _, blob := range [][]byte{raw, gzipBytes(t, raw)} {
		layerPath := fmt.Sprintf("blobs/sha256/%x", sha256.Sum256(blob))
		manifest, err := json.Marshal([]dockerManifest{{Config: configPath, Layers: []string{layerPath}}})
		require.NoError(t, err)
		path := writeArchive(t, map[string][]byte{"manifest.json": manifest, configPath: config, layerPath: blob})
		resolver := NewArchiveResolver(path)
		fresh, err := AnalyzeWithOptions(t.Context(), resolver, path, AnalyzeOptions{CacheRoot: root, NoCache: true})
		require.NoError(t, err)
		require.Equal(t, int64(len(raw)), fresh.UncompressedSize)
		require.Equal(t, int64(len(blob)), fresh.TotalSize)
		require.Equal(t, "linux/amd64", fresh.Platform)
		require.Equal(t, fmt.Sprintf("sha256:%x", sha256.Sum256(raw)), fresh.Layers[0].DiffID)
		cached, err := AnalyzeWithOptions(t.Context(), resolver, path, AnalyzeOptions{CacheRoot: root})
		require.NoError(t, err)
		assert.Equal(t, fresh.UncompressedSize, cached.UncompressedSize)
		assert.Equal(t, fresh.Platform, cached.Platform)
		assert.Equal(t, fresh.Layers[0].DiffID, cached.Layers[0].DiffID)
		if reference != nil {
			assert.Equal(t, reference.UncompressedSize, fresh.UncompressedSize)
			assert.Equal(t, 1.0, EfficiencyFromAnalysis(fresh).Score)
		}
		reference = fresh
	}
}

func TestNormalizedLayerRejectsCorruptGzipTrailer(t *testing.T) {
	raw := buildSimpleLayerTar(t, map[string][]byte{"app/data": []byte("ok")})
	blob := gzipBytes(t, raw)
	blob[len(blob)-8] ^= 0xff
	manifest, _ := json.Marshal([]dockerManifest{{Config: "config.json", Layers: []string{"layer/layer.tar"}}})
	path := writeArchive(t, map[string][]byte{"manifest.json": manifest, "config.json": []byte(`{}`), "layer/layer.tar": blob})
	_, err := NewArchiveResolver(path).Resolve(t.Context(), path)
	require.Error(t, err)
}

func TestLayerMatchingSurvivesInsertedStepsAndAmbiguousCommands(t *testing.T) {
	before := []Layer{{DiffID: "base", Command: "base", UncompressedSize: 10}, {DiffID: "app", Command: "COPY app", UncompressedSize: 20}}
	after := []Layer{{DiffID: "base", Command: "base", UncompressedSize: 10}, {DiffID: "extra", Command: "RUN extra", UncompressedSize: 5}, {DiffID: "app", Command: "COPY app", UncompressedSize: 20}}
	diffs := buildLayerDiffs(before, after)
	require.Len(t, diffs, 3)
	assert.Equal(t, "shared", diffs[2].Match)
	assert.Equal(t, 1, diffs[2].BeforeIndex)
	assert.Equal(t, 2, diffs[2].AfterIndex)
	assert.Equal(t, int64(0), diffs[2].SizeDelta)
	assert.Equal(t, "added", diffs[1].Match)
	old := []Layer{{Command: "RUN same"}, {Command: "RUN same"}}
	new := []Layer{{Command: "RUN same"}, {Command: "RUN same"}}
	for _, d := range buildLayerDiffs(old, new) {
		assert.NotEqual(t, "command", d.Match)
	}
}

func TestLayerStreamCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := &layerStream{ctx: ctx, reader: bytes.NewReader([]byte("test")), hash: sha256.New()}
	_, err := r.Read(make([]byte, 4))
	assert.ErrorIs(t, err, context.Canceled)
}
