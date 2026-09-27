package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall"

	"github.com/deveshctl/layerx/image"
	"github.com/deveshctl/layerx/image/engine"
)

// friendlyCLIError converts an analyze/resolve error into a one-line
// stderr message. Mirrors tui/friendlyError with terser tone.
func friendlyCLIError(err error) string {
	if errors.Is(err, context.Canceled) {
		return "interrupted"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	if e, ok := errors.AsType[*image.ErrDaemonNotRunning](err); ok {
		return daemonNotRunningLine(e)
	}
	if e, ok := errors.AsType[*image.ErrImageNotFound](err); ok {
		return fmt.Sprintf("image %q not found", e.Ref)
	}
	if e, ok := errors.AsType[*image.ErrPullFailed](err); ok {
		return fmt.Sprintf("failed to pull image %q: %v", e.Ref, e.Cause)
	}
	if e, ok := errors.AsType[*image.ErrArchiveNotFound](err); ok {
		return fmt.Sprintf("archive %q not found", e.Path)
	}
	if e, ok := errors.AsType[*image.ErrArchivePermission](err); ok {
		return fmt.Sprintf("permission denied opening archive %q", e.Path)
	}
	if e, ok := errors.AsType[*image.ErrInvalidArchive](err); ok {
		return fmt.Sprintf("not a valid image archive %q (expected docker-save or OCI layout tarball)", e.Path)
	}
	if e, ok := errors.AsType[*image.ErrArchiveInfra](err); ok {
		// The disk-space hint is only trustworthy when the cause is
		// actually a full disk; appending it to an I/O error on a network
		// mount misdirects the user. Show it only on ENOSPC.
		if errors.Is(e.Cause, syscall.ENOSPC) {
			return fmt.Sprintf("could not %s: %v (free up disk space or set TMPDIR)", e.Op, e.Cause)
		}
		return fmt.Sprintf("could not %s: %v", e.Op, e.Cause)
	}
	if e, ok := errors.AsType[*image.ErrPodmanSocketNotSet](err); ok {
		return fmt.Sprintf("--engine podman on %s: no Podman connection configured "+
			"(run `podman system connection add <name> <uri>` and "+
			"`podman system connection default <name>`, or set CONTAINER_HOST / DOCKER_HOST)",
			e.Platform)
	}
	if e, ok := errors.AsType[*image.ErrNoEngineFound](err); ok {
		return fmt.Sprintf("no container engine found; tried: %s "+
			"(start Docker or Podman, run `docker context use` / "+
			"`podman system connection default`, or set DOCKER_HOST / CONTAINER_HOST)",
			strings.Join(e.Tried, ", "))
	}
	if e, ok := errors.AsType[*image.ErrPlatformInvalid](err); ok {
		return e.Error()
	}
	if e, ok := errors.AsType[*image.ErrPlatformNotInImage](err); ok {
		return e.Error()
	}
	if e, ok := errors.AsType[*engine.ErrConnectionNotFound](err); ok {
		// The typed error already renders a helpful multi-line list of
		// available names; surface it verbatim rather than the generic
		// err.Error() catch-all which prefixes "Error:" twice.
		return e.Error()
	}
	if e, ok := errors.AsType[*engine.ErrConfigMalformed](err); ok {
		return e.Error()
	}
	return err.Error()
}

// presentCLIError writes a friendly stderr line and returns err unchanged
// so main.go's exit-code mapping still applies.
func presentCLIError(w io.Writer, err error) error {
	if err == nil {
		return nil
	}
	fmt.Fprintf(w, "Error: %s\n", friendlyCLIError(err))
	return err
}

// daemonNotRunningLine renders a one-line stderr message for an
// ErrDaemonNotRunning, using the engine and host tags the resolver
// attached. Falls back to a generic "container engine" phrasing when
// the resolver did not know its engine name, so a test double or a
// FromEnv-only resolver still produces a sensible message.
func daemonNotRunningLine(e *image.ErrDaemonNotRunning) string {
	// Both engines are optional: layerx reads a `docker save` / `podman save`
	// / OCI-layout archive straight from disk. Point every daemon-down path
	// at that fallback so a user without a running engine is not dead-ended.
	const archiveHint = " Or pass a saved-image archive path instead (no engine needed)."
	engine := e.Engine
	switch engine {
	case "docker":
		if e.Host != "" {
			return fmt.Sprintf("Docker daemon at %s is not reachable. Is Docker running there?", e.Host) + archiveHint
		}
		return "Docker daemon is not reachable. Is Docker running?" + archiveHint
	case "podman":
		if e.Host != "" {
			return fmt.Sprintf("Podman connection at %s is not reachable. Check the connection with `podman system connection list` / `podman info`.", e.Host) + archiveHint
		}
		return "Podman is not reachable. Check the connection with `podman system connection list` / `podman info`." + archiveHint
	default:
		if e.Host != "" {
			return fmt.Sprintf("Container engine at %s is not reachable.", e.Host) + archiveHint
		}
		return "Container engine is not reachable." + archiveHint
	}
}
