package engine

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// env abstracts the environment lookup so tests can inject values without
// touching the real process environment. os.Getenv is the production impl.
type env func(key string) string

// files abstracts the file-system reads a Resolver performs. Every call
// takes an absolute path so tests can back the interface with an
// in-memory map keyed by path.
//
// The abstraction is deliberately narrow — we only read whole small
// config files (< 10 KiB typical, capped at 1 MiB by readFile) and check
// existence. No traversal, no writes.
type files interface {
	// readFile returns the file's bytes or a wrapped *fs.PathError. It
	// caps reads at 1 MiB — a real Docker/Podman config file is a few
	// hundred bytes; anything bigger is either corruption or a symlink
	// pointed somewhere it shouldn't be, and refusing to load it stops
	// a bad config from wedging layerx.
	readFile(path string) ([]byte, error)

	stat(path string) (fs.FileInfo, error)

	// readDir returns the immediate children of dir. Used to enumerate
	// ~/.docker/contexts/meta/*/ so ErrConnectionNotFound can list the
	// context names actually present on disk.
	readDir(dir string) ([]fs.DirEntry, error)

	homeDir() (string, error)

	// configDir returns the OS's per-user config directory, matching
	// os.UserConfigDir semantics ($XDG_CONFIG_HOME on Linux,
	// %AppData% on Windows, ~/Library/Application Support on macOS).
	// Used to locate ~/.config/containers/ on Linux without hardcoding
	// that path (XDG_CONFIG_HOME override).
	configDir() (string, error)
}

// osFiles is the production files impl, backed by os.ReadFile / os.Stat /
// os.UserHomeDir / os.UserConfigDir. Kept as a value type so tests can
// assert the injected impl is not this one.
type osFiles struct{}

// maxConfigBytes is an anti-wedge cap on any config file we read. A real
// Docker config.json is ~200 B; a real Podman connections.json with 20
// entries is a few KiB; a containers.conf with heavy comments is under
// 100 KiB. Anything past 1 MiB is a red flag (symlink misdirection,
// corruption, log-file-masquerading-as-config).
const maxConfigBytes = 1 << 20 // 1 MiB

func (osFiles) readFile(path string) ([]byte, error) {
	// Stat + bounded read rather than ReadFile so a truncated read on an
	// oversized file surfaces as a distinct error the caller can attribute
	// to config size rather than parse failure.
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxConfigBytes {
		return nil, &fs.PathError{
			Op:   "read",
			Path: path,
			Err:  errConfigTooLarge,
		}
	}
	return os.ReadFile(path)
}

func (osFiles) stat(path string) (fs.FileInfo, error) { return os.Stat(path) }

func (osFiles) readDir(dir string) ([]fs.DirEntry, error) { return os.ReadDir(dir) }

func (osFiles) homeDir() (string, error) { return os.UserHomeDir() }

func (osFiles) configDir() (string, error) { return os.UserConfigDir() }

// errConfigTooLarge is the fs.PathError.Err returned when a config exceeds
// maxConfigBytes. Not exported: callers Unwrap to fs.PathError and read Path.
var errConfigTooLarge = &pathTooLargeErr{}

type pathTooLargeErr struct{}

func (*pathTooLargeErr) Error() string {
	return "config file exceeds 1 MiB — refusing to load"
}

func homePath(f files, elem ...string) string {
	home, err := f.homeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(append([]string{home}, elem...)...)
}

// configPath joins the user config directory with elements. Falls back to
// $HOME/.config on Linux when UserConfigDir errors — matching Podman's
// own behaviour of expecting containers.conf under $XDG_CONFIG_HOME with
// $HOME/.config as the historic default.
func configPath(f files, elem ...string) string {
	dir, err := f.configDir()
	if err == nil && dir != "" {
		return filepath.Join(append([]string{dir}, elem...)...)
	}
	if runtime.GOOS == "linux" {
		if home, herr := f.homeDir(); herr == nil && home != "" {
			return filepath.Join(append([]string{home, ".config"}, elem...)...)
		}
	}
	return ""
}
