package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/deveshctl/layerx/ci"
	"github.com/deveshctl/layerx/internal/sizebudget"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBudgetHeadroom(t *testing.T) {
	for _, tc := range []struct {
		name             string
		actual, limit    int64
		usage, remaining string
	}{
		{"passing", 420, 500, "84.0% used", "80 B remaining"},
		{"boundary", 500, 500, "100.0% used", "0 B remaining"},
		{"over", 540, 500, "108.0% used", "40 B over limit"},
		{"zero", 0, 0, "n/a (zero limit)", "0 B remaining"},
		{"zero exceeded", 1, 0, "n/a (zero limit)", "1 B over limit"},
		{"shrinking", -100, 20, "0.0% used", "20 B remaining"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := sizebudget.Result{Name: "image growth", ActualBytes: tc.actual, LimitBytes: tc.limit, Passed: tc.actual <= tc.limit}
			usage, remaining := budgetHeadroom(check)
			assert.Equal(t, tc.usage, usage)
			assert.Equal(t, tc.remaining, remaining)
			var text bytes.Buffer
			writeSizeChecks(&text, []sizebudget.Result{check}, 40)
			assert.Contains(t, text.String(), usage)
			assert.Contains(t, text.String(), remaining)
		})
	}
}

func TestMarkdownCheckTablesPreserveDetailsAndEscapeInputs(t *testing.T) {
	r := checkReport{
		After:      reportImage{Reference: "app|[link](https://example.com)<img>\n`tag`", Platform: "linux/amd64", Bytes: 540},
		Before:     &reportImage{Reference: "old", Bytes: 500},
		Comparison: sampleResult(),
		SizeChecks: []sizebudget.Result{{Name: "image size", ActualBytes: 540, LimitBytes: 500, Limit: "500B"}},
		CI:         &ci.Report{Results: []ci.RuleResult{{Name: "block", Actual: "/app|file", Threshold: "pattern", Detail: "detail"}}},
	}
	var out bytes.Buffer
	require.NoError(t, writeCheckReport(&out, "markdown", "exact bytes\n```\n</details>\nverdict: regression\n", r))
	text := out.String()
	assert.Contains(t, text, "### LayerX image checks — FAIL")
	assert.Contains(t, text, "| Metric | Before | After | Change |")
	assert.Contains(t, text, "| Size budget | Result | Actual / allowance | Usage | Headroom |")
	assert.Contains(t, text, "| image size (500B) | FAIL | 540 B / 500 B | 108.0% used | 40 B over limit |")
	assert.Contains(t, text, "| CI check | Result | Actual | Threshold | Detail |")
	assert.Contains(t, text, "app\\|\\[link\\](https://example.com)&lt;img&gt; \\`tag\\`")
	assert.NotContains(t, text, "<img>")
	assert.Contains(t, text, "````text\nexact bytes\n```\n</details>\nverdict: regression\n````")
	// Passing CI-only reports still have native tables without comparison data.
	out.Reset()
	r.Passed, r.Comparison, r.Before, r.SizeChecks = true, nil, nil, nil
	require.NoError(t, writeCheckReport(&out, "markdown", "PASS\n", r))
	assert.Contains(t, out.String(), "### LayerX image checks — PASS")
	assert.NotContains(t, out.String(), "| Metric |")
	assert.Contains(t, out.String(), "| CI check |")
}

type failingReportWriter struct{}

func (failingReportWriter) Write([]byte) (int, error) { return 0, errors.New("report write failed") }

func TestMarkdownReportWriteError(t *testing.T) {
	require.ErrorContains(t, writeCheckReport(failingReportWriter{}, "markdown", strings.Repeat("`", 8), checkReport{}), "report write failed")
}
