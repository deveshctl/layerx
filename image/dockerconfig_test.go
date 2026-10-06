package image

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/moby/moby/api/pkg/authconfig"
	registrytypes "github.com/moby/moby/api/types/registry"
)

// ---------------------------------------------------------------------------
// registryHostFrom
// ---------------------------------------------------------------------------

func TestRegistryHostFrom(t *testing.T) {
	tests := []struct {
		ref  string
		want string
	}{
		{"alpine", "docker.io"},
		{"alpine:3.20", "docker.io"},
		{"library/ubuntu:latest", "docker.io"},
		{"ghcr.io/some/private-image:latest", "ghcr.io"},
		{"localhost:5001/private/testimage:latest", "localhost:5001"},
		{"registry.example.com/org/image:tag", "registry.example.com"},
		{"", ""},
	}
	for _, tt := range tests {
		got := registryHostFrom(tt.ref)
		assert.Equal(t, tt.want, got, "registryHostFrom(%q)", tt.ref)
	}
}

// ---------------------------------------------------------------------------
// normalizeRegistryKey
// ---------------------------------------------------------------------------

func TestNormalizeRegistryKey_DockerHub(t *testing.T) {
	assert.Equal(t, dockerHubRegistryKey, normalizeRegistryKey("docker.io"))
	assert.Equal(t, dockerHubRegistryKey, normalizeRegistryKey("index.docker.io"))
}

func TestNormalizeRegistryKey_OtherRegistries(t *testing.T) {
	for _, reg := range []string{
		"ghcr.io",
		"quay.io",
		"registry.example.com",
		"localhost:5001",
		"public.ecr.aws",
	} {
		assert.Equal(t, reg, normalizeRegistryKey(reg), "normalizeRegistryKey(%q)", reg)
	}
}

// ---------------------------------------------------------------------------
// resolveRegistryAuth — Docker config.json
// ---------------------------------------------------------------------------

func TestResolveRegistryAuth_NoConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	t.Setenv("REGISTRY_AUTH_FILE", filepath.Join(dir, "no-such-auth.json"))
	auth, err := resolveRegistryAuth(context.Background(), "ghcr.io")
	assert.NoError(t, err)
	assert.Equal(t, "", auth)
}

func TestResolveRegistryAuth_InlineAuth(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	t.Setenv("REGISTRY_AUTH_FILE", filepath.Join(dir, "no-such-auth.json"))

	inlineToken := base64.StdEncoding.EncodeToString([]byte("user:pass"))
	cfg := map[string]any{
		"auths": map[string]any{
			"localhost:5001": map[string]any{"auth": inlineToken},
		},
	}
	writeDockerConfig(t, dir, cfg)

	auth, err := resolveRegistryAuth(context.Background(), "localhost:5001")
	assert.NoError(t, err)
	assert.True(t, auth != "", "expected non-empty RegistryAuth for inline credentials")

	decoded, err := base64.URLEncoding.DecodeString(auth)
	assert.NoError(t, err)
	var ac registrytypes.AuthConfig
	assert.NoError(t, json.Unmarshal(decoded, &ac))
	assert.Equal(t, "user", ac.Username)
	assert.Equal(t, "pass", ac.Password)
	assert.Equal(t, "localhost:5001", ac.ServerAddress)
}

func TestResolveRegistryAuth_NoMatchingEntry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	t.Setenv("REGISTRY_AUTH_FILE", filepath.Join(dir, "no-such-auth.json"))

	cfg := map[string]any{
		"auths": map[string]any{
			"other.registry.io": map[string]any{"auth": "dXNlcjpwYXNz"},
		},
	}
	writeDockerConfig(t, dir, cfg)

	auth, err := resolveRegistryAuth(context.Background(), "ghcr.io")
	assert.NoError(t, err)
	assert.Equal(t, "", auth)
}

func TestResolveRegistryAuth_MissingCredsStore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	t.Setenv("REGISTRY_AUTH_FILE", filepath.Join(dir, "no-such-auth.json"))

	inlineToken := base64.StdEncoding.EncodeToString([]byte("user:pass"))
	cfg := map[string]any{
		"credsStore": "nonexistent-helper-xyz",
		"auths": map[string]any{
			"localhost:5001": map[string]any{"auth": inlineToken},
		},
	}
	writeDockerConfig(t, dir, cfg)

	auth, err := resolveRegistryAuth(context.Background(), "localhost:5001")
	assert.NoError(t, err)
	assert.True(t, auth != "", "expected inline auth as fallback when credsStore helper is absent")
}

func TestResolveRegistryAuth_CredHelperPerRegistry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	t.Setenv("REGISTRY_AUTH_FILE", filepath.Join(dir, "no-such-auth.json"))

	cfg := map[string]any{
		"credHelpers": map[string]any{
			"ghcr.io": "nonexistent-helper-xyz",
		},
	}
	writeDockerConfig(t, dir, cfg)

	auth, err := resolveRegistryAuth(context.Background(), "ghcr.io")
	assert.NoError(t, err)
	assert.Equal(t, "", auth)
}

// ---------------------------------------------------------------------------
// Docker Hub credential key normalisation
// ---------------------------------------------------------------------------

// TestResolveRegistryAuth_DockerHub_CanonicalKey verifies that credentials
// stored under "https://index.docker.io/v1/" (the key written by docker login)
// are found when resolving a Docker Hub image reference ("docker.io").
func TestResolveRegistryAuth_DockerHub_CanonicalKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	t.Setenv("REGISTRY_AUTH_FILE", filepath.Join(dir, "no-such-auth.json"))

	inlineToken := base64.StdEncoding.EncodeToString([]byte("hubuser:hubpass"))
	cfg := map[string]any{
		"auths": map[string]any{
			// Stored under the key docker login writes, NOT "docker.io"
			"https://index.docker.io/v1/": map[string]any{"auth": inlineToken},
		},
	}
	writeDockerConfig(t, dir, cfg)

	auth, err := resolveRegistryAuth(context.Background(), "docker.io")
	assert.NoError(t, err)
	assert.True(t, auth != "", "expected credentials for Docker Hub canonical key")

	decoded, err := base64.URLEncoding.DecodeString(auth)
	assert.NoError(t, err)
	var ac registrytypes.AuthConfig
	assert.NoError(t, json.Unmarshal(decoded, &ac))
	assert.Equal(t, "hubuser", ac.Username)
	assert.Equal(t, "hubpass", ac.Password)
}

// TestResolveRegistryAuth_DockerHub_ShortRef verifies the full chain from a
// bare image reference like "deveshxd/alphastore:latest" through
// registryHostFrom → "docker.io" → normalizeRegistryKey →
// "https://index.docker.io/v1/" → credential lookup.
func TestResolveRegistryAuth_DockerHub_ShortRef(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	t.Setenv("REGISTRY_AUTH_FILE", filepath.Join(dir, "no-such-auth.json"))

	inlineToken := base64.StdEncoding.EncodeToString([]byte("user:secret"))
	cfg := map[string]any{
		"auths": map[string]any{
			"https://index.docker.io/v1/": map[string]any{"auth": inlineToken},
		},
	}
	writeDockerConfig(t, dir, cfg)

	registry := registryHostFrom("deveshxd/alphastore:latest") // → "docker.io"
	auth, err := resolveRegistryAuth(context.Background(), registry)
	assert.NoError(t, err)
	assert.True(t, auth != "", "expected credentials for short Docker Hub ref")
}

// ---------------------------------------------------------------------------
// identityToken inline field
// ---------------------------------------------------------------------------

func TestResolveRegistryAuth_IdentityToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	t.Setenv("REGISTRY_AUTH_FILE", filepath.Join(dir, "no-such-auth.json"))

	cfg := map[string]any{
		"auths": map[string]any{
			"ghcr.io": map[string]any{"identityToken": "ghs_mytoken123"},
		},
	}
	writeDockerConfig(t, dir, cfg)

	auth, err := resolveRegistryAuth(context.Background(), "ghcr.io")
	assert.NoError(t, err)
	assert.True(t, auth != "", "expected non-empty RegistryAuth for identityToken entry")

	decoded, err := base64.URLEncoding.DecodeString(auth)
	assert.NoError(t, err)
	var ac registrytypes.AuthConfig
	assert.NoError(t, json.Unmarshal(decoded, &ac))
	assert.Equal(t, "ghs_mytoken123", ac.IdentityToken)
	assert.Equal(t, "", ac.Username)
	assert.Equal(t, "", ac.Password)
}

// ---------------------------------------------------------------------------
// Podman auth.json via REGISTRY_AUTH_FILE
// ---------------------------------------------------------------------------

func TestResolveRegistryAuth_PodmanAuthFile(t *testing.T) {
	dockerDir := t.TempDir()
	podmanDir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dockerDir)
	podmanAuth := filepath.Join(podmanDir, "auth.json")
	t.Setenv("REGISTRY_AUTH_FILE", podmanAuth)

	// Docker config has no entry for quay.io
	writeDockerConfig(t, dockerDir, map[string]any{})

	// Podman auth.json has credentials for quay.io
	inlineToken := base64.StdEncoding.EncodeToString([]byte("podmanuser:podmanpass"))
	writePodmanAuth(t, podmanAuth, map[string]any{
		"auths": map[string]any{
			"quay.io": map[string]any{"auth": inlineToken},
		},
	})

	auth, err := resolveRegistryAuth(context.Background(), "quay.io")
	assert.NoError(t, err)
	assert.True(t, auth != "", "expected credentials from Podman auth.json")

	decoded, err := base64.URLEncoding.DecodeString(auth)
	assert.NoError(t, err)
	var ac registrytypes.AuthConfig
	assert.NoError(t, json.Unmarshal(decoded, &ac))
	assert.Equal(t, "podmanuser", ac.Username)
	assert.Equal(t, "podmanpass", ac.Password)
}

// TestResolveRegistryAuth_PodmanAuthFile_DockerHub verifies that Podman's
// "docker.io" key is found when looking up Docker Hub credentials from
// REGISTRY_AUTH_FILE. Podman stores Hub creds under "docker.io", not the
// canonical "https://index.docker.io/v1/", so both keys are tried.
func TestResolveRegistryAuth_PodmanAuthFile_DockerHub(t *testing.T) {
	dockerDir := t.TempDir()
	podmanDir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dockerDir)
	podmanAuth := filepath.Join(podmanDir, "auth.json")
	t.Setenv("REGISTRY_AUTH_FILE", podmanAuth)

	writeDockerConfig(t, dockerDir, map[string]any{})

	inlineToken := base64.StdEncoding.EncodeToString([]byte("puser:ppass"))
	writePodmanAuth(t, podmanAuth, map[string]any{
		"auths": map[string]any{
			"docker.io": map[string]any{"auth": inlineToken},
		},
	})

	auth, err := resolveRegistryAuth(context.Background(), "docker.io")
	assert.NoError(t, err)
	assert.True(t, auth != "", "expected Podman Docker Hub credentials")

	decoded, err := base64.URLEncoding.DecodeString(auth)
	assert.NoError(t, err)
	var ac registrytypes.AuthConfig
	assert.NoError(t, json.Unmarshal(decoded, &ac))
	assert.Equal(t, "puser", ac.Username)
	assert.Equal(t, "ppass", ac.Password)
}

// TestResolveRegistryAuth_DockerTakesPrecedenceOverPodman verifies that
// Docker config credentials win over Podman auth.json for the same registry.
func TestResolveRegistryAuth_DockerTakesPrecedenceOverPodman(t *testing.T) {
	dockerDir := t.TempDir()
	podmanDir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dockerDir)
	podmanAuth := filepath.Join(podmanDir, "auth.json")
	t.Setenv("REGISTRY_AUTH_FILE", podmanAuth)

	dockerToken := base64.StdEncoding.EncodeToString([]byte("dockeruser:dockerpass"))
	writeDockerConfig(t, dockerDir, map[string]any{
		"auths": map[string]any{
			"ghcr.io": map[string]any{"auth": dockerToken},
		},
	})

	podmanToken := base64.StdEncoding.EncodeToString([]byte("podmanuser:podmanpass"))
	writePodmanAuth(t, podmanAuth, map[string]any{
		"auths": map[string]any{
			"ghcr.io": map[string]any{"auth": podmanToken},
		},
	})

	auth, err := resolveRegistryAuth(context.Background(), "ghcr.io")
	assert.NoError(t, err)
	assert.True(t, auth != "")

	decoded, err := base64.URLEncoding.DecodeString(auth)
	assert.NoError(t, err)
	var ac registrytypes.AuthConfig
	assert.NoError(t, json.Unmarshal(decoded, &ac))
	assert.Equal(t, "dockeruser", ac.Username, "Docker config must take precedence over Podman auth.json")
}

// ---------------------------------------------------------------------------
// encodeInlineAuth
// ---------------------------------------------------------------------------

func TestEncodeInlineAuth_BasicCreds(t *testing.T) {
	token := base64.StdEncoding.EncodeToString([]byte("user:pass"))
	auth, err := encodeInlineAuth(dockerConfigAuth{Auth: token}, "registry.example.com")
	assert.NoError(t, err)
	assert.True(t, auth != "")

	decoded, err := base64.URLEncoding.DecodeString(auth)
	assert.NoError(t, err)
	var ac registrytypes.AuthConfig
	assert.NoError(t, json.Unmarshal(decoded, &ac))
	assert.Equal(t, "user", ac.Username)
	assert.Equal(t, "pass", ac.Password)
}

func TestEncodeInlineAuth_IdentityToken(t *testing.T) {
	auth, err := encodeInlineAuth(dockerConfigAuth{IdentityToken: "tok123"}, "ghcr.io")
	assert.NoError(t, err)
	assert.True(t, auth != "")

	decoded, err := base64.URLEncoding.DecodeString(auth)
	assert.NoError(t, err)
	var ac registrytypes.AuthConfig
	assert.NoError(t, json.Unmarshal(decoded, &ac))
	assert.Equal(t, "tok123", ac.IdentityToken)
	assert.Equal(t, "", ac.Username)
}

func TestEncodeInlineAuth_IdentityTokenTakesPrecedenceOverAuth(t *testing.T) {
	// When both are set, identityToken wins.
	token := base64.StdEncoding.EncodeToString([]byte("user:pass"))
	auth, err := encodeInlineAuth(dockerConfigAuth{Auth: token, IdentityToken: "tok"}, "ghcr.io")
	assert.NoError(t, err)
	assert.True(t, auth != "")

	decoded, err := base64.URLEncoding.DecodeString(auth)
	assert.NoError(t, err)
	var ac registrytypes.AuthConfig
	assert.NoError(t, json.Unmarshal(decoded, &ac))
	assert.Equal(t, "tok", ac.IdentityToken)
	assert.Equal(t, "", ac.Username)
}

func TestEncodeInlineAuth_Empty(t *testing.T) {
	auth, err := encodeInlineAuth(dockerConfigAuth{}, "ghcr.io")
	assert.NoError(t, err)
	assert.Equal(t, "", auth)
}

// ---------------------------------------------------------------------------
// authconfig.Encode round-trip
// ---------------------------------------------------------------------------

func TestEncodeRegistryAuth(t *testing.T) {
	ac := registrytypes.AuthConfig{
		Username:      "user",
		Password:      "pass",
		ServerAddress: "localhost:5001",
	}
	encoded, err := authconfig.Encode(ac)
	assert.NoError(t, err)
	assert.True(t, encoded != "")

	raw, err := base64.URLEncoding.DecodeString(encoded)
	assert.NoError(t, err)
	var got registrytypes.AuthConfig
	assert.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, "user", got.Username)
	assert.Equal(t, "pass", got.Password)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func writeDockerConfig(t *testing.T, dir string, cfg map[string]any) {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal docker config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}
}

func writePodmanAuth(t *testing.T, path string, cfg map[string]any) {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal podman auth: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write podman auth.json: %v", err)
	}
}
