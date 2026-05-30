package ci

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluate_AllPass(t *testing.T) {
	eff := &image.EfficiencyResult{
		Score:       0.95,
		WastedBytes: 100,
		WastedFiles: []image.WastedFile{},
	}
	rules := []Rule{
		LowestEfficiency{Threshold: 0.9},
		HighestWastedBytes{Threshold: 1000},
		HighestUserWastedPercent{Threshold: 0.1},
	}
	report := Evaluate(EvalContext{Efficiency: eff, TotalSize: 10000}, rules)
	assert.True(t, report.Passed)
	assert.Equal(t, 0, report.ExitCode())
}

func TestEvaluate_OneFails(t *testing.T) {
	eff := &image.EfficiencyResult{
		Score:       0.80,
		WastedBytes: 100,
		WastedFiles: []image.WastedFile{},
	}
	rules := []Rule{
		LowestEfficiency{Threshold: 0.9},
		HighestWastedBytes{Threshold: 1000},
	}
	report := Evaluate(EvalContext{Efficiency: eff, TotalSize: 10000}, rules)
	assert.False(t, report.Passed)
	assert.Equal(t, 1, report.ExitCode())
}

func TestEvaluate_AllFail(t *testing.T) {
	eff := &image.EfficiencyResult{
		Score:       0.50,
		WastedBytes: 5000,
		WastedFiles: []image.WastedFile{
			{Path: "/tmp/big.tar", TotalWasted: 3000, LayerCount: 3},
		},
	}
	rules := []Rule{
		LowestEfficiency{Threshold: 0.9},
		HighestWastedBytes{Threshold: 1000},
		HighestUserWastedPercent{Threshold: 0.1},
	}
	report := Evaluate(EvalContext{Efficiency: eff, TotalSize: 10000}, rules)
	assert.False(t, report.Passed)
	assert.Equal(t, 1, report.ExitCode())
	assert.Equal(t, 3, len(report.Results))
}

func TestReport_Print_Pass(t *testing.T) {
	eff := &image.EfficiencyResult{
		Score:       0.95,
		WastedBytes: 0,
		WastedFiles: []image.WastedFile{},
	}
	report := Evaluate(EvalContext{Efficiency: eff, TotalSize: 1000}, []Rule{LowestEfficiency{Threshold: 0.9}})
	var buf bytes.Buffer
	report.Print(&buf)
	assert.Contains(t, buf.String(), "PASS")
	assert.Contains(t, buf.String(), "95%")
}

func TestReport_Print_Fail(t *testing.T) {
	eff := &image.EfficiencyResult{
		Score:       0.80,
		WastedBytes: 5000,
		WastedFiles: []image.WastedFile{
			{Path: "/var/cache/apt/foo.deb", TotalWasted: 3000, LayerCount: 2},
			{Path: "/tmp/output.tar", TotalWasted: 2000, LayerCount: 3},
		},
	}
	rules := []Rule{
		LowestEfficiency{Threshold: 0.9},
	}
	report := Evaluate(EvalContext{Efficiency: eff, TotalSize: 10000}, rules)
	var buf bytes.Buffer
	report.Print(&buf)

	output := buf.String()
	assert.Contains(t, output, "FAIL")
	assert.Contains(t, output, "efficiency:")
	assert.Contains(t, output, "Top wasted files:")
	assert.Contains(t, output, "/var/cache/apt/foo.deb")
}

func TestEvaluate_TopWasteLimitedTo10(t *testing.T) {
	var files []image.WastedFile
	for i := range 15 {
		files = append(files, image.WastedFile{
			Path:        fmt.Sprintf("/file%d", i),
			TotalWasted: int64(100 - i),
			LayerCount:  2,
		})
	}
	eff := &image.EfficiencyResult{
		Score:       0.5,
		WastedBytes: 1500,
		WastedFiles: files,
	}
	report := Evaluate(EvalContext{Efficiency: eff, TotalSize: 3000}, []Rule{LowestEfficiency{Threshold: 0.9}})
	require.Len(t, report.TopWaste, 10)
}

func TestEvaluate_NoRules(t *testing.T) {
	eff := &image.EfficiencyResult{
		Score:       0.5,
		WastedBytes: 1000,
		WastedFiles: []image.WastedFile{},
	}
	report := Evaluate(EvalContext{Efficiency: eff, TotalSize: 2000}, []Rule{})
	assert.True(t, report.Passed)
	assert.Equal(t, 0, report.ExitCode())
}

func TestEvaluate_TopWasteIsIndependent(t *testing.T) {
	files := []image.WastedFile{
		{Path: "/a", TotalWasted: 300, LayerCount: 2},
		{Path: "/b", TotalWasted: 200, LayerCount: 2},
		{Path: "/c", TotalWasted: 100, LayerCount: 2},
	}
	eff := &image.EfficiencyResult{
		Score:       0.5,
		WastedBytes: 600,
		WastedFiles: files,
	}
	report := Evaluate(EvalContext{Efficiency: eff, TotalSize: 1200}, nil)
	require.Len(t, report.TopWaste, 3)
	assert.Equal(t, "/a", report.TopWaste[0].Path)

	// Mutating the original slice must not corrupt the report.
	eff.WastedFiles[0].Path = "MUTATED"
	eff.WastedFiles[0].TotalWasted = -1
	assert.Equal(t, "/a", report.TopWaste[0].Path, "TopWaste must own its backing array")
	assert.Equal(t, int64(300), report.TopWaste[0].TotalWasted)
}

// One BlockPathRule matching N paths produces N entries in Report.Results,
// each with Passed: false. This pins the per-violation contract that the
// report printer relies on.
func TestEvaluate_OneViolationPerResult(t *testing.T) {
	layer := image.Layer{
		Index: 0,
		ID:    "lay0",
		Tree:  image.NewFileTree(),
	}
	for _, p := range []string{"/tmp/a", "/tmp/b", "/tmp/c", "/tmp/d", "/tmp/e"} {
		layer.Tree.Root.AddChild(&image.FileNode{
			Name: p, Path: p, DiffType: image.Added,
		})
	}
	r := BlockPathRule{ID: "block", Patterns: []string{"/tmp/**"}}
	report := Evaluate(EvalContext{
		Efficiency: &image.EfficiencyResult{},
		Layers:     []image.Layer{layer},
	}, []Rule{r})
	failures := 0
	for _, r := range report.Results {
		if !r.Passed {
			failures++
		}
	}
	assert.Equal(t, 5, failures, "one rule, 5 matches → 5 failure entries in report.Results")
}

// Mixing global + path rules: globals pass, one path rule fails. The report
// must contain ALL four results (3 globals + 1 path) and Passed: false.
func TestEvaluate_MixedRulesPassFail(t *testing.T) {
	eff := &image.EfficiencyResult{
		Score:       0.95,
		WastedBytes: 100,
		WastedFiles: []image.WastedFile{},
	}
	layer := image.Layer{
		Index: 0, ID: "lay0", Tree: image.NewFileTree(),
	}
	layer.Tree.Root.AddChild(&image.FileNode{
		Name: "/tmp/x", Path: "/tmp/x", DiffType: image.Added,
	})
	rules := []Rule{
		LowestEfficiency{Threshold: 0.9},
		HighestWastedBytes{Threshold: 1000},
		HighestUserWastedPercent{Threshold: 0.1},
		BlockPathRule{ID: "block", Patterns: []string{"/tmp/**"}},
	}
	report := Evaluate(EvalContext{
		Efficiency: eff,
		TotalSize:  10000,
		Layers:     []image.Layer{layer},
	}, rules)
	assert.False(t, report.Passed)
	assert.Len(t, report.Results, 4, "3 globals + 1 path-rule violation = 4 results")
}

// Print groups results into "Global Rules:" / "Path Rules:" sections in
// the documented order. CHANGELOG calls this a Breaking change for log
// scrapers, so it gets an explicit pin.
func TestReport_Print_GroupedSections(t *testing.T) {
	eff := &image.EfficiencyResult{
		Score:       0.85,
		WastedBytes: 100,
		WastedFiles: []image.WastedFile{},
	}
	layer := image.Layer{
		Index: 0, ID: "lay0", Tree: image.NewFileTree(),
	}
	layer.Tree.Root.AddChild(&image.FileNode{
		Name: "/tmp/x", Path: "/tmp/x", DiffType: image.Added,
	})
	rules := []Rule{
		LowestEfficiency{Threshold: 0.9},
		BlockPathRule{ID: "block", Patterns: []string{"/tmp/**"}},
	}
	report := Evaluate(EvalContext{Efficiency: eff, TotalSize: 10000, Layers: []image.Layer{layer}}, rules)
	var buf bytes.Buffer
	report.Print(&buf)
	out := buf.String()
	assert.Contains(t, out, "Global Rules:")
	assert.Contains(t, out, "Path Rules:")
	// Globals must appear before Path rules.
	gIdx := strings.Index(out, "Global Rules:")
	pIdx := strings.Index(out, "Path Rules:")
	require.Greater(t, gIdx, -1)
	require.Greater(t, pIdx, gIdx, "Path Rules section must follow Global Rules section")
}
