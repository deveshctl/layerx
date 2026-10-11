package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deveshctl/layerx/image"
	"github.com/deveshctl/layerx/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func prepareBuildRoutingTest(t *testing.T) {
	t.Helper()
	resetRootCmdFlags(t)
	resetEngineState(t)
	writeConfig(t, "")
	t.Setenv("CI", "")
	prevConfig := flagConfig
	prevRun := runBuildCommand
	prevTUI := runBuildTUI
	flagConfig = ""
	t.Cleanup(func() {
		flagConfig = prevConfig
		runBuildCommand = prevRun
		runBuildTUI = prevTUI
	})
	swapLookPath(t, map[string]string{"docker": "test-engine", "podman": "test-engine"})
}

func TestBuild_JSONRouting(t *testing.T) {
	for _, location := range []string{"before_build", "after_build", "equals_form", "untagged", "ci_environment"} {
		t.Run(location, func(t *testing.T) {
			prepareBuildRoutingTest(t)
			withFakeResolver(t, okResolver(passingLayers()...))
			output := filepath.Join(t.TempDir(), "analysis.json")
			args := []string{"--engine", "docker", "--json", output, "build", "-t", "app:test", "."}
			switch location {
			case "after_build":
				args = []string{"build", "--engine", "podman", "--json", output, "-t", "app:test", "."}
			case "equals_form":
				args = []string{"build", "--engine=docker", "--json=" + output, "-t", "app:test", "."}
			case "untagged":
				args = []string{"--engine", "docker", "--json", output, "build", "."}
			case "ci_environment":
				t.Setenv("CI", "true")
			}
			var forwarded []string
			var iidPath string
			runBuildCommand = func(c *exec.Cmd) error {
				forwarded = append([]string(nil), c.Args[1:]...)
				if location == "untagged" {
					for i, arg := range forwarded {
						if arg == "--iidfile" && i+1 < len(forwarded) {
							iidPath = forwarded[i+1]
							return os.WriteFile(iidPath, []byte("sha256:built-image\n"), 0o600)
						}
					}
					t.Fatal("untagged build must request an image ID")
				}
				return nil
			}
			tuiCalls := 0
			runBuildTUI = func(tui.Config) error {
				tuiCalls++
				return nil
			}
			rootCmd.SetArgs(args)
			require.NoError(t, rootCmd.Execute())
			assert.Zero(t, tuiCalls, "JSON output must not start the interactive viewer")
			require.NotEmpty(t, forwarded)
			assert.Equal(t, "build", forwarded[0])
			assert.NotContains(t, forwarded, "--json")
			assert.NotContains(t, forwarded, "--json="+output)
			data, err := os.ReadFile(output)
			require.NoError(t, err)
			var got jsonExport
			require.NoError(t, json.Unmarshal(data, &got))
			assert.Equal(t, jsonSchemaVersion, got.SchemaVersion)
			assert.Equal(t, 1, got.LayerCount)
			wantRef := "app:test"
			if location == "untagged" {
				wantRef = "sha256:built-image"
				_, err := os.Stat(iidPath)
				assert.True(t, os.IsNotExist(err), "owned image-ID file must be cleaned up")
			} else {
				assert.Equal(t, []string{"build", "-t", "app:test", "."}, forwarded)
			}
			assert.Equal(t, wantRef, got.ImageRef)
		})
	}
}

func TestBuild_DefaultRoutingRemainsInteractive(t *testing.T) {
	for _, ciEnv := range []string{"", "true"} {
		t.Run("CI="+ciEnv, func(t *testing.T) {
			prepareBuildRoutingTest(t)
			t.Setenv("CI", ciEnv)
			resolver := okResolver(passingLayers()...)
			withFakeResolver(t, resolver)
			runBuildCommand = func(c *exec.Cmd) error {
				assert.Equal(t, []string{"build", "--no-cache", "-t", "app:test", "--target", "release", "."}, c.Args[1:])
				return nil
			}
			tuiCalls := 0
			runBuildTUI = func(cfg tui.Config) error {
				tuiCalls++
				assert.Equal(t, "app:test", cfg.ImageRef)
				assert.Same(t, resolver, cfg.Resolver)
				assert.True(t, cfg.NoCache)
				return nil
			}
			rootCmd.SetArgs([]string{"--engine", "docker", "--no-cache", "build", "-t", "app:test", "--target", "release", "."})
			require.NoError(t, rootCmd.Execute())
			assert.Equal(t, 1, tuiCalls)
		})
	}
}

func TestBuild_EngineFailureSkipsInspection(t *testing.T) {
	prepareBuildRoutingTest(t)
	output := filepath.Join(t.TempDir(), "analysis.json")
	runBuildCommand = func(*exec.Cmd) error {
		child := exec.Command(os.Args[0], "-test.run=^TestBuildEngineExit$")
		child.Env = append(os.Environ(), "LAYERX_TEST_ENGINE_EXIT=1")
		return child.Run()
	}
	prevSelect := selectResolver
	selectResolver = func(string) (image.Resolver, error) {
		t.Fatal("failed builds must not resolve an image")
		return nil, nil
	}
	t.Cleanup(func() { selectResolver = prevSelect })
	runBuildTUI = func(tui.Config) error {
		t.Fatal("failed builds must not start the interactive viewer")
		return nil
	}
	rootCmd.SetArgs([]string{"--engine", "docker", "--json", output, "build", "-t", "app:test", "."})
	err := rootCmd.Execute()
	require.Error(t, err)
	failed, ok := errors.AsType[*ErrBuildFailed](err)
	require.True(t, ok)
	assert.Equal(t, 7, failed.ExitCode)
	_, statErr := os.Stat(output)
	assert.True(t, os.IsNotExist(statErr))
}

func TestBuild_JSONFailureSkipsTUI(t *testing.T) {
	prepareBuildRoutingTest(t)
	withFakeResolver(t, okResolver(passingLayers()...))
	runBuildCommand = func(*exec.Cmd) error { return nil }
	runBuildTUI = func(tui.Config) error {
		t.Fatal("failed JSON exports must not fall back to the interactive viewer")
		return nil
	}
	target := filepath.Join(t.TempDir(), "missing", "out.json")
	rootCmd.SetArgs([]string{"--engine", "docker", "--json", target, "build", "-t", "app:test", "."})
	stderr, err := captureCLIStderr(t, rootCmd.Execute)
	require.ErrorIs(t, err, os.ErrNotExist)
	assert.Contains(t, stderr, "could not write JSON")
	assert.Equal(t, 1, strings.Count(stderr, "Error:"))
}

func TestBuild_JSONStillValidatesConfig(t *testing.T) {
	prepareBuildRoutingTest(t)
	output := filepath.Join(t.TempDir(), "analysis.json")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("path-rules:\n  - id: app-policy\n    type: block\n"), 0o600))
	runBuildCommand = func(*exec.Cmd) error { return nil }
	previous := selectResolver
	selectResolver = func(string) (image.Resolver, error) {
		t.Fatal("invalid configuration must stop JSON analysis")
		return nil, nil
	}
	t.Cleanup(func() { selectResolver = previous })
	runBuildTUI = func(tui.Config) error {
		t.Fatal("invalid configuration must not start the interactive viewer")
		return nil
	}
	rootCmd.SetArgs([]string{"--engine", "docker", "--config", configPath, "--json", output, "build", "-t", "app:test", "."})
	err := rootCmd.Execute()
	require.ErrorContains(t, err, "app-policy")
	_, statErr := os.Stat(output)
	assert.True(t, os.IsNotExist(statErr))
}

func TestBuildEngineExit(t *testing.T) {
	if os.Getenv("LAYERX_TEST_ENGINE_EXIT") == "1" {
		os.Exit(7)
	}
}
