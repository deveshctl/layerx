package cmd

import (
	"fmt"
	"os"
	"runtime"
	"strconv"

	"github.com/deveshctl/layerx/image"
	"github.com/deveshctl/layerx/image/engine"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

var engineFlag = "auto"

// platformFlag holds the raw --platform string. Empty = "use the daemon's
// default platform" (historic behaviour). Parsed once per Run via
// selectResolverDefault so a typo surfaces as a single error before any I/O.
var platformFlag = ""

var selectResolver = selectResolverDefault

var socketProberImpl socketProber = osSocketProber{}

// endpointResolvers reports the active daemon endpoint for each engine
// name, following the engine's own config layout (Docker contexts, Podman
// connections). Wrapped in package-level vars so tests can inject fakes
// without touching real ~/.docker/ or ~/.config/containers/ files.
var (
	dockerEndpointResolver engineEndpointResolver = engine.NewDockerResolver()
	podmanEndpointResolver engineEndpointResolver = engine.NewPodmanResolver()
)

// engineEndpointResolver mirrors engine.Resolver as a local interface so
// tests can swap the resolvers without importing engine internals into
// every test file. Any type implementing engine.Resolver satisfies this.
type engineEndpointResolver interface {
	Name() string
	Resolve() (engine.Endpoint, error)
}

type socketProber interface {
	Exists(path string) bool
}

type osSocketProber struct{}

func (osSocketProber) Exists(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	return true
}

func selectResolverDefault(imageRef string) (image.Resolver, error) {
	plat, err := image.ParsePlatform(platformFlag)
	if err != nil {
		return nil, err
	}
	if isRegularFilePath(imageRef) {
		return image.NewArchiveResolverWithPlatform(imageRef, plat), nil
	}
	return selectDockerLikeResolver(engineFlag, plat)
}

// activePlatformDisplay returns the canonical "os/arch[/variant]" form of the
// active --platform flag, or "" when no pin is set or the spec was malformed
// (the malformed case is reported elsewhere by selectResolver). Used by the
// JSON exporter and the TUI bridge to surface which variant is on screen
// without each caller re-implementing parse + format.
func activePlatformDisplay() string {
	if platformFlag == "" {
		return ""
	}
	plat, err := image.ParsePlatform(platformFlag)
	if err != nil || plat == nil {
		return ""
	}
	return image.FormatPlatform(plat)
}

func selectDockerLikeResolver(engineName string, plat *ocispec.Platform) (image.Resolver, error) {
	switch engineName {
	case "docker":
		host, err := dockerHost()
		if err != nil {
			return nil, err
		}
		if host == "" {
			// No context, no DOCKER_HOST — moby client's FromEnv falls
			// back to the platform-default socket, matching the pre-
			// context behaviour so users who have never touched contexts
			// see no change.
			r, err := image.NewDockerResolver(image.WithPlatform(plat))
			if err != nil {
				return nil, fmt.Errorf("failed to initialize: %w", err)
			}
			return r, nil
		}
		return image.NewDockerResolverWithHost(host, image.WithPlatform(plat))
	case "podman":
		host, err := podmanHost()
		if err != nil {
			return nil, err
		}
		if host == "" {
			return image.NewDockerResolver(image.WithPlatform(plat))
		}
		return image.NewDockerResolverWithHost(host, image.WithPlatform(plat))
	case "auto":
		host, err := autoEngineHost()
		if err != nil {
			return nil, err
		}
		if host == "" {
			return image.NewDockerResolver(image.WithPlatform(plat))
		}
		return image.NewDockerResolverWithHost(host, image.WithPlatform(plat))
	default:
		return nil, fmt.Errorf("unknown engine %q (expected docker, podman, or auto)", engineName)
	}
}

// dockerHost resolves the endpoint for --engine docker. Precedence:
//   1. DOCKER_HOST env (kept in step with the docker CLI itself)
//   2. Active Docker context (~/.docker/config.json + contexts/meta/…)
//   3. "" → moby client falls back to the platform default socket
//
// The engine.Resolver captures 1 and 2 already; we return its Endpoint.Host
// verbatim. Errors (malformed config, missing named context) propagate so
// the user sees the same message the docker CLI would give.
func dockerHost() (string, error) {
	ep, err := dockerEndpointResolver.Resolve()
	if err != nil {
		return "", err
	}
	return ep.Host, nil
}

// podmanHost resolves the endpoint for --engine podman. Precedence:
//   1. CONTAINER_HOST / DOCKER_HOST env (either is respected)
//   2. CONTAINER_CONNECTION env, otherwise active Podman connection
//   3. Rootless / rootful socket probe on Linux
//   4. ErrPodmanSocketNotSet (non-Linux without any of the above)
//
// The Linux socket probe remains a legitimate last resort: many first-time
// Podman users on Linux have `systemctl --user enable podman.socket`
// running without having ever run `podman system connection add`, so a
// bare socket at /run/user/<uid>/podman/podman.sock is what "just works"
// today. Matching that expectation is why the probe stays after the
// resolver, not before it.
func podmanHost() (string, error) {
	ep, err := podmanEndpointResolver.Resolve()
	if err != nil {
		return "", err
	}
	if ep.Host != "" {
		return ep.Host, nil
	}
	if runtime.GOOS != "linux" {
		return "", &image.ErrPodmanSocketNotSet{Platform: runtime.GOOS}
	}
	candidates := podmanSocketCandidates()
	if path, ok := probeFirst(candidates); ok {
		return "unix://" + path, nil
	}
	return "", &image.ErrNoEngineFound{Tried: candidates}
}

// autoEngineHost picks an endpoint when the user has not specified an
// engine. Precedence:
//   1. Docker resolver (env or active context) — Docker has been the
//      default since layerx v1.4.0, so it's still tried first
//   2. Podman resolver (env or active connection)
//   3. Docker socket probe
//   4. Podman socket probe (Linux)
//   5. ErrNoEngineFound listing everything we tried
func autoEngineHost() (string, error) {
	if ep, err := dockerEndpointResolver.Resolve(); err != nil {
		return "", err
	} else if ep.Host != "" {
		// DOCKER_HOST callers already relied on the moby client's
		// FromEnv seeing the variable; keep returning "" for that case
		// so we don't reroute through WithHost and change the moby
		// client's own error surface.
		if ep.Source == "env:DOCKER_HOST" {
			return "", nil
		}
		return ep.Host, nil
	}
	if ep, err := podmanEndpointResolver.Resolve(); err != nil {
		return "", err
	} else if ep.Host != "" {
		return ep.Host, nil
	}

	docker := dockerSocketCandidates()
	if path, ok := probeFirst(docker); ok {
		return "unix://" + path, nil
	}
	var podman []string
	if runtime.GOOS == "linux" {
		podman = podmanSocketCandidates()
		if path, ok := probeFirst(podman); ok {
			return "unix://" + path, nil
		}
	}
	tried := make([]string, 0, len(docker)+len(podman))
	tried = append(tried, docker...)
	tried = append(tried, podman...)
	return "", &image.ErrNoEngineFound{Tried: tried}
}

func probeFirst(candidates []string) (string, bool) {
	for _, p := range candidates {
		if socketProberImpl.Exists(p) {
			return p, true
		}
	}
	return "", false
}

func dockerSocketCandidates() []string {
	switch runtime.GOOS {
	case "linux":
		return []string{"/var/run/docker.sock"}
	case "darwin":
		home, _ := os.UserHomeDir()
		paths := []string{}
		if home != "" {
			paths = append(paths, home+"/.docker/run/docker.sock")
		}
		paths = append(paths, "/var/run/docker.sock")
		return paths
	case "windows":
		return []string{`\\.\pipe\docker_engine`}
	default:
		return nil
	}
}

func podmanSocketCandidates() []string {
	if runtime.GOOS != "linux" {
		return nil
	}
	return []string{
		podmanRootlessSocketPath(),
		"/run/podman/podman.sock",
	}
}

func podmanRootlessSocketPath() string {
	return "/run/user/" + strconv.Itoa(os.Getuid()) + "/podman/podman.sock"
}

func isRegularFilePath(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}
