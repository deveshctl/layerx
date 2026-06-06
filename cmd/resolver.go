package cmd

import (
	"fmt"
	"os"
	"runtime"
	"strconv"

	"github.com/deveshctl/layerx/image"
)

var engineFlag = "auto"

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
	if isRegularFilePath(imageRef) {
		return image.NewArchiveResolver(imageRef), nil
	}
	return selectDockerLikeResolver(engineFlag)
}

func selectDockerLikeResolver(engine string) (image.Resolver, error) {
	switch engine {
	case "docker":
		r, err := image.NewDockerResolver()
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
			return image.NewDockerResolver()
		}
		return image.NewDockerResolverWithHost(host)
	case "auto":
		host, err := autoEngineHost()
		if err != nil {
			return nil, err
		}
		if host == "" {
			return image.NewDockerResolver()
		}
		return image.NewDockerResolverWithHost(host)
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
