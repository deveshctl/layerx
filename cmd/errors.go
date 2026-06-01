package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/deveshctl/layerx/image"
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
	if _, ok := errors.AsType[*image.ErrDaemonNotRunning](err); ok {
		return "Docker daemon is not reachable. Is Docker running?"
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
		return fmt.Sprintf("could not %s: %v (free up disk space or set TMPDIR)", e.Op, e.Cause)
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
