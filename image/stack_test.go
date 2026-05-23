package image

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTree(nodes ...*FileNode) *FileTree {
	root := &FileNode{Name: "/", Path: "/", IsDir: true, Children: nodes}
	return &FileTree{Root: root}
}

func makeDir(name, path string, children ...*FileNode) *FileNode {
	return &FileNode{Name: name, Path: path, IsDir: true, Children: children}
}

func makeFile(name, path string, size int64) *FileNode {
	return &FileNode{Name: name, Path: path, Size: size, IsDir: false}
}

func TestStack_SingleLayer_AllAdded(t *testing.T) {
	tree := makeTree(
		makeDir("etc", "/etc",
			makeFile("passwd", "/etc/passwd", 100),
			makeFile("group", "/etc/group", 200),
		),
		makeFile("README", "/README", 50),
	)
	layers := []Layer{{Index: 0, Tree: tree}}

	result := Stack(layers)
	require.Len(t, result, 1)

	root := result[0].Root
	require.NotNil(t, root)

	etc := root.FindChild("etc")
	require.NotNil(t, etc)
	assert.Equal(t, Added, etc.DiffType)

	passwd := etc.FindChild("passwd")
	require.NotNil(t, passwd)
	assert.Equal(t, Added, passwd.DiffType)
	assert.Equal(t, int64(100), passwd.Size)

	group := etc.FindChild("group")
	require.NotNil(t, group)
	assert.Equal(t, Added, group.DiffType)

	readme := root.FindChild("README")
	require.NotNil(t, readme)
	assert.Equal(t, Added, readme.DiffType)
	assert.Equal(t, int64(50), readme.Size)
}

func TestStack_TwoLayers_ModifiedFile(t *testing.T) {
	layer0 := makeTree(
		makeDir("etc", "/etc",
			makeFile("config", "/etc/config", 100),
		),
	)
	layer1 := makeTree(
		makeDir("etc", "/etc",
			makeFile("config", "/etc/config", 250),
		),
	)
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: layer1},
	}

	result := Stack(layers)
	require.Len(t, result, 2)

	r0 := result[0].Root
	assert.Equal(t, Added, r0.FindChild("etc").DiffType)
	assert.Equal(t, Added, r0.FindChild("etc").FindChild("config").DiffType)

	r1 := result[1].Root
	etc := r1.FindChild("etc")
	require.NotNil(t, etc)
	assert.Equal(t, Modified, etc.DiffType)

	config := etc.FindChild("config")
	require.NotNil(t, config)
	assert.Equal(t, Modified, config.DiffType)
	assert.Equal(t, int64(250), config.Size)
}

func TestStack_TwoLayers_AddNewFile(t *testing.T) {
	layer0 := makeTree(
		makeFile("existing", "/existing", 100),
	)
	layer1 := makeTree(
		makeFile("newfile", "/newfile", 200),
	)
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: layer1},
	}

	result := Stack(layers)
	require.Len(t, result, 2)

	r1 := result[1].Root
	existing := r1.FindChild("existing")
	require.NotNil(t, existing)
	assert.Equal(t, Unchanged, existing.DiffType)

	newfile := r1.FindChild("newfile")
	require.NotNil(t, newfile)
	assert.Equal(t, Added, newfile.DiffType)
	assert.Equal(t, int64(200), newfile.Size)
}

func TestStack_WhiteoutRemovesSpecificFile(t *testing.T) {
	layer0 := makeTree(
		makeDir("etc", "/etc",
			makeFile("resolv.conf", "/etc/resolv.conf", 50),
			makeFile("hostname", "/etc/hostname", 10),
		),
	)
	layer1 := makeTree(
		makeDir("etc", "/etc",
			makeFile(".wh.resolv.conf", "/etc/.wh.resolv.conf", 0),
		),
	)
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: layer1},
	}

	result := Stack(layers)
	require.Len(t, result, 2)

	r1 := result[1].Root
	etc := r1.FindChild("etc")
	require.NotNil(t, etc)
	assert.Equal(t, Modified, etc.DiffType)

	resolv := etc.FindChild("resolv.conf")
	require.NotNil(t, resolv, "removed file must appear as Removed")
	assert.Equal(t, Removed, resolv.DiffType)

	whiteout := etc.FindChild(".wh.resolv.conf")
	assert.Nil(t, whiteout, "whiteout file itself must not appear in stacked tree")

	hostname := etc.FindChild("hostname")
	require.NotNil(t, hostname)
	assert.Equal(t, Unchanged, hostname.DiffType)
}

func TestStack_OpaqueWhiteoutRemovesAllPreviousContents(t *testing.T) {
	layer0 := makeTree(
		makeDir("var", "/var",
			makeDir("cache", "/var/cache",
				makeFile("old1.dat", "/var/cache/old1.dat", 100),
				makeFile("old2.dat", "/var/cache/old2.dat", 200),
			),
		),
	)
	layer1 := makeTree(
		makeDir("var", "/var",
			makeDir("cache", "/var/cache",
				makeFile(".wh..wh..opq", "/var/cache/.wh..wh..opq", 0),
				makeFile("new.dat", "/var/cache/new.dat", 300),
			),
		),
	)
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: layer1},
	}

	result := Stack(layers)
	require.Len(t, result, 2)

	r1 := result[1].Root
	cache := r1.FindChild("var").FindChild("cache")
	require.NotNil(t, cache)
	assert.Equal(t, Modified, cache.DiffType)

	old1 := cache.FindChild("old1.dat")
	require.NotNil(t, old1, "previous file must appear as Removed")
	assert.Equal(t, Removed, old1.DiffType)

	old2 := cache.FindChild("old2.dat")
	require.NotNil(t, old2, "previous file must appear as Removed")
	assert.Equal(t, Removed, old2.DiffType)

	newFile := cache.FindChild("new.dat")
	require.NotNil(t, newFile, "new file from current layer must appear as Added")
	assert.Equal(t, Added, newFile.DiffType)
	assert.Equal(t, int64(300), newFile.Size)

	opq := cache.FindChild(".wh..wh..opq")
	assert.Nil(t, opq, "opaque whiteout file must not appear in stacked tree")
}

func TestStack_ThreeLayers_CumulativeChanges(t *testing.T) {
	layer0 := makeTree(
		makeDir("app", "/app",
			makeFile("main.go", "/app/main.go", 500),
		),
	)
	layer1 := makeTree(
		makeDir("app", "/app",
			makeFile("util.go", "/app/util.go", 300),
		),
	)
	layer2 := makeTree(
		makeDir("app", "/app",
			makeFile(".wh.util.go", "/app/.wh.util.go", 0),
			makeFile("main.go", "/app/main.go", 600),
		),
	)
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: layer1},
		{Index: 2, Tree: layer2},
	}

	result := Stack(layers)
	require.Len(t, result, 3)

	r0 := result[0].Root.FindChild("app")
	assert.Equal(t, Added, r0.DiffType)
	assert.Equal(t, Added, r0.FindChild("main.go").DiffType)

	r1 := result[1].Root.FindChild("app")
	assert.Equal(t, Modified, r1.DiffType)
	assert.Equal(t, Unchanged, r1.FindChild("main.go").DiffType)
	assert.Equal(t, Added, r1.FindChild("util.go").DiffType)

	r2 := result[2].Root.FindChild("app")
	assert.Equal(t, Modified, r2.DiffType)
	assert.Equal(t, Modified, r2.FindChild("main.go").DiffType)
	assert.Equal(t, int64(600), r2.FindChild("main.go").Size)
	assert.Equal(t, Removed, r2.FindChild("util.go").DiffType)
	assert.Nil(t, r2.FindChild(".wh.util.go"))
}

func TestStack_EmptyLayerInMiddle_EverythingUnchanged(t *testing.T) {
	layer0 := makeTree(
		makeFile("data.bin", "/data.bin", 1024),
	)
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: nil},
		{Index: 2, Tree: &FileTree{Root: &FileNode{Name: "/", Path: "/", IsDir: true}}},
	}

	result := Stack(layers)
	require.Len(t, result, 3)

	r0 := result[0].Root.FindChild("data.bin")
	require.NotNil(t, r0)
	assert.Equal(t, Added, r0.DiffType)

	r1 := result[1].Root.FindChild("data.bin")
	require.NotNil(t, r1)
	assert.Equal(t, Unchanged, r1.DiffType)

	r2 := result[2].Root.FindChild("data.bin")
	require.NotNil(t, r2)
	assert.Equal(t, Unchanged, r2.DiffType)
}

func TestStack_NilTreeFirstLayer_EmptyResult(t *testing.T) {
	layers := []Layer{
		{Index: 0, Tree: nil},
	}

	result := Stack(layers)
	require.Len(t, result, 1)

	root := result[0].Root
	require.NotNil(t, root)
	assert.Empty(t, root.Children)
}

func TestStack_WhiteoutFileNotInFirstLayer(t *testing.T) {
	layer0 := makeTree(
		makeDir("etc", "/etc",
			makeFile(".wh.ghost", "/etc/.wh.ghost", 0),
			makeFile("real", "/etc/real", 10),
		),
	)
	layers := []Layer{{Index: 0, Tree: layer0}}

	result := Stack(layers)
	require.Len(t, result, 1)

	etc := result[0].Root.FindChild("etc")
	require.NotNil(t, etc)
	assert.Equal(t, Added, etc.DiffType)

	ghost := etc.FindChild(".wh.ghost")
	assert.Nil(t, ghost, "whiteout files must be stripped from first layer")

	real := etc.FindChild("real")
	require.NotNil(t, real)
	assert.Equal(t, Added, real.DiffType)
}

func TestStack_RemovedFileNotInCumulative(t *testing.T) {
	layer0 := makeTree(
		makeDir("tmp", "/tmp",
			makeFile("scratch", "/tmp/scratch", 64),
		),
	)
	layer1 := makeTree(
		makeDir("tmp", "/tmp",
			makeFile(".wh.scratch", "/tmp/.wh.scratch", 0),
		),
	)
	layer2 := makeTree(
		makeFile("other", "/other", 10),
	)
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: layer1},
		{Index: 2, Tree: layer2},
	}

	result := Stack(layers)
	require.Len(t, result, 3)

	r1tmp := result[1].Root.FindChild("tmp")
	require.NotNil(t, r1tmp)
	assert.Equal(t, Removed, r1tmp.FindChild("scratch").DiffType)

	r2tmp := result[2].Root.FindChild("tmp")
	require.NotNil(t, r2tmp)
	scratch := r2tmp.FindChild("scratch")
	assert.Nil(t, scratch, "removed file must not reappear in later layers")
}

func TestStack_EmptySlice(t *testing.T) {
	result := Stack(nil)
	assert.Empty(t, result)

	result = Stack([]Layer{})
	assert.Empty(t, result)
}

func TestStack_IntroducedInLayer_SingleLayer(t *testing.T) {
	tree := makeTree(
		makeDir("etc", "/etc",
			makeFile("passwd", "/etc/passwd", 100),
		),
		makeFile("README", "/README", 50),
	)
	layers := []Layer{{Index: 0, Tree: tree}}

	result := Stack(layers)
	require.Len(t, result, 1)

	root := result[0].Root
	assert.Equal(t, 0, root.FindChild("etc").IntroducedInLayer)
	assert.Equal(t, 0, root.FindChild("etc").FindChild("passwd").IntroducedInLayer)
	assert.Equal(t, 0, root.FindChild("README").IntroducedInLayer)
}

func TestStack_IntroducedInLayer_FileAddedInLaterLayer(t *testing.T) {
	layer0 := makeTree(
		makeFile("old", "/old", 100),
	)
	layer1 := makeTree(
		makeFile("new", "/new", 200),
	)
	layer2 := makeTree(
		makeFile("newest", "/newest", 300),
	)
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: layer1},
		{Index: 2, Tree: layer2},
	}

	result := Stack(layers)
	require.Len(t, result, 3)

	r1 := result[1].Root
	assert.Equal(t, 0, r1.FindChild("old").IntroducedInLayer, "unchanged file keeps origin layer")
	assert.Equal(t, 1, r1.FindChild("new").IntroducedInLayer, "new file gets current layer")

	r2 := result[2].Root
	assert.Equal(t, 0, r2.FindChild("old").IntroducedInLayer, "still layer 0")
	assert.Equal(t, 1, r2.FindChild("new").IntroducedInLayer, "still layer 1")
	assert.Equal(t, 2, r2.FindChild("newest").IntroducedInLayer, "added in layer 2")
}

func TestStack_IntroducedInLayer_ModifiedFileKeepsOrigin(t *testing.T) {
	layer0 := makeTree(
		makeDir("etc", "/etc",
			makeFile("config", "/etc/config", 100),
		),
	)
	layer1 := makeTree(
		makeDir("etc", "/etc",
			makeFile("config", "/etc/config", 250),
		),
	)
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: layer1},
	}

	result := Stack(layers)
	require.Len(t, result, 2)

	config := result[1].Root.FindChild("etc").FindChild("config")
	assert.Equal(t, Modified, config.DiffType)
	assert.Equal(t, 0, config.IntroducedInLayer, "modified file keeps original introduction layer")
}

func TestStack_IntroducedInLayer_OpaqueWhiteout(t *testing.T) {
	layer0 := makeTree(
		makeDir("var", "/var",
			makeFile("old.dat", "/var/old.dat", 100),
		),
	)
	layer1 := makeTree(
		makeDir("var", "/var",
			makeFile(".wh..wh..opq", "/var/.wh..wh..opq", 0),
			makeFile("new.dat", "/var/new.dat", 300),
		),
	)
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: layer1},
	}

	result := Stack(layers)
	require.Len(t, result, 2)

	varDir := result[1].Root.FindChild("var")
	newFile := varDir.FindChild("new.dat")
	require.NotNil(t, newFile)
	assert.Equal(t, 1, newFile.IntroducedInLayer, "file after opaque whiteout gets current layer")

	oldFile := varDir.FindChild("old.dat")
	require.NotNil(t, oldFile)
	assert.Equal(t, 0, oldFile.IntroducedInLayer, "removed file preserves original origin")
}

func TestStack_DirMetadataChange_Mode(t *testing.T) {
	layer0 := makeTree(
		&FileNode{Name: "app", Path: "/app", IsDir: true, Mode: 0o755, UID: 0, GID: 0,
			Children: []*FileNode{makeFile("file.txt", "/app/file.txt", 100)},
		},
	)
	layer1 := makeTree(
		&FileNode{Name: "app", Path: "/app", IsDir: true, Mode: 0o777, UID: 0, GID: 0},
	)
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: layer1},
	}

	result := Stack(layers)
	require.Len(t, result, 2)

	app := result[1].Root.FindChild("app")
	require.NotNil(t, app)
	assert.Equal(t, fs.FileMode(0o777), app.Mode, "Mode must propagate from later layer")
	assert.Equal(t, Modified, app.DiffType, "metadata-only change must mark dir Modified")
	assert.Equal(t, 1, app.IntroducedInLayer, "IntroducedInLayer must update on metadata change")

	child := app.FindChild("file.txt")
	require.NotNil(t, child)
	assert.Equal(t, Unchanged, child.DiffType, "child contents are unchanged")
}

func TestStack_DirMetadataChange_UIDGID(t *testing.T) {
	layer0 := makeTree(
		&FileNode{Name: "app", Path: "/app", IsDir: true, Mode: 0o755, UID: 0, GID: 0},
	)
	layer1 := makeTree(
		&FileNode{Name: "app", Path: "/app", IsDir: true, Mode: 0o755, UID: 1000, GID: 1000},
	)
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: layer1},
	}

	app := Stack(layers)[1].Root.FindChild("app")
	require.NotNil(t, app)
	assert.Equal(t, 1000, app.UID)
	assert.Equal(t, 1000, app.GID)
	assert.Equal(t, Modified, app.DiffType)
	assert.Equal(t, 1, app.IntroducedInLayer)
}

func TestStack_NoMetadataChange_StaysUnchanged(t *testing.T) {
	// Regression: a layer that touches /other but not /app must NOT mark /app Modified.
	layer0 := makeTree(
		&FileNode{Name: "app", Path: "/app", IsDir: true, Mode: 0o755},
		&FileNode{Name: "other", Path: "/other", IsDir: true, Mode: 0o755},
	)
	layer1 := makeTree(
		&FileNode{Name: "other", Path: "/other", IsDir: true, Mode: 0o755,
			Children: []*FileNode{makeFile("new", "/other/new", 10)},
		},
	)
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: layer1},
	}

	app := Stack(layers)[1].Root.FindChild("app")
	require.NotNil(t, app)
	assert.Equal(t, Unchanged, app.DiffType, "untouched dir must stay Unchanged")
	assert.Equal(t, 0, app.IntroducedInLayer, "IntroducedInLayer must not change")
}

func TestStack_DirMetadataPropagatesToL2(t *testing.T) {
	// Verifies cumulative carries the new metadata forward through cloneStructure.
	layer0 := makeTree(
		&FileNode{Name: "app", Path: "/app", IsDir: true, Mode: 0o755},
	)
	layer1 := makeTree(
		&FileNode{Name: "app", Path: "/app", IsDir: true, Mode: 0o777},
	)
	layer2 := makeTree(
		&FileNode{Name: "other", Path: "/other", IsDir: true, Mode: 0o755},
	)
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: layer1},
		{Index: 2, Tree: layer2},
	}

	r2 := Stack(layers)[2].Root
	app := r2.FindChild("app")
	require.NotNil(t, app)
	assert.Equal(t, fs.FileMode(0o777), app.Mode, "L1's metadata change must be preserved at L2")
	assert.Equal(t, Unchanged, app.DiffType, "no change at L2 itself")
}
