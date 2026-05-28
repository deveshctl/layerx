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
	if err == nil {
		return
	}
	if _, ok := errors.AsType[*cmd.ErrCIFailed](err); ok {
		os.Exit(1)
	}
	if _, ok := errors.AsType[*cmd.ErrCompareRegression](err); ok {
		os.Exit(1)
	}
	os.Exit(2)
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
