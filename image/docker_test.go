package image

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildTar creates an in-memory tar archive with the given file entries.
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

func TestParseLayers_ValidManifest(t *testing.T) {
	manifest := []dockerManifest{{
		Config: "sha256:abc123.json",
		Layers: []string{
			"aabbccddee11223344556677/layer.tar",
			"ff00112233445566778899aa/layer.tar",
			"deadbeefcafe000011112222/layer.tar",
		},
	}}
	data, err := json.Marshal(manifest)
	require.NoError(t, err)

	tarBuf := buildTar(t, map[string][]byte{
		"manifest.json": data,
	})

	layers, err := parseLayers(tarBuf)
	require.NoError(t, err)
	assert.Len(t, layers, 3)

	assert.Equal(t, 0, layers[0].Index)
	assert.Equal(t, "aabbccddee11", layers[0].ID)

	assert.Equal(t, 1, layers[1].Index)
	assert.Equal(t, "ff0011223344", layers[1].ID)

	assert.Equal(t, 2, layers[2].Index)
	assert.Equal(t, "deadbeefcafe", layers[2].ID)
}

func TestParseLayers_SingleLayer(t *testing.T) {
	manifest := []dockerManifest{{
		Config: "sha256:abc.json",
		Layers: []string{"abcdef123456789/layer.tar"},
	}}
	data, err := json.Marshal(manifest)
	require.NoError(t, err)

	tarBuf := buildTar(t, map[string][]byte{
		"manifest.json": data,
	})

	layers, err := parseLayers(tarBuf)
	require.NoError(t, err)
	assert.Len(t, layers, 1)
	assert.Equal(t, 0, layers[0].Index)
	assert.Equal(t, "abcdef123456", layers[0].ID)
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
