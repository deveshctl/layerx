package engine

import (
	"errors"
	"strings"
	"testing"
)

func TestPodmanResolver_ContainerHostEnvWins(t *testing.T) {
	fs := newMemFS("/h", "/h/.config")
	// A populated connections file must not be consulted when
	// CONTAINER_HOST is set.
	fs.putStr("/h/.config/containers/podman-connections.json", `{
"Connection":{"Default":"prod","Connections":{"prod":{"URI":"unused"}}}}`)

	r := newPodmanResolverWithDeps(staticEnv(map[string]string{
		"CONTAINER_HOST": "tcp://scripted:2375",
	}), fs)
	ep, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ep.Host != "tcp://scripted:2375" {
		t.Fatalf("Host = %q, want env passthrough", ep.Host)
	}
	if ep.Source != "env:CONTAINER_HOST" {
		t.Fatalf("Source = %q", ep.Source)
	}
}

func TestPodmanResolver_DockerHostEnvHonouredForBackCompat(t *testing.T) {
	// Users who scripted DOCKER_HOST=$(podman system connection list...)
	// against v1.4.x layerx must keep working. DOCKER_HOST is a last-resort
	// fallback: it wins when no Podman-specific env var or config file named
	// a connection (empty Default and no CONTAINER_CONNECTION).
	fs := newMemFS("/h", "/h/.config")
	// Connections file present but Default is empty — no named connection
	// selected, so resolution falls through to DOCKER_HOST.
	fs.putStr("/h/.config/containers/podman-connections.json", `{
"Connection":{"Default":"","Connections":{"prod":{"URI":"unused"}}}}`)

	r := newPodmanResolverWithDeps(staticEnv(map[string]string{
		"DOCKER_HOST": "tcp://legacy:2375",
	}), fs)
	ep, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ep.Host != "tcp://legacy:2375" {
		t.Fatalf("Host = %q, want DOCKER_HOST passthrough", ep.Host)
	}
	if ep.Source != "env:DOCKER_HOST" {
		t.Fatalf("Source = %q", ep.Source)
	}
}

func TestPodmanResolver_ConnectionsJSONDefault(t *testing.T) {
	fs := newMemFS("/h", "/h/.config")
	fs.putStr("/h/.config/containers/podman-connections.json", `{
	"Connection": {
		"Default": "staging",
		"Connections": {
			"staging": {"URI": "ssh://user@stg/run/user/1000/podman/podman.sock"},
			"prod":    {"URI": "ssh://user@prod/run/user/1000/podman/podman.sock"}
		}
	}
}`)

	r := newPodmanResolverWithDeps(staticEnv(nil), fs)
	ep, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ep.Host != "ssh://user@stg/run/user/1000/podman/podman.sock" {
		t.Fatalf("Host = %q", ep.Host)
	}
	if ep.Source != "podman-connection:staging" {
		t.Fatalf("Source = %q", ep.Source)
	}
}

func TestPodmanResolver_ContainerConnectionEnvBeatsDefault(t *testing.T) {
	fs := newMemFS("/h", "/h/.config")
	fs.putStr("/h/.config/containers/podman-connections.json", `{
	"Connection": {
		"Default": "staging",
		"Connections": {
			"staging": {"URI": "ssh://user@stg/podman.sock"},
			"prod":    {"URI": "ssh://user@prod/podman.sock"}
		}
	}
}`)

	r := newPodmanResolverWithDeps(staticEnv(map[string]string{
		"CONTAINER_CONNECTION": "prod",
	}), fs)
	ep, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ep.Host != "ssh://user@prod/podman.sock" {
		t.Fatalf("Host = %q, want prod override", ep.Host)
	}
}

func TestPodmanResolver_NoConfigReturnsZero(t *testing.T) {
	fs := newMemFS("/h", "/h/.config")
	r := newPodmanResolverWithDeps(staticEnv(nil), fs)
	ep, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !ep.IsZero() {
		t.Fatalf("Endpoint = %+v, want zero", ep)
	}
}

func TestPodmanResolver_ContainersConfFallback(t *testing.T) {
	// When podman-connections.json is absent, containers.conf must still
	// be honoured — this is the path older Podman installations still
	// exercise, and admin-provided system defaults live here.
	fs := newMemFS("/h", "/h/.config")
	fs.putStr("/h/.config/containers/containers.conf", `
# admin default
[engine]
active_service = "corp"

[engine.service_destinations.corp]
uri = "ssh://ci@corp-host/run/user/1000/podman/podman.sock"
identity = "/etc/ci/id_rsa"

[engine.service_destinations.local]
uri = "unix:///run/user/1000/podman/podman.sock"
`)

	r := newPodmanResolverWithDeps(staticEnv(nil), fs)
	ep, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ep.Host != "ssh://ci@corp-host/run/user/1000/podman/podman.sock" {
		t.Fatalf("Host = %q", ep.Host)
	}
	if ep.Source != "podman-connection:corp" {
		t.Fatalf("Source = %q", ep.Source)
	}
}

func TestPodmanResolver_ContainersConfQuotedSectionName(t *testing.T) {
	// Podman quotes section-name segments that contain dashes or dots.
	// The parser must peel that quoting to match the destination key.
	fs := newMemFS("/h", "/h/.config")
	fs.putStr("/h/.config/containers/containers.conf", `
[engine]
active_service = "my-dev"

[engine.service_destinations."my-dev"]
uri = "tcp://dev.example:2376"
`)

	r := newPodmanResolverWithDeps(staticEnv(nil), fs)
	ep, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ep.Host != "tcp://dev.example:2376" {
		t.Fatalf("Host = %q, want quoted-section lookup to succeed", ep.Host)
	}
}

func TestPodmanResolver_JSONWinsOverContainersConf(t *testing.T) {
	// When both files are present, podman-connections.json is
	// authoritative — this matches Podman's own read order.
	fs := newMemFS("/h", "/h/.config")
	fs.putStr("/h/.config/containers/podman-connections.json", `{
"Connection":{"Default":"json","Connections":{"json":{"URI":"unix:///from-json.sock"}}}}`)
	fs.putStr("/h/.config/containers/containers.conf", `
[engine]
active_service = "conf"
[engine.service_destinations.conf]
uri = "unix:///from-conf.sock"
`)

	r := newPodmanResolverWithDeps(staticEnv(nil), fs)
	ep, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ep.Host != "unix:///from-json.sock" {
		t.Fatalf("Host = %q, want json to win", ep.Host)
	}
}

func TestPodmanResolver_MalformedJSONErrors(t *testing.T) {
	fs := newMemFS("/h", "/h/.config")
	fs.putStr("/h/.config/containers/podman-connections.json", `not-json`)

	r := newPodmanResolverWithDeps(staticEnv(nil), fs)
	_, err := r.Resolve()
	var mal *ErrConfigMalformed
	if !errors.As(err, &mal) {
		t.Fatalf("err = %v, want *ErrConfigMalformed", err)
	}
	if !strings.Contains(mal.Path, "podman-connections.json") {
		t.Fatalf("Path = %q", mal.Path)
	}
}

func TestPodmanResolver_UnknownConnectionListsAvailable(t *testing.T) {
	fs := newMemFS("/h", "/h/.config")
	fs.putStr("/h/.config/containers/podman-connections.json", `{
"Connection":{"Default":"","Connections":{
"prod":{"URI":"unix:///prod.sock"},
"staging":{"URI":"unix:///stg.sock"}
}}}`)

	r := newPodmanResolverWithDeps(staticEnv(map[string]string{
		"CONTAINER_CONNECTION": "typo",
	}), fs)
	_, err := r.Resolve()
	var nf *ErrConnectionNotFound
	if !errors.As(err, &nf) {
		t.Fatalf("err = %v, want *ErrConnectionNotFound", err)
	}
	if nf.Requested != "typo" {
		t.Fatalf("Requested = %q", nf.Requested)
	}
	if len(nf.Available) != 2 || nf.Available[0] != "prod" || nf.Available[1] != "staging" {
		t.Fatalf("Available = %v, want sorted [prod staging]", nf.Available)
	}
}

func TestPodmanResolver_EmptyDefaultIsZeroEndpoint(t *testing.T) {
	// A file with connections but no Default and no env override must
	// yield the zero Endpoint (fall back to socket probe), not error.
	fs := newMemFS("/h", "/h/.config")
	fs.putStr("/h/.config/containers/podman-connections.json", `{
"Connection":{"Default":"","Connections":{"prod":{"URI":"unix:///prod.sock"}}}}`)

	r := newPodmanResolverWithDeps(staticEnv(nil), fs)
	ep, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !ep.IsZero() {
		t.Fatalf("Endpoint = %+v, want zero", ep)
	}
}

func TestParsePodmanContainersConf_LineCommentsAndBareValues(t *testing.T) {
	// Coverage for the scoped INI-shape parser: # comments, ; comments,
	// bare-value URIs (no quotes), CRLF line endings.
	data := []byte("# top comment\r\n[engine]\r\nactive_service = corp ; inline\r\n\r\n[engine.service_destinations.corp]\r\nuri = unix:///bare.sock\r\n")

	conns, def, err := parsePodmanContainersConf(data, "containers.conf")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if def != "corp" {
		t.Fatalf("default = %q", def)
	}
	if conns["corp"] != "unix:///bare.sock" {
		t.Fatalf("corp URI = %q", conns["corp"])
	}
}
