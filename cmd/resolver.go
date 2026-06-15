package cmd

import (
	"fmt"
	"os"
	"runtime"
	"strconv"

	"github.com/deveshctl/layerx/image"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

var engineFlag = "auto"

// platformFlag holds the raw --platform string. Empty = "use the daemon's
// default platform" (historic behaviour). Parsed once per Run via
// selectResolverDefault so a typo surfaces as a single error before any I/O.
var platformFlag = ""

var selectResolver = selectResolverDefault

var socketProberImpl socketProber = osSocketProber{}

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

func selectDockerLikeResolver(engine string, plat *ocispec.Platform) (image.Resolver, error) {
	switch engine {
	case "docker":
		r, err := image.NewDockerResolver(image.WithPlatform(plat))
		if err != nil {
			return nil, fmt.Errorf("failed to initialize: %w", err)
		}
		return r, nil
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
		return nil, fmt.Errorf("unknown engine %q (expected docker, podman, or auto)", engine)
	}
}

func podmanHost() (string, error) {
	if v := os.Getenv("DOCKER_HOST"); v != "" {
		return v, nil
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

func autoEngineHost() (string, error) {
	if os.Getenv("DOCKER_HOST") != "" {
		return "", nil
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
