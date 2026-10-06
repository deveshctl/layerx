package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/deveshctl/layerx/cmd"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if version == "dev" && commit == "none" {
		if info, ok := debug.ReadBuildInfo(); ok {
			if c, d := resolveBuildInfo(info); c != "" {
				commit = c
				if d != "" {
					date = d
				}
			}
		}
	}
	cmd.SetVersionInfo(version, commit, date)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := cmd.ExecuteContext(ctx)
	if code := exitCodeFor(err); code != 0 {
		os.Exit(code)
	}
}

// exitCodeFor maps an error from cmd.ExecuteContext to an OS exit code.
// Extracted from main so the mapping logic is unit-testable without os.Exit.
//
//   - nil → 0
//   - ErrCIFailed / ErrCompareRegression → 1
//   - ErrBuildFailed → engine exit code (negative mapped to 1)
//   - anything else → 2
func exitCodeFor(err error) int {
	if err == nil {
		return 0
	}
	if _, ok := errors.AsType[*cmd.ErrCIFailed](err); ok {
		return 1
	}
	if _, ok := errors.AsType[*cmd.ErrCompareRegression](err); ok {
		return 1
	}
	if e, ok := errors.AsType[*cmd.ErrBuildFailed](err); ok {
		// Mirror the engine's exit code so CI scripts treat
		// `layerx build` exactly like `docker build` / `podman build`.
		// Preserve all positive codes (126 = not executable, 137 = OOM kill,
		// 255 = daemon-reported failure) — only map negative values (signal-
		// killed processes return -1 from ExitCode()) to 1 so os.Exit never
		// receives a negative argument.
		code := e.ExitCode
		if code < 0 {
			code = 1
		}
		return code
	}
	// Exit 2 covers ErrCIUsage, ErrCompareUsage, and any unrecognised error.
	// Usage errors are deliberately distinct from rule failures (ErrCIFailed →
	// 1) and build failures (ErrBuildFailed → engine code): do NOT remap
	// ErrCIUsage/ErrCompareUsage to exit 1.
	return 2
}

// resolveBuildInfo is consulted only when ldflags were not injected at link time;
// release builds supply commit and date directly.
func resolveBuildInfo(info *debug.BuildInfo) (commit, date string) {
	if info == nil {
		return "", ""
	}
	var rev, vcsTime, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			vcsTime = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}

	if rev != "" {
		const shortLen = 7
		if len(rev) > shortLen {
			commit = rev[:shortLen]
		} else {
			commit = rev
		}
		if modified == "true" {
			commit += "-dirty"
		}
	}

	if vcsTime != "" {
		if t, err := time.Parse(time.RFC3339, vcsTime); err == nil {
			date = t.UTC().Format("2006-01-02")
		}
	}

	return commit, date
}
