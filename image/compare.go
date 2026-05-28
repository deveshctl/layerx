package image

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// scoreEpsilon absorbs float drift in efficiency-score comparisons.
const scoreEpsilon = 1e-9

// ImageSummary captures top-level identity and size of an analyzed image.
type ImageSummary struct {
	ImageRef   string
	LayerCount int
	TotalSize  int64
}

// EfficiencySummary mirrors EfficiencyResult plus deltas relative to a peer.
// Percent fields are 0 when their denominator is 0 (no sentinel value).
type EfficiencySummary struct {
	Score              float64
	WastedBytes        int64
	ScoreDelta         float64
	ScoreDeltaPercent  float64
	WastedBytesDelta   int64
	WastedBytesPercent float64
}

// LayerDiff is a side-by-side comparison of two layers at the same index.
//
// Index-based matching is the only stable option for Docker layers across
// rebuilds: a layer inserted in the middle of a Dockerfile shifts every
// subsequent index. CommandsMatch and the command-divergence warning surface
// that condition so consumers can interpret later rows skeptically.
type LayerDiff struct {
	Index          int
	BeforeSize     int64
	AfterSize      int64
	SizeDelta      int64
	BeforeNetDelta int64
	AfterNetDelta  int64
	NetDeltaDiff   int64
	BeforeCommand  string
	AfterCommand   string
	CommandsMatch  bool
}

// FileDiff records a change to one file in the final stacked filesystem.
// ChangeReason is set only for Modified entries and lists the differing
// fields in the canonical order: size,mode,uid,gid,linkname,hardlink.
type FileDiff struct {
	Path         string
	DiffType     DiffType
	BeforeSize   int64
	AfterSize    int64
	SizeDelta    int64
	ChangeReason string
}

// FileDiffSummary aggregates FileDiffs into counts and byte totals.
type FileDiffSummary struct {
	AddedCount    int
	RemovedCount  int
	ModifiedCount int
	BytesAdded    int64
	BytesRemoved  int64
}

// WasteDiff captures how a single path's contribution to wasted bytes
// shifted between two images.
type WasteDiff struct {
	Path         string
	BeforeWasted int64
	AfterWasted  int64
	WastedDelta  int64
}

// CompareResult is the aggregate output of CompareAnalysis.
type CompareResult struct {
	Before ImageSummary
	After  ImageSummary

	BeforeEfficiency EfficiencySummary
	AfterEfficiency  EfficiencySummary

	LayerDiffs  []LayerDiff
	FileDiffs   []FileDiff
	WasteDiffs  []WasteDiff
	FileSummary FileDiffSummary

	Warnings []string
}

// CompareAnalysis computes a diff between two image analyses. Either input
// may be nil — a nil analysis is treated as an empty image (no layers, no
// files), so "compare against nothing" is symmetric with regular diffs.
// Returns nil only when both inputs are nil. CompareAnalysis never mutates
// its inputs.
func CompareAnalysis(before, after *Analysis) *CompareResult {
	if before == nil && after == nil {
		return nil
	}
	if before == nil {
		before = &Analysis{}
	}
	if after == nil {
		after = &Analysis{}
	}

	beforeEff := Efficiency(before.Layers)
	afterEff := Efficiency(after.Layers)

	r := &CompareResult{
		Before: ImageSummary{
			ImageRef:   before.ImageRef,
			LayerCount: len(before.Layers),
			TotalSize:  before.TotalSize,
		},
		After: ImageSummary{
			ImageRef:   after.ImageRef,
			LayerCount: len(after.Layers),
			TotalSize:  after.TotalSize,
		},
		BeforeEfficiency: efficiencySummary(beforeEff, nil),
		AfterEfficiency:  efficiencySummary(afterEff, beforeEff),
	}

	r.LayerDiffs = buildLayerDiffs(before.Layers, after.Layers)
	r.FileDiffs, r.FileSummary = buildFileDiffs(finalStacked(before), finalStacked(after))
	r.WasteDiffs = buildWasteDiffs(beforeEff, afterEff)
	r.Warnings = buildWarnings(before.Layers, after.Layers, r.LayerDiffs)

	return r
}

// IsRegression reports whether `after` is materially worse than `before`:
// wasted bytes increased OR efficiency score dropped beyond a small epsilon.
// Returns false on a nil receiver.
func (r *CompareResult) IsRegression() bool {
	if r == nil {
		return false
	}
	if r.AfterEfficiency.WastedBytes > r.BeforeEfficiency.WastedBytes {
		return true
	}
	if r.BeforeEfficiency.Score-r.AfterEfficiency.Score > scoreEpsilon {
		return true
	}
	return false
}

func efficiencySummary(self, peer *EfficiencyResult) EfficiencySummary {
	s := EfficiencySummary{
		Score:       self.Score,
		WastedBytes: self.WastedBytes,
	}
	if peer == nil {
		return s
	}
	s.ScoreDelta = self.Score - peer.Score
	s.WastedBytesDelta = self.WastedBytes - peer.WastedBytes
	if peer.Score != 0 {
		s.ScoreDeltaPercent = 100 * s.ScoreDelta / peer.Score
	}
	if peer.WastedBytes != 0 {
		s.WastedBytesPercent = 100 * float64(s.WastedBytesDelta) / float64(peer.WastedBytes)
	}
	return s
}

func buildLayerDiffs(before, after []Layer) []LayerDiff {
	n := max(len(before), len(after))
	if n == 0 {
		return nil
	}
	out := make([]LayerDiff, n)
	for i := range n {
		d := LayerDiff{Index: i}
		bothPresent := i < len(before) && i < len(after)
		if i < len(before) {
			d.BeforeSize = before[i].Size
			d.BeforeNetDelta = before[i].NetDelta
			d.BeforeCommand = before[i].Command
		}
		if i < len(after) {
			d.AfterSize = after[i].Size
			d.AfterNetDelta = after[i].NetDelta
			d.AfterCommand = after[i].Command
		}
		d.SizeDelta = d.AfterSize - d.BeforeSize
		d.NetDeltaDiff = d.AfterNetDelta - d.BeforeNetDelta
		d.CommandsMatch = bothPresent && d.BeforeCommand == d.AfterCommand
		out[i] = d
	}
	return out
}

// fileSnapshot captures the metadata fields that define file identity for
// Modified detection. IntroducedInLayer is intentionally excluded — it
// shifts on every rebuild and is not a content/identity signal.
type fileSnapshot struct {
	Size       int64
	Mode       fs.FileMode
	UID        int
	GID        int
	Linkname   string
	IsHardlink bool
}

// finalStacked returns the last stacked tree of an analysis, or nil if there
// isn't one. The last entry represents the live filesystem of the image.
func finalStacked(a *Analysis) *FileTree {
	if a == nil || len(a.StackedTrees) == 0 {
		return nil
	}
	return a.StackedTrees[len(a.StackedTrees)-1]
}

// collectLiveFiles walks a final stacked tree and records every live file.
// Skipped: directories (not files), whiteout entries (control files, not
// real entries), and Removed clones (the stacked tree retains those for TUI
// visibility but they are not part of the live filesystem).
func collectLiveFiles(tree *FileTree) map[string]fileSnapshot {
	out := make(map[string]fileSnapshot)
	if tree == nil || tree.Root == nil {
		return out
	}
	var walk func(n *FileNode)
	walk = func(n *FileNode) {
		for _, c := range n.Children {
			if isWhiteoutName(c.Name) {
				continue
			}
			if c.DiffType == Removed {
				continue
			}
			if c.IsDir {
				walk(c)
				continue
			}
			out[c.Path] = fileSnapshot{
				Size:       c.Size,
				Mode:       c.Mode,
				UID:        c.UID,
				GID:        c.GID,
				Linkname:   c.Linkname,
				IsHardlink: c.IsHardlink,
			}
		}
	}
	walk(tree.Root)
	return out
}

func buildFileDiffs(before, after *FileTree) ([]FileDiff, FileDiffSummary) {
	beforeMap := collectLiveFiles(before)
	afterMap := collectLiveFiles(after)

	var diffs []FileDiff
	var sum FileDiffSummary

	for path, b := range beforeMap {
		a, ok := afterMap[path]
		if !ok {
			diffs = append(diffs, FileDiff{
				Path:       path,
				DiffType:   Removed,
				BeforeSize: b.Size,
				SizeDelta:  -b.Size,
			})
			sum.RemovedCount++
			sum.BytesRemoved += b.Size
			continue
		}
		reason := changedFields(b, a)
		if reason == "" {
			continue
		}
		delta := a.Size - b.Size
		diffs = append(diffs, FileDiff{
			Path:         path,
			DiffType:     Modified,
			BeforeSize:   b.Size,
			AfterSize:    a.Size,
			SizeDelta:    delta,
			ChangeReason: reason,
		})
		sum.ModifiedCount++
		if delta > 0 {
			sum.BytesAdded += delta
		} else if delta < 0 {
			sum.BytesRemoved += -delta
		}
	}

	for path, a := range afterMap {
		if _, ok := beforeMap[path]; ok {
			continue
		}
		diffs = append(diffs, FileDiff{
			Path:      path,
			DiffType:  Added,
			AfterSize: a.Size,
			SizeDelta: a.Size,
		})
		sum.AddedCount++
		sum.BytesAdded += a.Size
	}

	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].Path < diffs[j].Path
	})
	return diffs, sum
}

// changedFields returns a comma-joined list of the fields differing between
// b and a, in the canonical order size,mode,uid,gid,linkname,hardlink. An
// empty string means the snapshots are equal under our identity rules.
func changedFields(b, a fileSnapshot) string {
	var fields []string
	if b.Size != a.Size {
		fields = append(fields, "size")
	}
	if b.Mode != a.Mode {
		fields = append(fields, "mode")
	}
	if b.UID != a.UID {
		fields = append(fields, "uid")
	}
	if b.GID != a.GID {
		fields = append(fields, "gid")
	}
	if b.Linkname != a.Linkname {
		fields = append(fields, "linkname")
	}
	if b.IsHardlink != a.IsHardlink {
		fields = append(fields, "hardlink")
	}
	return strings.Join(fields, ",")
}

func buildWasteDiffs(before, after *EfficiencyResult) []WasteDiff {
	beforeMap := wastedFilesMap(before)
	afterMap := wastedFilesMap(after)

	seen := make(map[string]struct{}, len(beforeMap)+len(afterMap))
	for p := range beforeMap {
		seen[p] = struct{}{}
	}
	for p := range afterMap {
		seen[p] = struct{}{}
	}

	var out []WasteDiff
	for path := range seen {
		bw := beforeMap[path]
		aw := afterMap[path]
		delta := aw - bw
		if delta == 0 {
			continue
		}
		out = append(out, WasteDiff{
			Path:         path,
			BeforeWasted: bw,
			AfterWasted:  aw,
			WastedDelta:  delta,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		ai, aj := absInt64(out[i].WastedDelta), absInt64(out[j].WastedDelta)
		if ai != aj {
			return ai > aj
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func wastedFilesMap(r *EfficiencyResult) map[string]int64 {
	m := make(map[string]int64)
	if r == nil {
		return m
	}
	for _, wf := range r.WastedFiles {
		m[wf.Path] = wf.TotalWasted
	}
	return m
}

func absInt64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func buildWarnings(before, after []Layer, diffs []LayerDiff) []string {
	var out []string
	if len(before) != len(after) {
		out = append(out, fmt.Sprintf("layer count differs: before=%d, after=%d", len(before), len(after)))
	}
	for _, d := range diffs {
		bothPresent := d.Index < len(before) && d.Index < len(after)
		if !bothPresent {
			continue
		}
		if !d.CommandsMatch {
			out = append(out, fmt.Sprintf("layer %d command differs (rebuild may have shifted layers; later indexes may be misaligned)", d.Index))
			break
		}
	}
	return out
}
