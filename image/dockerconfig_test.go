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

func TestResolveRegistryAuth_NoConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir) // points at an empty dir, no config.json
	auth, err := resolveRegistryAuth(context.Background(), "ghcr.io")
	assert.NoError(t, err)
	assert.Equal(t, "", auth)
}

func TestResolveRegistryAuth_InlineAuth(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)

	// Inline auth entry: base64("user:pass")
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

	// Verify the encoded value round-trips to a valid AuthConfig with Username+Password
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

	cfg := map[string]any{
		"auths": map[string]any{
			"other.registry.io": map[string]any{"auth": "dXNlcjpwYXNz"},
		},
	}
	writeDockerConfig(t, dir, cfg)

	// Different registry — should return empty, not an error
	auth, err := resolveRegistryAuth(context.Background(), "ghcr.io")
	assert.NoError(t, err)
	assert.Equal(t, "", auth)
}

func TestResolveRegistryAuth_MissingCredsStore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)

	// credsStore points at a helper that doesn't exist — should fall through
	// to inline auths (none here) and return empty without error.
	inlineToken := base64.StdEncoding.EncodeToString([]byte("user:pass"))
	cfg := map[string]any{
		"credsStore": "nonexistent-helper-xyz",
		"auths": map[string]any{
			"localhost:5001": map[string]any{"auth": inlineToken},
		},
	}
	writeDockerConfig(t, dir, cfg)

	// credsStore helper is absent; inline auth for localhost:5001 should be returned
	auth, err := resolveRegistryAuth(context.Background(), "localhost:5001")
	assert.NoError(t, err)
	assert.True(t, auth != "", "expected inline auth as fallback when credsStore helper is absent")
}

func TestResolveRegistryAuth_CredHelperPerRegistry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)

	// credHelpers entry pointing at a non-existent helper — should return empty
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

// writeDockerConfig writes a config.json into dir from a map value.
func writeDockerConfig(t *testing.T, dir string, cfg map[string]any) {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}
}
