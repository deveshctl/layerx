package image

import (
	"io/fs"
	"sort"
	"strings"
)

// scoreEpsilon absorbs float drift in efficiency-score comparisons.
const scoreEpsilon = 1e-9

type ImageSummary struct {
	ImageRef         string `json:"imageRef"`
	LayerCount       int    `json:"layerCount"`
	TotalSize        int64  `json:"storedLayerBytes"`
	UncompressedSize int64  `json:"uncompressedBytes"`
	Platform         string `json:"platform"`
}

// EfficiencySummary mirrors EfficiencyResult plus deltas relative to a peer.
// Percent fields are 0 when their denominator is 0 (no sentinel value).
type EfficiencySummary struct {
	Score              float64 `json:"score"`
	WastedBytes        int64   `json:"wastedBytes"`
	ScoreDelta         float64 `json:"scoreDelta"`
	ScoreDeltaPercent  float64 `json:"scoreDeltaPercent"`
	WastedBytesDelta   int64   `json:"wastedBytesDelta"`
	WastedBytesPercent float64 `json:"wastedBytesPercent"`
}

// LayerDiff pairs shared uncompressed content or unique command text.
// A missing side is indexed -1. Index is retained for deterministic ranking.
type LayerDiff struct {
	Index          int    `json:"index"`
	BeforeIndex    int    `json:"beforeIndex"`
	AfterIndex     int    `json:"afterIndex"`
	Match          string `json:"match"` // shared, command, added, removed
	BeforeSize     int64  `json:"beforeSize"`
	AfterSize      int64  `json:"afterSize"`
	SizeDelta      int64  `json:"sizeDelta"`
	BeforeNetDelta int64  `json:"beforeNetDelta"`
	AfterNetDelta  int64  `json:"afterNetDelta"`
	NetDeltaDiff   int64  `json:"netDeltaDiff"`
	BeforeCommand  string `json:"beforeCommand"`
	AfterCommand   string `json:"afterCommand"`
	CommandsMatch  bool   `json:"commandsMatch"`
}

// FileDiff records a change to one file in the final stacked filesystem.
// ChangeReason is set only for Modified entries and lists the differing
// fields in the canonical order: size,mode,uid,gid,linkname,hardlink.
type FileDiff struct {
	Path         string   `json:"path"`
	DiffType     DiffType `json:"diffType"`
	BeforeSize   int64    `json:"beforeSize"`
	AfterSize    int64    `json:"afterSize"`
	SizeDelta    int64    `json:"sizeDelta"`
	ChangeReason string   `json:"changeReason"`
}

type FileDiffSummary struct {
	AddedCount    int   `json:"addedCount"`
	RemovedCount  int   `json:"removedCount"`
	ModifiedCount int   `json:"modifiedCount"`
	BytesAdded    int64 `json:"bytesAdded"`
	BytesRemoved  int64 `json:"bytesRemoved"`
}

type WasteDiff struct {
	Path         string `json:"path"`
	BeforeWasted int64  `json:"beforeWasted"`
	AfterWasted  int64  `json:"afterWasted"`
	WastedDelta  int64  `json:"wastedDelta"`
}

type CompareResult struct {
	Before ImageSummary `json:"before"`
	After  ImageSummary `json:"after"`

	BeforeEfficiency EfficiencySummary `json:"beforeEfficiency"`
	AfterEfficiency  EfficiencySummary `json:"afterEfficiency"`

	LayerDiffs  []LayerDiff     `json:"layerDiffs"`
	FileDiffs   []FileDiff      `json:"fileDiffs"`
	WasteDiffs  []WasteDiff     `json:"wasteDiffs"`
	FileSummary FileDiffSummary `json:"fileSummary"`

	Warnings []string `json:"warnings"`
}

// CompareAnalysis computes a diff between two image analyses. Either input
// may be nil — a nil analysis is treated as an empty image (no layers, no
// files), so "compare against nothing" is symmetric with regular diffs.
// Returns nil only when both inputs are nil. CompareAnalysis never mutates
// its inputs.
//
// When one side is empty (nil or zero layers), Efficiency() returns the
// degenerate Score=1.0 baseline for that side. To prevent that phantom
// baseline from making the populated side look "worse" (a spurious
// regression in the verdict), the EfficiencySummary deltas are zeroed when
// either side has no layers — the diff still surfaces every Added/Removed
// file, but score/waste deltas only carry meaning when both sides are real.
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

	beforeEff := EfficiencyFromAnalysis(before)
	afterEff := EfficiencyFromAnalysis(after)

	bothPopulated := len(before.Layers) > 0 && len(after.Layers) > 0
	var afterPeer *EfficiencyResult
	if bothPopulated {
		afterPeer = beforeEff
	}

	r := &CompareResult{
		Before: ImageSummary{
			ImageRef:         before.ImageRef,
			LayerCount:       len(before.Layers),
			TotalSize:        before.TotalSize,
			UncompressedSize: before.UncompressedSize,
			Platform:         before.Platform,
		},
		After: ImageSummary{
			ImageRef:         after.ImageRef,
			LayerCount:       len(after.Layers),
			TotalSize:        after.TotalSize,
			UncompressedSize: after.UncompressedSize,
			Platform:         after.Platform,
		},
		BeforeEfficiency: efficiencySummary(beforeEff, nil),
		AfterEfficiency:  efficiencySummary(afterEff, afterPeer),
	}

	r.LayerDiffs = buildLayerDiffs(before.Layers, after.Layers)
	r.FileDiffs, r.FileSummary = buildFileDiffs(finalStacked(before), finalStacked(after))
	r.WasteDiffs = buildWasteDiffs(beforeEff, afterEff)
	r.Warnings = buildWarnings(before.Layers, after.Layers, r.LayerDiffs)

	return r
}

// IsRegression reports whether `after` is materially worse than `before`:
// wasted bytes increased OR efficiency score dropped beyond a small epsilon.
// Returns false on a nil receiver. Reads the precomputed delta fields so
// the verdict and the rendered numbers share one source of truth.
func (r *CompareResult) IsRegression() bool {
	if r == nil {
		return false
	}
	if -r.AfterEfficiency.ScoreDelta > scoreEpsilon {
		return true
	}
	if r.AfterEfficiency.WastedBytesDelta > 0 {
		return true
	}
	return false
}

// RegressionReasons returns the canonical list of regression reasons, in
// the stable order ["efficiency", "waste"]. Empty slice when no regression.
// Defined alongside IsRegression so the verdict line and the gate share a
// single decision; callers must not recompute reasons from raw fields.
func (r *CompareResult) RegressionReasons() []string {
	if r == nil {
		return nil
	}
	var reasons []string
	if -r.AfterEfficiency.ScoreDelta > scoreEpsilon {
		reasons = append(reasons, "efficiency")
	}
	if r.AfterEfficiency.WastedBytesDelta > 0 {
		reasons = append(reasons, "waste")
	}
	return reasons
}

func efficiencySummary(self, peer *EfficiencyResult) EfficiencySummary {
	if self == nil {
		return EfficiencySummary{}
	}
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

// Match identical uncompressed streams first. Only unique remaining commands
// can be paired; ambiguous/repeated commands remain explicitly unpaired.
func buildLayerDiffs(before, after []Layer) []LayerDiff {
	used := make([]bool, len(before))
	pairs := make([]int, len(after))
	for i := range pairs {
		pairs[i] = -1
	}
	kinds := make([]string, len(after))
	byHash := make(map[string][]int)
	for i, l := range before {
		if l.DiffID != "" {
			byHash[l.DiffID] = append(byHash[l.DiffID], i)
		}
	}
	for i, l := range after {
		if l.DiffID == "" {
			continue
		}
		candidates := byHash[l.DiffID]
		if len(candidates) > 0 {
			j := candidates[0]
			byHash[l.DiffID] = candidates[1:]
			pairs[i] = j
			used[j] = true
			kinds[i] = "shared"
		}
	}
	oldCommands, newCommands := make(map[string][]int), make(map[string][]int)
	for i, l := range before {
		if !used[i] && l.Command != "" {
			oldCommands[l.Command] = append(oldCommands[l.Command], i)
		}
	}
	for i, l := range after {
		if pairs[i] < 0 && l.Command != "" {
			newCommands[l.Command] = append(newCommands[l.Command], i)
		}
	}
	for command, indices := range newCommands {
		old := oldCommands[command]
		if len(indices) == 1 && len(old) == 1 {
			i, j := indices[0], old[0]
			pairs[i] = j
			used[j] = true
			kinds[i] = "command"
		}
	}
	var out []LayerDiff
	for i, l := range after {
		d := LayerDiff{Index: i, BeforeIndex: -1, AfterIndex: i, Match: "added", AfterSize: l.UncompressedSize, AfterCommand: l.Command, AfterNetDelta: l.NetDelta}
		if j := pairs[i]; j >= 0 {
			b := before[j]
			d.BeforeIndex = j
			d.BeforeSize = b.UncompressedSize
			d.BeforeCommand = b.Command
			d.BeforeNetDelta = b.NetDelta
			d.CommandsMatch = b.Command == l.Command
			d.Match = kinds[i]
		}
		d.SizeDelta = d.AfterSize - d.BeforeSize
		d.NetDeltaDiff = d.AfterNetDelta - d.BeforeNetDelta
		out = append(out, d)
	}
	for i, l := range before {
		if !used[i] {
			out = append(out, LayerDiff{Index: i, BeforeIndex: i, AfterIndex: -1, Match: "removed", BeforeSize: l.UncompressedSize, SizeDelta: -l.UncompressedSize, BeforeCommand: l.Command, BeforeNetDelta: l.NetDelta, NetDeltaDiff: -l.NetDelta})
		}
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
	out := []string{"File changes compare size and metadata, not file contents; same-size content changes may be absent."}
	for _, d := range diffs {
		if d.Match == "command" {
			out = append(out, "Changed layers are paired by unique command text; matching commands do not prove matching build steps.")
			break
		}
	}
	return out
}
