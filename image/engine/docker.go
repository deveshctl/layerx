package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// DockerResolver reports the endpoint the Docker CLI would talk to, given
// the current process environment and ~/.docker/ layout.
//
// Resolution order, mirroring `docker` itself:
//  1. DOCKER_HOST  — the explicit override every Docker script already uses
//  2. DOCKER_CONTEXT — env-selected context (overrides config.json "currentContext")
//  3. currentContext in ~/.docker/config.json
//  4. no active endpoint (Endpoint{}, nil)
//
// The zero value is a Docker resolver backed by os.Getenv and the real
// filesystem; the env/files fields are only overridden in tests.
type DockerResolver struct {
	env   env
	files files
}

// NewDockerResolver returns a resolver using the real process environment
// and filesystem. Test constructors go through newDockerResolverWithDeps.
func NewDockerResolver() *DockerResolver {
	return &DockerResolver{env: os.Getenv, files: osFiles{}}
}

// newDockerResolverWithDeps is the test seam. Not exported: production
// code paths must construct via NewDockerResolver.
func newDockerResolverWithDeps(e env, f files) *DockerResolver {
	return &DockerResolver{env: e, files: f}
}

func (r *DockerResolver) Name() string { return "docker" }

func (r *DockerResolver) Resolve() (Endpoint, error) {
	if h := r.env("DOCKER_HOST"); h != "" {
		return Endpoint{Host: h, Source: "env:DOCKER_HOST"}, nil
	}

	activeName, activeOrigin, cfg, err := r.activeContext()
	if err != nil {
		return Endpoint{}, err
	}
	if activeName == "" || activeName == "default" {
		// "default" is the docker CLI's sentinel for "no context, use the
		// platform default socket". Treat it as no-active-endpoint so the
		// caller falls through to socket probing.
		return Endpoint{}, nil
	}

	host, available, err := r.readContextHost(activeName)
	if err != nil {
		if errors.Is(err, errNoActiveEndpoint) {
			nameSort(available)
			return Endpoint{}, &ErrConnectionNotFound{
				Engine:    "docker",
				Requested: activeName,
				Available: available,
				Origin:    activeOrigin,
			}
		}
		return Endpoint{}, err
	}
	if host == "" {
		// The context exists but declares no docker Host — treat as no
		// active endpoint rather than an error. This mirrors what the
		// docker CLI does: fall back to platform default.
		_ = cfg
		return Endpoint{}, nil
	}
	return Endpoint{
		Host:   host,
		Source: "docker-context:" + activeName,
	}, nil
}

// dockerConfig is the subset of ~/.docker/config.json layerx cares about.
// Docker CLI writes many other keys (auths, credsStore, plugins, aliases…);
// we ignore all of them.
type dockerConfig struct {
	CurrentContext string `json:"currentContext"`
}

func (r *DockerResolver) activeContext() (string, string, *dockerConfig, error) {
	if v := r.env("DOCKER_CONTEXT"); v != "" {
		return v, "env:DOCKER_CONTEXT", nil, nil
	}
	cfgPath := homePath(r.files, ".docker", "config.json")
	if cfgPath == "" {
		return "", "", nil, nil
	}
	data, err := r.files.readFile(cfgPath)
	if err != nil {
		if isNotExist(err) {
			return "", "", nil, nil
		}
		return "", "", nil, err
	}
	var cfg dockerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", "", nil, &ErrConfigMalformed{
			Engine: "docker",
			Path:   cfgPath,
			Cause:  err,
		}
	}
	if cfg.CurrentContext == "" {
		return "", "", &cfg, nil
	}
	return cfg.CurrentContext, "docker config", &cfg, nil
}

// contextMetadata mirrors the on-disk shape of
// ~/.docker/contexts/meta/<hash>/meta.json. Docker CLI's own struct nests
// Endpoints as map[string]any and defers concrete typing to a plugin
// registry at runtime; we only need Endpoints["docker"].Host, so we
// declare the concrete shape directly and let unknown extra keys be
// silently ignored by encoding/json.
type contextMetadata struct {
	Name      string                          `json:"Name"`
	Endpoints map[string]contextEndpointEntry `json:"Endpoints"`
}

type contextEndpointEntry struct {
	Host string `json:"Host"`
}

// readContextHost returns the docker Host declared for context name, along
// with the list of context names discovered on disk (for the error path
// when the requested name isn't there).
//
// Returns (host, available, nil) on success, ("", available, errNoActiveEndpoint)
// when name is not found on disk, or ("", nil, err) on a real I/O /
// parse failure.
func (r *DockerResolver) readContextHost(name string) (string, []string, error) {
	metaRoot := homePath(r.files, ".docker", "contexts", "meta")
	if metaRoot == "" {
		return "", nil, errNoActiveEndpoint
	}

	// Docker CLI hashes context names to directory identifiers with
	// SHA-256(name)-hex, matching the reference implementation in
	// docker/cli's cli/context/store package. We compute the same digest
	// so we can jump straight at the target directory without listing
	// every meta subdirectory when the config is well-formed.
	targetDir := filepath.Join(metaRoot, sha256Hex(name))
	metaPath := filepath.Join(targetDir, "meta.json")

	data, err := r.files.readFile(metaPath)
	if err == nil {
		host, herr := decodeContextHost(data, metaPath)
		if herr != nil {
			return "", nil, herr
		}
		if host != "" {
			return host, nil, nil
		}
		// Well-formed meta.json without a docker Host entry — treat as
		// "no active endpoint"; caller falls back to socket probing.
		return "", nil, nil
	}
	if !isNotExist(err) {
		return "", nil, err
	}

	// The name-hashed path isn't on disk. Fall back to enumerating the
	// meta directory: the user may have imported a context from another
	// machine where the hashing salt differs across Docker CLI versions
	// (defensive), or the name simply doesn't exist. Either way we build
	// the Available list here so ErrConnectionNotFound can render it.
	available, listErr := r.enumerateContexts(metaRoot)
	if listErr != nil {
		return "", nil, listErr
	}
	return "", available, errNoActiveEndpoint
}

// enumerateContexts walks ~/.docker/contexts/meta/*/meta.json and returns
// the Name field from each. Directories that fail to parse are skipped
// (best-effort — a single broken meta.json shouldn't blank out the
// Available list the user needs to see).
func (r *DockerResolver) enumerateContexts(metaRoot string) ([]string, error) {
	info, err := r.files.stat(metaRoot)
	if err != nil {
		if isNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}

	// osFiles.readFile is bounded per file; we still need directory
	// iteration. Route it through the files interface so tests can back
	// enumeration with an in-memory fake instead of a real ~/.docker/
	// tree.
	entries, err := r.files.readDir(metaRoot)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		metaPath := filepath.Join(metaRoot, e.Name(), "meta.json")
		data, err := r.files.readFile(metaPath)
		if err != nil {
			continue
		}
		var m contextMetadata
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m.Name != "" {
			out = append(out, m.Name)
		}
	}
	return out, nil
}

func decodeContextHost(data []byte, path string) (string, error) {
	var m contextMetadata
	if err := json.Unmarshal(data, &m); err != nil {
		return "", &ErrConfigMalformed{Engine: "docker", Path: path, Cause: err}
	}
	docker, ok := m.Endpoints["docker"]
	if !ok {
		return "", nil
	}
	return docker.Host, nil
}

// sha256Hex returns the lowercase hex encoding of SHA-256(s). Matches the
// contextdirOf output from docker/cli.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// isNotExist unwraps to fs.ErrNotExist. The os.ReadFile / os.Stat family
// wrap the underlying error in *fs.PathError; we use errors.Is to reach
// the leaf.
func isNotExist(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	// Some Windows shims report ENOTDIR when a component in the path is
	// a file. Callers of readFile treat that the same as "not present".
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) && errors.Is(pathErr.Err, fs.ErrNotExist) {
		return true
	}
	return false
}
