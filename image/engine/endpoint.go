// Package engine resolves which endpoint layerx should talk to for a given
// container engine (Docker, Podman).
//
// Container engines have their own notion of "which daemon am I currently
// talking to": Docker has contexts (`docker context use my-remote`), Podman
// has connections (`podman system connection default staging`). A tool that
// speaks the Docker Engine REST API but ignores those files ends up talking
// to a different daemon than the user's own CLI would — either failing to
// connect or, worse, silently inspecting the wrong image on the wrong host.
//
// The Resolver interface here is the single place layerx asks "for engine E,
// what's the active endpoint on this machine, and how did we pick it?".
// Adding support for a future engine means implementing one more Resolver;
// nothing in cmd/ needs to change beyond wiring the new engine name in.
package engine

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Endpoint is a resolved daemon target for one engine.
//
// Host is the URL the moby client understands (unix://, npipe://, tcp://,
// ssh://). Source describes where the value came from ("env:DOCKER_HOST",
// "docker-context:my-remote", "podman-connection:staging"); it is intended
// for human-readable diagnostic output, not for programmatic dispatch.
//
// The zero value means "no active endpoint configured" — a legitimate
// outcome, distinct from an error. Callers should treat it as a signal to
// fall back to the historical socket-probing path so users who have never
// touched contexts or connections see no behaviour change.
type Endpoint struct {
	Host   string
	Source string
}

func (e Endpoint) IsZero() bool { return e.Host == "" }

// Resolver reports the currently active endpoint for one engine on this
// machine. Implementations must not touch the network; they read only local
// config files and environment variables.
type Resolver interface {
	// Name is the short engine identifier ("docker", "podman") used in log
	// lines and error messages.
	Name() string

	// Resolve returns the active endpoint or the zero Endpoint when no
	// context/connection is configured for this engine.
	//
	// Implementations MUST honour engine-native environment overrides
	// (DOCKER_HOST / DOCKER_CONTEXT for Docker, CONTAINER_HOST /
	// CONTAINER_CONNECTION for Podman) so scripting and CI workflows that
	// already export those variables keep working unchanged.
	//
	// A malformed config file returns ErrConfigMalformed with enough
	// context to point the user at the offending file. Missing config
	// files are not errors — they return the zero Endpoint.
	Resolve() (Endpoint, error)
}

// ErrConfigMalformed is returned when a config file exists but cannot be
// parsed. Distinct from "no config present" (Endpoint{}, nil) so callers
// can surface a specific message instead of silently falling back to the
// historical socket probe on a user's broken JSON/TOML.
type ErrConfigMalformed struct {
	Engine string
	Path   string
	Cause  error
}

func (e *ErrConfigMalformed) Error() string {
	return fmt.Sprintf("%s: cannot parse %s: %v", e.Engine, e.Path, e.Cause)
}

func (e *ErrConfigMalformed) Unwrap() error { return e.Cause }

// ErrConnectionNotFound is returned when the user has explicitly named a
// context/connection (via DOCKER_CONTEXT / CONTAINER_CONNECTION or as the
// active default) that is not present in the on-disk config. Distinct from
// ErrConfigMalformed and from the zero Endpoint: the config is fine, the
// name just doesn't resolve. Available lists the names that DO exist, so
// the error can surface them without the caller re-reading the file.
type ErrConnectionNotFound struct {
	Engine    string
	Requested string
	Available []string
	// Origin describes where the requested name came from ("env" or
	// "active default in config"), so the message can distinguish a bad
	// env var from a stale config default.
	Origin string
}

func (e *ErrConnectionNotFound) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s %q not found", e.Engine, e.connectionNoun(), e.Requested)
	if e.Origin != "" {
		fmt.Fprintf(&b, " (from %s)", e.Origin)
	}
	if len(e.Available) > 0 {
		fmt.Fprintf(&b, "\n\nAvailable %ss:\n- %s",
			e.connectionNoun(), strings.Join(e.Available, "\n- "))
	}
	return b.String()
}

func (e *ErrConnectionNotFound) connectionNoun() string {
	if e.Engine == "docker" {
		return "context"
	}
	return "connection"
}

// nameSort sorts a slice of names in-place, case-insensitively. Used to
// produce deterministic Available lists in ErrConnectionNotFound regardless
// of map-iteration order.
func nameSort(names []string) {
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
}

// errNoActiveEndpoint is a sentinel used internally when a helper wants to
// say "I looked, nothing is set" without allocating an error wrapper. Not
// exported: callers see (Endpoint{}, nil) instead.
var errNoActiveEndpoint = errors.New("no active endpoint")
