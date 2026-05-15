package image

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildTar(t *testing.T, files map[string][]byte) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Size: int64(len(content)),
			Mode: 0644,
		}
		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	return &buf
}

func buildConfig(t *testing.T, commands []string, emptyIndices ...int) []byte {
	t.Helper()
	emptySet := make(map[int]bool)
	for _, idx := range emptyIndices {
		emptySet[idx] = true
	}

	var history []configHistoryEntry
	for i, cmd := range commands {
		history = append(history, configHistoryEntry{
			CreatedBy:  cmd,
			EmptyLayer: emptySet[i],
		})
	}

	data, err := json.Marshal(imageConfig{History: history})
	require.NoError(t, err)
	return data
}

func TestParseLayers_ValidManifest(t *testing.T) {
	manifest := []dockerManifest{{
		Config: "sha256abc123.json",
		Layers: []string{
			"aabbccddee11223344556677/layer.tar",
			"ff00112233445566778899aa/layer.tar",
			"deadbeefcafe000011112222/layer.tar",
		},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	configData := buildConfig(t, []string{
		"/bin/sh -c apt-get update",
		"/bin/sh -c #(nop) COPY file:abc in /app",
		"/bin/sh -c #(nop)  CMD [\"sh\"]",
	})

	tarBuf := buildTar(t, map[string][]byte{
		"manifest.json":                      manifestData,
		"sha256abc123.json":                  configData,
		"aabbccddee11223344556677/layer.tar": make([]byte, 5242880),
		"ff00112233445566778899aa/layer.tar": make([]byte, 131072),
		"deadbeefcafe000011112222/layer.tar": make([]byte, 46080),
	})

	layers, err := parseLayers(tarBuf)
	require.NoError(t, err)
	assert.Len(t, layers, 3)

	assert.Equal(t, 0, layers[0].Index)
	assert.Equal(t, "aabbccddee11", layers[0].ID)
	assert.Equal(t, int64(5242880), layers[0].Size)
	assert.Equal(t, "/bin/sh -c apt-get update", layers[0].Command)

	assert.Equal(t, 1, layers[1].Index)
	assert.Equal(t, "ff0011223344", layers[1].ID)
	assert.Equal(t, int64(131072), layers[1].Size)
	assert.Equal(t, "/bin/sh -c #(nop) COPY file:abc in /app", layers[1].Command)

	assert.Equal(t, 2, layers[2].Index)
	assert.Equal(t, "deadbeefcafe", layers[2].ID)
	assert.Equal(t, int64(46080), layers[2].Size)
	assert.Equal(t, "/bin/sh -c #(nop)  CMD [\"sh\"]", layers[2].Command)
}

func TestParseLayers_SingleLayer(t *testing.T) {
	manifest := []dockerManifest{{
		Config: "config.json",
		Layers: []string{"abcdef123456789/layer.tar"},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	configData := buildConfig(t, []string{"/bin/sh -c echo hello"})

	tarBuf := buildTar(t, map[string][]byte{
		"manifest.json":             manifestData,
		"config.json":               configData,
		"abcdef123456789/layer.tar": make([]byte, 1024),
	})

	layers, err := parseLayers(tarBuf)
	require.NoError(t, err)
	assert.Len(t, layers, 1)
	assert.Equal(t, 0, layers[0].Index)
	assert.Equal(t, "abcdef123456", layers[0].ID)
	assert.Equal(t, int64(1024), layers[0].Size)
	assert.Equal(t, "/bin/sh -c echo hello", layers[0].Command)
}

func TestParseLayers_MissingManifest(t *testing.T) {
	tarBuf := buildTar(t, map[string][]byte{
		"some-other-file.json": []byte("{}"),
	})

	_, err := parseLayers(tarBuf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "manifest.json not found")
}

func TestParseLayers_MalformedManifest(t *testing.T) {
	tarBuf := buildTar(t, map[string][]byte{
		"manifest.json": []byte("not valid json{{{"),
	})

	_, err := parseLayers(tarBuf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot parse manifest")
}

func TestParseLayers_EmptyManifestArray(t *testing.T) {
	data, err := json.Marshal([]dockerManifest{})
	require.NoError(t, err)

	tarBuf := buildTar(t, map[string][]byte{
		"manifest.json": data,
	})

	_, err = parseLayers(tarBuf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty manifest")
}

func TestParseLayers_EmptyLayerSkipped(t *testing.T) {
	manifest := []dockerManifest{{
		Config: "config.json",
		Layers: []string{
			"aaaa00000000000000000000/layer.tar",
			"bbbb00000000000000000000/layer.tar",
		},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	configData := buildConfig(t, []string{
		"/bin/sh -c #(nop)  ENV PATH=/usr/local",
		"/bin/sh -c apt-get install -y curl",
		"/bin/sh -c #(nop)  EXPOSE 80",
		"/bin/sh -c #(nop) COPY dir:abc in /app",
	}, 0, 2)

	tarBuf := buildTar(t, map[string][]byte{
		"manifest.json":                      manifestData,
		"config.json":                        configData,
		"aaaa00000000000000000000/layer.tar": make([]byte, 2048),
		"bbbb00000000000000000000/layer.tar": make([]byte, 4096),
	})

	layers, err := parseLayers(tarBuf)
	require.NoError(t, err)
	assert.Len(t, layers, 2)

	assert.Equal(t, "/bin/sh -c apt-get install -y curl", layers[0].Command)
	assert.Equal(t, int64(2048), layers[0].Size)

	assert.Equal(t, "/bin/sh -c #(nop) COPY dir:abc in /app", layers[1].Command)
	assert.Equal(t, int64(4096), layers[1].Size)
}

func TestParseLayers_HistoryMismatch(t *testing.T) {
	manifest := []dockerManifest{{
		Config: "config.json",
		Layers: []string{
			"aaaa00000000000000000000/layer.tar",
			"bbbb00000000000000000000/layer.tar",
			"cccc00000000000000000000/layer.tar",
		},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	configData := buildConfig(t, []string{
		"/bin/sh -c #(nop)  ENV X=1",
		"/bin/sh -c echo hello",
	}, 0)

	tarBuf := buildTar(t, map[string][]byte{
		"manifest.json":                      manifestData,
		"config.json":                        configData,
		"aaaa00000000000000000000/layer.tar": make([]byte, 100),
		"bbbb00000000000000000000/layer.tar": make([]byte, 200),
		"cccc00000000000000000000/layer.tar": make([]byte, 300),
	})

	layers, err := parseLayers(tarBuf)
	require.NoError(t, err)
	assert.Len(t, layers, 3)

	assert.Equal(t, "/bin/sh -c echo hello", layers[0].Command)
	assert.Equal(t, "", layers[1].Command)
	assert.Equal(t, "", layers[2].Command)

	assert.Equal(t, int64(100), layers[0].Size)
	assert.Equal(t, int64(200), layers[1].Size)
	assert.Equal(t, int64(300), layers[2].Size)
}

func TestParseLayers_MissingConfig(t *testing.T) {
	manifest := []dockerManifest{{
		Config: "nonexistent.json",
		Layers: []string{"aaa000000000000000000000/layer.tar"},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	tarBuf := buildTar(t, map[string][]byte{
		"manifest.json":                     manifestData,
		"aaa000000000000000000000/layer.tar": make([]byte, 100),
	})

	_, err = parseLayers(tarBuf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config")
	assert.Contains(t, err.Error(), "not found")
}

func TestExtractShortID_LongHash(t *testing.T) {
	id := extractShortID("aabbccddee112233445566778899aabb/layer.tar")
	assert.Equal(t, "aabbccddee11", id)
}

func TestExtractShortID_ShortDir(t *testing.T) {
	id := extractShortID("abc/layer.tar")
	assert.Equal(t, "abc", id)
}

func TestExtractShortID_ExactlyTwelve(t *testing.T) {
	id := extractShortID("123456789012/layer.tar")
	assert.Equal(t, "123456789012", id)
}

func TestExtractShortID_OCIFormat(t *testing.T) {
	id := extractShortID("blobs/sha256/aabbccddee112233445566778899aabbccddeeff00112233445566778899aabb")
	assert.Equal(t, "aabbccddee11", id)
}

func TestParseLayers_OCIFormat(t *testing.T) {
	configDigest := "aabbccddee112233445566778899aabbccddeeff00112233445566778899aa00"
	layerDigest1 := "1111111111111111111111111111111111111111111111111111111111111111"
	layerDigest2 := "2222222222222222222222222222222222222222222222222222222222222222"

	manifest := []dockerManifest{{
		Config: "blobs/sha256/" + configDigest,
		Layers: []string{
			"blobs/sha256/" + layerDigest1,
			"blobs/sha256/" + layerDigest2,
		},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	configData := buildConfig(t, []string{
		"/bin/sh -c apt-get update",
		"/bin/sh -c #(nop)  CMD [\"nginx\"]",
	})

	tarBuf := buildTar(t, map[string][]byte{
		"manifest.json":                    manifestData,
		"blobs/sha256/" + configDigest:     configData,
		"blobs/sha256/" + layerDigest1:     make([]byte, 4096),
		"blobs/sha256/" + layerDigest2:     make([]byte, 2048),
	})

	layers, err := parseLayers(tarBuf)
	require.NoError(t, err)
	assert.Len(t, layers, 2)

	assert.Equal(t, 0, layers[0].Index)
	assert.Equal(t, "111111111111", layers[0].ID)
	assert.Equal(t, int64(4096), layers[0].Size)
	assert.Equal(t, "/bin/sh -c apt-get update", layers[0].Command)

	assert.Equal(t, 1, layers[1].Index)
	assert.Equal(t, "222222222222", layers[1].ID)
	assert.Equal(t, int64(2048), layers[1].Size)
	assert.Equal(t, "/bin/sh -c #(nop)  CMD [\"nginx\"]", layers[1].Command)
}

// buildGzipLayerTar creates a gzip-compressed tar containing the given entries,
// simulating how Docker 25+ OCI format stores layer blobs.
func buildGzipLayerTar(t *testing.T, entries []struct {
	Name string
	Size int64
	Type byte
}) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.Name,
			Size:     e.Size,
			Typeflag: e.Type,
		}
		require.NoError(t, tw.WriteHeader(hdr))
		if e.Size > 0 {
			_, err := tw.Write(make([]byte, e.Size))
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func TestParseLayers_OCIGzipCompressedLayers(t *testing.T) {
	configDigest := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	layerDigest := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"

	manifest := []dockerManifest{{
		Config: "blobs/sha256/" + configDigest,
		Layers: []string{
			"blobs/sha256/" + layerDigest,
		},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	configData := buildConfig(t, []string{"/bin/sh -c apt-get update"})

	// Build a gzip-compressed layer tar with known files
	layerData := buildGzipLayerTar(t, []struct {
		Name string
		Size int64
		Type byte
	}{
		{Name: "etc/", Size: 0, Type: tar.TypeDir},
		{Name: "etc/hostname", Size: 12, Type: tar.TypeReg},
		{Name: "usr/", Size: 0, Type: tar.TypeDir},
		{Name: "usr/bin/", Size: 0, Type: tar.TypeDir},
		{Name: "usr/bin/curl", Size: 1024, Type: tar.TypeReg},
	})

	// Verify the layer data actually starts with gzip magic bytes
	require.True(t, len(layerData) >= 2 && layerData[0] == 0x1f && layerData[1] == 0x8b,
		"test setup: layer data must be gzip-compressed")

	tarBuf := buildTar(t, map[string][]byte{
		"manifest.json":                manifestData,
		"blobs/sha256/" + configDigest: configData,
		"blobs/sha256/" + layerDigest:  layerData,
	})

	layers, err := parseLayers(tarBuf)
	require.NoError(t, err)
	require.Len(t, layers, 1)

	// The critical assertion: Tree must be populated from gzip-compressed data
	require.NotNil(t, layers[0].Tree, "Tree must be populated for gzip-compressed OCI layers")

	// Verify the tree contents are correct
	etc := layers[0].Tree.Root.FindChild("etc")
	require.NotNil(t, etc, "etc directory must exist")
	assert.True(t, etc.IsDir)

	hostname := etc.FindChild("hostname")
	require.NotNil(t, hostname, "etc/hostname must exist")
	assert.Equal(t, int64(12), hostname.Size)

	usr := layers[0].Tree.Root.FindChild("usr")
	require.NotNil(t, usr, "usr directory must exist")

	bin := usr.FindChild("bin")
	require.NotNil(t, bin, "usr/bin directory must exist")

	curl := bin.FindChild("curl")
	require.NotNil(t, curl, "usr/bin/curl must exist")
	assert.Equal(t, int64(1024), curl.Size)
}
