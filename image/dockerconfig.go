package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	distreference "github.com/distribution/reference"
	"github.com/moby/moby/api/pkg/authconfig"
	registrytypes "github.com/moby/moby/api/types/registry"
)

// registryHostFrom extracts the registry hostname (including port if present)
// from an image reference. Returns "docker.io" for bare image names like
// "alpine" or "library/ubuntu:latest" — matching Docker's normalisation.
func registryHostFrom(imageRef string) string {
	named, err := distreference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return ""
	}
	return distreference.Domain(named)
}

// dockerHubRegistryKey is the canonical key written by "docker login" for
// Docker Hub. docker.io references normalise to this before any credential
// lookup so that inline auths, credsStore, and per-registry credHelpers all
// find the entry regardless of how the key was originally stored.
const dockerHubRegistryKey = "https://index.docker.io/v1/"

// normalizeRegistryKey maps the logical registry hostname (as returned by
// registryHostFrom) to the key used in credential stores and config files.
// Docker Hub is the only registry with a historical mismatch: docker login
// writes "https://index.docker.io/v1/" but image references normalise to
// "docker.io".
func normalizeRegistryKey(registry string) string {
	if registry == "docker.io" || registry == "index.docker.io" {
		return dockerHubRegistryKey
	}
	return registry
}

// dockerConfigFile is the on-disk layout of ~/.docker/config.json that is
// relevant for credential lookup. Only the fields we need are decoded.
type dockerConfigFile struct {
	Auths       map[string]dockerConfigAuth `json:"auths"`
	CredsStore  string                      `json:"credsStore"`
	CredHelpers map[string]string           `json:"credHelpers"`
}

type dockerConfigAuth struct {
	Auth          string `json:"auth"`
	IdentityToken string `json:"identityToken"`
}

// resolveRegistryAuth returns a base64-encoded RegistryAuth value for the
// given registry hostname by reading credential stores and config files in
// the following priority order (matching Docker CLI behaviour):
//
//  1. Per-registry credHelper entry  (credHelpers["registry"])
//  2. Global credential store        (credsStore)
//  3. Inline auth/identityToken      (auths["registry"])
//
// Both Docker and Podman credential files are consulted:
//   - Docker: $DOCKER_CONFIG/config.json or ~/.docker/config.json
//   - Podman: $REGISTRY_AUTH_FILE or ~/.config/containers/auth.json
//     (Podman's native auth.json uses the same auths structure as Docker)
//
// The Docker config is always tried first. If it yields a non-empty result
// that result is returned immediately. The Podman auth file is tried only
// when the Docker config has no entry for the given registry.
//
// Returns ("", nil) when no credentials are found — callers treat that as
// an anonymous pull, not an error.
func resolveRegistryAuth(ctx context.Context, registry string) (string, error) {
	// Docker Hub: "docker login" writes the canonical key
	// "https://index.docker.io/v1/" while image-reference parsing produces
	// "docker.io". normalizeRegistryKey maps the latter to the former so
	// inline auths, credsStore, and credHelpers all find the entry.
	credKey := normalizeRegistryKey(registry)

	if auth, err := resolveFromDockerConfig(ctx, credKey); err != nil || auth != "" {
		return auth, err
	}
	return resolveFromPodmanAuth(credKey)
}

// resolveFromDockerConfig looks up credentials in the Docker config.json.
func resolveFromDockerConfig(ctx context.Context, credKey string) (string, error) {
	cfgPath, err := dockerConfigPath()
	if err != nil {
		return "", nil
	}
	return resolveFromConfigFile(ctx, cfgPath, credKey)
}

// resolveFromPodmanAuth looks up credentials in the Podman auth.json.
// Podman's native auth file uses the same JSON structure as Docker's auths
// block but without credsStore / credHelpers at the top level — only the
// auths map is consulted. Podman stores Docker Hub credentials under
// "docker.io" (not the canonical Docker Hub URL), so both keys are tried.
func resolveFromPodmanAuth(credKey string) (string, error) {
	path := podmanAuthPath()
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil
	}
	var f struct {
		Auths map[string]dockerConfigAuth `json:"auths"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return "", nil
	}
	// Podman uses "docker.io" directly for Docker Hub — try both the
	// normalized key (https://index.docker.io/v1/) and the original
	// "docker.io" so users who ran "podman login docker.io" are covered.
	keys := []string{credKey}
	if credKey == dockerHubRegistryKey {
		keys = append(keys, "docker.io")
	}
	for _, k := range keys {
		if auth, err := encodeInlineAuth(f.Auths[k], k); err == nil && auth != "" {
			return auth, nil
		}
	}
	return "", nil
}

// resolveFromConfigFile reads a Docker-format config file and looks up
// credentials for credKey: per-registry credHelper → global credsStore →
// inline auths.
func resolveFromConfigFile(ctx context.Context, cfgPath, credKey string) (string, error) {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return "", nil
	}
	var cfg dockerConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", nil
	}

	// 1. Per-registry credHelper
	if helper, ok := cfg.CredHelpers[credKey]; ok {
		return credHelperGet(ctx, helper, credKey)
	}

	// 2. Global credential store
	if cfg.CredsStore != "" {
		auth, err := credHelperGet(ctx, cfg.CredsStore, credKey)
		if err == nil && auth != "" {
			return auth, nil
		}
		// Fall through to inline auths when the store has no entry for this
		// registry or the helper binary is not installed.
	}

	// 3. Inline auth / identity token
	return encodeInlineAuth(cfg.Auths[credKey], credKey)
}

// encodeInlineAuth converts a dockerConfigAuth entry to a base64-encoded
// RegistryAuth value. Handles both "auth" (base64("user:pass")) and
// "identityToken" — identityToken takes precedence when both are set.
func encodeInlineAuth(entry dockerConfigAuth, serverAddress string) (string, error) {
	if entry.IdentityToken != "" {
		return authconfig.Encode(registrytypes.AuthConfig{
			IdentityToken: entry.IdentityToken,
			ServerAddress: serverAddress,
		})
	}
	if entry.Auth == "" {
		return "", nil
	}
	// auth is base64("username:password"). Decode and re-encode as a full
	// AuthConfig JSON so the daemon gets Username+Password rather than a
	// bare token, which some registry implementations reject.
	decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
	if err != nil {
		return "", nil
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", nil
	}
	return authconfig.Encode(registrytypes.AuthConfig{
		Username:      parts[0],
		Password:      parts[1],
		ServerAddress: serverAddress,
	})
}

// credHelperGet invokes docker-credential-<helper> get for the given server
// address and returns a base64-encoded RegistryAuth value on success.
func credHelperGet(ctx context.Context, helper, serverURL string) (string, error) {
	helperBin := "docker-credential-" + helper
	cmd := exec.CommandContext(ctx, helperBin, "get")
	cmd.Stdin = strings.NewReader(serverURL + "\n")
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		// Helper not installed or has no entry — treat as "no credentials".
		return "", nil
	}

	type helperResponse struct {
		Username string `json:"Username"`
		Secret   string `json:"Secret"`
	}
	var resp helperResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		return "", fmt.Errorf("credential helper %s returned unexpected output: %w", helperBin, err)
	}
	if resp.Username == "" && resp.Secret == "" {
		return "", nil
	}

	ac := registrytypes.AuthConfig{ServerAddress: serverURL}
	// Credential helpers use Username="<token>" to signal a bearer/identity token.
	// In that case Secret is the token value, not a password.
	if resp.Username == "<token>" {
		ac.IdentityToken = resp.Secret
	} else {
		ac.Username = resp.Username
		ac.Password = resp.Secret
	}
	return authconfig.Encode(ac)
}

// dockerConfigPath returns the path to the active Docker config.json.
// Respects DOCKER_CONFIG env var; falls back to ~/.docker/config.json.
func dockerConfigPath() (string, error) {
	if dir := os.Getenv("DOCKER_CONFIG"); dir != "" {
		return filepath.Join(dir, "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".docker", "config.json"), nil
}

// podmanAuthPath returns the path to the active Podman auth.json, or ""
// when no path can be determined. Respects REGISTRY_AUTH_FILE (Podman's
// documented env override); falls back to ~/.config/containers/auth.json.
func podmanAuthPath() string {
	if p := os.Getenv("REGISTRY_AUTH_FILE"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "containers", "auth.json")
}
