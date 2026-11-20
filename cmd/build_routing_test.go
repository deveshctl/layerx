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
			runBuildCommand = func(c *exec.Cmd) error {
				forwarded = append([]string(nil), c.Args[1:]...)
				return writeTestBuildIID(t, c)
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
			assert.Equal(t, "sha256:built-image", got.ImageRef)
			require.GreaterOrEqual(t, len(forwarded), 3)
			assert.Equal(t, "--iidfile", forwarded[len(forwarded)-2])
			_, err = os.Stat(forwarded[len(forwarded)-1])
			assert.ErrorIs(t, err, os.ErrNotExist, "owned image-ID file must be cleaned up")
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
				assert.Equal(t, []string{"build", "--no-cache", "-t", "app:test", "--target", "release", "."}, c.Args[1:len(c.Args)-2])
				return writeTestBuildIID(t, c)
			}
			tuiCalls := 0
			runBuildTUI = func(cfg tui.Config) error {
				tuiCalls++
				assert.Equal(t, "sha256:built-image", cfg.ImageRef)
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
	runBuildCommand = func(c *exec.Cmd) error { return writeTestBuildIID(t, c) }
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
	runBuildCommand = func(c *exec.Cmd) error { return writeTestBuildIID(t, c) }
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

func writeTestBuildIID(t *testing.T, c *exec.Cmd) error {
	t.Helper()
	require.GreaterOrEqual(t, len(c.Args), 3)
	require.Equal(t, "--iidfile", c.Args[len(c.Args)-2])
	return os.WriteFile(c.Args[len(c.Args)-1], []byte("sha256:built-image\n"), 0o600)
}

func TestBuildCheckDoesNotAnalyzeExistingTag(t *testing.T) {
	prepareBuildRoutingTest(t)
	output := filepath.Join(t.TempDir(), "analysis.json")
	runBuildCommand = func(c *exec.Cmd) error {
		assert.Equal(t, []string{"build", "--check", "-t", "app:test", "."}, c.Args[1:])
		return nil
	}
	previous := selectResolver
	selectResolver = func(string) (image.Resolver, error) {
		t.Fatal("check-only builds must not inspect an existing tag")
		return nil, nil
	}
	t.Cleanup(func() { selectResolver = previous })
	rootCmd.SetArgs([]string{"build", "--engine", "docker", "--json", output, "--check", "-t", "app:test", "."})
	require.NoError(t, rootCmd.Execute())
	_, err := os.Stat(output)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestBuildRejectsUnchangedUserIIDFile(t *testing.T) {
	prepareBuildRoutingTest(t)
	iid := filepath.Join(t.TempDir(), "previous.iid")
	require.NoError(t, os.WriteFile(iid, []byte("sha256:previous"), 0o600))
	runBuildCommand = func(*exec.Cmd) error { return nil }
	rootCmd.SetArgs([]string{"build", "--engine", "docker", "--iidfile", iid, "."})
	require.ErrorContains(t, rootCmd.Execute(), "did not update the image ID file")
}

func TestBuildEngineExit(t *testing.T) {
	if os.Getenv("LAYERX_TEST_ENGINE_EXIT") == "1" {
		os.Exit(7)
	}
}
