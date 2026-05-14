package image

import (
	"archive/tar"
	"bytes"
	"testing"

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
