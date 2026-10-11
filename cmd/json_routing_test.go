package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CLI presentation uses os.Stderr, so capture it separately from Cobra's
// error writer. These tests must remain serial.
func captureCLIStderr(t *testing.T, run func() error) (string, error) {
	t.Helper()
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	previous := os.Stderr
	os.Stderr = writer
	t.Cleanup(func() {
		os.Stderr = previous
		_ = reader.Close()
		_ = writer.Close()
	})
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(&output, reader)
		done <- err
	}()
	runErr := run()
	os.Stderr = previous
	require.NoError(t, writer.Close())
	require.NoError(t, <-done)
	return output.String(), runErr
}

func TestInspect_JSONWriteFailurePresentedOnce(t *testing.T) {
	for _, targetType := range []string{"missing_parent", "directory"} {
		t.Run(targetType, func(t *testing.T) {
			resetRootCmdFlags(t)
			resetPersistentFlags(t)
			writeConfig(t, "")
			t.Setenv("CI", "")
			withFakeResolver(t, okResolver(passingLayers()...))
			dir := t.TempDir()
			target := filepath.Join(dir, "missing", "out.json")
			if targetType == "directory" {
				target = filepath.Join(dir, "out.json")
				require.NoError(t, os.Mkdir(target, 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(target, "keep"), []byte("keep"), 0o600))
			}
			var cobraErrors bytes.Buffer
			rootCmd.SetErr(&cobraErrors)
			rootCmd.SetArgs([]string{"--json", target, "app:test"})
			stderr, err := captureCLIStderr(t, rootCmd.Execute)
			require.Error(t, err)
			var cause error
			if targetType == "missing_parent" {
				var pathErr *os.PathError
				require.True(t, errors.As(err, &pathErr), "write cause must remain in the error chain: %v", err)
				cause = pathErr.Err
			} else {
				var linkErr *os.LinkError
				require.True(t, errors.As(err, &linkErr), "rename cause must remain in the error chain: %v", err)
				cause = linkErr.Err
			}
			allErrors := stderr + cobraErrors.String()
			assert.Contains(t, allErrors, "could not write JSON")
			assert.Contains(t, allErrors, cause.Error())
			assert.Equal(t, 1, strings.Count(allErrors, "Error:"))
			assert.NotContains(t, allErrors, "Usage:")
			leftovers, globErr := readDirGlob(dir, ".layerx-json-", ".tmp")
			require.NoError(t, globErr)
			assert.Empty(t, leftovers)
		})
	}
}

func TestCI_JSONWriteFailurePreservesResult(t *testing.T) {
	for _, command := range []string{"ci", "environment"} {
		for _, failing := range []bool{false, true} {
			name := command + "/pass"
			if failing {
				name = command + "/fail"
			}
			t.Run(name, func(t *testing.T) {
				resetRootCmdFlags(t)
				resetPersistentFlags(t)
				writeConfig(t, "rules:\n  lowest-efficiency: 0.9\n  highest-user-wasted-percent: 0\n")
				t.Setenv("CI", "")
				layers := passingLayers()
				if failing {
					layers = failingLayers()
				}
				withFakeResolver(t, okResolver(layers...))
				target := filepath.Join(t.TempDir(), "missing", "out.json")
				args := []string{"ci", "--json", target, "app:test"}
				if command == "environment" {
					t.Setenv("CI", "true")
					args = []string{"--json", target, "app:test"}
				}
				rootCmd.SetArgs(args)
				stderr, err := captureCLIStderr(t, rootCmd.Execute)
				require.Error(t, err)
				var failed *ErrCIFailed
				assert.Equal(t, failing, errors.As(err, &failed))
				assert.Equal(t, 1, strings.Count(stderr, "warning: JSON export failed:"))
				assert.NotContains(t, stderr, "Error:")
				assert.Contains(t, stderr, "could not write JSON")
				if !failing {
					assert.ErrorIs(t, err, os.ErrNotExist)
				}
			})
		}
	}
}
