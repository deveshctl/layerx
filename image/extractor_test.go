package image

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsBinary_NullBytes(t *testing.T) {
	data := []byte("hello\x00world")
	assert.True(t, IsBinary(data))
}

func TestIsBinary_PureText(t *testing.T) {
	data := []byte("hello world\nthis is text\n")
	assert.False(t, IsBinary(data))
}

func TestIsBinary_EmptySlice(t *testing.T) {
	assert.False(t, IsBinary([]byte{}))
}

func TestIsBinary_UTF8Text(t *testing.T) {
	data := []byte("café résumé naïve")
	assert.False(t, IsBinary(data))
}

func TestIsBinary_ELFHeader(t *testing.T) {
	data := []byte("\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	assert.True(t, IsBinary(data))
}

func TestProcessContent_TextFile(t *testing.T) {
	data := []byte("line1\nline2\nline3\n")
	fc := processContent("/etc/config", data, int64(len(data)))
	assert.Equal(t, "/etc/config", fc.Path)
	assert.Equal(t, data, fc.Data)
	assert.Equal(t, int64(len(data)), fc.Size)
	assert.False(t, fc.Truncated)
	assert.False(t, fc.Binary)
}

func TestProcessContent_Binary(t *testing.T) {
	data := []byte("\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	fc := processContent("/bin/sh", data, int64(len(data)))
	assert.True(t, fc.Binary)
	assert.Empty(t, fc.Data)
}

func TestProcessContent_Truncated(t *testing.T) {
	bigData := make([]byte, MaxViewSize+100)
	for i := range bigData {
		bigData[i] = 'a'
	}
	fc := processContent("/big.log", bigData, int64(len(bigData)))
	assert.True(t, fc.Truncated)
	assert.Equal(t, MaxViewSize, len(fc.Data))
	assert.Equal(t, int64(MaxViewSize+100), fc.Size)
}

func TestProcessContent_Empty(t *testing.T) {
	fc := processContent("/empty", []byte{}, 0)
	assert.False(t, fc.Binary)
	assert.False(t, fc.Truncated)
	assert.Empty(t, fc.Data)
}

// --- readFullFileFromTar -----------------------------------------------------

func TestReadFullFileFromTar_NoSizeLimit(t *testing.T) {
	content := bytes.Repeat([]byte("x"), MaxViewSize+500)
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	err := tw.WriteHeader(&tar.Header{
		Name:     "bigfile.dat",
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	})
	require.NoError(t, err)
	_, err = tw.Write(content)
	require.NoError(t, err)
	require.NoError(t, tw.Close())

	data, err := readFullFileFromTar(&buf)
	require.NoError(t, err)
	assert.Equal(t, len(content), len(data))
}

func TestReadFullFileFromTar_SkipsDirectories(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tw.WriteHeader(&tar.Header{Name: "dir/", Typeflag: tar.TypeDir})
	tw.WriteHeader(&tar.Header{Name: "dir/file.txt", Size: 5, Typeflag: tar.TypeReg})
	tw.Write([]byte("hello"))
	tw.Close()

	data, err := readFullFileFromTar(&buf)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), data)
}

func TestReadFullFileFromTar_EmptyTar(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tw.Close()

	_, err := readFullFileFromTar(&buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no file found")
}

// --- ExtractFromLayer / ExtractRawFromLayer (Bug #3) -----------------------

// buildLayerTarFromMap constructs an inner layer.tar with the given files.
// Returns raw (uncompressed) tar bytes.
func buildLayerTarFromMap(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, content := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:     name,
			Size:     int64(len(content)),
			Mode:     0o644,
			Typeflag: tar.TypeReg,
		}))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	return buf.Bytes()
}

// buildLayerTarGzFromMap wraps buildLayerTarFromMap in gzip compression
// (Docker 25+ OCI format).
func buildLayerTarGzFromMap(t *testing.T, files map[string]string) []byte {
	t.Helper()
	raw := buildLayerTarFromMap(t, files)
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, err := gw.Write(raw)
	require.NoError(t, err)
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

// fakeImageSaveClient implements just enough of client.APIClient for ExtractFromLayer.
// Only ImageSave returns real data; other methods inherit the nil embedded interface
// and will panic if called (none are called by the layer-walk paths).
type fakeImageSaveClient struct {
	client.APIClient
	saveData []byte
	saveErr  error
}

func (f *fakeImageSaveClient) ImageSave(_ context.Context, _ []string, _ ...client.ImageSaveOption) (client.ImageSaveResult, error) {
	if f.saveErr != nil {
		return nil, f.saveErr
	}
	return &nopImageSaveResult{ReadCloser: io.NopCloser(bytes.NewReader(f.saveData))}, nil
}

type nopImageSaveResult struct {
	io.ReadCloser
}

// buildThreeLayerImageTar constructs a Docker-format outer image tar with
// three layers, suitable for handing to a fakeImageSaveClient.
//
//	L0: tmp/a.txt = "v0"
//	L1: etc/other = "x"  (no a.txt change)
//	L2: tmp/a.txt = "v2"
func buildThreeLayerImageTar(t *testing.T) []byte {
	t.Helper()

	manifest := []dockerManifest{{
		Config: "config.json",
		Layers: []string{
			"layer0/layer.tar",
			"layer1/layer.tar",
			"layer2/layer.tar",
		},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	configData := buildConfig(t, []string{"L0", "L1", "L2"})

	outer := buildTar(t, map[string][]byte{
		"manifest.json":    manifestData,
		"config.json":      configData,
		"layer0/layer.tar": buildLayerTarFromMap(t, map[string]string{"tmp/a.txt": "v0"}),
		"layer1/layer.tar": buildLayerTarFromMap(t, map[string]string{"etc/other": "x"}),
		"layer2/layer.tar": buildLayerTarFromMap(t, map[string]string{"tmp/a.txt": "v2"}),
	})
	return outer.Bytes()
}

func TestExtractFromLayer_OriginLayer(t *testing.T) {
	cli := &fakeImageSaveClient{saveData: buildThreeLayerImageTar(t)}
	e := NewDockerExtractor(cli)

	fc, err := e.ExtractFromLayer(context.Background(), "img", "/tmp/a.txt", 0)
	require.NoError(t, err)
	assert.Equal(t, "v0", string(fc.Data))
}

func TestExtractFromLayer_WalkBackUnchanged(t *testing.T) {
	cli := &fakeImageSaveClient{saveData: buildThreeLayerImageTar(t)}
	e := NewDockerExtractor(cli)

	fc, err := e.ExtractFromLayer(context.Background(), "img", "/tmp/a.txt", 1)
	require.NoError(t, err)
	assert.Equal(t, "v0", string(fc.Data), "L1 has no a.txt; walk back should find L0's v0")
}

func TestExtractFromLayer_LatestVersion(t *testing.T) {
	cli := &fakeImageSaveClient{saveData: buildThreeLayerImageTar(t)}
	e := NewDockerExtractor(cli)

	fc, err := e.ExtractFromLayer(context.Background(), "img", "/tmp/a.txt", 2)
	require.NoError(t, err)
	assert.Equal(t, "v2", string(fc.Data))
}

func TestExtractFromLayer_NotFound(t *testing.T) {
	cli := &fakeImageSaveClient{saveData: buildThreeLayerImageTar(t)}
	e := NewDockerExtractor(cli)

	_, err := e.ExtractFromLayer(context.Background(), "img", "/missing", 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in any layer")
}

func TestExtractFromLayer_OutOfRange(t *testing.T) {
	cli := &fakeImageSaveClient{saveData: buildThreeLayerImageTar(t)}
	e := NewDockerExtractor(cli)

	_, err := e.ExtractFromLayer(context.Background(), "img", "/tmp/a.txt", 99)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestExtractFromLayer_WhiteoutStop(t *testing.T) {
	// L3 adds a whiteout for tmp/a.txt — extraction at cursor=3 must fail
	// with a "removed" error, not silently fall back to L0's v0.
	manifest := []dockerManifest{{
		Config: "config.json",
		Layers: []string{
			"layer0/layer.tar",
			"layer1/layer.tar",
			"layer2/layer.tar",
			"layer3/layer.tar",
		},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)
	configData := buildConfig(t, []string{"L0", "L1", "L2", "L3"})

	outer := buildTar(t, map[string][]byte{
		"manifest.json":    manifestData,
		"config.json":      configData,
		"layer0/layer.tar": buildLayerTarFromMap(t, map[string]string{"tmp/a.txt": "v0"}),
		"layer1/layer.tar": buildLayerTarFromMap(t, map[string]string{"etc/other": "x"}),
		"layer2/layer.tar": buildLayerTarFromMap(t, map[string]string{"tmp/a.txt": "v2"}),
		"layer3/layer.tar": buildLayerTarFromMap(t, map[string]string{"tmp/.wh.a.txt": ""}),
	})

	cli := &fakeImageSaveClient{saveData: outer.Bytes()}
	e := NewDockerExtractor(cli)

	_, err = e.ExtractFromLayer(context.Background(), "img", "/tmp/a.txt", 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "removed in layer")
}

func TestExtractFromLayer_GzippedBlob(t *testing.T) {
	// Docker 25+ OCI: layer blobs are gzip-compressed. Extractor must decompress.
	manifest := []dockerManifest{{
		Config: "config.json",
		Layers: []string{"layer0/layer.tar"},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)
	configData := buildConfig(t, []string{"L0"})

	outer := buildTar(t, map[string][]byte{
		"manifest.json":    manifestData,
		"config.json":      configData,
		"layer0/layer.tar": buildLayerTarGzFromMap(t, map[string]string{"tmp/a.txt": "v0"}),
	})

	cli := &fakeImageSaveClient{saveData: outer.Bytes()}
	e := NewDockerExtractor(cli)

	fc, err := e.ExtractFromLayer(context.Background(), "img", "/tmp/a.txt", 0)
	require.NoError(t, err)
	assert.Equal(t, "v0", string(fc.Data))
}

func TestExtractFromLayer_SingleLayerImage(t *testing.T) {
	// Fence-post check: a 1-layer image at cursor=0 must succeed for both
	// "found in this layer" and "not found" without iterating off the end.
	manifest := []dockerManifest{{
		Config: "config.json",
		Layers: []string{"layer0/layer.tar"},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)
	configData := buildConfig(t, []string{"L0"})

	outer := buildTar(t, map[string][]byte{
		"manifest.json":    manifestData,
		"config.json":      configData,
		"layer0/layer.tar": buildLayerTarFromMap(t, map[string]string{"only.txt": "solo"}),
	})
	cli := &fakeImageSaveClient{saveData: outer.Bytes()}
	e := NewDockerExtractor(cli)

	fc, err := e.ExtractFromLayer(context.Background(), "img", "/only.txt", 0)
	require.NoError(t, err)
	assert.Equal(t, "solo", string(fc.Data))

	_, err = e.ExtractFromLayer(context.Background(), "img", "/missing.txt", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in any layer")
}

func TestExtractRawFromLayer_NoTruncation(t *testing.T) {
	// 2MB content > MaxViewSize (1MB) — Raw variant must return all bytes.
	big := strings.Repeat("a", 2*1024*1024)

	manifest := []dockerManifest{{
		Config: "config.json",
		Layers: []string{"layer0/layer.tar"},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)
	configData := buildConfig(t, []string{"L0"})

	outer := buildTar(t, map[string][]byte{
		"manifest.json":    manifestData,
		"config.json":      configData,
		"layer0/layer.tar": buildLayerTarFromMap(t, map[string]string{"big.dat": big}),
	})

	cli := &fakeImageSaveClient{saveData: outer.Bytes()}
	e := NewDockerExtractor(cli)

	data, err := e.ExtractRawFromLayer(context.Background(), "img", "/big.dat", 0)
	require.NoError(t, err)
	assert.Equal(t, len(big), len(data))
}
