package image

import (
	"fmt"
	"strings"
)

// ErrDaemonNotRunning signals that the container engine's daemon socket /
// endpoint could not be reached. Engine and Host are best-effort context
// used by friendly renderers:
//   - Engine is the layerx --engine label ("docker", "podman", "") that
//     was in effect when the failure happened. Empty means the resolver
//     did not know its engine name (test fakes, or a resolver constructed
//     via the FromEnv path with no cmd-level tag). Callers that render
//     user-facing text must treat empty Engine as "container engine",
//     never as "docker".
//   - Host is the URL the client was pointed at ("tcp://…", "unix://…",
//     "ssh://…"), when the resolver knew it up front. NewDockerResolver
//     via FromEnv leaves this empty because the client resolves the host
//     from DOCKER_HOST internally; NewDockerResolverWithHost fills it in.
//
// Never use fmt.Sprintf("Docker daemon …") in code that consumes this
// error — the engine may be Podman.
type ErrDaemonNotRunning struct {
	Engine string
	Host   string
	Cause  error
}

func (e *ErrDaemonNotRunning) Error() string {
	engine := e.Engine
	if engine == "" {
		engine = "container engine"
	}
	// Cause carries the underlying moby-client transport message, which
	// on a broken active context / connection already includes the
	// target address ("Cannot connect to the Docker daemon at
	// tcp://…"). We do NOT re-render Host here because that would
	// duplicate the address the cause already prints; friendly
	// renderers that suppress the cause read Host directly.
	return fmt.Sprintf("cannot connect to %s: %v", engine, e.Cause)
}

func (e *ErrDaemonNotRunning) Unwrap() error { return e.Cause }

type ErrImageNotFound struct {
	Ref   string
	Cause error
}

func (e *ErrImageNotFound) Error() string {
	return fmt.Sprintf("image %q not found: %v", e.Ref, e.Cause)
}

func (e *ErrImageNotFound) Unwrap() error { return e.Cause }

type ErrPullFailed struct {
	Ref   string
	Cause error
}

func (e *ErrPullFailed) Error() string {
	return fmt.Sprintf("failed to pull image %q: %v", e.Ref, e.Cause)
}

func (e *ErrPullFailed) Unwrap() error { return e.Cause }

// ErrArchiveNotFound is returned when the archive file path does not exist
// or is not a regular file. Distinct from ErrInvalidArchive so callers can
// give a different message for "wrong path" vs "wrong contents".
type ErrArchiveNotFound struct {
	Path  string
	Cause error
}

func (e *ErrArchiveNotFound) Error() string {
	return fmt.Sprintf("archive %q not found: %v", e.Path, e.Cause)
}

func (e *ErrArchiveNotFound) Unwrap() error { return e.Cause }

// ErrArchivePermission is returned when the archive file exists but the
// current user cannot open it (permission denied). Distinct from
// ErrArchiveNotFound so the user sees "fix your permissions" rather than
// "fix your path".
type ErrArchivePermission struct {
	Path  string
	Cause error
}

func (e *ErrArchivePermission) Error() string {
	return fmt.Sprintf("permission denied opening archive %q: %v", e.Path, e.Cause)
}

func (e *ErrArchivePermission) Unwrap() error { return e.Cause }

// ErrInvalidArchive is returned when the file exists and is readable but is
// not a valid docker-save / OCI image archive (missing manifest.json,
// malformed manifest, malformed config, etc).
type ErrInvalidArchive struct {
	Path  string
	Cause error
}

func (e *ErrInvalidArchive) Error() string {
	return fmt.Sprintf("not a valid image archive %q: %v", e.Path, e.Cause)
}

func (e *ErrInvalidArchive) Unwrap() error { return e.Cause }

// ErrArchiveInfra signals an infrastructure-class failure encountered while
// processing an archive: temp file creation, disk-full while spooling, etc.
// Distinct from ErrInvalidArchive — the archive may be perfectly valid; the
// host environment failed. Callers should surface the cause directly rather
// than telling the user their tarball is malformed.
type ErrArchiveInfra struct {
	Op    string
	Cause error
}

func (e *ErrArchiveInfra) Error() string {
	return fmt.Sprintf("%s: %v", e.Op, e.Cause)
}

func (e *ErrArchiveInfra) Unwrap() error { return e.Cause }

type ErrPodmanSocketNotSet struct {
	Platform string
}

func (e *ErrPodmanSocketNotSet) Error() string {
	return fmt.Sprintf("--engine podman on %s: no active Podman connection or endpoint. "+
		"Configure one with `podman system connection add` and "+
		"`podman system connection default`, or set CONTAINER_HOST / DOCKER_HOST.",
		e.Platform)
}

type ErrNoEngineFound struct {
	Tried []string
}

func (e *ErrNoEngineFound) Error() string {
	return fmt.Sprintf("no container engine found; tried: %s",
		strings.Join(e.Tried, ", "))
}

// ErrPlatformInvalid is returned when --platform cannot be parsed at all
// (empty component, too many slashes). Distinct from ErrPlatformNotInImage:
// the spec itself is malformed, no image lookup happened.
type ErrPlatformInvalid struct {
	Spec   string
	Reason string
}

func (e *ErrPlatformInvalid) Error() string {
	return fmt.Sprintf("invalid --platform %q: %s "+
		"(expected ARCH, OS/ARCH, or OS/ARCH/VARIANT, e.g. \"linux/amd64\" or \"linux/arm64/v8\")",
		e.Spec, e.Reason)
}

// ErrPlatformNotInImage is returned when --platform is well-formed but the
// requested variant does not exist in the image's manifest list. Available
// holds the platforms the image does carry, formatted as "os/arch[/variant]";
// it is best-effort and may be empty when the daemon does not expose a
// manifest list (legacy image store, older daemons). When Available is
// empty the error renders as the terse "platform X not found" — the user
// still sees what they asked for, with no guess at what the image carries.
//
// The Error() output deliberately omits the image ref to keep the user-
// facing message tight (the ref is already in the user's command line).
// Callers that want to render the ref alongside the message should read
// e.Ref directly rather than parse Error() output.
type ErrPlatformNotInImage struct {
	Ref       string
	Requested string
	Available []string
}

func (e *ErrPlatformNotInImage) Error() string {
	if len(e.Available) == 0 {
		return fmt.Sprintf("platform %s not found", e.Requested)
	}
	return fmt.Sprintf("platform %s not found\n\nAvailable platforms:\n- %s",
		e.Requested, strings.Join(e.Available, "\n- "))
}
