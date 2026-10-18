package image

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExplainWasteAttribution(t *testing.T) {
	write := func(size int64) *FileTree { return makeTree(makeFile("payload", "/payload", size)) }
	remove := func() *FileTree { return makeTree(makeFile(".wh.payload", "/.wh.payload", 0)) }
	tests := []struct {
		name    string
		trees   []*FileTree
		waste   int64
		charged []int64
		layers  []int
	}{
		{"single live copy", []*FileTree{write(100)}, 0, []int64{0}, []int{0}},
		{"overwritten", []*FileTree{write(100), write(80)}, 100, []int64{100, 0}, []int{0, 1}},
		{"deleted", []*FileTree{write(100), remove(), nil}, 100, []int64{100, 0}, []int{0, 1}},
		{"reinstalled", []*FileTree{write(100), remove(), write(60)}, 100, []int64{100, 0, 0}, []int{0, 1, 2}},
		{"rewritten then deleted", []*FileTree{write(100), write(80), remove()}, 180, []int64{100, 80, 0}, []int{0, 1, 2}},
		{"opaque deletion", []*FileTree{write(100), makeTree(makeFile(".wh..wh..opq", "/.wh..wh..opq", 0))}, 100, []int64{100, 0}, []int{0, 1}},
		{"unchanged carryover", []*FileTree{write(100), nil, write(80)}, 100, []int64{100, 0}, []int{0, 2}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := &Analysis{}
			for i, tree := range tc.trees {
				a.Layers = append(a.Layers, Layer{Tree: tree, Command: "RUN build", Index: i})
			}
			// Missing snapshots must use the same fallback as the efficiency score.
			e := ExplainWaste(a, "/payload")
			assert.Equal(t, tc.waste, e.TotalWasted)
			assert.Equal(t, EfficiencyFromAnalysis(a).WastedBytes, e.TotalWasted)
			require.Len(t, e.Events, len(tc.charged))
			var sum int64
			for i, event := range e.Events {
				assert.Equal(t, tc.layers[i], event.Layer)
				assert.Equal(t, tc.charged[i], event.WastedBytes)
				assert.Equal(t, "RUN build", event.Command)
				sum += event.WastedBytes
			}
			assert.Equal(t, e.TotalWasted, sum)
		})
	}
}

func TestExplainWasteReasonsAndHardlink(t *testing.T) {
	a := &Analysis{Layers: []Layer{
		{Tree: makeTree(makeFile("payload", "/payload", 100))},
		{Tree: makeTree(makeFile("payload", "/payload", 80))},
		{Tree: makeTree(makeFile(".wh.payload", "/.wh.payload", 0))},
	}}
	e := ExplainWaste(a, "/payload")
	require.Len(t, e.Events, 3)
	assert.Equal(t, "Overwritten in L2", e.Events[0].Reason)
	assert.Equal(t, "Deleted in L3", e.Events[1].Reason)
	assert.Equal(t, Removed, e.Events[2].Change)
	assert.Zero(t, e.Events[2].Size)

	alias := makeFile("alias", "/alias", 0)
	alias.IsHardlink, alias.Linkname = true, "payload"
	a.Layers[0].Tree.Root.AddChild(alias)
	a.Layers = append(a.Layers[:1], a.Layers[2])
	e = ExplainWaste(a, "/payload")
	require.Len(t, e.Events, 2)
	assert.Zero(t, e.TotalWasted)
	assert.Contains(t, e.Events[0].Reason, "surviving hardlink")
	assert.Equal(t, EfficiencyFromAnalysis(a).WastedBytes, e.TotalWasted)
}

func TestExplainWasteMissingHistory(t *testing.T) {
	assert.Empty(t, ExplainWaste(nil, "/missing").Events)
	a := &Analysis{Layers: []Layer{{Tree: NewFileTree()}}}
	assert.Empty(t, ExplainWaste(a, "/missing").Events)
}
