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

func TestStack_WhiteoutThenReaddInSameLayer(t *testing.T) {
	// Layer 1 emits both `.wh.resolv.conf` and a fresh `resolv.conf` (size 80).
	// Overlayfs upper-layer semantics let a re-add shadow a deletion in the
	// same layer, and findFileInLayer resolves the regular entry as the winner.
	// The stacked tree must agree: exactly one resolv.conf child, marked
	// Modified at the new size — not a Removed tombstone.
	layer0 := makeTree(
		makeDir("etc", "/etc",
			makeFile("resolv.conf", "/etc/resolv.conf", 50),
			makeFile("hostname", "/etc/hostname", 10),
		),
	)
	layer1 := makeTree(
		makeDir("etc", "/etc",
			makeFile(".wh.resolv.conf", "/etc/.wh.resolv.conf", 0),
			makeFile("resolv.conf", "/etc/resolv.conf", 80),
		),
	)
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: layer1},
	}

	result := Stack(layers)
	require.Len(t, result, 2)

	etc := result[1].Root.FindChild("etc")
	require.NotNil(t, etc)

	var resolvs []*FileNode
	for _, c := range etc.Children {
		if c.Name == "resolv.conf" {
			resolvs = append(resolvs, c)
		}
	}
	require.Len(t, resolvs, 1, "re-added name must appear exactly once, not as both Removed and a re-add")
	assert.Equal(t, Modified, resolvs[0].DiffType, "re-add in same layer as whiteout must win over the deletion")
	assert.Equal(t, int64(80), resolvs[0].Size)

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

func TestStack_OpaqueWhiteoutReintroducesName(t *testing.T) {
	// Layer 0 has /var/cache/foo.dat (size 100).
	// Layer 1 emits an opaque whiteout AND a fresh foo.dat (size 300).
	// Expectation: exactly one foo.dat child of cache, marked Added at size 300.
	// Without the fix, the opaque branch clones foo.dat as Removed AND appends
	// it as Added, producing two children with the same Name.
	layer0 := makeTree(
		makeDir("var", "/var",
			makeDir("cache", "/var/cache",
				makeFile("foo.dat", "/var/cache/foo.dat", 100),
			),
		),
	)
	layer1 := makeTree(
		makeDir("var", "/var",
			makeDir("cache", "/var/cache",
				makeFile(".wh..wh..opq", "/var/cache/.wh..wh..opq", 0),
				makeFile("foo.dat", "/var/cache/foo.dat", 300),
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

	var foos []*FileNode
	for _, c := range cache.Children {
		if c.Name == "foo.dat" {
			foos = append(foos, c)
		}
	}
	require.Len(t, foos, 1, "reintroduced name must appear exactly once after opaque whiteout")
	assert.Equal(t, Added, foos[0].DiffType)
	assert.Equal(t, int64(300), foos[0].Size)
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

func TestStack_DirReplacedByFile_EmitsRemovedForChildren(t *testing.T) {
	// Layer 0: /x is a directory containing /x/a and /x/b.
	layer0 := makeTree(
		makeDir("x", "/x",
			makeFile("a", "/x/a", 100),
			makeFile("b", "/x/b", 200),
		),
	)
	// Layer 1: /x is now a regular file.
	layer1 := makeTree(
		makeFile("x", "/x", 50),
	)
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: layer1},
	}

	result := Stack(layers)
	require.Len(t, result, 2)

	r1 := result[1].Root

	// /x must appear as a Modified regular file with the new size.
	x := r1.FindChild("x")
	require.NotNil(t, x)
	assert.False(t, x.IsDir)
	assert.Equal(t, Modified, x.DiffType)
	assert.Equal(t, int64(50), x.Size)

	// The lost children must appear as Removed at the merged-root level.
	var foundA, foundB *FileNode
	for _, child := range r1.Children {
		switch child.Path {
		case "/x/a":
			foundA = child
		case "/x/b":
			foundB = child
		}
	}
	require.NotNil(t, foundA, "/x/a must appear as Removed in stacked tree")
	require.NotNil(t, foundB, "/x/b must appear as Removed in stacked tree")
	assert.Equal(t, Removed, foundA.DiffType)
	assert.Equal(t, int64(100), foundA.Size)
	assert.Equal(t, Removed, foundB.DiffType)
	assert.Equal(t, int64(200), foundB.Size)
}

func TestStack_PreservesHardlinkMetadata(t *testing.T) {
	// Layer 0 introduces /bin/ls as a regular file.
	layer0 := makeTree(
		makeDir("bin", "/bin",
			makeFile("ls", "/bin/ls", 100),
		),
	)
	// Layer 1 introduces /usr/bin/ls as a hardlink pointing at /bin/ls.
	hardlink := &FileNode{
		Name:       "ls",
		Path:       "/usr/bin/ls",
		Size:       0,
		IsDir:      false,
		IsHardlink: true,
		Linkname:   "/bin/ls",
	}
	layer1 := makeTree(
		makeDir("usr", "/usr",
			makeDir("bin", "/usr/bin", hardlink),
		),
	)
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: layer1},
	}

	result := Stack(layers)
	require.Len(t, result, 2)

	// Layer 1 stacked tree must carry the hardlink fields through cloneAsAdded.
	r1usrbin := result[1].Root.FindChild("usr").FindChild("bin")
	require.NotNil(t, r1usrbin)
	r1ls := r1usrbin.FindChild("ls")
	require.NotNil(t, r1ls, "hardlink must appear in stacked tree")
	assert.True(t, r1ls.IsHardlink, "IsHardlink must propagate through cloneAsAdded")
	assert.Equal(t, "/bin/ls", r1ls.Linkname, "Linkname must propagate through cloneAsAdded")

	// And /bin/ls must remain Unchanged on layer 1 — exercising cloneAsUnchanged.
	r1binls := result[1].Root.FindChild("bin").FindChild("ls")
	require.NotNil(t, r1binls)
	assert.Equal(t, Unchanged, r1binls.DiffType)

	// Add a layer 2 hardlink modification to exercise the Modified branch in mergeLayer.
	layer2hl := &FileNode{
		Name:       "ls",
		Path:       "/usr/bin/ls",
		Size:       0,
		IsDir:      false,
		IsHardlink: true,
		Linkname:   "/bin/busybox",
	}
	layer2 := makeTree(
		makeDir("usr", "/usr",
			makeDir("bin", "/usr/bin", layer2hl),
		),
	)
	layersWith2 := append(layers, Layer{Index: 2, Tree: layer2})
	result2 := Stack(layersWith2)
	r2ls := result2[2].Root.FindChild("usr").FindChild("bin").FindChild("ls")
	require.NotNil(t, r2ls)
	assert.Equal(t, Modified, r2ls.DiffType)
	assert.True(t, r2ls.IsHardlink, "IsHardlink must propagate through Modified branch")
	assert.Equal(t, "/bin/busybox", r2ls.Linkname, "Linkname must propagate through Modified branch")
}

// findChildPath walks down a node by Name segments. Test helper for the
// aggregated-walker tests below, which need to assert DiffType at nested
// paths without re-typing FindChild chains.
func findChildPath(t *testing.T, root *FileNode, names ...string) *FileNode {
	t.Helper()
	cur := root
	for _, name := range names {
		require.NotNil(t, cur, "intermediate node nil while looking for %v", names)
		cur = cur.FindChild(name)
	}
	return cur
}

// aggregateThreeLayerFixture is the spec's Worked Example:
//
//	L0: adds /a (size 10)
//	L1: modifies /a (size 20), adds /b
//	L2: adds /c (does not touch /a or /b)
//
// Used by both TestBuildAggregatedTrees_PreservesPriorDiffTypeAcrossUntouchedLayers
// and TestStack_StillCompareSingleLayer to lock the divergent semantics of
// the two walkers on identical input.
func aggregateThreeLayerFixture() []Layer {
	layer0 := makeTree(makeFile("a", "/a", 10))
	layer1 := makeTree(
		makeFile("a", "/a", 20),
		makeFile("b", "/b", 30),
	)
	layer2 := makeTree(makeFile("c", "/c", 40))
	return []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: layer1},
		{Index: 2, Tree: layer2},
	}
}

func TestBuildAggregatedTrees_LengthMatchesLayers(t *testing.T) {
	layers := aggregateThreeLayerFixture()
	got := BuildAggregatedTrees(layers)
	require.Len(t, got, len(layers))
	for i, tree := range got {
		require.NotNilf(t, tree, "result[%d] must not be nil for non-empty fixture", i)
		require.NotNilf(t, tree.Root, "result[%d].Root must not be nil", i)
	}
}

func TestBuildAggregatedTrees_AtL0_EqualsStack(t *testing.T) {
	layers := aggregateThreeLayerFixture()
	stacked := Stack(layers)
	aggregated := BuildAggregatedTrees(layers)

	require.NotNil(t, stacked[0])
	require.NotNil(t, aggregated[0])

	// Walk both trees in parallel and assert path+DiffType equivalence.
	var walk func(a, b *FileNode)
	walk = func(a, b *FileNode) {
		require.Equalf(t, a.Path, b.Path, "path mismatch at L0")
		require.Equalf(t, a.DiffType, b.DiffType, "DiffType mismatch at L0 for %s", a.Path)
		require.Equalf(t, len(a.Children), len(b.Children), "child count mismatch at %s", a.Path)
		for i := range a.Children {
			walk(a.Children[i], b.Children[i])
		}
	}
	walk(stacked[0].Root, aggregated[0].Root)
}

func TestBuildAggregatedTrees_PreservesPriorDiffTypeAcrossUntouchedLayers(t *testing.T) {
	layers := aggregateThreeLayerFixture()
	aggregated := BuildAggregatedTrees(layers)
	require.Len(t, aggregated, 3)

	r2 := aggregated[2].Root
	a := findChildPath(t, r2, "a")
	require.NotNil(t, a, "/a must survive untouched L2 in aggregated view")
	assert.Equal(t, Modified, a.DiffType, "/a was Modified in L1; aggregated view must carry forward through L2")

	b := findChildPath(t, r2, "b")
	require.NotNil(t, b, "/b must survive untouched L2 in aggregated view")
	assert.Equal(t, Added, b.DiffType, "/b was Added in L1; aggregated view must carry forward through L2")

	c := findChildPath(t, r2, "c")
	require.NotNil(t, c)
	assert.Equal(t, Added, c.DiffType, "/c added in L2")
}

func TestStack_StillCompareSingleLayer(t *testing.T) {
	// Regression guard: the same fixture as the aggregated test above must
	// still produce CompareSingleLayer semantics under Stack, with prior
	// DiffType labels stripped at every iteration. This locks Stack's
	// behaviour against accidental drift from the mergeLayerWith refactor.
	layers := aggregateThreeLayerFixture()
	stacked := Stack(layers)
	require.Len(t, stacked, 3)

	r2 := stacked[2].Root
	a := findChildPath(t, r2, "a")
	require.NotNil(t, a)
	assert.Equal(t, Unchanged, a.DiffType, "Stack[2] must show /a Unchanged — L2 didn't touch it")

	b := findChildPath(t, r2, "b")
	require.NotNil(t, b)
	assert.Equal(t, Unchanged, b.DiffType, "Stack[2] must show /b Unchanged — L2 didn't touch it")

	c := findChildPath(t, r2, "c")
	require.NotNil(t, c)
	assert.Equal(t, Added, c.DiffType)
}

func TestBuildAggregatedTrees_RemovedCarriesForward(t *testing.T) {
	// L0 adds /x; L1 whiteouts /x; L2 adds /y (does not touch /x).
	// Aggregated[2] must retain /x as Removed — L1's tombstone propagates.
	layer0 := makeTree(makeFile("x", "/x", 100))
	layer1 := makeTree(makeFile(".wh.x", "/.wh.x", 0))
	layer2 := makeTree(makeFile("y", "/y", 50))
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: layer1},
		{Index: 2, Tree: layer2},
	}

	aggregated := BuildAggregatedTrees(layers)
	require.Len(t, aggregated, 3)

	r2 := aggregated[2].Root
	x := findChildPath(t, r2, "x")
	require.NotNil(t, x, "/x must remain visible as Removed at L2")
	assert.Equal(t, Removed, x.DiffType)

	y := findChildPath(t, r2, "y")
	require.NotNil(t, y)
	assert.Equal(t, Added, y.DiffType)
}

func TestBuildAggregatedTrees_OpaqueWhiteoutAtMidLayer(t *testing.T) {
	// L0 creates /cache containing /cache/old.
	// L1 emits an opaque whiteout in /cache and adds /cache/new.
	// L2 adds /other (does not touch /cache).
	// Aggregated[2] must show /cache/old Removed — the recursion-propagation
	// fix in mergeLayerWith is what carries the Removed child forward
	// through L2's untouched-directory merge.
	layer0 := makeTree(
		makeDir("cache", "/cache",
			makeFile("old", "/cache/old", 100),
		),
	)
	layer1 := makeTree(
		makeDir("cache", "/cache",
			makeFile(".wh..wh..opq", "/cache/.wh..wh..opq", 0),
			makeFile("new", "/cache/new", 50),
		),
	)
	layer2 := makeTree(makeFile("other", "/other", 25))
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: layer1},
		{Index: 2, Tree: layer2},
	}

	aggregated := BuildAggregatedTrees(layers)
	require.Len(t, aggregated, 3)

	r2 := aggregated[2].Root
	cache := findChildPath(t, r2, "cache")
	require.NotNil(t, cache)
	assert.Equal(t, Modified, cache.DiffType)

	old := findChildPath(t, r2, "cache", "old")
	require.NotNil(t, old, "/cache/old Removed must carry through L2's untouched merge")
	assert.Equal(t, Removed, old.DiffType)

	newF := findChildPath(t, r2, "cache", "new")
	require.NotNil(t, newF)
	assert.Equal(t, Added, newF.DiffType)

	other := findChildPath(t, r2, "other")
	require.NotNil(t, other)
	assert.Equal(t, Added, other.DiffType)
}

func TestBuildAggregatedTrees_EmptyLayerSnapshotsBaseline(t *testing.T) {
	// L0 adds /a; L1 has nil tree; L2 adds /b. Mirrors Stack's empty-layer
	// handling: result[1] is a snapshot of the post-L0 baseline.
	layer0 := makeTree(makeFile("a", "/a", 10))
	layer2 := makeTree(makeFile("b", "/b", 20))
	layers := []Layer{
		{Index: 0, Tree: layer0},
		{Index: 1, Tree: nil},
		{Index: 2, Tree: layer2},
	}

	aggregated := BuildAggregatedTrees(layers)
	require.Len(t, aggregated, 3)

	r1 := aggregated[1].Root
	require.NotNil(t, r1)
	a1 := findChildPath(t, r1, "a")
	require.NotNil(t, a1, "empty L1 must snapshot baseline; /a from L0 must remain")
	assert.Equal(t, Added, a1.DiffType)
	assert.Nil(t, r1.FindChild("b"), "/b not yet introduced at index 1")

	r2 := aggregated[2].Root
	a2 := findChildPath(t, r2, "a")
	require.NotNil(t, a2)
	assert.Equal(t, Added, a2.DiffType)
	b2 := findChildPath(t, r2, "b")
	require.NotNil(t, b2)
	assert.Equal(t, Added, b2.DiffType)
}
