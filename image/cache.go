package image

import (
	"fmt"
	"os"
	"path/filepath"
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
