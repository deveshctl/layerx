package cmd

import (
	"errors"
	"os"
	"runtime"
	"testing"

	"github.com/deveshctl/layerx/image"
	"github.com/deveshctl/layerx/image/engine"
)

type fakeProber struct{ present map[string]bool }

func (f *fakeProber) Exists(path string) bool { return f.present[path] }

func newFakeProber(paths ...string) *fakeProber {
	m := make(map[string]bool, len(paths))
	for _, p := range paths {
		m[p] = true
	}
	return &fakeProber{present: m}
}

func swapProber(fp *fakeProber) func() {
	prev := socketProberImpl
	socketProberImpl = fp
	return func() { socketProberImpl = prev }
}

// fakeEndpointResolver is a test double for engineEndpointResolver so unit
// tests never touch the real ~/.docker/ or ~/.config/containers/ trees.
// Every socket-probing test path silences both resolvers by installing
// fakes that report "no active endpoint" (Endpoint{}, nil), preserving
// the original probe-based test expectations.
type fakeEndpointResolver struct {
	name string
	ep   engine.Endpoint
	err  error
}

func (f *fakeEndpointResolver) Name() string { return f.name }

func (f *fakeEndpointResolver) Resolve() (engine.Endpoint, error) {
	return f.ep, f.err
}

// swapEndpointResolvers installs empty-endpoint fakes for both docker and
// podman so the socket-probing paths run in isolation. Returns a restorer
// the caller defers.
func swapEndpointResolvers() func() {
	prevDocker := dockerEndpointResolver
	prevPodman := podmanEndpointResolver
	dockerEndpointResolver = &fakeEndpointResolver{name: "docker"}
	podmanEndpointResolver = &fakeEndpointResolver{name: "podman"}
	return func() {
		dockerEndpointResolver = prevDocker
		podmanEndpointResolver = prevPodman
	}
}

var osCreate = os.Create

func writeEmptyFile(path string) error {
	f, err := osCreate(path)
	if err != nil {
		return err
	}
	return f.Close()
}

func TestSelectDockerLikeResolver_PodmanLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only path")
	}
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("CONTAINER_HOST", "")
	t.Setenv("CONTAINER_CONNECTION", "")
	defer swapEndpointResolvers()()

	rootless := podmanRootlessSocketPath()
	rootful := "/run/podman/podman.sock"

	cases := []struct {
		name     string
		present  []string
		wantHost string
	}{
		{"rootless wins", []string{rootless, rootful}, "unix://" + rootless},
		{"rootful only", []string{rootful}, "unix://" + rootful},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer swapProber(newFakeProber(tc.present...))()
			host, err := podmanHost()
			if err != nil {
				t.Fatalf("podmanHost() error: %v", err)
			}
			if host != tc.wantHost {
				t.Fatalf("host = %q, want %q", host, tc.wantHost)
			}
		})
	}
}

func TestSelectDockerLikeResolver_PodmanLinuxNoSocket(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only path")
	}
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("CONTAINER_HOST", "")
	t.Setenv("CONTAINER_CONNECTION", "")
	defer swapEndpointResolvers()()
	defer swapProber(newFakeProber())()

	_, err := podmanHost()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var noEngine *image.ErrNoEngineFound
	if !errors.As(err, &noEngine) {
		t.Fatalf("error type = %T, want *image.ErrNoEngineFound", err)
	}
	if len(noEngine.Tried) == 0 {
		t.Fatal("ErrNoEngineFound.Tried should list probed paths")
	}
}

func TestSelectDockerLikeResolver_PodmanNonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("non-linux path")
	}
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("CONTAINER_HOST", "")
	t.Setenv("CONTAINER_CONNECTION", "")
	defer swapEndpointResolvers()()

	_, err := podmanHost()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var sockNotSet *image.ErrPodmanSocketNotSet
	if !errors.As(err, &sockNotSet) {
		t.Fatalf("error type = %T, want *image.ErrPodmanSocketNotSet", err)
	}
	if sockNotSet.Platform != runtime.GOOS {
		t.Fatalf("Platform = %q, want %q", sockNotSet.Platform, runtime.GOOS)
	}
}

func TestSelectDockerLikeResolver_PodmanRespectsEnv(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://example.invalid:2375")
	t.Setenv("CONTAINER_HOST", "")

	host, err := podmanHost()
	if err != nil {
		t.Fatalf("podmanHost() error: %v", err)
	}
	if host != "tcp://example.invalid:2375" {
		t.Fatalf("host = %q, want DOCKER_HOST passthrough", host)
	}
}

func TestSelectDockerLikeResolver_AutoEnvWins(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://example.invalid:2375")
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("CONTAINER_HOST", "")
	t.Setenv("CONTAINER_CONNECTION", "")
	defer swapProber(newFakeProber())()

	engineName, host, err := autoEngineHost()
	if err != nil {
		t.Fatalf("autoEngineHost() error: %v", err)
	}
	if host != "" {
		t.Fatalf("host = %q, want empty (env-driven)", host)
	}
	if engineName != "docker" {
		t.Fatalf("engine = %q, want docker (DOCKER_HOST is Docker's env)", engineName)
	}
}

func TestSelectDockerLikeResolver_AutoFallback(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only fallback chain")
	}
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("CONTAINER_HOST", "")
	t.Setenv("CONTAINER_CONNECTION", "")
	defer swapEndpointResolvers()()

	docker := "/var/run/docker.sock"
	podmanRootless := podmanRootlessSocketPath()

	cases := []struct {
		name       string
		present    []string
		wantHost   string
		wantEngine string
	}{
		{"docker present", []string{docker}, "unix://" + docker, "docker"},
		{"docker missing, podman present", []string{podmanRootless}, "unix://" + podmanRootless, "podman"},
		{"both present, docker wins", []string{docker, podmanRootless}, "unix://" + docker, "docker"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer swapProber(newFakeProber(tc.present...))()
			engineName, host, err := autoEngineHost()
			if err != nil {
				t.Fatalf("autoEngineHost() error: %v", err)
			}
			if host != tc.wantHost {
				t.Fatalf("host = %q, want %q", host, tc.wantHost)
			}
			if engineName != tc.wantEngine {
				t.Fatalf("engine = %q, want %q", engineName, tc.wantEngine)
			}
		})
	}
}

func TestSelectDockerLikeResolver_AutoNoneFound(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only fallback chain")
	}
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("CONTAINER_HOST", "")
	t.Setenv("CONTAINER_CONNECTION", "")
	defer swapEndpointResolvers()()
	defer swapProber(newFakeProber())()

	_, _, err := autoEngineHost()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var noEngine *image.ErrNoEngineFound
	if !errors.As(err, &noEngine) {
		t.Fatalf("error type = %T, want *image.ErrNoEngineFound", err)
	}
}

func TestSelectResolverDefault_RegularFileBypassesEngine(t *testing.T) {
	tmp := t.TempDir() + "/fake.tar"
	if err := writeEmptyFile(tmp); err != nil {
		t.Fatal(err)
	}
	prevFlag := engineFlag
	t.Cleanup(func() { engineFlag = prevFlag })
	for _, eng := range []string{"docker", "podman", "auto"} {
		engineFlag = eng
		r, err := selectResolverDefault(tmp)
		if err != nil {
			t.Fatalf("engine=%s: %v", eng, err)
		}
		if r == nil {
			t.Fatalf("engine=%s: nil resolver", eng)
		}
	}
}

func TestSelectResolverDefault_InvalidPlatformIsRejectedEarly(t *testing.T) {
	// A malformed --platform must not reach any image-resolution code path.
	// selectResolverDefault returns ErrPlatformInvalid synchronously so the
	// CLI presents a clear error before any daemon call or archive open.
	prevPlat := platformFlag
	t.Cleanup(func() { platformFlag = prevPlat })
	platformFlag = "linux/amd64/v8/extra"

	tmp := t.TempDir() + "/fake.tar"
	if err := writeEmptyFile(tmp); err != nil {
		t.Fatal(err)
	}
	_, err := selectResolverDefault(tmp)
	if err == nil {
		t.Fatal("expected error for malformed platform, got nil")
	}
	var inv *image.ErrPlatformInvalid
	if !errors.As(err, &inv) {
		t.Fatalf("error type = %T, want *image.ErrPlatformInvalid", err)
	}
}

func TestSelectResolverDefault_ValidPlatformPassesThrough(t *testing.T) {
	// A well-formed --platform on an archive path must produce a resolver
	// with the platform pin set; we test the seam by confirming no error
	// from selectResolverDefault and that subsequent Resolve would reject
	// a mismatching archive (covered by archive_test.go).
	prevPlat := platformFlag
	t.Cleanup(func() { platformFlag = prevPlat })
	platformFlag = "linux/arm64"

	tmp := t.TempDir() + "/fake.tar"
	if err := writeEmptyFile(tmp); err != nil {
		t.Fatal(err)
	}
	r, err := selectResolverDefault(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r == nil {
		t.Fatal("nil resolver")
	}
}

func TestDockerHost_HonoursActiveContext(t *testing.T) {
	// When a Docker context is active the resolver reports its Host, and
	// dockerHost() returns it verbatim so selectDockerLikeResolver builds
	// a moby client pointed at that URL. Env is empty in this test — a
	// dockerHost() call that returned "" here would silently fall through
	// to the platform default socket, exactly the bug this feature fixes.
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "")

	prev := dockerEndpointResolver
	dockerEndpointResolver = &fakeEndpointResolver{
		name: "docker",
		ep: engine.Endpoint{
			Host:   "tcp://remote.example:2376",
			Source: "docker-context:remote",
		},
	}
	t.Cleanup(func() { dockerEndpointResolver = prev })

	host, err := dockerHost()
	if err != nil {
		t.Fatalf("dockerHost() error: %v", err)
	}
	if host != "tcp://remote.example:2376" {
		t.Fatalf("host = %q, want tcp://remote.example:2376", host)
	}
}

func TestDockerHost_EnvOverrideStillWins(t *testing.T) {
	// DOCKER_HOST must beat any active Docker context. The resolver
	// itself returns the env value; we verify dockerHost() passes it
	// through unchanged and does NOT consult the context path.
	t.Setenv("DOCKER_HOST", "tcp://scripted-override:2376")

	prev := dockerEndpointResolver
	dockerEndpointResolver = &fakeEndpointResolver{
		name: "docker",
		ep: engine.Endpoint{
			Host:   "tcp://scripted-override:2376",
			Source: "env:DOCKER_HOST",
		},
	}
	t.Cleanup(func() { dockerEndpointResolver = prev })

	host, err := dockerHost()
	if err != nil {
		t.Fatalf("dockerHost() error: %v", err)
	}
	if host != "tcp://scripted-override:2376" {
		t.Fatalf("host = %q, want env passthrough", host)
	}
}

func TestDockerHost_NoContextReturnsEmpty(t *testing.T) {
	// A user who has never run `docker context use` must see the historic
	// behaviour: dockerHost() returns "", and the caller lets the moby
	// client fall back to the platform default socket.
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "")

	prev := dockerEndpointResolver
	dockerEndpointResolver = &fakeEndpointResolver{name: "docker"}
	t.Cleanup(func() { dockerEndpointResolver = prev })

	host, err := dockerHost()
	if err != nil {
		t.Fatalf("dockerHost() error: %v", err)
	}
	if host != "" {
		t.Fatalf("host = %q, want empty for no-context fallback", host)
	}
}

func TestPodmanHost_HonoursActiveConnection(t *testing.T) {
	// The whole point of the change: `podman system connection default X`
	// must be enough — no DOCKER_HOST workaround required. podmanHost()
	// returns the resolver's Endpoint.Host verbatim.
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("CONTAINER_HOST", "")
	t.Setenv("CONTAINER_CONNECTION", "")

	prev := podmanEndpointResolver
	podmanEndpointResolver = &fakeEndpointResolver{
		name: "podman",
		ep: engine.Endpoint{
			Host:   "unix:///run/user/1000/podman/podman.sock",
			Source: "podman-connection:staging",
		},
	}
	t.Cleanup(func() { podmanEndpointResolver = prev })

	host, err := podmanHost()
	if err != nil {
		t.Fatalf("podmanHost() error: %v", err)
	}
	if host != "unix:///run/user/1000/podman/podman.sock" {
		t.Fatalf("host = %q, want resolver value", host)
	}
}

func TestPodmanHost_ResolverErrorSurfaces(t *testing.T) {
	// A malformed podman-connections.json must not be silently swallowed;
	// the user sees the resolver's error rather than a confusing "no
	// container engine found".
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("CONTAINER_HOST", "")
	t.Setenv("CONTAINER_CONNECTION", "")

	sentinel := errors.New("podman-connections.json: unexpected EOF")
	prev := podmanEndpointResolver
	podmanEndpointResolver = &fakeEndpointResolver{name: "podman", err: sentinel}
	t.Cleanup(func() { podmanEndpointResolver = prev })

	_, err := podmanHost()
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel", err)
	}
}

func TestAutoEngineHost_ContextTakesPrecedenceOverSocket(t *testing.T) {
	// Auto mode must not silently fall through to the local Docker socket
	// when a Docker context points at a remote daemon. This is the exact
	// scenario the dive bug (#490) hit before it was fixed there.
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("CONTAINER_HOST", "")

	prevDocker := dockerEndpointResolver
	prevPodman := podmanEndpointResolver
	dockerEndpointResolver = &fakeEndpointResolver{
		name: "docker",
		ep: engine.Endpoint{
			Host:   "tcp://remote.example:2376",
			Source: "docker-context:remote",
		},
	}
	podmanEndpointResolver = &fakeEndpointResolver{name: "podman"}
	t.Cleanup(func() {
		dockerEndpointResolver = prevDocker
		podmanEndpointResolver = prevPodman
	})
	// Even with a local Docker socket "available", the context wins.
	defer swapProber(newFakeProber("/var/run/docker.sock"))()

	engineName, host, err := autoEngineHost()
	if err != nil {
		t.Fatalf("autoEngineHost() error: %v", err)
	}
	if host != "tcp://remote.example:2376" {
		t.Fatalf("host = %q, want tcp://remote.example:2376 (context wins)", host)
	}
	if engineName != "docker" {
		t.Fatalf("engine = %q, want docker", engineName)
	}
}

func TestAutoEngineHost_PodmanConnectionUsedWhenDockerAbsent(t *testing.T) {
	// A machine with only Podman configured (no docker context, no
	// DOCKER_HOST) must pick the Podman connection over the local Podman
	// socket probe — the connection is more specific than the socket.
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("CONTAINER_HOST", "")
	t.Setenv("CONTAINER_CONNECTION", "")

	prevDocker := dockerEndpointResolver
	prevPodman := podmanEndpointResolver
	dockerEndpointResolver = &fakeEndpointResolver{name: "docker"}
	podmanEndpointResolver = &fakeEndpointResolver{
		name: "podman",
		ep: engine.Endpoint{
			Host:   "ssh://user@dev-host/run/user/1000/podman/podman.sock",
			Source: "podman-connection:dev",
		},
	}
	t.Cleanup(func() {
		dockerEndpointResolver = prevDocker
		podmanEndpointResolver = prevPodman
	})
	// A local socket is present but the connection is more specific.
	defer swapProber(newFakeProber("/run/user/1000/podman/podman.sock"))()

	engineName, host, err := autoEngineHost()
	if err != nil {
		t.Fatalf("autoEngineHost() error: %v", err)
	}
	if host != "ssh://user@dev-host/run/user/1000/podman/podman.sock" {
		t.Fatalf("host = %q, want connection URI", host)
	}
	if engineName != "podman" {
		t.Fatalf("engine = %q, want podman (so ErrDaemonNotRunning is tagged correctly)", engineName)
	}
}
