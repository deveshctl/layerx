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
	"sort"
	"strconv"
	"strings"
	"syscall"
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

// Prune defaults — cache directory hygiene. Both can be overridden by
// LAYERX_CACHE_TTL_DAYS and LAYERX_CACHE_MAX_BYTES respectively; setting
// either env var to "0" disables that limit. Picked so a typical user
// running layerx against a handful of images per week never sees prune
// touch their cache; heavy users get a 1 GiB ceiling without surprise.
const (
	defaultCacheTTLDays  = 30
	defaultCacheMaxBytes = 1 << 30 // 1 GiB
	// maxCacheTTLDays caps LAYERX_CACHE_TTL_DAYS. time.Duration is int64
	// nanoseconds; ttlDays * 24h overflows around 10^7 days. 100000 days
	// (~273 years) is safely below that and well past any practical cache
	// retention. Larger user values fall back to the default with a warn.
	maxCacheTTLDays = 100000
)

type CacheEntry struct {
	Digest   string
	Size     int64     // bytes — combined size of all layers*.gob files in the digest directory
	CachedAt time.Time // newest mtime among all layers*.gob files; honestly named (not "LastUsed")
	// ImageRef is the image reference the cache was originally written with
	// (e.g. "nginx:latest", "/build/app.tar"). Empty when the sidecar
	// meta.json is missing — entries written by older versions, or by
	// PruneCache which doesn't bother reading the sidecar. Display-only;
	// loadCache and PruneCache do not consult this field.
	ImageRef string
}

// PruneOptions controls what PruneCache evicts. Zero value is a no-op.
//
//	TTL > 0       remove entries whose CachedAt is older than now-TTL
//	MaxBytes > 0  after the TTL pass, evict oldest entries until the
//	              surviving total is at or below MaxBytes
//	All == true   ignore TTL/MaxBytes; remove every entry under root
//	DryRun        walk and decide, but never call os.RemoveAll
//	Keep          digest never to evict, regardless of other options.
//	              Used by saveCache to protect the just-written entry,
//	              even when its own size exceeds MaxBytes. The user-
//	              driven `cache prune --all` always passes Keep="", so
//	              it genuinely empties the cache.
//	Now           injectable clock for tests; nil → time.Now
type PruneOptions struct {
	TTL      time.Duration
	MaxBytes int64
	All      bool
	DryRun   bool
	Keep     string
	Now      func() time.Time
}

// PruneResult is the return value of PruneCache.
//
//	Removed   entries that were (or, in DryRun, would be) evicted, in
//	          the order they were processed: TTL victims first (by
//	          scan order), then size-cap victims oldest-first.
//	Kept      entries that survived this prune, in scan order.
//	Warnings  user-facing strings about partial failures (RemoveAll
//	          errors, single-entry-exceeds-cap). cmd/ chooses how to
//	          render; image/ does not print.
type PruneResult struct {
	Removed  []CacheEntry
	Kept     []CacheEntry
	Warnings []string
}

// nowFn lets prune tests freeze time without plumbing a clock interface
// through every helper. Stdlib precedent: net/http/cookiejar uses the
// same shape. Production code never overwrites it.
var nowFn = time.Now

// removeAllFn lets prune tests inject RemoveAll failures deterministically.
// chmod-based failure injection is unreliable across kernels and Go
// versions because os.RemoveAll has grown logic to chmod-up unreadable
// dirs and retry. Production code never overwrites it.
var removeAllFn = os.RemoveAll

func loadPruneLimits(progress chan<- ProgressEvent) (ttl time.Duration, maxBytes int64) {
	ttlDays := defaultCacheTTLDays
	if v := os.Getenv("LAYERX_CACHE_TTL_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		switch {
		case err != nil:
			emitCacheWarn(progress, fmt.Sprintf("ignoring LAYERX_CACHE_TTL_DAYS=%q: %v", v, err))
		case n < 0:
			emitCacheWarn(progress, fmt.Sprintf("ignoring LAYERX_CACHE_TTL_DAYS=%q: negative", v))
		case n > maxCacheTTLDays:
			emitCacheWarn(progress, fmt.Sprintf("ignoring LAYERX_CACHE_TTL_DAYS=%q: exceeds max %d days", v, maxCacheTTLDays))
		default:
			ttlDays = n
		}
	}

	maxBytes = defaultCacheMaxBytes
	if v := os.Getenv("LAYERX_CACHE_MAX_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		switch {
		case err != nil:
			emitCacheWarn(progress, fmt.Sprintf("ignoring LAYERX_CACHE_MAX_BYTES=%q: %v", v, err))
		case n < 0:
			emitCacheWarn(progress, fmt.Sprintf("ignoring LAYERX_CACHE_MAX_BYTES=%q: negative", v))
		default:
			maxBytes = n
		}
	}

	return time.Duration(ttlDays) * 24 * time.Hour, maxBytes
}

// pruneEntry is one candidate during prune. mtime is the newest layers*.gob
// modtime — used as CachedAt display proxy. oldest is the oldest layers*.gob
// modtime — used as the effective age for TTL eviction, so a stale
// platform-pinned file is not shielded by a freshly-written sibling.
type pruneEntry struct {
	name   string // digest directory name (no path separators)
	path   string // {root}/{name}
	mtime  time.Time
	oldest time.Time
	size   int64
}

// pruneCache enforces TTL and size-cap limits on the cache root.
// Best-effort: errors emit at most one PhaseCacheWarn per call (via
// progress) and the function returns. keepDigest is the just-written,
// already-normalized digest; it is never evicted, even if its own size
// exceeds maxBytes. progress may be nil.
//
// Foreign files at the root level (e.g. a stray README) are left alone.
// Directories whose names fail normalizeDigest are likewise ignored.
func pruneCache(root, keepDigest string, progress chan<- ProgressEvent) {
	ttl, maxBytes := loadPruneLimits(progress)
	if ttl == 0 && maxBytes == 0 {
		return
	}
	res, err := PruneCache(root, PruneOptions{
		TTL: ttl, MaxBytes: maxBytes, Keep: keepDigest, Now: nowFn,
	})
	if err != nil {
		emitCacheWarn(progress, fmt.Sprintf("cache prune skipped: %v", err))
		return
	}
	for _, w := range res.Warnings {
		emitCacheWarn(progress, w)
	}
}

func cachePath(root, digest string) (string, error) {
	return cachePathWithPlatform(root, digest, "")
}

// cachePathWithPlatform returns the path to the cache file for the given
// digest and optional platform. When platform is non-empty the cache file is
// stored as "layers-<platform-slug>.gob" alongside "layers.gob" so that
// analyses pinned to different platforms of the same image never share a
// cache entry. Both files live under the same digest directory and are
// recognised by ListCache/PruneCache via the "layers*.gob" glob.
func cachePathWithPlatform(root, digest, platform string) (string, error) {
	norm, err := normalizeDigest(digest)
	if err != nil {
		return "", err
	}
	file := "layers.gob"
	if platform != "" {
		// Replace '/' with '-' so the slug is a valid file-name component on
		// all supported OSes. "linux/amd64" → "layers-linux-amd64.gob".
		// activePlatformDisplay() sanitises via ParsePlatform/FormatPlatform,
		// but defence-in-depth here costs nothing.
		slug, err := platformSlug(platform)
		if err != nil {
			return "", err
		}
		file = "layers-" + slug + ".gob"
	}
	return filepath.Join(root, norm, file), nil
}

// platformSlug converts a platform string ("linux/amd64") to a filename
// component ("linux-amd64") and validates it against known attack vectors:
// path traversal sequences (".."), path separators, and Windows reserved
// device names (NUL, CON, PRN, AUX, COM1-COM9, LPT1-LPT9). On Windows,
// a path component that equals a reserved name (case-insensitive) refers
// to the device rather than a file — NUL discards data, CON reads stdin.
// The '-' separator used by the slug means each original segment maps to
// a single slug segment, so we check each segment independently.
func platformSlug(platform string) (string, error) {
	slug := strings.ReplaceAll(platform, "/", "-")
	if strings.Contains(slug, "..") || strings.ContainsAny(slug, `/\`) {
		return "", fmt.Errorf("invalid platform slug %q", slug)
	}
	for _, seg := range strings.Split(slug, "-") {
		upper := strings.ToUpper(seg)
		switch upper {
		case "CON", "PRN", "AUX", "NUL":
			return "", fmt.Errorf("invalid platform slug %q: reserved device name", slug)
		}
		if len(upper) == 4 && (upper[:3] == "COM" || upper[:3] == "LPT") {
			if upper[3] >= '1' && upper[3] <= '9' {
				return "", fmt.Errorf("invalid platform slug %q: reserved device name", slug)
			}
		}
	}
	return slug, nil
}

// canonical form of a Docker/OCI content digest: exactly 64 lowercase
// hexadecimal characters. Anything else is rejected.
//
// The strict form is a defense against `LAYERX_CACHE_DIR` misconfiguration.
// PruneCache uses this validator to decide which subdirectories are cache
// entries eligible for eviction; a loose validator that accepts arbitrary
// names (e.g. "scratch-work", "README") would let prune delete unrelated
// user data if the cache root points at a shared parent directory.
func normalizeDigest(digest string) (string, error) {
	rest, _ := strings.CutPrefix(digest, "sha256:")
	if len(rest) != 64 {
		return "", errBadDigest
	}
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		isDigit := c >= '0' && c <= '9'
		isHex := c >= 'a' && c <= 'f'
		if !isDigit && !isHex {
			return "", errBadDigest
		}
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
	return loadCacheWithPlatform(root, digest, "")
}

func loadCacheWithPlatform(root, digest, platform string) (layers []Layer, ok bool, err error) {
	path, err := cachePathWithPlatform(root, digest, platform)
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
	// A successfully-decoded envelope with zero layers is impossible for any
	// real Docker image. Treat it as cache corruption (truncated write,
	// concurrent modification, manual edit) rather than a legitimate empty
	// image — otherwise the TUI silently presents an empty filesystem.
	if len(env.Layers) == 0 {
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
// failure (network share glitch, closed pipe) rather than confirmed file
// corruption. A syscall error during gob.Decode can be wrapped in os.PathError;
// it does not establish that the bytes in the cache file are corrupt.
func isTransientIOError(err error) bool {
	// io.ErrClosedPipe is an explicit transient signal.
	if errors.Is(err, io.ErrClosedPipe) {
		return true
	}
	// Bare syscall errors (EIO, EBUSY, EAGAIN, …) that surface through
	// gob.Decode on a network share or slow disk indicate a read-path hiccup,
	// not file corruption. The temp+rename write pattern means a successfully
	// renamed cache file was always written complete, so a syscall.Errno seen
	// during a read is transient, not proof of corruption.
	var errno syscall.Errno
	return errors.As(err, &errno)
}

// saveCache writes layers to {root}/{digest}/layers.gob using a temp file +
// fsync + atomic rename. Errors are returned but should be treated as
// non-fatal by callers (the user already has the live result).
func saveCache(root, digest string, imageRef string, layers []Layer, progress chan<- ProgressEvent) error {
	return saveCacheWithPlatform(root, digest, "", imageRef, layers, progress)
}

func saveCacheWithPlatform(root, digest, platform, imageRef string, layers []Layer, progress chan<- ProgressEvent) error {
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

	file := "layers.gob"
	if platform != "" {
		slug, slugErr := platformSlug(platform)
		if slugErr != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("invalid platform slug: %w", slugErr)
		}
		file = "layers-" + slug + ".gob"
	}
	finalPath := filepath.Join(dir, file)
	if renameErr := os.Rename(tmpPath, finalPath); renameErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming cache file: %w", renameErr)
	}

	// Display-only sidecar: imageRef the cache was first written with, so
	// `layerx cache list` can show "nginx:latest" alongside the digest.
	// Best-effort — a write failure here does not invalidate the cache; the
	// list view will show "<unknown>" for this digest until next save.
	writeMetaSidecar(dir, imageRef)

	// Opportunistic prune at the tail of every successful write.
	// pruneCache never panics and never returns an error; its result
	// is on disk and on the progress channel only.
	pruneCache(root, norm, progress)
	return nil
}

// writeMetaSidecar writes {dir}/meta.json containing the image ref via the
// same temp-file + rename pattern saveCache uses for layers.gob, so a crash
// mid-write cannot leave a half-written sidecar that ListCache would parse.
// Errors are silently ignored: the sidecar is purely cosmetic (used by
// `layerx cache list`), and a missing sidecar already has a graceful path
// in ListCache. imageRef is written verbatim — callers must not pass
// secrets here; layerx never has any in this code path (the ref is the
// CLI argument the user typed).
func writeMetaSidecar(dir, imageRef string) {
	if imageRef == "" {
		return
	}
	tmpName, err := tempFilename("meta.json.tmp-")
	if err != nil {
		return
	}
	tmpPath := filepath.Join(dir, tmpName)
	body := []byte(`{"image_ref":` + strconv.Quote(imageRef) + "}\n")
	if writeErr := os.WriteFile(tmpPath, body, 0o600); writeErr != nil {
		return
	}
	if renameErr := os.Rename(tmpPath, filepath.Join(dir, "meta.json")); renameErr != nil {
		_ = os.Remove(tmpPath)
	}
}

func readMetaSidecar(dir string) string {
	body, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return ""
	}
	// Tiny ad-hoc parse instead of pulling encoding/json into the cache
	// hot-path: the file we wrote is always {"image_ref":"..."}\n.
	// A hand-edited sidecar that doesn't match falls through to "".
	const prefix = `{"image_ref":`
	s := strings.TrimSpace(string(body))
	if !strings.HasPrefix(s, prefix) || !strings.HasSuffix(s, "}") {
		return ""
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(s, prefix), "}")
	ref, err := strconv.Unquote(strings.TrimSpace(inner))
	if err != nil {
		return ""
	}
	return ref
}

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
//
// Uses os.ReadDir rather than filepath.Glob to avoid ErrBadPattern when dir
// contains bracket characters (e.g. a Windows username like "user[1]").
func sweepOrphanTempFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-1 * time.Hour)
	const prefix = "layers.gob.tmp-"
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		info, statErr := e.Info()
		if statErr != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// gobFilesInDir returns the combined size, newest mtime, and oldest mtime of
// all cache gob files in dir (exactly "layers.gob" and "layers-*.gob").
// ok is false when no such file exists.
//
// Uses os.ReadDir rather than filepath.Glob to avoid ErrBadPattern when dir
// contains bracket characters (e.g. a Windows username like "user[1]").
//
// newest is used as CachedAt in display (shows when any platform was last
// cached). oldest is used by PruneCache's TTL check so that a stale
// per-platform file is not shielded from eviction by a freshly-written
// sibling: the directory's effective age is its oldest resident file.
func gobFilesInDir(dir string) (size int64, newest, oldest time.Time, ok bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, time.Time{}, time.Time{}, false
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !isCacheGobName(name) {
			continue
		}
		info, statErr := e.Info()
		if statErr != nil {
			continue
		}
		ok = true
		size += info.Size()
		mt := info.ModTime()
		if mt.After(newest) {
			newest = mt
		}
		if oldest.IsZero() || mt.Before(oldest) {
			oldest = mt
		}
	}
	return size, newest, oldest, ok
}

// isCacheGobName reports whether name is one of the two file shapes the
// cache writes: exactly "layers.gob", or "layers-" followed by a non-empty
// slug and ".gob". The narrower match prevents stray files (e.g.
// "layersuser.gob") from inflating size or skewing mtime calculations.
func isCacheGobName(name string) bool {
	if name == "layers.gob" {
		return true
	}
	rest, ok := strings.CutPrefix(name, "layers-")
	return ok && strings.HasSuffix(rest, ".gob") && len(rest) > len(".gob")
}

// ListCache returns every valid digest dir under root with its size and
// mtime. A missing root is not an error; it returns ([], nil, nil) — the
// cache simply hasn't been populated yet. Foreign files at root, dirs
// whose names fail digest validation, and dirs with no layers*.gob files
// are silently skipped (same rule as the auto-prune).
//
// Warnings are returned as plain strings so the caller (cmd/cache.go)
// can render them. ListCache itself never writes to stderr.
//
// Entries are returned sorted oldest-first by mtime. Predictable order
// keeps the renderer test-stable and matches PruneCache's eviction order.
func ListCache(root string) ([]CacheEntry, []string, error) {
	dirs, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []CacheEntry{}, nil, nil
		}
		return nil, nil, err
	}

	out := make([]CacheEntry, 0, len(dirs))
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		if _, err := normalizeDigest(d.Name()); err != nil {
			continue
		}
		entryDir := filepath.Join(root, d.Name())
		size, newest, _, ok := gobFilesInDir(entryDir)
		if !ok {
			continue
		}
		out = append(out, CacheEntry{
			Digest:   d.Name(),
			Size:     size,
			CachedAt: newest,
			ImageRef: readMetaSidecar(entryDir),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CachedAt.Before(out[j].CachedAt)
	})
	return out, nil, nil
}

// PruneCache applies opts and returns what it did. Best-effort: a
// RemoveAll failure is recorded in Warnings and skipped, never returned
// as the function's err. The function's err is reserved for "could not
// even read the cache root" — every other failure is per-entry.
//
// The first RemoveAll failure within a single call is appended to
// Warnings as `cache prune partial: <err>`; subsequent failures during
// the same call are silently counted to preserve I-03's "fifty broken
// evictees should not produce fifty stderr lines" contract.
//
// Concurrent prunes against the same root may both try to remove the
// same entry; the second RemoveAll returns ErrNotExist which is treated
// as success (Removed gets the entry, no warning).
func PruneCache(root string, opts PruneOptions) (PruneResult, error) {
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}

	dirs, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return PruneResult{}, nil
		}
		return PruneResult{}, err
	}

	records := make([]pruneEntry, 0, len(dirs))
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		if _, err := normalizeDigest(d.Name()); err != nil {
			continue
		}
		entryPath := filepath.Join(root, d.Name())
		size, newest, oldest, ok := gobFilesInDir(entryPath)
		if !ok {
			continue
		}
		records = append(records, pruneEntry{
			name:   d.Name(),
			path:   entryPath,
			mtime:  newest,
			oldest: oldest,
			size:   size,
		})
	}

	var res PruneResult
	warned := false
	tryRemove := func(p string) bool {
		if opts.DryRun {
			return true
		}
		if rmErr := removeAllFn(p); rmErr != nil {
			if !warned {
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("cache prune partial: %v", rmErr))
				warned = true
			}
			return false
		}
		return true
	}

	toEntry := func(r pruneEntry) CacheEntry {
		return CacheEntry{Digest: r.name, Size: r.size, CachedAt: r.mtime}
	}

	// All=true short-circuits TTL/MaxBytes: every record except Keep is
	// evicted. Auto-prune never sets All; the user-driven `prune --all`
	// passes All=true with Keep="".
	if opts.All {
		for _, r := range records {
			if r.name == opts.Keep {
				res.Kept = append(res.Kept, toEntry(r))
				continue
			}
			if tryRemove(r.path) {
				res.Removed = append(res.Removed, toEntry(r))
			} else {
				res.Kept = append(res.Kept, toEntry(r))
			}
		}
		return res, nil
	}

	if opts.TTL <= 0 && opts.MaxBytes <= 0 {
		for _, r := range records {
			res.Kept = append(res.Kept, toEntry(r))
		}
		return res, nil
	}

	if opts.TTL > 0 {
		survivors := records[:0]
		t := now()
		for _, r := range records {
			if r.name != opts.Keep && t.Sub(r.oldest) > opts.TTL && tryRemove(r.path) {
				res.Removed = append(res.Removed, toEntry(r))
				continue
			}
			survivors = append(survivors, r)
		}
		records = survivors
	}

	if opts.MaxBytes > 0 {
		var total int64
		for _, r := range records {
			total += r.size
		}
		if total > opts.MaxBytes {
			sort.Slice(records, func(i, j int) bool {
				return records[i].oldest.Before(records[j].oldest)
			})
			survivors := records[:0]
			for _, r := range records {
				if total <= opts.MaxBytes {
					survivors = append(survivors, r)
					continue
				}
				if r.name == opts.Keep {
					survivors = append(survivors, r)
					continue
				}
				if tryRemove(r.path) {
					total -= r.size
					res.Removed = append(res.Removed, toEntry(r))
				} else {
					survivors = append(survivors, r)
				}
			}
			records = survivors
			if total > opts.MaxBytes {
				res.Warnings = append(res.Warnings,
					"cache size cap exceeded by single entry; kept")
			}
		}
	}

	for _, r := range records {
		res.Kept = append(res.Kept, toEntry(r))
	}
	return res, nil
}
