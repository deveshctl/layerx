package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
)

func TestFriendlyCLIError_DaemonNotRunning(t *testing.T) {
	err := &image.ErrDaemonNotRunning{Cause: errors.New("connection refused")}
	assert.Contains(t, friendlyCLIError(err), "Docker daemon is not reachable")
}

func TestFriendlyCLIError_ImageNotFound(t *testing.T) {
	err := &image.ErrImageNotFound{Ref: "ghost:latest", Cause: errors.New("manifest unknown")}
	msg := friendlyCLIError(err)
	assert.Contains(t, msg, "ghost:latest")
	assert.Contains(t, msg, "not found")
}

func TestFriendlyCLIError_PullFailed(t *testing.T) {
	err := &image.ErrPullFailed{Ref: "registry.example/x:1", Cause: errors.New("tls handshake timeout")}
	msg := friendlyCLIError(err)
	assert.Contains(t, msg, "registry.example/x:1")
	assert.Contains(t, msg, "tls handshake timeout")
}

func TestFriendlyCLIError_ArchiveNotFound(t *testing.T) {
	err := &image.ErrArchiveNotFound{Path: "/tmp/missing.tar", Cause: errors.New("no such file")}
	msg := friendlyCLIError(err)
	assert.Contains(t, msg, "/tmp/missing.tar")
	assert.Contains(t, msg, "not found")
}

func TestFriendlyCLIError_ArchivePermission(t *testing.T) {
	err := &image.ErrArchivePermission{Path: "/root/img.tar", Cause: errors.New("EACCES")}
	msg := friendlyCLIError(err)
	assert.Contains(t, msg, "/root/img.tar")
	assert.Contains(t, msg, "permission denied")
}

func TestFriendlyCLIError_InvalidArchive(t *testing.T) {
	err := &image.ErrInvalidArchive{Path: "/tmp/junk.tar", Cause: errors.New("missing manifest.json")}
	msg := friendlyCLIError(err)
	assert.Contains(t, msg, "/tmp/junk.tar")
	assert.Contains(t, msg, "not a valid image archive")
}

func TestFriendlyCLIError_ArchiveInfra(t *testing.T) {
	err := &image.ErrArchiveInfra{Op: "spooling image archive", Cause: errors.New("no space left on device")}
	msg := friendlyCLIError(err)
	assert.Contains(t, msg, "spooling image archive")
	assert.Contains(t, msg, "no space left on device")
}

func TestFriendlyCLIError_PodmanSocketNotSet_Darwin(t *testing.T) {
	err := &image.ErrPodmanSocketNotSet{Platform: "darwin"}
	msg := friendlyCLIError(err)
	assert.Contains(t, msg, "--engine podman on darwin")
	assert.Contains(t, msg, "podman system connection add")
	assert.Contains(t, msg, "CONTAINER_HOST")
}

func TestFriendlyCLIError_PodmanSocketNotSet_Windows(t *testing.T) {
	err := &image.ErrPodmanSocketNotSet{Platform: "windows"}
	msg := friendlyCLIError(err)
	assert.Contains(t, msg, "--engine podman on windows")
	assert.Contains(t, msg, "podman system connection add")
}

func TestFriendlyCLIError_NoEngineFound(t *testing.T) {
	err := &image.ErrNoEngineFound{Tried: []string{
		"/var/run/docker.sock",
		"/run/user/1000/podman/podman.sock",
	}}
	msg := friendlyCLIError(err)
	assert.Contains(t, msg, "no container engine found")
	assert.Contains(t, msg, "/var/run/docker.sock")
	assert.Contains(t, msg, "/run/user/1000/podman/podman.sock")
	assert.Contains(t, msg, "set DOCKER_HOST")
}

func TestFriendlyCLIError_FallsBackToErrorString(t *testing.T) {
	err := errors.New("totally unexpected failure")
	assert.Equal(t, "totally unexpected failure", friendlyCLIError(err))
}

func TestPresentCLIError_WritesAndReturnsErr(t *testing.T) {
	var buf bytes.Buffer
	original := &image.ErrDaemonNotRunning{Cause: errors.New("x")}

	out := presentCLIError(&buf, original)

	assert.Same(t, original, out)
	got := buf.String()
	assert.True(t, strings.HasPrefix(got, "Error: "))
	assert.Contains(t, got, "Docker daemon is not reachable")
	assert.True(t, strings.HasSuffix(got, "\n"))
}

func TestPresentCLIError_NilIsNoop(t *testing.T) {
	var buf bytes.Buffer
	out := presentCLIError(&buf, nil)
	assert.Nil(t, out)
	assert.Empty(t, buf.String())
}

func TestFriendlyCLIError_ContextCanceled(t *testing.T) {
	assert.Equal(t, "interrupted", friendlyCLIError(context.Canceled))
}

func TestFriendlyCLIError_ContextDeadlineExceeded(t *testing.T) {
	assert.Equal(t, "timed out", friendlyCLIError(context.DeadlineExceeded))
}

func TestFriendlyCLIError_WrappedContextCanceled(t *testing.T) {
	wrapped := fmt.Errorf("resolve: %w", context.Canceled)
	assert.Equal(t, "interrupted", friendlyCLIError(wrapped))
}
