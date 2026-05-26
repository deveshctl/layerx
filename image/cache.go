package image

import (
	"crypto/rand"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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
		expanded, expandErr := expandHome(override)
		if expandErr != nil {
			fmt.Fprintf(os.Stderr,
				"layerx: ignoring LAYERX_CACHE_DIR=%q (%v); falling back to default\n",
				override, expandErr)
		} else {
			if usable, _ := dirIsUsable(expanded); usable {
				return expanded, nil
			}
			// Bad override is non-fatal but the user almost certainly wants to
			// know — they set the env var on purpose. Fall back to the default
			// after warning once on stderr.
			fmt.Fprintf(os.Stderr,
				"layerx: ignoring LAYERX_CACHE_DIR=%q (not a usable directory); falling back to default\n",
				override)
		}
	}
	uc, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolving user cache dir: %w", err)
	}
	return filepath.Join(uc, "layerx"), nil
}

// expandHome resolves a leading "~" or "~/..." to the user's home directory.
// Paths without a "~" prefix are returned unchanged. Any other use of "~"
// (e.g. "~user/foo") is rejected — Go has no portable getpwnam.
func expandHome(p string) (string, error) {
	if p == "" || p[0] != '~' {
		return p, nil
	}
	if p != "~" && p[1] != '/' && p[1] != '\\' {
		return "", fmt.Errorf("unsupported ~user form: %q", p)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home dir for ~ expansion: %w", err)
	}
	if p == "~" {
		return home, nil
	}
	return filepath.Join(home, p[2:]), nil
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

// errBadDigest is returned by cachePath/normalizeDigest when a caller
// passes a digest that would escape the cache root (path separators, "..",
// or empty). It is treated by callers as "do not cache" rather than fatal.
var errBadDigest = errors.New("invalid cache digest")

// cachePath returns the absolute path to the gob file for a given digest
// under root. The digest is normalized (sha256: prefix stripped) and
// validated to ensure the directory name cannot escape root.
func cachePath(root, digest string) (string, error) {
	norm, err := normalizeDigest(digest)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, norm, "layers.gob"), nil
}

// normalizeDigest strips the "sha256:" prefix when present and rejects
// anything that could escape a single directory component (empty, path
// separators, "..", control chars).
func normalizeDigest(digest string) (string, error) {
	rest, _ := strings.CutPrefix(digest, "sha256:")
	if rest == "" {
		return "", errBadDigest
	}
	if rest == "." || rest == ".." {
		return "", errBadDigest
	}
	if strings.ContainsAny(rest, `/\`) || strings.Contains(rest, "..") {
		return "", errBadDigest
	}
	return rest, nil
}

// loadCache returns the persisted layers for the given digest.
//
// Return semantics:
//   - (layers, true,  nil)  hit
//   - (nil,    false, nil)  soft miss: file absent, schema mismatch, digest
//     mismatch, or gob corruption. The offending file (if any) is removed.
//   - (nil,    false, err)  hard miss: unexpected I/O the caller should log.
//     The file is NOT removed in this branch — a transient EIO/EBUSY/perm
//     failure should not evict an otherwise-valid cache.
func loadCache(root, digest string) (layers []Layer, ok bool, err error) {
	path, err := cachePath(root, digest)
	if err != nil {
		return nil, false, err
	}

	env, readErr, transient := readCacheFile(path)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return nil, false, nil
		}
		if transient {
			return nil, false, readErr
		}
		// Confirmed corruption. The handle is already closed by readCacheFile,
		// so os.Remove succeeds on Windows (which holds a sharing lock for as
		// long as a handle is open without FILE_SHARE_DELETE).
		_ = os.Remove(path)
		return nil, false, nil
	}

	if env.SchemaVersion != SchemaVersion {
		_ = os.Remove(path)
		return nil, false, nil
	}
	normDigest, _ := normalizeDigest(digest)
	if env.Digest != normDigest {
		_ = os.Remove(path)
		return nil, false, nil
	}
	return fromCachedLayers(env.Layers), true, nil
}

// readCacheFile opens path, decodes one cacheEnvelope, and closes the file
// before returning. Splitting this out of loadCache guarantees the OS handle
// is released before any os.Remove call — required on Windows, where files
// opened by the os package do not include FILE_SHARE_DELETE in their sharing
// mode and cannot be deleted while a handle is still open.
//
// Returns (env, nil, false) on success, (zero, err, true) on transient I/O
// (caller should NOT remove the file), and (zero, err, false) on confirmed
// corruption or missing-file (caller may remove).
func readCacheFile(path string) (cacheEnvelope, error, bool) {
	var env cacheEnvelope
	f, err := os.Open(path)
	if err != nil {
		// os.ErrNotExist is a soft miss (caller returns no error).
		// Anything else from os.Open — EACCES, EBUSY, network share
		// hiccup — is a transient I/O failure: the cache file may be
		// fine, we just couldn't read it right now. Do NOT delete it.
		if errors.Is(err, os.ErrNotExist) {
			return env, fmt.Errorf("opening cache %s: %w", path, err), false
		}
		return env, fmt.Errorf("opening cache %s: %w", path, err), true
	}
	defer f.Close()
	if decErr := gob.NewDecoder(f).Decode(&env); decErr != nil {
		// Distinguish gob corruption (delete + miss) from transient I/O
		// during read (keep file, surface error). io.ErrUnexpectedEOF can
		// arrive from either source — the safe call is to treat it as
		// corruption since the temp+rename pattern means a successful
		// rename always produced a complete file.
		if isTransientIOError(decErr) {
			return env, fmt.Errorf("reading cache %s: %w", path, decErr), true
		}
		return env, decErr, false
	}
	return env, nil, false
}

// isTransientIOError returns true when err looks like a recoverable I/O
// failure (permission denied, device busy, network share glitch) rather
// than confirmed file corruption.
func isTransientIOError(err error) bool {
	if _, ok := errors.AsType[*os.PathError](err); ok {
		return true
	}
	// Bare syscall errors and io.ErrClosedPipe are also transient. Plain
	// EOF without context is treated as corruption (file truncated).
	if errors.Is(err, io.ErrClosedPipe) {
		return true
	}
	return false
}

// saveCache writes layers to {root}/{digest}/layers.gob using a temp file +
// fsync + atomic rename. Errors are returned but should be treated as
// non-fatal by callers (the user already has the live result).
func saveCache(root, digest string, layers []Layer) error {
	norm, err := normalizeDigest(digest)
	if err != nil {
		return err
	}
	dir := filepath.Join(root, norm)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating cache dir %s: %w", dir, err)
	}

	sweepOrphanTempFiles(dir)

	tmpName, err := tempFilename("layers.gob.tmp-")
	if err != nil {
		return fmt.Errorf("generating temp name: %w", err)
	}
	tmpPath := filepath.Join(dir, tmpName)

	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("creating temp cache file: %w", err)
	}

	env := cacheEnvelope{
		Digest:        norm,
		SchemaVersion: SchemaVersion,
		CachedAt:      time.Now().UTC(),
		Layers:        toCachedLayers(layers),
	}
	if encErr := gob.NewEncoder(f).Encode(env); encErr != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("encoding cache: %w", encErr)
	}
	// fsync the file before rename so the data is durable on disk before the
	// directory entry flips. Best-effort: fsync is unsupported on some
	// platforms (Plan 9, certain FS configurations); a sync error is logged
	// at the outer call site via the returned error chain.
	if syncErr := f.Sync(); syncErr != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("syncing temp cache file: %w", syncErr)
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

// tempFilename returns prefix + 16 random hex chars. The caller chooses the
// directory; uniqueness is provided by the random suffix and enforced by
// O_EXCL on the saveCache create call.
func tempFilename(prefix string) (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(buf[:]), nil
}

// sweepOrphanTempFiles removes layers.gob.tmp-* files older than one hour
// from dir. Best effort: errors are ignored. A SIGKILL during saveCache can
// orphan a temp file; without this sweep, repeated crashes accumulate.
func sweepOrphanTempFiles(dir string) {
	matches, err := filepath.Glob(filepath.Join(dir, "layers.gob.tmp-*"))
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-1 * time.Hour)
	for _, m := range matches {
		info, statErr := os.Stat(m)
		if statErr != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(m)
		}
	}
}
