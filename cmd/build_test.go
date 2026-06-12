package cmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func resetEngineState(t *testing.T) {
	t.Helper()
	prevEngine := engineFlag
	prevJSON := flagJSON
	prevNoCache := flagNoCacheFl
	t.Cleanup(func() {
		engineFlag = prevEngine
		flagJSON = prevJSON
		flagNoCacheFl = prevNoCache
	})
	engineFlag = "auto"
	flagJSON = ""
	flagNoCacheFl = false
}

func TestExtractLayerxFlags_StripsEngineSpaceForm(t *testing.T) {
	resetEngineState(t)
	out, err := extractLayerxFlags([]string{"--engine", "podman", "-t", "x", "."})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if engineFlag != "podman" {
		t.Fatalf("engineFlag = %q, want podman", engineFlag)
	}
	want := []string{"-t", "x", "."}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("forwarded args = %v, want %v", out, want)
	}
}

func TestExtractLayerxFlags_StripsEngineEqualsForm(t *testing.T) {
	resetEngineState(t)
	out, err := extractLayerxFlags([]string{"--engine=docker", "-t", "x", "."})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if engineFlag != "docker" {
		t.Fatalf("engineFlag = %q, want docker", engineFlag)
	}
	want := []string{"-t", "x", "."}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("forwarded args = %v, want %v", out, want)
	}
}

func TestExtractLayerxFlags_NoCacheForwardsAndApplies(t *testing.T) {
	resetEngineState(t)
	out, err := extractLayerxFlags([]string{"--no-cache", "-t", "x", "."})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !flagNoCacheFl {
		t.Fatal("flagNoCacheFl was not set")
	}
	want := []string{"--no-cache", "-t", "x", "."}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("forwarded args = %v, want %v", out, want)
	}
}

func TestExtractLayerxFlags_DoubleDashStopsProcessing(t *testing.T) {
	resetEngineState(t)
	out, err := extractLayerxFlags([]string{"-t", "x", "--", "--engine", "podman", "."})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if engineFlag != "auto" {
		t.Fatalf("engineFlag changed past --: got %q", engineFlag)
	}
	want := []string{"-t", "x", "--", "--engine", "podman", "."}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("forwarded args = %v, want %v", out, want)
	}
}

func TestExtractLayerxFlags_RejectsInvalidEngine(t *testing.T) {
	resetEngineState(t)
	_, err := extractLayerxFlags([]string{"--engine", "containerd"})
	if err == nil {
		t.Fatal("expected error for invalid engine, got nil")
	}
	if !strings.Contains(err.Error(), "containerd") {
		t.Fatalf("error should name the bad value, got: %v", err)
	}
}

func TestExtractLayerxFlags_DanglingEngineErrors(t *testing.T) {
	resetEngineState(t)
	_, err := extractLayerxFlags([]string{"--engine"})
	if err == nil {
		t.Fatal("expected error for dangling --engine, got nil")
	}
}

func TestEnsureIIDFile_RespectsUserPath(t *testing.T) {
	args := []string{"-t", "x", "--iidfile", "/tmp/user.iid", "."}
	path, owns, err := ensureIIDFile(&args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/tmp/user.iid" {
		t.Fatalf("path = %q, want /tmp/user.iid", path)
	}
	if owns {
		t.Fatal("user-supplied iidfile must not be owned/cleaned by layerx")
	}
	want := []string{"-t", "x", "--iidfile", "/tmp/user.iid", "."}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args mutated: %v, want %v", args, want)
	}
}

func TestEnsureIIDFile_RespectsUserEqualsForm(t *testing.T) {
	args := []string{"-t", "x", "--iidfile=/tmp/user.iid", "."}
	path, owns, err := ensureIIDFile(&args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/tmp/user.iid" {
		t.Fatalf("path = %q, want /tmp/user.iid", path)
	}
	if owns {
		t.Fatal("user-supplied iidfile must not be owned by layerx")
	}
}

func TestEnsureIIDFile_AppendsTempWhenAbsent(t *testing.T) {
	args := []string{"-t", "x", "."}
	path, owns, err := ensureIIDFile(&args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !owns {
		t.Fatal("temp iidfile must be owned by layerx")
	}
	defer os.Remove(path)
	if path == "" {
		t.Fatal("path is empty")
	}
	if len(args) != 5 || args[3] != "--iidfile" || args[4] != path {
		t.Fatalf("args not appended correctly: %v", args)
	}
}

func TestReadIIDFile_TrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "iid")
	if err := os.WriteFile(p, []byte("sha256:abc123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := readIIDFile(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "sha256:abc123" {
		t.Fatalf("id = %q, want sha256:abc123", id)
	}
}

func TestReadIIDFile_EmptyIsError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "iid")
	if err := os.WriteFile(p, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readIIDFile(p); err == nil {
		t.Fatal("expected error for empty iidfile, got nil")
	}
}

func TestReadIIDFile_MissingIsError(t *testing.T) {
	if _, err := readIIDFile(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected error for missing iidfile, got nil")
	}
}

func swapLookPath(t *testing.T, found map[string]string) {
	t.Helper()
	prev := lookPath
	lookPath = func(name string) (string, error) {
		if p, ok := found[name]; ok {
			return p, nil
		}
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	t.Cleanup(func() { lookPath = prev })
}

func TestPickEngineBinary_DockerExplicit(t *testing.T) {
	swapLookPath(t, map[string]string{"docker": "/usr/bin/docker"})
	got, err := pickEngineBinary("docker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/usr/bin/docker" {
		t.Fatalf("got %q, want /usr/bin/docker", got)
	}
}

func TestPickEngineBinary_PodmanExplicitMissing(t *testing.T) {
	swapLookPath(t, map[string]string{})
	if _, err := pickEngineBinary("podman"); err == nil {
		t.Fatal("expected error when podman is not on PATH")
	}
}

func TestPickEngineBinary_AutoFallsBackToWhateverIsOnPath(t *testing.T) {
	defer swapProber(newFakeProber())()
	swapLookPath(t, map[string]string{"podman": "/usr/bin/podman"})
	got, err := pickEngineBinary("auto")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/usr/bin/podman" {
		t.Fatalf("got %q, want /usr/bin/podman", got)
	}
}

func TestPickEngineBinary_AutoPrefersDockerWhenSocketAndCLIPresent(t *testing.T) {
	defer swapProber(newFakeProber(dockerSocketCandidates()...))()
	swapLookPath(t, map[string]string{
		"docker": "/usr/bin/docker",
		"podman": "/usr/bin/podman",
	})
	got, err := pickEngineBinary("auto")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/usr/bin/docker" {
		t.Fatalf("got %q, want /usr/bin/docker", got)
	}
}

func TestPickEngineBinary_AutoPrefersPodmanWhenOnlyPodmanSocket(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("podman socket probing only runs on linux")
	}
	defer swapProber(newFakeProber(podmanSocketCandidates()...))()
	swapLookPath(t, map[string]string{
		"docker": "/usr/bin/docker",
		"podman": "/usr/bin/podman",
	})
	got, err := pickEngineBinary("auto")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/usr/bin/podman" {
		t.Fatalf("got %q, want /usr/bin/podman", got)
	}
}

func TestPickEngineBinary_AutoNeitherInstalled(t *testing.T) {
	defer swapProber(newFakeProber())()
	swapLookPath(t, map[string]string{})
	if _, err := pickEngineBinary("auto"); err == nil {
		t.Fatal("expected error when neither engine is installed")
	}
}

func TestErrBuildFailed_IsTyped(t *testing.T) {
	var err error = &ErrBuildFailed{ExitCode: 7}
	var target *ErrBuildFailed
	if !errors.As(err, &target) {
		t.Fatal("ErrBuildFailed should match its own type via errors.As")
	}
	if target.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", target.ExitCode)
	}
}

func TestFirstTagFromArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"short flag space form", []string{"-t", "myimage", "."}, "myimage"},
		{"short flag equals form", []string{"-t=myimage:1.0", "."}, "myimage:1.0"},
		{"long flag space form", []string{"--tag", "myimage", "."}, "myimage"},
		{"long flag equals form", []string{"--tag=myimage:dev", "."}, "myimage:dev"},
		{"first of multiple tags wins", []string{"-t", "first:latest", "-t", "second:1.0", "."}, "first:latest"},
		{"no tag", []string{"--build-arg", "X=1", "."}, ""},
		{"trailing -t with no value", []string{"-t"}, ""},
		{"-- stops parsing", []string{"--", "-t", "tricky", "."}, ""},
		{"tag-like positional after build-arg is not picked up", []string{"--build-arg", "VERSION=1", "."}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := firstTagFromArgs(tc.args)
			if got != tc.want {
				t.Fatalf("firstTagFromArgs(%v) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}
