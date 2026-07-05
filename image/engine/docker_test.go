package engine

import (
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// memFS is an in-memory files impl. Every test that touches disk builds
// one of these instead of a real ~/.docker/ tree so tests are hermetic
// across dev machines and CI runners.
type memFS struct {
	files map[string][]byte
	home  string
	cfg   string
}

func newMemFS(home, cfg string) *memFS {
	return &memFS{files: map[string][]byte{}, home: home, cfg: cfg}
}

func (m *memFS) put(path string, data []byte) { m.files[filepath.Clean(path)] = data }
func (m *memFS) putStr(path string, s string) { m.put(path, []byte(s)) }

func (m *memFS) readFile(path string) ([]byte, error) {
	if b, ok := m.files[filepath.Clean(path)]; ok {
		return b, nil
	}
	return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
}

func (m *memFS) stat(path string) (fs.FileInfo, error) {
	p := filepath.Clean(path)
	if _, ok := m.files[p]; ok {
		return &memInfo{name: filepath.Base(p), size: int64(len(m.files[p]))}, nil
	}
	// Treat any path that is a prefix of a stored file as a directory —
	// enough for the enumerator's IsDir check.
	prefix := p + string(filepath.Separator)
	for k := range m.files {
		if strings.HasPrefix(k, prefix) {
			return &memInfo{name: filepath.Base(p), dir: true}, nil
		}
	}
	return nil, &fs.PathError{Op: "stat", Path: path, Err: fs.ErrNotExist}
}

func (m *memFS) readDir(dir string) ([]fs.DirEntry, error) {
	d := filepath.Clean(dir) + string(filepath.Separator)
	seen := map[string]bool{}
	var out []fs.DirEntry
	for k := range m.files {
		if !strings.HasPrefix(k, d) {
			continue
		}
		rest := strings.TrimPrefix(k, d)
		parts := strings.Split(rest, string(filepath.Separator))
		head := parts[0]
		if seen[head] {
			continue
		}
		seen[head] = true
		out = append(out, &memInfo{name: head, dir: len(parts) > 1})
	}
	if len(out) == 0 {
		if _, err := m.stat(filepath.Clean(dir)); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (m *memFS) homeDir() (string, error) {
	if m.home == "" {
		return "", errors.New("no home")
	}
	return m.home, nil
}

func (m *memFS) configDir() (string, error) {
	if m.cfg == "" {
		return "", errors.New("no config dir")
	}
	return m.cfg, nil
}

type memInfo struct {
	name string
	size int64
	dir  bool
}

func (i *memInfo) Name() string { return i.name }
func (i *memInfo) Size() int64  { return i.size }
func (i *memInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (i *memInfo) ModTime() time.Time { return time.Time{} }
func (i *memInfo) IsDir() bool        { return i.dir }
func (i *memInfo) Sys() any           { return nil }

// fs.DirEntry interface.
func (i *memInfo) Type() fs.FileMode          { return i.Mode().Type() }
func (i *memInfo) Info() (fs.FileInfo, error) { return i, nil }

// staticEnv makes a stub env(key) function backed by a map.
func staticEnv(kv map[string]string) env {
	return func(key string) string { return kv[key] }
}

func TestDockerResolver_EnvOverrideBeatsContext(t *testing.T) {
	// DOCKER_HOST must beat any active context — this is the escape hatch
	// scripting and CI workflows rely on.
	fs := newMemFS("/h", "/h/.config")
	// Even a fully-populated context store must not be consulted.
	writeCtx(t, fs, "/h/.docker", "remote", "tcp://unused:2376")
	fs.putStr("/h/.docker/config.json", `{"currentContext":"remote"}`)

	r := newDockerResolverWithDeps(staticEnv(map[string]string{
		"DOCKER_HOST": "tcp://scripted:2375",
	}), fs)
	ep, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ep.Host != "tcp://scripted:2375" {
		t.Fatalf("Host = %q, want env passthrough", ep.Host)
	}
	if ep.Source != "env:DOCKER_HOST" {
		t.Fatalf("Source = %q, want env:DOCKER_HOST", ep.Source)
	}
}

func TestDockerResolver_ActiveContextInConfigJSON(t *testing.T) {
	fs := newMemFS("/h", "/h/.config")
	writeCtx(t, fs, "/h/.docker", "my-remote", "tcp://remote.example:2376")
	fs.putStr("/h/.docker/config.json", `{"currentContext":"my-remote"}`)

	r := newDockerResolverWithDeps(staticEnv(nil), fs)
	ep, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ep.Host != "tcp://remote.example:2376" {
		t.Fatalf("Host = %q", ep.Host)
	}
	if ep.Source != "docker-context:my-remote" {
		t.Fatalf("Source = %q", ep.Source)
	}
}

func TestDockerResolver_EnvContextBeatsConfigJSON(t *testing.T) {
	fs := newMemFS("/h", "/h/.config")
	writeCtx(t, fs, "/h/.docker", "one", "tcp://one:2376")
	writeCtx(t, fs, "/h/.docker", "two", "tcp://two:2376")
	fs.putStr("/h/.docker/config.json", `{"currentContext":"one"}`)

	r := newDockerResolverWithDeps(staticEnv(map[string]string{
		"DOCKER_CONTEXT": "two",
	}), fs)
	ep, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ep.Host != "tcp://two:2376" {
		t.Fatalf("Host = %q, want DOCKER_CONTEXT to win", ep.Host)
	}
}

func TestDockerResolver_NoConfigReturnsZero(t *testing.T) {
	// A user who has never touched contexts must see the zero Endpoint —
	// NOT an error — so cmd/resolver.go falls back to the historic socket
	// probe. This is the "no regression for existing users" guarantee.
	fs := newMemFS("/h", "/h/.config")
	r := newDockerResolverWithDeps(staticEnv(nil), fs)
	ep, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !ep.IsZero() {
		t.Fatalf("Endpoint = %+v, want zero", ep)
	}
}

func TestDockerResolver_DefaultContextSentinelIsZero(t *testing.T) {
	// "default" is the docker CLI's sentinel for "no context, use platform
	// default"; treat it as no-active-endpoint rather than trying to look
	// up a meta.json for the literal name "default".
	fs := newMemFS("/h", "/h/.config")
	fs.putStr("/h/.docker/config.json", `{"currentContext":"default"}`)

	r := newDockerResolverWithDeps(staticEnv(nil), fs)
	ep, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !ep.IsZero() {
		t.Fatalf("Endpoint = %+v, want zero for default sentinel", ep)
	}
}

func TestDockerResolver_MalformedConfigErrors(t *testing.T) {
	fs := newMemFS("/h", "/h/.config")
	fs.putStr("/h/.docker/config.json", `{not json`)

	r := newDockerResolverWithDeps(staticEnv(nil), fs)
	_, err := r.Resolve()
	var mal *ErrConfigMalformed
	if !errors.As(err, &mal) {
		t.Fatalf("err = %v, want *ErrConfigMalformed", err)
	}
	if !strings.Contains(mal.Path, "config.json") {
		t.Fatalf("Path = %q, want config.json", mal.Path)
	}
}

func TestDockerResolver_UnknownContextListsAvailable(t *testing.T) {
	fs := newMemFS("/h", "/h/.config")
	writeCtx(t, fs, "/h/.docker", "prod", "tcp://prod:2376")
	writeCtx(t, fs, "/h/.docker", "staging", "tcp://staging:2376")

	r := newDockerResolverWithDeps(staticEnv(map[string]string{
		"DOCKER_CONTEXT": "typo",
	}), fs)
	_, err := r.Resolve()
	var nf *ErrConnectionNotFound
	if !errors.As(err, &nf) {
		t.Fatalf("err = %v, want *ErrConnectionNotFound", err)
	}
	if nf.Requested != "typo" {
		t.Fatalf("Requested = %q", nf.Requested)
	}
	// Available list is alphabetised for deterministic messages.
	if len(nf.Available) != 2 || nf.Available[0] != "prod" || nf.Available[1] != "staging" {
		t.Fatalf("Available = %v, want [prod staging]", nf.Available)
	}
	if !strings.Contains(nf.Error(), "prod") || !strings.Contains(nf.Error(), "staging") {
		t.Fatalf("Error() missing available names: %s", nf.Error())
	}
}

// writeCtx populates fs with a Docker context named `name` pointing at
// `host`, using the same SHA-256 directory hashing docker/cli does. Kept
// as a test helper so each test reads cleanly.
func writeCtx(t *testing.T, m *memFS, root, name, host string) {
	t.Helper()
	dir := filepath.Join(root, "contexts", "meta", sha256Hex(name))
	meta := contextMetadata{
		Name: name,
		Endpoints: map[string]contextEndpointEntry{
			"docker": {Host: host},
		},
	}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal ctx: %v", err)
	}
	m.put(filepath.Join(dir, "meta.json"), data)
}
