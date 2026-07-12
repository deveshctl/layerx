package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"testing"

	"github.com/deveshctl/layerx/image"
	"github.com/stretchr/testify/assert"
)

func TestFriendlyCLIError_DaemonNotRunning(t *testing.T) {
	// An untagged ErrDaemonNotRunning (no Engine, no Host) is engine-
	// agnostic — the renderer must NOT invent a "Docker daemon" wording
	// out of thin air. This is the behaviour the Podman path relied on
	// before Engine tagging existed; keeping it as the fallback lets any
	// caller construct the error without extra plumbing.
	err := &image.ErrDaemonNotRunning{Cause: errors.New("connection refused")}
	msg := friendlyCLIError(err)
	assert.Contains(t, msg, "Container engine is not reachable")
	assert.NotContains(t, msg, "Docker")
	assert.NotContains(t, msg, "Podman")
}

func TestFriendlyCLIError_DaemonNotRunning_PodmanEngine(t *testing.T) {
	// Regression: a broken Podman connection used to render as
	// "Docker daemon is not reachable. Is Docker running?" — the
	// message must reflect the engine the user actually asked for so
	// they don't chase Docker when Podman is what failed.
	err := &image.ErrDaemonNotRunning{
		Engine: "podman",
		Host:   "ssh://user@host/run/podman.sock",
		Cause:  errors.New("connect: connection refused"),
	}
	msg := friendlyCLIError(err)
	assert.Contains(t, msg, "Podman")
	assert.Contains(t, msg, "ssh://user@host/run/podman.sock")
	assert.NotContains(t, msg, "Docker daemon")
	assert.NotContains(t, msg, "Is Docker running")
}

func TestFriendlyCLIError_DaemonNotRunning_DockerEngineWithHost(t *testing.T) {
	// When the resolver knows the target URL (active context / explicit
	// DOCKER_HOST) the friendly line surfaces it so users see where the
	// tool tried to connect — matching what `docker` itself prints.
	err := &image.ErrDaemonNotRunning{
		Engine: "docker",
		Host:   "tcp://remote.example:2376",
		Cause:  errors.New("dial tcp: i/o timeout"),
	}
	msg := friendlyCLIError(err)
	assert.Contains(t, msg, "Docker daemon at tcp://remote.example:2376")
	// Both engines support reading a saved-image archive from disk, so
	// every daemon-down line offers that fallback.
	assert.Contains(t, msg, "archive")
}

func TestFriendlyCLIError_DaemonNotRunning_UnknownEngine(t *testing.T) {
	// A resolver constructed without an engine tag must never render as
	// "Docker …" — the message stays engine-agnostic.
	err := &image.ErrDaemonNotRunning{
		Host:  "tcp://example:2376",
		Cause: errors.New("connection refused"),
	}
	msg := friendlyCLIError(err)
	assert.NotContains(t, msg, "Docker")
	assert.NotContains(t, msg, "Podman")
	assert.Contains(t, msg, "tcp://example:2376")
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
	// A genuine out-of-space error keeps the disk-space hint. Wrapping
	// syscall.ENOSPC mirrors what os file writes surface on a full disk.
	err := &image.ErrArchiveInfra{
		Op:    "spooling image archive",
		Cause: fmt.Errorf("write temp: %w", syscall.ENOSPC),
	}
	msg := friendlyCLIError(err)
	assert.Contains(t, msg, "spooling image archive")
	assert.Contains(t, msg, "free up disk space")
}

func TestFriendlyCLIError_ArchiveInfra_NonDiskError(t *testing.T) {
	// A non-ENOSPC infra failure (e.g. an I/O error on a network mount)
	// must NOT claim the disk is full — the hint would misdirect the user.
	err := &image.ErrArchiveInfra{Op: "spooling image archive", Cause: errors.New("input/output error")}
	msg := friendlyCLIError(err)
	assert.Contains(t, msg, "spooling image archive")
	assert.Contains(t, msg, "input/output error")
	assert.NotContains(t, msg, "free up disk space")
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
	// Use a docker-tagged error so the output has a specific expected
	// wording — the zero-Engine fallback ("Container engine …") is
	// covered by TestFriendlyCLIError_DaemonNotRunning directly.
	original := &image.ErrDaemonNotRunning{
		Engine: "docker",
		Cause:  errors.New("x"),
	}

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
