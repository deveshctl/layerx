package ci

import (
	"fmt"
	"math"

	"github.com/deveshctl/layerx/image"
)

// EvalContext bundles everything a rule might need to evaluate. Carrying
// Layers and StackedTrees on the context (rather than threading them through
// every rule's signature) keeps the existing efficiency-only rules simple
// while letting path-aware rules walk per-layer trees.
//
// Layers and StackedTrees may be nil — efficiency-only tests don't need
// them, and rules MUST nil-check before iterating.
type EvalContext struct {
	Efficiency   *image.EfficiencyResult
	TotalSize    int64
	Layers       []image.Layer
	StackedTrees []*image.FileTree
}

// Rule evaluates one aspect of image efficiency. A rule may produce multiple
// RuleResults (e.g. one per matched path for BlockPathRule); efficiency rules
// always return exactly one.
//
// When invoked through the package-level Evaluate(ctx, rules) function,
// ctx.Efficiency is guaranteed non-nil — the evaluator nil-guards before
// running rules so each rule body can dereference ctx.Efficiency directly.
// Tests calling rule.Evaluate(EvalContext{}) directly must populate
// ctx.Efficiency themselves; rules nil-check ctx.Layers and
// ctx.StackedTrees because those are optional even in production.
type Rule interface {
	Name() string
	Evaluate(ctx EvalContext) []RuleResult
}

// RuleKind classifies a rule's reporting bucket. Report.Print uses Kind to
// route results into the "Global Rules:" / "Path Rules:" output sections —
// each rule stamps Kind onto every RuleResult it emits, so the printer never
// needs to consult an allowlist or the Rule that produced the result.
type RuleKind int

const (
	// RuleKindGlobal — rules that evaluate one image-wide property
	// (efficiency score, total wasted bytes). Always emit a single
	// RuleResult; render under the "Global Rules:" section.
	RuleKindGlobal RuleKind = iota

	// RuleKindPath — rules that match against per-layer trees or wasted
	// files. Emit one RuleResult per finding (or one PASS result if no
	// findings); render under the "Path Rules:" section.
	RuleKindPath
)

// RuleResult holds the outcome of evaluating a single rule.
//
//   - RuleID identifies the specific finding for log-grep / future
//     --rule-id filtering. For globals, RuleID equals Name. For path
//     rules, RuleID typically encodes the location of the finding
//     (e.g. "block:/root/.cache@layer-3" or "deny-pyc:/usr/lib/foo.pyc").
//   - Name is the human-readable rule kind shown in the report column
//     ("efficiency", "block", "deny-waste"). Multiple findings from the
//     same rule share the same Name.
//   - Kind controls which output section (Global vs Path Rules) the
//     printer routes the result into.
//   - Actual / Threshold render in the standard "X (threshold: Y)"
//     format. For path rules, Actual is the bare path; the layer index,
//     count, and byte size live in Detail.
//   - Detail is optional context shown between Actual and the threshold
//     parens for path rules — file path qualifier, layer location, etc.
//     Globals leave Detail empty; the printer skips it for global
//     results.
type RuleResult struct {
	RuleID    string
	Name      string
	Kind      RuleKind
	Passed    bool
	Actual    string
	Threshold string
	Detail    string
}

// LowestEfficiency fails if the efficiency score is below the threshold.
// A threshold of 0 (or any non-positive value) disables this rule. A NaN
// score (e.g. an empty image with zero total bytes) is treated as a pass —
// the rule cannot meaningfully evaluate a non-finite score.
type LowestEfficiency struct {
	Threshold float64
}

func (r LowestEfficiency) Name() string { return "efficiency" }

func (r LowestEfficiency) Evaluate(ctx EvalContext) []RuleResult {
	result := ctx.Efficiency
	passed := true
	if r.Threshold > 0 && !math.IsNaN(result.Score) {
		passed = result.Score >= r.Threshold
	}
	return []RuleResult{{
		RuleID:    r.Name(),
		Name:      r.Name(),
		Kind:      RuleKindGlobal,
		Passed:    passed,
		Actual:    fmt.Sprintf("%.1f%%", result.Score*100),
		Threshold: fmt.Sprintf("%.1f%%", r.Threshold*100),
	}}
}

type HighestWastedBytes struct {
	Threshold int64
}

func (r HighestWastedBytes) Name() string { return "wasted bytes" }

func (r HighestWastedBytes) Evaluate(ctx EvalContext) []RuleResult {
	result := ctx.Efficiency
	passed := true
	if r.Threshold > 0 {
		passed = result.WastedBytes <= r.Threshold
	}
	return []RuleResult{{
		RuleID:    r.Name(),
		Name:      r.Name(),
		Kind:      RuleKindGlobal,
		Passed:    passed,
		Actual:    image.FormatBytes(result.WastedBytes),
		Threshold: image.FormatBytes(r.Threshold),
	}}
}

type HighestUserWastedPercent struct {
	Threshold float64
}

func (r HighestUserWastedPercent) Name() string { return "wasted %" }

func (r HighestUserWastedPercent) Evaluate(ctx EvalContext) []RuleResult {
	result := ctx.Efficiency
	totalSize := ctx.TotalSize
	var pct float64
	if totalSize > 0 {
		pct = float64(result.WastedBytes) / float64(totalSize)
	}
	passed := true
	if r.Threshold > 0 {
		passed = pct <= r.Threshold
	}
	return []RuleResult{{
		RuleID:    r.Name(),
		Name:      r.Name(),
		Kind:      RuleKindGlobal,
		Passed:    passed,
		Actual:    fmt.Sprintf("%.1f%%", pct*100),
		Threshold: fmt.Sprintf("%.1f%%", r.Threshold*100),
	}}
}
