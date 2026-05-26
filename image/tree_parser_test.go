package image

import (
	"archive/tar"
	"bytes"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type tarEntry struct {
	Name string
	Size int64
	Type byte
}

func buildLayerTar(t *testing.T, entries []tarEntry) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	tw := tar.NewWriter(buf)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.Name,
			Size:     e.Size,
			Typeflag: e.Type,
		}
		err := tw.WriteHeader(hdr)
		require.NoError(t, err)
		if e.Size > 0 {
			_, err = tw.Write(make([]byte, e.Size))
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	return buf
}

func TestParseLayerTar_EmptyTar(t *testing.T) {
	buf := buildLayerTar(t, nil)
	tree, err := ParseLayerTar(buf)
	require.NoError(t, err)
	require.NotNil(t, tree)
	assert.Nil(t, tree.Root.Children)
}

func TestParseLayerTar_SimpleFiles(t *testing.T) {
	entries := []tarEntry{
		{Name: "etc/", Type: tar.TypeDir},
		{Name: "etc/passwd", Size: 100, Type: tar.TypeReg},
		{Name: "etc/group", Size: 200, Type: tar.TypeReg},
	}
	buf := buildLayerTar(t, entries)
	tree, err := ParseLayerTar(buf)
	require.NoError(t, err)

	etc := tree.Root.FindChild("etc")
	require.NotNil(t, etc, "etc directory must exist")
	assert.True(t, etc.IsDir)
	assert.Equal(t, "/etc", etc.Path)

	passwd := etc.FindChild("passwd")
	require.NotNil(t, passwd)
	assert.False(t, passwd.IsDir)
	assert.Equal(t, int64(100), passwd.Size)
	assert.Equal(t, "/etc/passwd", passwd.Path)

	group := etc.FindChild("group")
	require.NotNil(t, group)
	assert.Equal(t, int64(200), group.Size)
	assert.Equal(t, "/etc/group", group.Path)
}

func TestParseLayerTar_NestedDirectories(t *testing.T) {
	entries := []tarEntry{
		{Name: "usr/", Type: tar.TypeDir},
		{Name: "usr/local/", Type: tar.TypeDir},
		{Name: "usr/local/bin/", Type: tar.TypeDir},
		{Name: "usr/local/bin/app", Size: 512, Type: tar.TypeReg},
	}
	buf := buildLayerTar(t, entries)
	tree, err := ParseLayerTar(buf)
	require.NoError(t, err)

	usr := tree.Root.FindChild("usr")
	require.NotNil(t, usr)
	assert.True(t, usr.IsDir)
	assert.Equal(t, "/usr", usr.Path)

	local := usr.FindChild("local")
	require.NotNil(t, local)
	assert.True(t, local.IsDir)
	assert.Equal(t, "/usr/local", local.Path)

	bin := local.FindChild("bin")
	require.NotNil(t, bin)
	assert.True(t, bin.IsDir)
	assert.Equal(t, "/usr/local/bin", bin.Path)

	app := bin.FindChild("app")
	require.NotNil(t, app)
	assert.False(t, app.IsDir)
	assert.Equal(t, int64(512), app.Size)
	assert.Equal(t, "/usr/local/bin/app", app.Path)
}

func TestParseLayerTar_ImplicitParentDirectories(t *testing.T) {
	// No directory entries — only the file; parents must be created implicitly.
	entries := []tarEntry{
		{Name: "usr/local/bin/app", Size: 256, Type: tar.TypeReg},
	}
	buf := buildLayerTar(t, entries)
	tree, err := ParseLayerTar(buf)
	require.NoError(t, err)

	usr := tree.Root.FindChild("usr")
	require.NotNil(t, usr, "implicit usr must be created")
	assert.True(t, usr.IsDir)

	local := usr.FindChild("local")
	require.NotNil(t, local, "implicit local must be created")
	assert.True(t, local.IsDir)

	bin := local.FindChild("bin")
	require.NotNil(t, bin, "implicit bin must be created")
	assert.True(t, bin.IsDir)

	app := bin.FindChild("app")
	require.NotNil(t, app)
	assert.Equal(t, int64(256), app.Size)
	assert.Equal(t, "/usr/local/bin/app", app.Path)
}

func TestParseLayerTar_WhiteoutFilesPreserved(t *testing.T) {
	entries := []tarEntry{
		{Name: "etc/", Type: tar.TypeDir},
		{Name: "etc/.wh.resolv.conf", Size: 0, Type: tar.TypeReg},
		{Name: "var/", Type: tar.TypeDir},
		{Name: "var/.wh..wh..opq", Size: 0, Type: tar.TypeReg},
	}
	buf := buildLayerTar(t, entries)
	tree, err := ParseLayerTar(buf)
	require.NoError(t, err)

	etc := tree.Root.FindChild("etc")
	require.NotNil(t, etc)
	wh := etc.FindChild(".wh.resolv.conf")
	require.NotNil(t, wh, "whiteout file must be stored as-is")
	assert.False(t, wh.IsDir)
	assert.Equal(t, "/etc/.wh.resolv.conf", wh.Path)

	varDir := tree.Root.FindChild("var")
	require.NotNil(t, varDir)
	opq := varDir.FindChild(".wh..wh..opq")
	require.NotNil(t, opq, "opaque whiteout must be stored as-is")
	assert.False(t, opq.IsDir)
	assert.Equal(t, "/var/.wh..wh..opq", opq.Path)
}

func TestParseLayerTar_SymlinksAreFiles(t *testing.T) {
	entries := []tarEntry{
		{Name: "lib/", Type: tar.TypeDir},
		{Name: "lib/libfoo.so", Size: 0, Type: tar.TypeSymlink},
	}
	buf := buildLayerTar(t, entries)
	tree, err := ParseLayerTar(buf)
	require.NoError(t, err)

	lib := tree.Root.FindChild("lib")
	require.NotNil(t, lib)
	sym := lib.FindChild("libfoo.so")
	require.NotNil(t, sym)
	assert.False(t, sym.IsDir)
	assert.Equal(t, int64(0), sym.Size)
	assert.Equal(t, "/lib/libfoo.so", sym.Path)
}

func TestParseLayerTar_LeadingSlashNormalized(t *testing.T) {
	entries := []tarEntry{
		{Name: "/etc/hostname", Size: 10, Type: tar.TypeReg},
	}
	buf := buildLayerTar(t, entries)
	tree, err := ParseLayerTar(buf)
	require.NoError(t, err)

	etc := tree.Root.FindChild("etc")
	require.NotNil(t, etc)
	host := etc.FindChild("hostname")
	require.NotNil(t, host)
	assert.Equal(t, "/etc/hostname", host.Path)
}

func TestParseLayerTar_DotSlashPrefixNormalized(t *testing.T) {
	entries := []tarEntry{
		{Name: "./etc/hostname", Size: 10, Type: tar.TypeReg},
	}
	buf := buildLayerTar(t, entries)
	tree, err := ParseLayerTar(buf)
	require.NoError(t, err)

	etc := tree.Root.FindChild("etc")
	require.NotNil(t, etc)
	host := etc.FindChild("hostname")
	require.NotNil(t, host)
	assert.Equal(t, "/etc/hostname", host.Path)
}

func TestParseLayerTar_EmbeddedDotSegmentNormalized(t *testing.T) {
	// Tar entries with embedded "./" segments (e.g. from busybox tar or hand-rolled
	// archives) must collapse to the clean path; otherwise a phantom "." node is
	// created and downstream whiteout matching and waste detection silently miss.
	entries := []tarEntry{
		{Name: "usr/./bin/sh", Size: 64, Type: tar.TypeReg},
	}
	buf := buildLayerTar(t, entries)
	tree, err := ParseLayerTar(buf)
	require.NoError(t, err)

	usr := tree.Root.FindChild("usr")
	require.NotNil(t, usr)
	assert.Nil(t, usr.FindChild("."), "phantom '.' node must not be created")

	bin := usr.FindChild("bin")
	require.NotNil(t, bin, "bin must be a direct child of usr after path cleaning")
	sh := bin.FindChild("sh")
	require.NotNil(t, sh)
	assert.Equal(t, "/usr/bin/sh", sh.Path)
}

func TestParseLayerTar_PathAlwaysHasLeadingSlash(t *testing.T) {
	entries := []tarEntry{
		{Name: "a/b/c", Size: 1, Type: tar.TypeReg},
	}
	buf := buildLayerTar(t, entries)
	tree, err := ParseLayerTar(buf)
	require.NoError(t, err)

	var collected []string
	tree.Walk(func(n *FileNode) {
		if n.Path != "/" {
			collected = append(collected, n.Path)
		}
	})
	for _, p := range collected {
		assert.Equal(t, "/", string(p[0]), "path %q must start with /", p)
	}
}

func TestInsertNode_PromotionToDirClearsFileSize(t *testing.T) {
	tree := NewFileTree()
	// First as a file with size 100.
	insertNode(tree.Root, "etc/foo", 100, false, false, "", 0644, 0, 0)
	// Then as a dir.
	insertNode(tree.Root, "etc/foo", 0, true, false, "", fs.ModeDir|0755, 0, 0)

	etc := tree.Root.FindChild("etc")
	require.NotNil(t, etc)
	foo := etc.FindChild("foo")
	require.NotNil(t, foo)
	assert.True(t, foo.IsDir, "promoted to dir")
	assert.Equal(t, int64(0), foo.Size, "size cleared on promotion")
}
