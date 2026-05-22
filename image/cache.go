package image

import (
	"crypto/rand"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CacheDir returns the directory where layerx persists analysis caches.
// Resolution order:
//  1. LAYERX_CACHE_DIR (if set and usable as a directory)
//  2. os.UserCacheDir() joined with "layerx"
//
// CacheDir does not create the directory; saveCache creates digest
// subdirectories on demand with mode 0700.
func CacheDir() (string, error) {
	if override := os.Getenv("LAYERX_CACHE_DIR"); override != "" {
		if usable, _ := dirIsUsable(override); usable {
			return override, nil
		}
		// Fall through to default; bad override is non-fatal.
	}
	uc, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolving user cache dir: %w", err)
	}
	return filepath.Join(uc, "layerx"), nil
}

// dirIsUsable returns true if path is an existing directory or can be
// created as one. The cache will MkdirAll on first write; here we only
// reject paths that are clearly wrong (e.g. exist as a non-directory).
func dirIsUsable(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		return info.IsDir(), nil
	}
	if os.IsNotExist(err) {
		// Will be created on first write. Treat as usable.
		return true, nil
	}
	return false, err
}

// cachePath returns the absolute path to the gob file for a given digest
// under root. The digest is normalized (sha256: prefix stripped) so the
// directory name is filesystem-safe on every OS.
func cachePath(root, digest string) string {
	return filepath.Join(root, normalizeDigest(digest), "layers.gob")
}

// normalizeDigest strips the "sha256:" prefix when present.
func normalizeDigest(digest string) string {
	if rest, ok := strings.CutPrefix(digest, "sha256:"); ok {
		return rest
	}
	return digest
}

// loadCache returns the persisted layers for the given digest, or ok=false
// on any miss (no file, schema mismatch, digest mismatch, decode error).
// On a soft miss the offending file is removed; the next call cold-resolves
// and writes a fresh cache. err is non-nil only for unexpected I/O errors
// the caller should report; misses are not errors.
func loadCache(root, digest string) (layers []Layer, ok bool, err error) {
	path := cachePath(root, digest)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("opening cache %s: %w", path, err)
	}
	defer f.Close()

	var env cacheEnvelope
	if decErr := gob.NewDecoder(f).Decode(&env); decErr != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, false, nil
	}
	if env.SchemaVersion != SchemaVersion {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, false, nil
	}
	if env.Digest != normalizeDigest(digest) {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, false, nil
	}
	return fromCachedLayers(env.Layers), true, nil
}

// saveCache writes layers to {root}/{digest}/layers.gob using a temp file +
// atomic rename. Errors are returned but should be treated as non-fatal by
// callers (the user already has the live result).
func saveCache(root, digest string, layers []Layer) error {
	dir := filepath.Join(root, normalizeDigest(digest))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating cache dir %s: %w", dir, err)
	}

	tmpName, err := tempFilename(dir, "layers.gob.tmp-")
	if err != nil {
		return fmt.Errorf("generating temp name: %w", err)
	}
	tmpPath := filepath.Join(dir, tmpName)

	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("creating temp cache file: %w", err)
	}

	env := cacheEnvelope{
		Digest:        normalizeDigest(digest),
		SchemaVersion: SchemaVersion,
		CachedAt:      time.Now().UTC(),
		Layers:        toCachedLayers(layers),
	}
	if encErr := gob.NewEncoder(f).Encode(env); encErr != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("encoding cache: %w", encErr)
	}
	if closeErr := f.Close(); closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("closing temp cache file: %w", closeErr)
	}

	finalPath := filepath.Join(dir, "layers.gob")
	if renameErr := os.Rename(tmpPath, finalPath); renameErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming cache file: %w", renameErr)
	}
	return nil
}

func tempFilename(_, prefix string) (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(buf[:]), nil
}
