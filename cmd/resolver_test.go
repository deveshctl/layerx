package cmd

import (
	"errors"
	"os"
	"runtime"
	"testing"

	"github.com/deveshctl/layerx/image"
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
	defer swapProber(newFakeProber())()

	host, err := autoEngineHost()
	if err != nil {
		t.Fatalf("autoEngineHost() error: %v", err)
	}
	if host != "" {
		t.Fatalf("host = %q, want empty (env-driven)", host)
	}
}

func TestSelectDockerLikeResolver_AutoFallback(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only fallback chain")
	}
	t.Setenv("DOCKER_HOST", "")

	docker := "/var/run/docker.sock"
	podmanRootless := podmanRootlessSocketPath()

	cases := []struct {
		name     string
		present  []string
		wantHost string
	}{
		{"docker present", []string{docker}, "unix://" + docker},
		{"docker missing, podman present", []string{podmanRootless}, "unix://" + podmanRootless},
		{"both present, docker wins", []string{docker, podmanRootless}, "unix://" + docker},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer swapProber(newFakeProber(tc.present...))()
			host, err := autoEngineHost()
			if err != nil {
				t.Fatalf("autoEngineHost() error: %v", err)
			}
			if host != tc.wantHost {
				t.Fatalf("host = %q, want %q", host, tc.wantHost)
			}
		})
	}
}

func TestSelectDockerLikeResolver_AutoNoneFound(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only fallback chain")
	}
	t.Setenv("DOCKER_HOST", "")
	defer swapProber(newFakeProber())()

	_, err := autoEngineHost()
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
