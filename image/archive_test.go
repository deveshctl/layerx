package image

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeArchive writes a tar archive (built from files) to a fresh file in
// t.TempDir() and returns its absolute path. The file is cleaned up by the
// test framework when the test ends.
func writeArchive(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "image.tar")
	buf := buildTar(t, files)
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
	return path
}

// gzipBytes returns data wrapped in a gzip stream. Used to build OCI-style
// layer blobs (Docker 25+ saves layer tars compressed as application/vnd.oci...).
func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, err := gw.Write(data)
	require.NoError(t, err)
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

// buildSimpleLayerTar writes one regular file per (name, content) pair into
// a tar stream and returns the tar bytes. Mirrors how a real layer blob is
// laid out. Distinct from tree_parser_test.go's buildLayerTar (which takes
// a richer []tarEntry with whiteouts and modes).
func buildSimpleLayerTar(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, content := range files {
		hdr := &tar.Header{
			Name:     name,
			Size:     int64(len(content)),
			Mode:     0644,
			Typeflag: tar.TypeReg,
		}
		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	return buf.Bytes()
}

func TestArchiveResolver_ResolveLegacyDockerSave(t *testing.T) {
	manifest := []dockerManifest{{
		Config: "sha256deadbeef.json",
		Layers: []string{
			"aaaaaa11111122223333444455/layer.tar",
			"bbbbbb22222233334444555566/layer.tar",
		},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	configData := buildConfig(t, []string{
		"/bin/sh -c apt-get update",
		"/bin/sh -c #(nop) CMD [\"sh\"]",
	})

	layer0 := buildSimpleLayerTar(t, map[string][]byte{"etc/os-release": []byte("name=test")})
	layer1 := buildSimpleLayerTar(t, map[string][]byte{"app/main.go": []byte("package main")})

	path := writeArchive(t, map[string][]byte{
		"manifest.json":                            manifestData,
		"sha256deadbeef.json":                      configData,
		"aaaaaa11111122223333444455/layer.tar":     layer0,
		"bbbbbb22222233334444555566/layer.tar":     layer1,
	})

	r := NewArchiveResolver(path)
	layers, err := r.Resolve(context.Background(), path)
	require.NoError(t, err)
	require.Len(t, layers, 2)

	assert.Equal(t, "aaaaaa111111", layers[0].ID)
	assert.Equal(t, "/bin/sh -c apt-get update", layers[0].Command)
	assert.NotNil(t, layers[0].Tree)

	assert.Equal(t, "bbbbbb222222", layers[1].ID)
	assert.Equal(t, "/bin/sh -c #(nop) CMD [\"sh\"]", layers[1].Command)
}

func TestArchiveResolver_ResolveOCIFormat(t *testing.T) {
	// OCI layout: config and layers live under blobs/sha256/<digest>; layer
	// blobs are gzipped tarballs.
	manifest := []dockerManifest{{
		Config: "blobs/sha256/cafebabecafebabe1111222233334444",
		Layers: []string{
			"blobs/sha256/aaaa1111aaaa1111aaaa1111aaaa1111",
			"blobs/sha256/bbbb2222bbbb2222bbbb2222bbbb2222",
		},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	configData := buildConfig(t, []string{"/bin/sh -c apt-get update", "CMD [\"sh\"]"})

	layer0 := gzipBytes(t, buildSimpleLayerTar(t, map[string][]byte{"etc/os-release": []byte("name=oci")}))
	layer1 := gzipBytes(t, buildSimpleLayerTar(t, map[string][]byte{"app/main.go": []byte("package main")}))

	path := writeArchive(t, map[string][]byte{
		"oci-layout":                              []byte(`{"imageLayoutVersion":"1.0.0"}`),
		"index.json":                              []byte("{}"),
		"manifest.json":                           manifestData,
		"blobs/sha256/cafebabecafebabe1111222233334444": configData,
		"blobs/sha256/aaaa1111aaaa1111aaaa1111aaaa1111": layer0,
		"blobs/sha256/bbbb2222bbbb2222bbbb2222bbbb2222": layer1,
	})

	r := NewArchiveResolver(path)
	layers, err := r.Resolve(context.Background(), path)
	require.NoError(t, err)
	require.Len(t, layers, 2)

	// extractShortID for OCI takes parts[2] (the digest), first 12 chars.
	assert.Equal(t, "aaaa1111aaaa", layers[0].ID)
	assert.Equal(t, "bbbb2222bbbb", layers[1].ID)
	assert.NotNil(t, layers[0].Tree)
	assert.NotNil(t, layers[1].Tree)
}

func TestArchiveResolver_ImageID_LegacyFormat(t *testing.T) {
	manifest := []dockerManifest{{Config: "sha256deadbeef.json", Layers: []string{}}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	configBytes := buildConfig(t, []string{})
	path := writeArchive(t, map[string][]byte{
		"manifest.json":       manifestData,
		"sha256deadbeef.json": configBytes,
	})

	r := NewArchiveResolver(path)
	id, err := r.ImageID(context.Background(), path)
	require.NoError(t, err)
	// Legacy format hashes the config blob bytes — independent of the
	// filename stem.
	sum := sha256.Sum256(configBytes)
	assert.Equal(t, "sha256:"+hex.EncodeToString(sum[:]), id)
}

func TestArchiveResolver_ImageID_OCIFormat(t *testing.T) {
	manifest := []dockerManifest{{
		Config: "blobs/sha256/cafebabecafebabe1111222233334444",
		Layers: []string{},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	path := writeArchive(t, map[string][]byte{
		"manifest.json": manifestData,
	})

	r := NewArchiveResolver(path)
	id, err := r.ImageID(context.Background(), path)
	require.NoError(t, err)
	assert.Equal(t, "sha256:cafebabecafebabe1111222233334444", id)
}

func TestArchiveResolver_ImageIDStableAcrossPaths(t *testing.T) {
	// Same image content in two locations should yield the same ImageID, so
	// the cache hits regardless of where the user moves the tarball.
	manifest := []dockerManifest{{Config: "sha256stable12.json", Layers: []string{}}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)
	files := map[string][]byte{
		"manifest.json":        manifestData,
		"sha256stable12.json":  buildConfig(t, []string{}),
	}

	pathA := writeArchive(t, files)
	pathB := writeArchive(t, files)
	require.NotEqual(t, pathA, pathB, "test should write to two different temp dirs")

	idA, err := NewArchiveResolver(pathA).ImageID(context.Background(), pathA)
	require.NoError(t, err)
	idB, err := NewArchiveResolver(pathB).ImageID(context.Background(), pathB)
	require.NoError(t, err)
	assert.Equal(t, idA, idB)
}

func TestArchiveResolver_ImageIDIsContentAddressed(t *testing.T) {
	// Same config bytes referenced via two different filename stems must
	// produce the same ImageID — proves the digest is derived from content,
	// not from the path. (Renaming a tar should not invalidate the cache.)
	configBytes := buildConfig(t, []string{"shared"})

	mfA := []dockerManifest{{Config: "configA.json", Layers: []string{}}}
	mfADataA, err := json.Marshal(mfA)
	require.NoError(t, err)
	pathA := writeArchive(t, map[string][]byte{
		"manifest.json": mfADataA,
		"configA.json":  configBytes,
	})

	mfB := []dockerManifest{{Config: "configB.json", Layers: []string{}}}
	mfBData, err := json.Marshal(mfB)
	require.NoError(t, err)
	pathB := writeArchive(t, map[string][]byte{
		"manifest.json": mfBData,
		"configB.json":  configBytes,
	})

	idA, err := NewArchiveResolver(pathA).ImageID(context.Background(), pathA)
	require.NoError(t, err)
	idB, err := NewArchiveResolver(pathB).ImageID(context.Background(), pathB)
	require.NoError(t, err)
	assert.Equal(t, idA, idB, "identical config bytes under different filenames must share ImageID")

	// And differing config bytes must produce different IDs.
	mfC := []dockerManifest{{Config: "configA.json", Layers: []string{}}}
	mfCData, err := json.Marshal(mfC)
	require.NoError(t, err)
	pathC := writeArchive(t, map[string][]byte{
		"manifest.json": mfCData,
		"configA.json":  buildConfig(t, []string{"different"}),
	})
	idC, err := NewArchiveResolver(pathC).ImageID(context.Background(), pathC)
	require.NoError(t, err)
	assert.NotEqual(t, idA, idC, "different config bytes must produce different ImageIDs")
}

func TestArchiveResolver_NotFound(t *testing.T) {
	r := NewArchiveResolver(filepath.Join(t.TempDir(), "does-not-exist.tar"))
	_, err := r.Resolve(context.Background(), "ignored")
	require.Error(t, err)
	var notFound *ErrArchiveNotFound
	require.True(t, errors.As(err, &notFound), "expected ErrArchiveNotFound, got %T: %v", err, err)
}

func TestArchiveResolver_InvalidArchive_NotATar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bogus.tar")
	require.NoError(t, os.WriteFile(path, []byte("this is not a tar file"), 0o644))

	r := NewArchiveResolver(path)
	_, err := r.Resolve(context.Background(), path)
	require.Error(t, err)
	var invalid *ErrInvalidArchive
	require.True(t, errors.As(err, &invalid), "expected ErrInvalidArchive, got %T: %v", err, err)
}

func TestArchiveResolver_InvalidArchive_NoManifest(t *testing.T) {
	// Valid tar but missing manifest.json.
	path := writeArchive(t, map[string][]byte{
		"some-other-file.txt": []byte("hello"),
	})
	r := NewArchiveResolver(path)
	_, err := r.Resolve(context.Background(), path)
	require.Error(t, err)
	var invalid *ErrInvalidArchive
	require.True(t, errors.As(err, &invalid))
}

func TestArchiveResolver_Inspect(t *testing.T) {
	manifest := []dockerManifest{{
		Config: "cfg.json",
		Layers: []string{"a/layer.tar", "b/layer.tar"},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	path := writeArchive(t, map[string][]byte{
		"manifest.json": manifestData,
		"cfg.json":      buildConfig(t, []string{"a", "b"}),
		"a/layer.tar":   make([]byte, 1024),
		"b/layer.tar":   make([]byte, 2048),
	})

	r := NewArchiveResolver(path)
	meta, err := r.Inspect(context.Background(), path)
	require.NoError(t, err)
	// Inspect sums all entry header sizes, including manifest.json and config.
	assert.Greater(t, meta.Size, int64(1024+2048))
}

func TestArchiveExtractor_ExtractFromLayer(t *testing.T) {
	manifest := []dockerManifest{{
		Config: "cfg.json",
		Layers: []string{
			"a/layer.tar",
			"b/layer.tar",
		},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	layerA := buildSimpleLayerTar(t, map[string][]byte{"app/main.go": []byte("package a")})
	layerB := buildSimpleLayerTar(t, map[string][]byte{"app/main.go": []byte("package b — overwrites a")})

	path := writeArchive(t, map[string][]byte{
		"manifest.json": manifestData,
		"cfg.json":      buildConfig(t, []string{"layer a", "layer b"}),
		"a/layer.tar":   layerA,
		"b/layer.tar":   layerB,
	})

	r := NewArchiveResolver(path)
	ex := r.NewExtractor()

	// At cursor 0: layer a's content.
	fc, err := ex.ExtractFromLayer(context.Background(), path, "/app/main.go", 0)
	require.NoError(t, err)
	assert.Equal(t, "package a", string(fc.Data))

	// At cursor 1: layer b's content (most recent regular hit wins).
	fc, err = ex.ExtractFromLayer(context.Background(), path, "/app/main.go", 1)
	require.NoError(t, err)
	assert.Equal(t, "package b — overwrites a", string(fc.Data))
}

func TestArchiveExtractor_RawFromLayer(t *testing.T) {
	manifest := []dockerManifest{{
		Config: "cfg.json",
		Layers: []string{"a/layer.tar"},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	binaryData := []byte{0xff, 0x00, 0xde, 0xad, 0xbe, 0xef}
	layerA := buildSimpleLayerTar(t, map[string][]byte{"data.bin": binaryData})

	path := writeArchive(t, map[string][]byte{
		"manifest.json": manifestData,
		"cfg.json":      buildConfig(t, []string{"layer a"}),
		"a/layer.tar":   layerA,
	})

	r := NewArchiveResolver(path)
	ex := r.NewExtractor()
	raw, err := ex.ExtractRawFromLayer(context.Background(), path, "/data.bin", 0)
	require.NoError(t, err)
	assert.Equal(t, binaryData, raw)
}

func TestArchiveExtractor_FileNotFound(t *testing.T) {
	manifest := []dockerManifest{{
		Config: "cfg.json",
		Layers: []string{"a/layer.tar"},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	layerA := buildSimpleLayerTar(t, map[string][]byte{"app/main.go": []byte("x")})
	path := writeArchive(t, map[string][]byte{
		"manifest.json": manifestData,
		"cfg.json":      buildConfig(t, []string{"layer a"}),
		"a/layer.tar":   layerA,
	})

	r := NewArchiveResolver(path)
	ex := r.NewExtractor()
	_, err = ex.ExtractFromLayer(context.Background(), path, "/nonexistent", 0)
	require.Error(t, err)
}

func TestArchiveResolver_ImplementsExtractorSource(t *testing.T) {
	// Compile-time + runtime check that ArchiveResolver satisfies both
	// Resolver and ExtractorSource. If this stops compiling, the TUI's
	// "src, ok := m.resolver.(image.ExtractorSource)" type assertion
	// would silently disable the file viewer.
	var _ Resolver = (*ArchiveResolver)(nil)
	var _ ExtractorSource = (*ArchiveResolver)(nil)
	var _ Extractor = (*ArchiveExtractor)(nil)

	r := NewArchiveResolver("ignored")
	src, ok := any(r).(ExtractorSource)
	require.True(t, ok)
	require.NotNil(t, src.NewExtractor())
}
