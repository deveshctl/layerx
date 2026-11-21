package image

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenLayerBlobFromSpoolStreamsBody(t *testing.T) {
	// A valid header with a missing body proves opening never buffers a blob.
	var header bytes.Buffer
	tw := tar.NewWriter(&header)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "layer.tar", Size: 1 << 30, Mode: 0o644}))
	f, err := os.CreateTemp(t.TempDir(), "spool-*.tar")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	_, err = f.Write(header.Bytes())
	require.NoError(t, err)
	r, err := openLayerBlobFromSpool(t.Context(), f, "layer.tar")
	require.NoError(t, err)
	_, err = io.ReadAll(io.LimitReader(r, 1))
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestFindFileInLayerStreamSkipsLargeEntries(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		t.Run(fmt.Sprintf("gzip=%t", compressed), func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "layer-*.tar")
			require.NoError(t, err)
			t.Cleanup(func() { _ = f.Close() })
			var writer io.Writer = f
			if compressed {
				gz := gzip.NewWriter(f)
				writer = gz
			}
			tw := tar.NewWriter(writer)
			require.NoError(t, tw.WriteHeader(&tar.Header{Name: "large", Size: 16 << 20, Mode: 0o644}))
			_, err = io.CopyN(tw, infiniteReader{}, 16<<20)
			require.NoError(t, err)
			require.NoError(t, tw.WriteHeader(&tar.Header{Name: "target", Size: 5, Mode: 0o644}))
			_, err = tw.Write([]byte("small"))
			require.NoError(t, err)
			require.NoError(t, tw.Close())
			if compressed {
				require.NoError(t, writer.(*gzip.Writer).Close())
			}
			_, err = f.Seek(0, io.SeekStart)
			require.NoError(t, err)
			data, found, err := findFileInLayerStream(t.Context(), f, "target", 5)
			require.NoError(t, err)
			assert.True(t, found)
			assert.Equal(t, "small", string(data))
			_, err = f.Seek(0, io.SeekStart)
			require.NoError(t, err)
			_, _, err = findFileInLayerStream(t.Context(), f, "target", 4)
			assert.ErrorContains(t, err, "limit 4")
		})
	}
}

func TestFindFileInLayerStreamRejectsIncompleteBlob(t *testing.T) {
	layer := buildSimpleLayerTar(t, map[string][]byte{"target": []byte("small")})
	var outer bytes.Buffer
	tw := tar.NewWriter(&outer)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "layer.tar", Size: int64(len(layer) + 1024), Mode: 0o644}))
	_, err := tw.Write(layer)
	require.NoError(t, err)
	f, err := os.CreateTemp(t.TempDir(), "spool-*.tar")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	_, err = f.Write(outer.Bytes())
	require.NoError(t, err)
	r, err := openLayerBlobFromSpool(t.Context(), f, "layer.tar")
	require.NoError(t, err)
	_, _, err = findFileInLayerStream(t.Context(), r, "target", 5)
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)

	compressed := gzipBytes(t, layer)
	compressed[len(compressed)-8] ^= 1 // Corrupt the gzip checksum after tar EOF.
	_, _, err = findFileInLayerStream(t.Context(), bytes.NewReader(compressed), "target", 5)
	assert.ErrorIs(t, err, gzip.ErrChecksum)
}

func TestFindFileInLayerBounded(t *testing.T) {
	layer := buildSimpleLayerTar(t, map[string][]byte{"file": bytes.Repeat([]byte("a"), 1025)})
	_, _, err := findFileInLayerBounded(t.Context(), layer, "file", 1024)
	assert.ErrorContains(t, err, "limit 1024")
	data, found, err := findFileInLayerBounded(t.Context(), layer, "file", 1025)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Len(t, data, 1025)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err = findFileInLayerBounded(ctx, layer, "file", 1025)
	assert.ErrorIs(t, err, context.Canceled)
}
