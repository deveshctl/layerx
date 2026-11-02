package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type compareContentExtractor struct {
	image.Extractor
	data   []byte
	gotRef string
	block  bool
}

func (e *compareContentExtractor) ExtractBoundedFromLayer(ctx context.Context, ref, _ string, _ int, max int64) ([]byte, error) {
	e.gotRef = ref
	if e.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if int64(len(e.data)) > max {
		return nil, context.DeadlineExceeded
	}
	return e.data, nil
}

type compareContentResolver struct {
	image.Resolver
	digest    string
	extractor *compareContentExtractor
}

func (r *compareContentResolver) ImageID(context.Context, string) (string, error) {
	return r.digest, nil
}
func (r *compareContentResolver) NewExtractor() image.Extractor { return r.extractor }

func TestCompareContentBoundedAndPinned(t *testing.T) {
	inputs := [2]image.CompareInput{}
	results := [2]image.CompareImage{}
	data := [2]string{"same\nold\nend", "same\nnew\nend"}
	for side := range inputs {
		ex := &compareContentExtractor{data: []byte(data[side])}
		inputs[side] = image.CompareInput{Ref: "mutable:tag", Resolver: &compareContentResolver{digest: "sha256:pinned", extractor: ex}}
		results[side] = image.CompareImage{Digest: "sha256:pinned", Analysis: &image.Analysis{Layers: []image.Layer{{}}}}
	}
	d := image.FileDiff{Path: "/file", DiffType: image.Modified, ChangeReason: "size"}
	before, after := &image.FileNode{Size: int64(len(data[0]))}, &image.FileNode{Size: int64(len(data[1]))}
	content, err := loadCompareContent(t.Context(), d, before, after, inputs, results)
	require.Empty(t, err)
	require.Len(t, content.rows, 4)
	assert.Equal(t, "same", content.rows[0].before)
	assert.Equal(t, "old", content.rows[1].before)
	assert.Zero(t, content.rows[1].afterLine)
	assert.Equal(t, "new", content.rows[2].after)
	for side := range inputs {
		assert.Equal(t, "sha256:pinned", inputs[side].Resolver.(*compareContentResolver).extractor.gotRef)
	}
	inputs[0].Resolver.(*compareContentResolver).digest = "sha256:changed"
	_, err = loadCompareContent(t.Context(), d, before, after, inputs, results)
	assert.Contains(t, err, "changed since analysis")
	inputs[0].Resolver.(*compareContentResolver).digest = "sha256:pinned"
	before.Size++
	_, err = loadCompareContent(t.Context(), d, before, after, inputs, results)
	assert.Contains(t, err, "truncated or changed")
}

func TestCompareContentRejectsUnsupportedPayloads(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		want string
	}{
		{"oversize", make([]byte, compareContentMaxBytes+1), "5 MiB"},
		{"invalid UTF-8", []byte{0xff}, "UTF-8"},
		{"binary", []byte{'a', 0, 'b'}, "binary"},
	} {
		t.Run(tc.name, func(t *testing.T) { assert.ErrorContains(t, validateCompareContent(tc.data), tc.want) })
	}
	_, err := compareContentDiff([]byte(strings.Repeat("x\n", compareContentMaxLines)), nil)
	assert.ErrorContains(t, err, "lines")
	_, err = compareContentDiff([]byte(strings.Repeat("x", compareContentMaxLineBytes+1)), nil)
	assert.ErrorContains(t, err, "line exceeds")
}

func TestCompareContentMetadataAndModalLifecycle(t *testing.T) {
	d := image.FileDiff{Path: "/link", DiffType: image.Modified, ChangeReason: "linkname"}
	content, err := loadCompareContent(t.Context(), d, &image.FileNode{Linkname: "/target"}, &image.FileNode{}, [2]image.CompareInput{}, [2]image.CompareImage{})
	require.Empty(t, err)
	assert.Contains(t, content.note, "Metadata changed")
	d.ChangeReason = "size,linkname"
	content, err = loadCompareContent(t.Context(), d, &image.FileNode{Linkname: "/target"}, &image.FileNode{}, [2]image.CompareInput{}, [2]image.CompareImage{})
	require.Empty(t, err)
	assert.Contains(t, content.note, "link or non-regular")
	m := compareModel{keys: defaultKeys(), theme: themeFor(""), width: 25, height: 7, contentOpen: true, contentRequest: 2, content: content}
	next, _ := m.Update(compareContentMsg{request: 1, err: "stale"})
	m = next.(compareModel)
	assert.Empty(t, m.contentError)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(compareModel)
	assert.False(t, m.contentOpen)
	for _, line := range strings.Split((&compareModel{theme: themeFor(""), width: 25, height: 7, content: content}).contentView().Content, "\n") {
		assert.LessOrEqual(t, ansi.StringWidth(line), 25)
	}
}

func TestCompareContentEscapesTerminalControls(t *testing.T) {
	rows, err := compareContentDiff([]byte("before\x1b[2J"), []byte("after\n"))
	require.NoError(t, err)
	m := compareModel{theme: themeFor(""), width: 90, height: 10, content: &compareContent{path: "/bad\nname", rows: rows}}
	out := m.contentView().Content
	assert.NotContains(t, out, "\x1b[2J")
	assert.Contains(t, out, "\\x1b")
	assert.Contains(t, out, "\\n")
}

func TestCompareContentNavigationAndResize(t *testing.T) {
	rows := make([]compareContentRow, 30)
	for i := range rows {
		rows[i] = compareContentRow{before: "same", after: "same", beforeLine: i + 1, afterLine: i + 1}
	}
	m := compareModel{keys: defaultKeys(), theme: themeFor(""), width: 95, height: 9, result: &image.CompareResult{}, contentOpen: true, content: &compareContent{rows: rows}}
	for range 20 {
		next, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
		m = next.(compareModel)
	}
	assert.Equal(t, 20, m.contentCursor)
	assert.Greater(t, m.contentOffset, 0)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 35, Height: 6})
	m = next.(compareModel)
	for _, line := range strings.Split(m.View().Content, "\n") {
		assert.LessOrEqual(t, ansi.StringWidth(line), 35)
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: 'g'})
	m = next.(compareModel)
	assert.Zero(t, m.contentCursor)
	assert.Zero(t, m.contentOffset)
}

func TestCompareContentCloseCancelsRequestAndIgnoresResult(t *testing.T) {
	ex := &compareContentExtractor{block: true}
	resolver := &compareContentResolver{digest: "sha256:pinned", extractor: ex}
	inputs := [2]image.CompareInput{{Ref: "tag", Resolver: resolver}}
	results := [2]image.CompareImage{{Digest: "sha256:pinned", Analysis: &image.Analysis{Layers: []image.Layer{{}}}}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	m := compareModel{session: &compareSession{ctx: ctx, inputs: inputs, results: results}, result: &image.CompareResult{FileDiffs: []image.FileDiff{{Path: "/file", DiffType: image.Removed}}}, fileIndex: &compareFileIndex{before: map[string]*image.FileNode{"/file": {Size: 1}}}, fileView: true}
	m.fileIndex.order[0] = []int{0}
	next, cmd := m.openContent()
	m = next.(compareModel)
	require.NotNil(t, cmd)
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	m.closeContent()
	msg := <-done
	next, _ = m.Update(msg)
	m = next.(compareModel)
	assert.False(t, m.contentOpen)
	assert.Nil(t, m.content)
	assert.Empty(t, m.contentError)
}

func TestCompareContentHorizontalScroll(t *testing.T) {
	for _, width := range []int{40, 100} {
		m := compareModel{keys: defaultKeys(), theme: themeFor(""), width: width, height: 10, content: &compareContent{rows: []compareContentRow{{after: strings.Repeat("x", 100) + "TARGET", afterLine: 1}}}}
		assert.NotContains(t, ansi.Strip(m.contentView().Content), "TARGET")
		for range 12 {
			m.updateContentKeys(tea.KeyPressMsg{Code: 'l'})
		}
		assert.Contains(t, ansi.Strip(m.contentView().Content), "TARGET")
		for _, line := range strings.Split(m.contentView().Content, "\n") {
			assert.LessOrEqual(t, ansi.StringWidth(line), width)
		}
		for range 20 {
			m.updateContentKeys(tea.KeyPressMsg{Code: tea.KeyLeft})
		}
		assert.Zero(t, m.contentHOffset)
	}
}
