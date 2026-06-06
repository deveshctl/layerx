package image

import "testing"

func TestIsDaemonUnreachable_EngineNeutral(t *testing.T) {
	cases := []struct {
		name string
		err  string
		want bool
	}{
		{"docker daemon down phrase", "Cannot connect to the Docker daemon at unix:///var/run/docker.sock", true},
		{"is docker running phrase", "is the docker daemon running?", true},
		{"daemon not running phrase", "the docker daemon is not running", true},
		{"socket missing", "dial unix /var/run/docker.sock: connect: no such file or directory", true},
		{"connection refused", "dial unix /run/user/1000/podman/podman.sock: connect: connection refused", true},
		{"permission denied", "dial unix /var/run/docker.sock: connect: permission denied", true},
		{"named pipe missing", "open //./pipe/docker_engine: file does not exist", true},
		{"unrelated error", "manifest unknown", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isDaemonUnreachable(syntheticClassifyErr{tc.err})
			if got != tc.want {
				t.Fatalf("isDaemonUnreachable(%q) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsImageNotFoundMessage_PodmanCoverage(t *testing.T) {
	cases := []struct {
		err  string
		want bool
	}{
		{"manifest unknown", true},
		{"manifest for foo/bar:latest not found", true},
		{"repository does not exist or may require authorization", true},
		{"pull access denied for foo/bar", true},
		{"name resolution failure", false},
	}
	for _, tc := range cases {
		t.Run(tc.err, func(t *testing.T) {
			got := isImageNotFoundMessage(tc.err)
			if got != tc.want {
				t.Fatalf("isImageNotFoundMessage(%q) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

type syntheticClassifyErr struct{ s string }

func (e syntheticClassifyErr) Error() string { return e.s }
