package tui

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func inspectionInputs(data [2][]byte) ([2]image.CompareInput, [2]image.CompareImage) {
	var inputs [2]image.CompareInput
	var results [2]image.CompareImage
	for side := range inputs {
		inputs[side] = image.CompareInput{Ref: "tag", Resolver: &compareContentResolver{digest: "pinned", extractor: &compareContentExtractor{data: data[side]}}}
		results[side] = image.CompareImage{Digest: "pinned", Analysis: &image.Analysis{Layers: []image.Layer{{Command: "COPY app /app"}}}}
	}
	return inputs, results
}

func TestCompareInspectionUnsupportedContentKeepsDetails(t *testing.T) {
	for _, tc := range []struct{ name, data, message string }{
		{"ten thousand lines", strings.Repeat("line\n", 10001), "1,500 lines"},
		{"long minified line", strings.Repeat("x", 4097), "4,096 bytes per line"},
		{"binary", "a\x00b", "binary"},
		{"invalid encoding", "\xff\xfe", "UTF-8"},
		{"compressed archive", "\x1f\x8b\x00\x00", "binary"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inputs, results := inspectionInputs([2][]byte{[]byte(tc.data), nil})
			c, err := loadCompareContent(t.Context(), image.FileDiff{Path: "/file", DiffType: image.Removed}, &image.FileNode{Size: int64(len(tc.data)), UID: 1000}, nil, inputs, results)
			require.Empty(t, err)
			require.NotNil(t, c)
			assert.Contains(t, c.note, tc.message)
			assert.Contains(t, c.metadata, "uid:1000")
			assert.Contains(t, c.metadata, "COPY app /app")
			assert.Empty(t, c.rows, "never present a partial diff as complete")
		})
	}
	for _, node := range []*image.FileNode{{Size: compareContentMaxBytes + 1}, {Linkname: "/target"}, {IsHardlink: true}, {Mode: fs.ModeNamedPipe}, {Mode: fs.ModeDevice}, {Mode: fs.ModeSocket}} {
		inputs, results := inspectionInputs([2][]byte{})
		c, err := loadCompareContent(t.Context(), image.FileDiff{Path: "/file", DiffType: image.Added}, nil, node, inputs, results)
		require.Empty(t, err)
		assert.NotEmpty(t, c.note)
		assert.Empty(t, inputs[1].Resolver.(*compareContentResolver).extractor.gotRef, "unsupported payload must not be extracted")
	}
}

func TestCompareInspectionPlainViewReadsOnlyChosenSide(t *testing.T) {
	for _, mode := range []compareContentMode{compareModeBefore, compareModeAfter} {
		inputs, results := inspectionInputs([2][]byte{[]byte("old\n"), []byte("new value\n")})
		nodes := [2]*image.FileNode{{Size: 4}, {Size: 10}}
		c, err := loadCompareContentMode(t.Context(), image.FileDiff{Path: "/file", DiffType: image.Modified, ChangeReason: "size"}, nodes[0], nodes[1], inputs, results, mode)
		require.Empty(t, err)
		require.Empty(t, c.note)
		selected := int(mode) - 1
		assert.Equal(t, "pinned", inputs[selected].Resolver.(*compareContentResolver).extractor.gotRef)
		assert.Empty(t, inputs[1-selected].Resolver.(*compareContentResolver).extractor.gotRef)
		for _, row := range c.rows {
			assert.True(t, row.beforeLine == 0 || row.afterLine == 0, "plain view must not align two sides")
		}
	}
}

func TestCompareInspectionChecksBothSizesBeforeReading(t *testing.T) {
	inputs, results := inspectionInputs([2][]byte{[]byte("a"), nil})
	diff := image.FileDiff{Path: "/file", DiffType: image.Modified, ChangeReason: "size"}
	content, err := loadCompareContent(t.Context(), diff, &image.FileNode{Size: 1}, &image.FileNode{Size: compareContentMaxBytes + 1}, inputs, results)
	require.Empty(t, err)
	assert.Contains(t, content.note, "Image B exceeds")
	for _, input := range inputs {
		assert.Empty(t, input.Resolver.(*compareContentResolver).extractor.gotRef)
	}
}

func TestCompareInspectionFailureKeepsPathAndDetails(t *testing.T) {
	path := "/usr/local/lib/" + strings.Repeat("nested-package/", 8) + "production-config.yaml"
	diff := image.FileDiff{Path: path, DiffType: image.Added}
	m := compareModel{result: &image.CompareResult{FileDiffs: []image.FileDiff{diff}}, fileView: true, theme: themeFor(""), width: 74, height: 24}
	assert.Contains(t, ansi.Strip(m.View().Content), "production-config.yaml")
	m.contentOpen = true
	m.contentRequest = 1
	next, _ := m.Update(compareContentMsg{request: 1, content: &compareContent{path: path, metadata: "After: uid:42"}, err: "image changed since analysis"})
	out := ansi.Strip(next.View().Content)
	assert.Contains(t, out, "production-config.yaml")
	assert.Contains(t, out, "Preview unavailable")
	assert.Contains(t, out, "After: uid:42")
	assert.NotContains(t, out, "Loading file")
}

func TestCompareInspectionDetailsDoNotRequireExtraction(t *testing.T) {
	d := image.FileDiff{Path: "/app", DiffType: image.Added}
	index := buildCompareFileIndex(t.Context(), []image.FileDiff{d}, [2]image.CompareImage{})().(compareFileIndexMsg).index
	index.after[d.Path] = &image.FileNode{Size: 10, UID: 42}
	m := compareModel{result: &image.CompareResult{FileDiffs: []image.FileDiff{d}}, fileIndex: index, fileView: true, keys: defaultKeys(), theme: themeFor(""), width: 140, height: 35}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(compareModel)
	assert.Nil(t, cmd)
	assert.True(t, m.contentOpen)
	assert.Equal(t, compareModeDetails, m.contentMode)
	assert.Contains(t, m.content.metadata, "uid:42")
	assert.Contains(t, ansi.Strip(m.View().Content), "Content has not been read")
}

func TestCompareInspectionMetadataCanReachEndAcrossSizes(t *testing.T) {
	for _, size := range [][2]int{{140, 35}, {100, 24}, {80, 20}, {40, 12}} {
		m := compareModel{result: &image.CompareResult{}, contentOpen: true, contentMode: compareModeDetails, content: &compareContent{path: "/file", note: "Binary file: text preview unavailable. File details remain available.", metadata: strings.Repeat("long command argument ", 400) + "END-OF-METADATA"}, keys: defaultKeys(), theme: themeFor(""), width: size[0], height: size[1]}
		m.updateContentKeys(tea.KeyPressMsg{Code: 'G'})
		out := ansi.Strip(m.View().Content)
		assert.Contains(t, out, "END-OF-METADATA")
		assert.LessOrEqual(t, len(strings.Split(out, "\n")), size[1])
		for _, line := range strings.Split(out, "\n") {
			assert.LessOrEqual(t, ansi.StringWidth(line), size[0])
		}
	}
}

func TestCompareInspectionSwitchModeCancelsAndIgnoresOldResult(t *testing.T) {
	inputs, results := inspectionInputs([2][]byte{nil, []byte("text")})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d := image.FileDiff{Path: "/file", DiffType: image.Added}
	index := buildCompareFileIndex(t.Context(), []image.FileDiff{d}, results)().(compareFileIndexMsg).index
	index.after[d.Path] = &image.FileNode{Size: 4}
	m := compareModel{session: &compareSession{ctx: ctx, inputs: inputs, results: results}, result: &image.CompareResult{FileDiffs: []image.FileDiff{d}}, fileIndex: index, fileView: true}
	next, cmd := m.openContent()
	m = next.(compareModel)
	require.NotNil(t, cmd)
	old := cmd()
	next, _ = m.Update(tea.KeyPressMsg{Code: '4'})
	m = next.(compareModel)
	next, _ = m.Update(old)
	m = next.(compareModel)
	assert.Equal(t, compareModeDetails, m.contentMode)
	assert.Empty(t, m.content.rows)
	assert.Contains(t, m.content.note, "Content has not been read")
}

func TestCompareInspectionCachedModesDoNotExportAgain(t *testing.T) {
	inputs, results := inspectionInputs([2][]byte{[]byte("old"), []byte("new text")})
	d := image.FileDiff{Path: "/file", DiffType: image.Modified, ChangeReason: "size"}
	before, after := &image.FileNode{Size: 3}, &image.FileNode{Size: 8}
	c, err := loadCompareContent(t.Context(), d, before, after, inputs, results)
	require.Empty(t, err)
	for _, input := range inputs {
		input.Resolver.(*compareContentResolver).extractor.block = true
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for _, mode := range []compareContentMode{compareModeBefore, compareModeAfter, compareModeDiff} {
		c, err = loadCompareContentCached(ctx, d, before, after, inputs, results, mode, c)
		require.Empty(t, err)
		require.Empty(t, c.note)
		require.NotEmpty(t, c.rows)
	}
}

func TestCompareInspectionDiffChangeNavigation(t *testing.T) {
	rows, err := compareContentDiff([]byte("same\nold\ncontext\nold2\nend"), []byte("same\nnew\ncontext\nnew2\nend"))
	require.NoError(t, err)
	m := compareModel{content: &compareContent{rows: rows}, keys: defaultKeys(), width: 140, height: 35}
	m.updateContentKeys(tea.KeyPressMsg{Code: 'n'})
	assert.Equal(t, 1, m.contentCursor)
	m.updateContentKeys(tea.KeyPressMsg{Code: 'n'})
	assert.Equal(t, 3, m.contentCursor)
	m.updateContentKeys(tea.KeyPressMsg{Code: 'n'})
	assert.Equal(t, 3, m.contentCursor)
	m.updateContentKeys(tea.KeyPressMsg{Code: 'N'})
	assert.Equal(t, 1, m.contentCursor)
}

func TestComparePreviewPreservesSourcePunctuation(t *testing.T) {
	source := `echo "$PATH"; printf '\n'`
	assert.Equal(t, source, compareTextLine(source))
	assert.Equal(t, `safe\x1b[2J\n\u202e`, compareTextLine("safe\x1b[2J\n\u202e"))
}
