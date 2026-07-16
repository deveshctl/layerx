package image

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"testing"

	mobyimage "github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
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

// buildConfigWithPlatform is buildConfig plus an os/arch[/variant] block in
// the JSON payload — needed for tests that exercise --platform compatibility
// against an archive's recorded variant.
func buildConfigWithPlatform(t *testing.T, os, arch, variant string, commands []string) []byte {
	t.Helper()
	var history []configHistoryEntry
	for _, cmd := range commands {
		history = append(history, configHistoryEntry{CreatedBy: cmd})
	}
	data, err := json.Marshal(imageConfig{
		OS:           os,
		Architecture: arch,
		Variant:      variant,
		History:      history,
	})
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

	layers, err := parseLayers(context.Background(), tarBuf)
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

	layers, err := parseLayers(context.Background(), tarBuf)
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

	_, err := parseLayers(context.Background(), tarBuf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "manifest.json not found")
}

// tarWithForgedHeaderSize writes a single entry whose tar header DECLARES
// declaredSize bytes while the body only carries len(body). A hostile archive
// uses this to trick an unbounded io.ReadAll into pre-sizing a huge buffer.
// tw.Close is deliberately skipped: a correct close validates that the full
// declared size was written and would fail the fixture. parseLayers reads via
// its own tar.Reader, which surfaces the declared hdr.Size during Next() —
// that is what readMetadataEntry's fast-fail path checks, before the body is
// consumed — so the fixture stays a few bytes on disk while claiming gigabytes.
func tarWithForgedHeaderSize(t *testing.T, name string, declaredSize int64, body []byte) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Size: declaredSize, Mode: 0644}))
	_, _ = tw.Write(body)
	return &buf
}

func TestParseLayers_ManifestExceedsMetadataCap(t *testing.T) {
	tarBuf := tarWithForgedHeaderSize(t, "manifest.json", MaxMetadataSize+1, []byte("[]"))

	_, err := parseLayers(context.Background(), tarBuf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest.json too large")
}

// readMetadataEntry must also reject a stream that overruns the cap when the
// header understated the size (a lying header in the opposite direction).
func TestReadMetadataEntry_StreamOverrunRejected(t *testing.T) {
	// Header claims 1 byte; body is MaxMetadataSize+1 bytes. The io.LimitReader
	// truncates and the post-read length check flags the overrun.
	oversized := bytes.Repeat([]byte("x"), int(MaxMetadataSize)+1)
	_, err := readMetadataEntry(bytes.NewReader(oversized), 1, "config.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config.json too large")
}

func TestReadMetadataEntry_AcceptsSmallDescriptor(t *testing.T) {
	body := []byte(`{"ok":true}`)
	got, err := readMetadataEntry(bytes.NewReader(body), int64(len(body)), "manifest.json")
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

func TestParseLayers_MalformedManifest(t *testing.T) {
	tarBuf := buildTar(t, map[string][]byte{
		"manifest.json": []byte("not valid json{{{"),
	})

	_, err := parseLayers(context.Background(), tarBuf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot parse manifest")
}

func TestParseLayers_EmptyManifestArray(t *testing.T) {
	data, err := json.Marshal([]dockerManifest{})
	require.NoError(t, err)

	tarBuf := buildTar(t, map[string][]byte{
		"manifest.json": data,
	})

	_, err = parseLayers(context.Background(), tarBuf)
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

	layers, err := parseLayers(context.Background(), tarBuf)
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

	// Inner layer.tar contents must be at least 1024 bytes of zeros so
	// archive/tar reads two end-of-archive blocks instead of EOFing mid-header.
	// The Size assertion below comes from the OUTER tar header (set by
	// buildTar), so the inner length is irrelevant to what parseLayers reports.
	tarBuf := buildTar(t, map[string][]byte{
		"manifest.json":                      manifestData,
		"config.json":                        configData,
		"aaaa00000000000000000000/layer.tar": make([]byte, 1024),
		"bbbb00000000000000000000/layer.tar": make([]byte, 1024),
		"cccc00000000000000000000/layer.tar": make([]byte, 1024),
	})

	layers, err := parseLayers(context.Background(), tarBuf)
	require.NoError(t, err)
	assert.Len(t, layers, 3)

	assert.Equal(t, "/bin/sh -c echo hello", layers[0].Command)
	assert.Equal(t, "", layers[1].Command)
	assert.Equal(t, "", layers[2].Command)

	assert.Equal(t, int64(1024), layers[0].Size)
	assert.Equal(t, int64(1024), layers[1].Size)
	assert.Equal(t, int64(1024), layers[2].Size)
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

	_, err = parseLayers(context.Background(), tarBuf)
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

	layers, err := parseLayers(context.Background(), tarBuf)
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

	layers, err := parseLayers(context.Background(), tarBuf)
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

// --- parseLayers error propagation (bug-scan 2026-05-25 #2) ----------------

// TestParseLayers_CorruptLayerSurfacesError ensures a layer blob whose tar is
// truncated mid-stream causes parseLayers to fail loudly rather than silently
// installing a nil Tree (which the TUI would render as an empty layer).
func TestParseLayers_CorruptLayerSurfacesError(t *testing.T) {
	// Build a valid tar header for one entry, then truncate the body so
	// tar.Reader.Next() errors after the first entry.
	var inner bytes.Buffer
	tw := tar.NewWriter(&inner)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "etc/hostname",
		Size:     1024, // claim 1024 bytes
		Typeflag: tar.TypeReg,
	}))
	_, err := tw.Write([]byte("only-a-few-bytes")) // far less than 1024
	require.NoError(t, err)
	// Deliberately do NOT call tw.Close() — leave the stream truncated.
	corrupt := inner.Bytes()

	manifest := []dockerManifest{{
		Config: "config.json",
		Layers: []string{"layer0/layer.tar"},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)
	configData := buildConfig(t, []string{"L0"})

	tarBuf := buildTar(t, map[string][]byte{
		"manifest.json":    manifestData,
		"config.json":      configData,
		"layer0/layer.tar": corrupt,
	})

	_, err = parseLayers(context.Background(), tarBuf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "layer0/layer.tar")
}

// TestParseLayers_BadGzipHeaderSurfacesError covers the sibling decompression
// path: a blob that begins with the gzip magic but is otherwise garbage must
// fail loudly rather than silently swallowing the decompression error.
func TestParseLayers_BadGzipHeaderSurfacesError(t *testing.T) {
	// Two-byte gzip magic followed by garbage that isn't a valid gzip stream.
	badGzip := append([]byte{0x1f, 0x8b}, []byte("not actually gzip")...)

	manifest := []dockerManifest{{
		Config: "config.json",
		Layers: []string{"layer0/layer.tar"},
	}}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)
	configData := buildConfig(t, []string{"L0"})

	tarBuf := buildTar(t, map[string][]byte{
		"manifest.json":    manifestData,
		"config.json":      configData,
		"layer0/layer.tar": badGzip,
	})

	_, err = parseLayers(context.Background(), tarBuf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "layer0/layer.tar")
}

func TestIsImageNotFoundMessage(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"docker hub manifest unknown", "manifest unknown", true},
		{"manifest for ref not found", "manifest for nginx:bogus not found", true},
		{"repository does not exist", "repository does not exist or may require 'docker login'", true},
		{"pull access denied", "pull access denied for private/foo", true},
		{"mixed case", "Manifest Unknown", true},
		{"credential-helper not found is not image-not-found",
			`credential store "osxkeychain" not found`, false},
		{"network error", "dial tcp: lookup registry: no such host", false},
		{"5xx", "received unexpected HTTP status: 500 Internal Server Error", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isImageNotFoundMessage(tc.in))
		})
	}
}

func TestIsDaemonUnreachable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"connection refused", errors.New("Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?"), true},
		{"named pipe", errors.New("error during connect: this error may indicate that the docker daemon is not running"), true},
		{"timeout", errors.New("context deadline exceeded"), false},
		{"random api error", errors.New("400 Bad Request"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isDaemonUnreachable(tc.err))
		})
	}
}

// fakeAPIClient embeds client.APIClient so unspecified methods compile but
// panic at runtime; only ImageList, ImagePull, and ImageInspect are wired.
type fakeAPIClient struct {
	client.APIClient
	imageList    func(ctx context.Context, options client.ImageListOptions) (client.ImageListResult, error)
	imagePull    func(ctx context.Context, ref string, options client.ImagePullOptions) (client.ImagePullResponse, error)
	imageInspect func(ctx context.Context, ref string) (client.ImageInspectResult, error)
}

func (f *fakeAPIClient) ImageList(ctx context.Context, options client.ImageListOptions) (client.ImageListResult, error) {
	return f.imageList(ctx, options)
}

func (f *fakeAPIClient) ImagePull(ctx context.Context, ref string, options client.ImagePullOptions) (client.ImagePullResponse, error) {
	return f.imagePull(ctx, ref, options)
}

func (f *fakeAPIClient) ImageInspect(ctx context.Context, ref string, _ ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	return f.imageInspect(ctx, ref)
}

func TestEnsureImage_PullNotFound_ReturnsErrImageNotFound(t *testing.T) {
	fake := &fakeAPIClient{
		imageList: func(_ context.Context, _ client.ImageListOptions) (client.ImageListResult, error) {
			return client.ImageListResult{}, nil
		},
		imagePull: func(_ context.Context, _ string, _ client.ImagePullOptions) (client.ImagePullResponse, error) {
			return nil, errors.New("Error response from daemon: manifest for nginx:bogus not found")
		},
	}
	r, err := NewDockerResolver(WithClient(fake))
	require.NoError(t, err)
	dr := r.(*DockerResolver)

	err = dr.ensureImageWithProgress(context.Background(), "nginx:bogus", nil)
	var notFound *ErrImageNotFound
	require.ErrorAs(t, err, &notFound)
	assert.Equal(t, "nginx:bogus", notFound.Ref)
}

func TestEnsureImage_PullGeneric_ReturnsErrPullFailed(t *testing.T) {
	fake := &fakeAPIClient{
		imageList: func(_ context.Context, _ client.ImageListOptions) (client.ImageListResult, error) {
			return client.ImageListResult{}, nil
		},
		imagePull: func(_ context.Context, _ string, _ client.ImagePullOptions) (client.ImagePullResponse, error) {
			return nil, errors.New("dial tcp: i/o timeout")
		},
	}
	r, err := NewDockerResolver(WithClient(fake))
	require.NoError(t, err)
	dr := r.(*DockerResolver)

	err = dr.ensureImageWithProgress(context.Background(), "registry.example/foo:latest", nil)
	var pullFailed *ErrPullFailed
	require.ErrorAs(t, err, &pullFailed)
	assert.Equal(t, "registry.example/foo:latest", pullFailed.Ref)
}

func TestImageID_DaemonDown_ReturnsErrDaemonNotRunning(t *testing.T) {
	fake := &fakeAPIClient{
		imageInspect: func(_ context.Context, _ string) (client.ImageInspectResult, error) {
			return client.ImageInspectResult{}, errors.New("Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?")
		},
	}
	r, err := NewDockerResolver(WithClient(fake))
	require.NoError(t, err)
	dr := r.(*DockerResolver)

	_, err = dr.ImageID(context.Background(), "anything:latest")
	var daemonDown *ErrDaemonNotRunning
	require.ErrorAs(t, err, &daemonDown)
}

func TestImageID_NotFound_ReturnsErrImageNotFound(t *testing.T) {
	fake := &fakeAPIClient{
		imageInspect: func(_ context.Context, _ string) (client.ImageInspectResult, error) {
			return client.ImageInspectResult{}, errors.New("Error response from daemon: No such image: ghost:latest")
		},
	}
	r, err := NewDockerResolver(WithClient(fake))
	require.NoError(t, err)
	dr := r.(*DockerResolver)

	_, err = dr.ImageID(context.Background(), "ghost:latest")
	var notFound *ErrImageNotFound
	require.ErrorAs(t, err, &notFound)
	assert.Equal(t, "ghost:latest", notFound.Ref)
}

func TestParseLayers_HonoursContextCancel(t *testing.T) {
	// Build a minimal 2-layer archive so the Pass-2 loop iterates at least
	// once before the ctx check fires. The bodies don't matter — we cancel
	// before the second iteration.
	manifest := []dockerManifest{{
		Config: "sha256abc123.json",
		Layers: []string{
			"aabbccddee11223344556677/layer.tar",
			"ff00112233445566778899aa/layer.tar",
		},
	}}
	manifestBytes, err := json.Marshal(manifest)
	require.NoError(t, err)

	configBytes := buildConfig(t, []string{"RUN a", "RUN b"})

	// Two empty layer tarballs; bytes are arbitrary, just need valid tar.
	emptyLayer := buildTar(t, map[string][]byte{}).Bytes()

	archive := buildTar(t, map[string][]byte{
		"manifest.json":                      manifestBytes,
		"sha256abc123.json":                  configBytes,
		"aabbccddee11223344556677/layer.tar": emptyLayer,
		"ff00112233445566778899aa/layer.tar": emptyLayer,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before we even call

	_, err = parseLayers(ctx, archive)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

func TestIsImageDigestRef(t *testing.T) {
	cases := []struct {
		name string
		ref  string
		want bool
	}{
		{"valid lowercase", "sha256:b217d8cee53a42fb1d63bd64968e49dff488a3f0ee2047fd440e28b68ccd1678", true},
		{"valid uppercase hex", "sha256:B217D8CEE53A42FB1D63BD64968E49DFF488A3F0EE2047FD440E28B68CCD1678", true},
		{"too short", "sha256:b217d8c", false},
		{"too long", "sha256:b217d8cee53a42fb1d63bd64968e49dff488a3f0ee2047fd440e28b68ccd16780", false},
		{"non-hex", "sha256:zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", false},
		{"missing prefix", "b217d8cee53a42fb1d63bd64968e49dff488a3f0ee2047fd440e28b68ccd1678", false},
		{"plain tag", "nginx:latest", false},
		{"empty", "", false},
		{"prefix only", "sha256:", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isImageDigestRef(tc.ref); got != tc.want {
				t.Fatalf("isImageDigestRef(%q) = %v, want %v", tc.ref, got, tc.want)
			}
		})
	}
}

func TestEnsureImage_WithPlatform_SkipsLocalCacheShortCircuit(t *testing.T) {
	// With --platform pinned, ensureImageWithProgress must NOT trust the
	// local-image-list short-circuit: a previous run may have populated the
	// daemon with a different variant under the same tag. The pull goes
	// through every time so the daemon resolves the right manifest.
	plat, err := ParsePlatform("linux/arm64")
	require.NoError(t, err)

	imageListCalls := 0
	imagePullCalls := 0
	var lastPullPlatforms []ocispec.Platform
	fake := &fakeAPIClient{
		imageList: func(_ context.Context, _ client.ImageListOptions) (client.ImageListResult, error) {
			imageListCalls++
			return client.ImageListResult{}, nil
		},
		imagePull: func(_ context.Context, _ string, opts client.ImagePullOptions) (client.ImagePullResponse, error) {
			imagePullCalls++
			lastPullPlatforms = opts.Platforms
			return nil, errors.New("dial tcp: i/o timeout") // short-circuit; we only care opts arrived
		},
	}
	r, err := NewDockerResolver(WithClient(fake), WithPlatform(plat))
	require.NoError(t, err)
	dr := r.(*DockerResolver)

	err = dr.ensureImageWithProgress(context.Background(), "nginx:latest", nil)
	require.Error(t, err)
	assert.Equal(t, 0, imageListCalls, "ImageList must NOT be consulted when --platform is pinned")
	assert.Equal(t, 1, imagePullCalls)
	require.Len(t, lastPullPlatforms, 1)
	assert.Equal(t, "linux", lastPullPlatforms[0].OS)
	assert.Equal(t, "arm64", lastPullPlatforms[0].Architecture)
}

func TestEnsureImage_WithoutPlatform_ConsultsImageList(t *testing.T) {
	// Backward-compat sanity: without --platform, the existing
	// "is the image already local?" short-circuit still runs and
	// suppresses a redundant pull.
	imageListCalls := 0
	imagePullCalls := 0
	fake := &fakeAPIClient{
		imageList: func(_ context.Context, _ client.ImageListOptions) (client.ImageListResult, error) {
			imageListCalls++
			return client.ImageListResult{Items: []mobyimage.Summary{{ID: "sha256:abc"}}}, nil
		},
		imagePull: func(_ context.Context, _ string, _ client.ImagePullOptions) (client.ImagePullResponse, error) {
			imagePullCalls++
			return nil, nil
		},
	}
	r, err := NewDockerResolver(WithClient(fake))
	require.NoError(t, err)
	dr := r.(*DockerResolver)

	err = dr.ensureImageWithProgress(context.Background(), "nginx:latest", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, imageListCalls)
	assert.Equal(t, 0, imagePullCalls, "image was already local; no pull should run")
}

func TestEnumeratePlatforms_ManifestsListAuthoritative(t *testing.T) {
	// When the daemon's containerd image store populates the Manifests
	// array, enumeratePlatforms returns it verbatim.
	fake := &fakeAPIClient{
		imageInspect: func(_ context.Context, _ string) (client.ImageInspectResult, error) {
			result := client.ImageInspectResult{}
			result.Manifests = []mobyimage.ManifestSummary{
				{ImageData: &mobyimage.ImageProperties{Platform: ocispec.Platform{OS: "linux", Architecture: "amd64"}}},
				{ImageData: &mobyimage.ImageProperties{Platform: ocispec.Platform{OS: "linux", Architecture: "arm64"}}},
				{ImageData: &mobyimage.ImageProperties{Platform: ocispec.Platform{OS: "linux", Architecture: "arm", Variant: "v7"}}},
			}
			return result, nil
		},
	}
	r, err := NewDockerResolver(WithClient(fake))
	require.NoError(t, err)
	dr := r.(*DockerResolver)

	got := dr.enumeratePlatforms(context.Background(), "nginx:latest")
	assert.Equal(t, []string{"linux/amd64", "linux/arm64", "linux/arm/v7"}, got)
}

func TestEnumeratePlatforms_LegacyStoreReturnsNil(t *testing.T) {
	// On the legacy daemon image store, ImageInspect returns an empty
	// Manifests array. enumeratePlatforms returns nil — we don't synthesise
	// a single-element list from the locally-cached variant, because that
	// would imply layerx knows what platforms the image carries when it
	// only knows what was already pulled.
	fake := &fakeAPIClient{
		imageInspect: func(_ context.Context, _ string) (client.ImageInspectResult, error) {
			result := client.ImageInspectResult{}
			result.Manifests = nil
			result.Os = "linux"
			result.Architecture = "amd64"
			return result, nil
		},
	}
	r, err := NewDockerResolver(WithClient(fake))
	require.NoError(t, err)
	dr := r.(*DockerResolver)

	got := dr.enumeratePlatforms(context.Background(), "nginx:latest")
	assert.Nil(t, got)
}

func TestEnumeratePlatforms_InspectFailsReturnsNil(t *testing.T) {
	// enumeratePlatforms is best-effort context for an error path; if the
	// inspect itself fails, return nil and let the caller emit the
	// terse "platform X not found" message without a list.
	fake := &fakeAPIClient{
		imageInspect: func(_ context.Context, _ string) (client.ImageInspectResult, error) {
			return client.ImageInspectResult{}, errors.New("daemon hung up")
		},
	}
	r, err := NewDockerResolver(WithClient(fake))
	require.NoError(t, err)
	dr := r.(*DockerResolver)

	got := dr.enumeratePlatforms(context.Background(), "nginx:latest")
	assert.Nil(t, got)
}

func TestClassifyPlatformMissing_LegacyStoreLeavesAvailableEmpty(t *testing.T) {
	// On the legacy daemon image store the manifest list is unavailable,
	// so the returned ErrPlatformNotInImage has an empty Available slice.
	// The error renders as the terse "platform X not found" — no guess at
	// what the image carries. Listing the locally-cached variant would
	// imply more knowledge than layerx actually has.
	plat, err := ParsePlatform("linux/arm64dd")
	require.NoError(t, err)

	fake := &fakeAPIClient{
		imageInspect: func(_ context.Context, _ string) (client.ImageInspectResult, error) {
			result := client.ImageInspectResult{}
			result.Os = "linux"
			result.Architecture = "amd64"
			return result, nil
		},
	}
	r, err := NewDockerResolver(WithClient(fake), WithPlatform(plat))
	require.NoError(t, err)
	dr := r.(*DockerResolver)

	err = dr.classifyPlatformMissing(context.Background(), "nginx:latest", errors.New("no matching manifest"))
	var pe *ErrPlatformNotInImage
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, "linux/arm64dd", pe.Requested)
	assert.Empty(t, pe.Available)
}

func TestClassifyPlatformMissing_ManifestListPopulatesAvailable(t *testing.T) {
	// When the daemon (containerd image store) returns a populated
	// manifest list, ErrPlatformNotInImage carries the full set so the
	// rendered error can show the user what the image actually does carry.
	plat, err := ParsePlatform("linux/ppc64le")
	require.NoError(t, err)

	fake := &fakeAPIClient{
		imageInspect: func(_ context.Context, _ string) (client.ImageInspectResult, error) {
			result := client.ImageInspectResult{}
			result.Manifests = []mobyimage.ManifestSummary{
				{ImageData: &mobyimage.ImageProperties{Platform: ocispec.Platform{OS: "linux", Architecture: "amd64"}}},
				{ImageData: &mobyimage.ImageProperties{Platform: ocispec.Platform{OS: "linux", Architecture: "arm64"}}},
			}
			return result, nil
		},
	}
	r, err := NewDockerResolver(WithClient(fake), WithPlatform(plat))
	require.NoError(t, err)
	dr := r.(*DockerResolver)

	err = dr.classifyPlatformMissing(context.Background(), "nginx:latest", errors.New("no matching manifest"))
	var pe *ErrPlatformNotInImage
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, []string{"linux/amd64", "linux/arm64"}, pe.Available)
}

// TestEnsureImage_PlatformMissing_ContainerdStore_NotMisclassifiedAsImageNotFound
// regression-locks the bug Gate C surfaced: with the containerd image store
// enabled, a bad --platform produced "image not found" instead of
// "platform not found". Root cause was the daemon's "no matching manifest
// for X in the manifest list entries" message also matching the broader
// "manifest for " substring used by isImageNotFoundMessage. The platform
// classifier must run first.
//
// The fake here mirrors the cold-pull scenario most clearly: the image is
// not locally cached, so the platform-less inspect that classifyPullNotFound
// would consult also fails — meaning the PRE-FIX code path could not have
// recovered into ErrPlatformNotInImage even via classifyPullNotFound, and
// would have surfaced ErrImageNotFound. The post-fix path bypasses that
// branch entirely because isPlatformPullFailure now wins on this message.
func TestEnsureImage_PlatformMissing_ContainerdStore_NotMisclassifiedAsImageNotFound(t *testing.T) {
	plat, err := ParsePlatform("linux/arm64dd")
	require.NoError(t, err)

	inspectCalls := 0
	fake := &fakeAPIClient{
		imageList: func(_ context.Context, _ client.ImageListOptions) (client.ImageListResult, error) {
			return client.ImageListResult{}, nil
		},
		imagePull: func(_ context.Context, _ string, _ client.ImagePullOptions) (client.ImagePullResponse, error) {
			// Verbatim message moby's containerd image store backend emits
			// for a platform miss (see daemon/containerd/image_pull.go).
			return nil, errors.New("no matching manifest for linux/arm64dd in the manifest list entries: not found")
		},
		imageInspect: func(_ context.Context, _ string) (client.ImageInspectResult, error) {
			inspectCalls++
			// First inspect call is enumeratePlatforms reading the manifest
			// list. With containerd image store, that succeeds and returns
			// the full set of platforms the image carries.
			result := client.ImageInspectResult{}
			result.Manifests = []mobyimage.ManifestSummary{
				{ImageData: &mobyimage.ImageProperties{Platform: ocispec.Platform{OS: "linux", Architecture: "amd64"}}},
				{ImageData: &mobyimage.ImageProperties{Platform: ocispec.Platform{OS: "linux", Architecture: "arm64"}}},
			}
			return result, nil
		},
	}
	r, err := NewDockerResolver(WithClient(fake), WithPlatform(plat))
	require.NoError(t, err)
	dr := r.(*DockerResolver)

	err = dr.ensureImageWithProgress(context.Background(), "nginx:latest", nil)
	var pe *ErrPlatformNotInImage
	require.ErrorAs(t, err, &pe, "platform-miss must surface as ErrPlatformNotInImage, not ErrImageNotFound")
	assert.Equal(t, "linux/arm64dd", pe.Requested)
	assert.Equal(t, []string{"linux/amd64", "linux/arm64"}, pe.Available)
	// The fix routes straight to classifyPlatformMissing → enumeratePlatforms,
	// which is one inspect call. The pre-fix code path would have first run
	// classifyPullNotFound (one inspect to probe existence) and only THEN
	// classifyPlatformMissing (a second inspect for the manifest list) — so
	// pinning to exactly one inspect locks the win in.
	assert.Equal(t, 1, inspectCalls, "platform classifier should reach enumeratePlatforms directly, no probe inspect needed")
}
