package ci

import (
	"fmt"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/deveshctl/layerx/image"
)

// normalizePath converts tar / OS-specific paths into the canonical form
// used by every path-rule matcher: leading slash, forward separators.
func normalizePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// BlockPathRule fails for any path written by any layer that matches one
// of Patterns. Whiteout markers (`.wh.<name>` per-file tombstones and
// `.wh..wh..opq` opaque-directory markers) are skipped — they represent
// deletion events, not writes; the actual blob is in whichever earlier
// layer added the file, which this rule will surface there.
//
// Whiteout detection is name-based, not DiffType-based, because per-layer
// trees (`Layer.Tree`) come straight from `image.ParseLayerTar` which
// leaves every node at the default `DiffType=Unchanged`. Removed status
// is only assigned downstream in stack.go / compare.go, against stacked
// or comparison trees this rule does not consume. The DiffType==Removed
// skip remains as defensive symmetry for callers that hand-construct
// trees with explicit deletion markers.
type BlockPathRule struct {
	ID       string
	Patterns []string
}

func (r BlockPathRule) Name() string { return "block" }

func (r BlockPathRule) Evaluate(ctx EvalContext) []RuleResult {
	var results []RuleResult
	seen := make(map[string]struct{})

	for _, layer := range ctx.Layers {
		if layer.Tree == nil {
			continue
		}
		layer.Tree.Walk(func(node *image.FileNode) {
			if node == nil || node.IsDir {
				return
			}
			if image.IsWhiteoutName(node.Name) || node.DiffType == image.Removed {
				return
			}
			normalized := normalizePath(node.Path)
			for _, pat := range r.Patterns {
				ok, _ := doublestar.Match(pat, normalized)
				if !ok {
					continue
				}
				key := fmt.Sprintf("%d:%s", layer.Index, node.Path)
				if _, dup := seen[key]; dup {
					return
				}
				seen[key] = struct{}{}
				results = append(results, RuleResult{
					RuleID:    fmt.Sprintf("%s:%s@layer-%d", r.ID, node.Path, layer.Index),
					Name:      r.Name(),
					Kind:      RuleKindPath,
					Passed:    false,
					Actual:    node.Path,
					Threshold: pat,
					Detail:    fmt.Sprintf("layer %d (%s)", layer.Index, layer.ID),
				})
				return
			}
		})
	}

	if len(results) == 0 {
		return []RuleResult{{
			RuleID:    r.ID,
			Name:      r.Name(),
			Kind:      RuleKindPath,
			Passed:    true,
			Actual:    "0",
			Threshold: fmt.Sprintf("%d patterns", len(r.Patterns)),
		}}
	}
	return results
}

// DenyWastePathRule fails for any wasted file (LayerCount >= 2) whose path
// matches one of Patterns. Reads ctx.Efficiency.WastedFiles directly.
type DenyWastePathRule struct {
	ID       string
	Patterns []string
}

func (r DenyWastePathRule) Name() string { return "deny-waste" }

func (r DenyWastePathRule) Evaluate(ctx EvalContext) []RuleResult {
	var results []RuleResult
	if ctx.Efficiency != nil {
		for _, wf := range ctx.Efficiency.WastedFiles {
			normalized := normalizePath(wf.Path)
			for _, pat := range r.Patterns {
				ok, _ := doublestar.Match(pat, normalized)
				if !ok {
					continue
				}
				results = append(results, RuleResult{
					RuleID:    fmt.Sprintf("%s:%s", r.ID, wf.Path),
					Name:      r.Name(),
					Kind:      RuleKindPath,
					Passed:    false,
					Actual:    wf.Path,
					Threshold: pat,
					Detail:    fmt.Sprintf("(%d layers, %s)", wf.LayerCount, image.FormatBytes(wf.TotalWasted)),
				})
				break
			}
		}
	}
	if len(results) == 0 {
		return []RuleResult{{
			RuleID:    r.ID,
			Name:      r.Name(),
			Kind:      RuleKindPath,
			Passed:    true,
			Actual:    "0",
			Threshold: fmt.Sprintf("%d patterns", len(r.Patterns)),
		}}
	}
	return results
}

// MaxLayerCountRule fails for any wasted file appearing in more than
// MaxCount layers. MaxCount of 0 disables the rule (always passes) — the
// config layer prevents 0 in practice, but a defensive guard avoids
// divide-by-nothing surprises if a caller constructs a zero-value rule.
type MaxLayerCountRule struct {
	ID       string
	MaxCount int
}

func (r MaxLayerCountRule) Name() string { return "max-layer-count" }

func (r MaxLayerCountRule) Evaluate(ctx EvalContext) []RuleResult {
	if r.MaxCount <= 0 {
		return []RuleResult{{
			RuleID:    r.ID,
			Name:      r.Name(),
			Kind:      RuleKindPath,
			Passed:    true,
			Actual:    "disabled",
			Threshold: "disabled",
		}}
	}
	var results []RuleResult
	if ctx.Efficiency != nil {
		for _, wf := range ctx.Efficiency.WastedFiles {
			if wf.LayerCount > r.MaxCount {
				results = append(results, RuleResult{
					RuleID:    fmt.Sprintf("%s:%s", r.ID, wf.Path),
					Name:      r.Name(),
					Kind:      RuleKindPath,
					Passed:    false,
					Actual:    wf.Path,
					Threshold: fmt.Sprintf("%d layers", r.MaxCount),
					Detail:    fmt.Sprintf("(%d layers)", wf.LayerCount),
				})
			}
		}
	}
	if len(results) == 0 {
		return []RuleResult{{
			RuleID:    r.ID,
			Name:      r.Name(),
			Kind:      RuleKindPath,
			Passed:    true,
			Actual:    "0",
			Threshold: fmt.Sprintf("%d layers", r.MaxCount),
		}}
	}
	return results
}
