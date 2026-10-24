package cmd

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/deveshctl/layerx/image"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/config"
	"github.com/deveshctl/layerx/internal/sizebudget"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sizeArchive(t *testing.T, arch string, n int) string {
	t.Helper()
	var layer bytes.Buffer
	tw := tar.NewWriter(&layer)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "app/file", Mode: 0644, Size: int64(n)}))
	_, err := tw.Write(bytes.Repeat([]byte("x"), n))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	config := fmt.Sprintf(`{"os":"linux","architecture":%q,"history":[{"created_by":%q}]}`, arch, fmt.Sprint(n))
	manifest := `[{"Config":"config.json","Layers":["layer/layer.tar"]}]`
	var archive bytes.Buffer
	tw = tar.NewWriter(&archive)
	for path, data := range map[string][]byte{"manifest.json": []byte(manifest), "config.json": []byte(config), "layer/layer.tar": layer.Bytes()} {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: path, Mode: 0644, Size: int64(len(data))}))
		_, err := tw.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	path := filepath.Join(t.TempDir(), "image.tar")
	require.NoError(t, os.WriteFile(path, archive.Bytes(), 0600))
	return path
}

func TestCompareConfiguredBudgetsAndExports(t *testing.T) {
	previousConfig, previousMode, previousTop, previousNoCache := flagConfig, flagCompareMode, flagCompareTop, flagNoCacheFl
	t.Cleanup(func() {
		flagConfig, flagCompareMode, flagCompareTop, flagNoCacheFl = previousConfig, previousMode, previousTop, previousNoCache
	})
	flagCompareMode, flagCompareTop, flagNoCacheFl = compareModeCompact, 10, true
	t.Setenv("LAYERX_CACHE_DIR", t.TempDir())
	flagConfig = filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(flagConfig, []byte("size-budget:\n  max: 0B\n  max-growth: 0%\n"), 0600))
	before, after, foreign := sizeArchive(t, "amd64", 100), sizeArchive(t, "amd64", 4096), sizeArchive(t, "arm64", 4096)
	for _, tc := range []struct {
		name      string
		args      []string
		old, new  string
		failed    bool
		errorText string
	}{
		{"budget failure", nil, before, after, true, ""},
		{"same image still checks maximum", nil, before, before, true, ""},
		{"CLI overrides", []string{"--max-image-size=1MB", "--max-growth=100KB"}, before, after, false, ""},
		{"platform mismatch", nil, before, foreign, false, "across platforms"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.SetContext(t.Context())
			addSizeBudgetFlags(cmd)
			require.NoError(t, cmd.Flags().Set("format", "json"))
			require.NoError(t, cmd.ParseFlags(tc.args))
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			err := runCompareCmdInner(cmd, []string{tc.old, tc.new})
			if tc.errorText != "" {
				require.ErrorContains(t, err, tc.errorText)
				assert.Empty(t, out.String())
				return
			}
			if tc.failed {
				var failure *ErrCompareRegression
				require.ErrorAs(t, err, &failure)
			} else {
				require.NoError(t, err)
			}
			var report checkReport
			require.NoError(t, json.Unmarshal(out.Bytes(), &report))
			assert.Equal(t, !tc.failed, report.Passed)
			assert.Equal(t, "uncompressed-layer-tar-bytes", report.Metric)
			assert.Equal(t, "linux/amd64", report.After.Platform)
			require.NotNil(t, report.Before)
			assert.NotEmpty(t, report.After.Identity)
			assert.Len(t, report.SizeChecks, 2)
		})
	}
}

func TestCIBudgetOnlyAndMissingBaseline(t *testing.T) {
	t.Setenv("LAYERX_CACHE_DIR", t.TempDir())
	cfg := config.Default()
	cfg.Rules = config.RulesConfig{}
	cfg.SizeBudget = sizebudget.Config{Max: "0B"}
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	addSizeBudgetFlags(cmd)
	cmd.Flags().String("baseline", "", "")
	require.NoError(t, cmd.Flags().Set("format", "json"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	a, err := runCICheckInner(t.Context(), sizeArchive(t, "amd64", 100), cfg, cmd, true, false, nil)
	require.NotNil(t, a)
	var failed *ErrCIFailed
	require.ErrorAs(t, err, &failed)
	var report checkReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &report))
	assert.False(t, report.Passed)
	cfg.SizeBudget = sizebudget.Config{MaxGrowth: "5%"}
	_, err = runCICheckInner(t.Context(), "unused", cfg, cmd, true, false, nil)
	require.ErrorContains(t, err, "requires --baseline")
}

func TestComparisonReadabilityAndMarkdownFence(t *testing.T) {
	r := sampleResult()
	r.FileDiffs[0].Path = "/some/very/long/目录/path/important-file.txt\x1b[31m\n"
	for _, width := range []int{40, 60, 80, 100} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			var out bytes.Buffer
			renderComparison(&out, r, compareModeFull, 10, nil, compareOptions{Width: width})
			assert.Contains(t, out.String(), "important-file.txt")
			assert.NotContains(t, out.String(), "\x1b")
			for _, line := range strings.Split(out.String(), "\n") {
				if strings.HasPrefix(line, "verdict:") {
					continue
				} // Stable machine-readable trailer is intentionally unwrapped.
				assert.LessOrEqual(t, ansi.StringWidth(line), width, "%q", line)
			}
		})
	}
	var out bytes.Buffer
	require.NoError(t, writeCheckReport(&out, "markdown", "untrusted ```text\n", checkReport{}))
	assert.Contains(t, out.String(), "````text\n")
}

func TestSmallEfficiencyRegressionRemainsReadable(t *testing.T) {
	assert.Equal(t, "-0.00001 pp", percentagePointDelta(-0.0000001))
	assert.Equal(t, "-1.5 pp", percentagePointDelta(-0.015))
}

func TestComparisonProgressFinishesBeforeDigestLine(t *testing.T) {
	t.Setenv("LAYERX_CACHE_DIR", t.TempDir())
	resolver := &compareFakeResolver{
		imageIDFn: func(context.Context, string) (string, error) { return "sha256:" + strings.Repeat("a", 64), nil },
		resolveFn: func(_ context.Context, _ string, progress chan<- image.ProgressEvent) ([]image.Layer, error) {
			progress <- image.ProgressEvent{Phase: image.PhaseParsing}
			return nil, nil
		},
	}
	var output bytes.Buffer
	_, _, err := analyzeForCompare(t.Context(), resolver, "app:local", true, "new", &output)
	require.NoError(t, err)
	assert.Less(t, strings.Index(output.String(), "parsing"), strings.Index(output.String(), "resolved"), output.String())
}
