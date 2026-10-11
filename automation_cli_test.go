package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Run the real entry point in a child process to verify output and OS exit
// codes without a container engine or an interactive terminal.
func TestAutomationCLIProcess(t *testing.T) {
	if os.Getenv("LAYERX_TEST_CLI_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"layerx"}, os.Args[i+1:]...)
			main()
			os.Exit(0)
		}
	}
	t.Fatal("missing CLI argument separator")
}

func runAutomationCLI(t *testing.T, ciEnv string, args ...string) (int, string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestAutomationCLIProcess$", "--"}, args...)...)
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "CI=") && !strings.HasPrefix(env, "LAYERX_CACHE_DIR=") {
			child.Env = append(child.Env, env)
		}
	}
	child.Env = append(child.Env, "LAYERX_TEST_CLI_PROCESS=1", "CI="+ciEnv, "LAYERX_CACHE_DIR="+t.TempDir())
	var stdout, stderr bytes.Buffer
	child.Stdout = &stdout
	child.Stderr = &stderr
	err := child.Run()
	require.NoError(t, ctx.Err(), "CLI did not finish: %s", stderr.String())
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr, "failed to start CLI")
		code = exitErr.ExitCode()
	}
	return code, stdout.String(), stderr.String()
}

func automationArchive(t *testing.T) string {
	t.Helper()
	var archive bytes.Buffer
	outer := tar.NewWriter(&archive)
	add := func(name string, data []byte) {
		require.NoError(t, outer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}))
		_, err := outer.Write(data)
		require.NoError(t, err)
	}
	var diffIDs, layers []string
	for i, size := range []int{100, 200} {
		var payload bytes.Buffer
		layer := tar.NewWriter(&payload)
		require.NoError(t, layer.WriteHeader(&tar.Header{Name: "app", Mode: 0o644, Size: int64(size)}))
		_, err := layer.Write(bytes.Repeat([]byte{'x'}, size))
		require.NoError(t, err)
		require.NoError(t, layer.Close())
		name := fmt.Sprintf("layer%d.tar", i)
		add(name, payload.Bytes())
		layers = append(layers, name)
		diffIDs = append(diffIDs, fmt.Sprintf("sha256:%x", sha256.Sum256(payload.Bytes())))
	}
	config, err := json.Marshal(map[string]any{
		"architecture": "amd64",
		"os":           "linux",
		"rootfs":       map[string]any{"type": "layers", "diff_ids": diffIDs},
		"history":      []map[string]string{{"created_by": "COPY app /app"}, {"created_by": "COPY app /app"}},
	})
	require.NoError(t, err)
	add("config.json", config)
	manifest, err := json.Marshal([]map[string]any{{"Config": "config.json", "RepoTags": []string{"app:test"}, "Layers": layers}})
	require.NoError(t, err)
	add("manifest.json", manifest)
	require.NoError(t, outer.Close())
	path := filepath.Join(t.TempDir(), "image.tar")
	require.NoError(t, os.WriteFile(path, archive.Bytes(), 0o600))
	return path
}

func TestAutomationCLI_JSONResults(t *testing.T) {
	archive := automationArchive(t)
	for _, route := range []string{"inspect", "ci", "environment"} {
		for _, outcome := range []string{"success", "missing_parent", "directory"} {
			for _, failing := range []bool{false, true} {
				if route == "inspect" && failing {
					continue
				}
				name := fmt.Sprintf("%s/%s/failing=%t", route, outcome, failing)
				t.Run(name, func(t *testing.T) {
					dir := t.TempDir()
					configPath := filepath.Join(dir, "config.yaml")
					threshold := "0.5"
					if failing {
						threshold = "0.9"
					}
					content := "rules:\n  lowest-efficiency: " + threshold + "\n  highest-user-wasted-percent: 0\n"
					require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))
					target := filepath.Join(dir, "out.json")
					switch outcome {
					case "missing_parent":
						target = filepath.Join(dir, "missing", "out.json")
					case "directory":
						require.NoError(t, os.Mkdir(target, 0o700))
						require.NoError(t, os.WriteFile(filepath.Join(target, "keep"), []byte("keep"), 0o600))
					}
					args := []string{"--config", configPath}
					if route == "ci" {
						args = append(args, "ci")
					}
					args = append(args, "--json", target, archive)
					ciEnv := ""
					if route == "environment" {
						ciEnv = "true"
					}
					code, stdout, stderr := runAutomationCLI(t, ciEnv, args...)
					wantCode := 0
					if failing {
						wantCode = 1
					} else if outcome != "success" {
						wantCode = 2
					}
					assert.Equal(t, wantCode, code, "stdout=%s stderr=%s", stdout, stderr)
					assert.NotContains(t, stdout+stderr, "Usage:")
					if outcome == "success" {
						data, err := os.ReadFile(target)
						require.NoError(t, err)
						var report struct {
							SchemaVersion string
							LayerCount    int
						}
						require.NoError(t, json.Unmarshal(data, &report))
						assert.Equal(t, "1.0.1", report.SchemaVersion)
						assert.Equal(t, 2, report.LayerCount)
						assert.Contains(t, stderr, "wrote analysis to")
					} else {
						assert.Contains(t, stderr, "could not write JSON")
						if route == "inspect" {
							assert.Equal(t, 1, strings.Count(stderr, "Error:"))
						} else {
							assert.Equal(t, 1, strings.Count(stderr, "warning: JSON export failed:"))
						}
						leftovers, err := filepath.Glob(filepath.Join(dir, ".layerx-json-*.tmp"))
						require.NoError(t, err)
						assert.Empty(t, leftovers)
					}
				})
			}
		}
	}
}
