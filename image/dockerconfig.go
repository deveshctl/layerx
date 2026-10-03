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

// dockerConfigFile is the on-disk layout of ~/.docker/config.json that is
// relevant for credential lookup. Only the fields we need are decoded.
type dockerConfigFile struct {
	Auths       map[string]dockerConfigAuth `json:"auths"`
	CredsStore  string                      `json:"credsStore"`
	CredHelpers map[string]string           `json:"credHelpers"`
}

type dockerConfigAuth struct {
	Auth string `json:"auth"`
}

// resolveRegistryAuth returns a base64-encoded RegistryAuth value for the
// given registry hostname by reading ~/.docker/config.json and, when
// necessary, invoking the configured credential helper.
//
// Lookup order (matches Docker CLI behaviour):
//  1. Per-registry credHelper entry  (credHelpers["registry"])
//  2. Global credential store        (credsStore)
//  3. Inline auth token              (auths["registry"].auth)
//
// Returns ("", nil) when no credentials are found — callers should treat
// that as an anonymous pull, not an error.
func resolveRegistryAuth(ctx context.Context, registry string) (string, error) {
	cfgPath, err := dockerConfigPath()
	if err != nil {
		return "", nil
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return "", nil
	}
	var cfg dockerConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", nil
	}

	// 1. Per-registry credHelper
	if helper, ok := cfg.CredHelpers[registry]; ok {
		return credHelperGet(ctx, helper, registry)
	}

	// 2. Global credential store
	if cfg.CredsStore != "" {
		auth, err := credHelperGet(ctx, cfg.CredsStore, registry)
		if err == nil && auth != "" {
			return auth, nil
		}
		// If the global store fails (helper not installed, no entry for this
		// registry), fall through to inline auths rather than erroring.
	}

	// 3. Inline auth token
	if entry, ok := cfg.Auths[registry]; ok && entry.Auth != "" {
		// The inline auth field is base64("username:password"). Decode it and
		// re-encode as a full AuthConfig JSON so the daemon gets Username+Password
		// rather than a bare auth token, which some registry implementations reject.
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
			ServerAddress: registry,
		})
	}

	return "", nil
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
