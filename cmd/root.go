package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/deveshctl/layerx/tui"
	"github.com/spf13/cobra"
)

var (
	flagJSON      string
	flagNoCacheFl bool
	flagConfig    string
	flagTheme     string
)

func noCacheRequested() bool {
	return flagNoCacheFl
}

var rootCmd = &cobra.Command{
	Use:   "layerx [flags] IMAGE_OR_ARCHIVE",
	Short: "Inspect container image layers (Docker, Podman, OCI archives)",
	Long: `LayerX Image Inspector opens a container image's layers, filesystem
changes, and wasted bytes.

Common usage:
  layerx IMAGE              browse layers interactively in a TUI
  layerx ci IMAGE           gate a build on efficiency thresholds
  layerx compare A B        diff two images for regressions
  layerx build [ARGS...]    build via the active engine, then inspect the result

IMAGE_OR_ARCHIVE accepts a container image reference (e.g. "nginx:latest") or
a path to a local archive produced by "docker save" / "podman save" or an
OCI-layout tarball. The two are auto-detected: an existing regular file is
read directly without contacting any runtime; anything else is resolved via
the active container engine (Docker or Podman). Archive mode needs no
daemon, no network, and no running containers.

Use --json to skip the TUI and export to a file. Set CI=true to make the
bare form behave as "layerx ci" with config-file (or default) thresholds;
threshold flags (--lowest-efficiency, --highest-wasted-bytes,
--highest-user-wasted-percent) are not accepted on this path — use
"layerx ci --lowest-efficiency 0.95 IMAGE" to pass thresholds directly.

Theme: pass --theme to select a TUI colour scheme (default: tokyo-night).
Valid values: catppuccin-mocha, tokyo-night, kanagawa, gruvbox-dark, rose-pine,
dracula, oxocarbon, cyberdream. Persists across runs when set via theme: in
.layerx.yaml; --theme overrides the config-file value for a single run.

Cache: results are cached per image digest. Use --no-cache to bypass for
a single run; "layerx cache list" inspects entries and "layerx cache prune"
evicts them (see "layerx cache --help" for full details).
Engines: layerx honours the active Docker context ("docker context use ...")
and the active Podman connection ("podman system connection default ...")
so it talks to the same daemon your engine's own CLI would. DOCKER_HOST /
CONTAINER_HOST still override. Pass --engine to force one engine.
Platforms: pass --platform OS/ARCH (e.g. linux/arm64) to inspect a specific
variant of a multi-platform image; layerx pulls and exports only that
variant. Without --platform, the daemon's default platform is used.`,
	Example: `  # Inspect an image interactively (Docker or Podman auto-detected)
  layerx nginx:latest

  # Inspect a local archive produced by "docker save" or "podman save"
  # (no daemon required)
  layerx ./build/app.tar

  # Inspect an OCI layout tarball
  layerx /tmp/myimage-oci.tar

  # Force a fresh analysis, ignoring any cached result
  layerx --no-cache nginx:latest

  # Inspect a specific platform variant of a multi-platform image
  layerx --platform linux/arm64 nginx:latest
  layerx --platform linux/amd64 alpine:3

  # Export analysis as JSON (skips the TUI; works with archives too)
  layerx --json out.json ./build/app.tar

  # Run efficiency checks (also triggered by CI=true)
  layerx ci nginx:latest

  # Use a different colour theme
  layerx --theme dracula nginx:latest

  # Use Podman instead of Docker (Linux: socket auto-detected)
  layerx --engine podman alpine:3`,
	Args: cobra.ExactArgs(1),
	RunE: runInspect,
	// SilenceUsage is intentionally left at its zero value (false) here —
	// runInspect flips it to true on entry. This keeps the usage block
	// visible for arg-validation failures (bare `layerx` with no image
	// argument) where the help text IS the right response, while still
	// silencing the 60-line dump for runtime errors (config parse, daemon
	// down, image not found) once we've reached RunE.
}

func init() {
	rootCmd.Flags().StringVar(&flagJSON, "json", "", "write analysis to PATH as JSON (skips TUI; composes with the ci subcommand)")
	rootCmd.PersistentFlags().BoolVar(&flagNoCacheFl, "no-cache", false, "bypass the analysis cache for this run; the run still writes the cache on success")
	rootCmd.PersistentFlags().StringVar(&flagConfig, "config", "", "path to .layerx.yaml config file (default: walk up from CWD, then $XDG_CONFIG_HOME/layerx/config.yaml)")
	rootCmd.Flags().StringVar(&flagTheme, "theme", "", "colour theme for the TUI (default: tokyo-night; valid: catppuccin-mocha, tokyo-night, kanagawa, gruvbox-dark, rose-pine, dracula, oxocarbon, cyberdream). Overrides theme: in .layerx.yaml.")
	rootCmd.PersistentFlags().Var(&engineValue{v: &engineFlag}, "engine",
		`container engine to use: "docker", "podman", or "auto". Each engine honours its own active context/connection ("docker context use", "podman system connection default"); DOCKER_HOST / CONTAINER_HOST env vars still override`)
	rootCmd.PersistentFlags().StringVar(&platformFlag, "platform", "",
		`target platform for multi-platform images (e.g. "linux/amd64", "linux/arm64", "linux/arm64/v8")`)
	_ = rootCmd.RegisterFlagCompletionFunc("platform", completePlatform)
	_ = rootCmd.RegisterFlagCompletionFunc("theme", completeTheme)
}

type engineValue struct{ v *string }

func (e *engineValue) String() string {
	if e.v == nil {
		return ""
	}
	return *e.v
}

func (e *engineValue) Type() string { return "string" }

func (e *engineValue) Set(s string) error {
	switch s {
	case "docker", "podman", "auto":
		*e.v = s
		return nil
	default:
		return fmt.Errorf("invalid engine %q (expected docker, podman, or auto)", s)
	}
}

var validThemes = map[string]bool{
	"catppuccin-mocha": true,
	"tokyo-night":      true,
	"kanagawa":         true,
	"gruvbox-dark":     true,
	"rose-pine":        true,
	"dracula":          true,
	"oxocarbon":        true,
	"cyberdream":       true,
}

// resolveTheme picks the effective theme name: --theme flag wins over the
// config file value; empty string means use the built-in default.
// Returns an error if the flag value is not a recognised theme name.
func resolveTheme(fromConfig, fromFlag string) (string, error) {
	if fromFlag != "" {
		if !validThemes[fromFlag] {
			return "", fmt.Errorf("unknown theme %q; valid themes: catppuccin-mocha, tokyo-night, kanagawa, gruvbox-dark, rose-pine, dracula, oxocarbon, cyberdream", fromFlag)
		}
		return fromFlag, nil
	}
	return fromConfig, nil
}

func SetVersionInfo(v, c, d string) {
	c = sanitizeVersionField(c)
	d = sanitizeVersionField(d)
	if c == "" || c == "none" {
		rootCmd.Version = v
		return
	}
	if d == "" || d == "unknown" {
		rootCmd.Version = fmt.Sprintf("%s (commit %s)", v, c)
		return
	}
	rootCmd.Version = fmt.Sprintf("%s (commit %s, built %s)", v, c, d)
}

// sanitizeVersionField trims surrounding whitespace, strips control characters
// (CR/LF/tab) that would smash a single-line --version output, and bounds
// the length so a malformed build-time inject of multi-KB junk can't blow up
// the help block. The return is intended for direct interpolation into the
// version string by SetVersionInfo.
func sanitizeVersionField(s string) string {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("\r", "", "\n", " ", "\t", " ").Replace(s)
	const maxLen = 64
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

func Execute() error {
	return rootCmd.Execute()
}

// ExecuteContext runs the root command with ctx wired through to every
// subcommand's RunE via cmd.Context(). main.go installs a signal-cancelled
// context here so Ctrl+C reaches into long-running analyze paths
// (image pulls, exports) instead of waiting for them to return.
func ExecuteContext(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}

func runInspect(cmd *cobra.Command, args []string) error {
	// Args validation has passed by the time RunE runs — from here on, any
	// error is a runtime failure (bad config, daemon down, image not found)
	// where cobra's default usage dump only buries the actual message. The
	// rootCmd declaration deliberately leaves SilenceUsage at false so
	// missing-argument errors still print usage; flip it now that we're
	// past that gate.
	cmd.SilenceUsage = true

	imageRef := args[0]

	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}

	noCache := noCacheRequested()

	if v := os.Getenv("CI"); v != "" && v != "0" && !strings.EqualFold(v, "false") {
		warnCIThresholdFlagsIgnored(os.Stderr)
		if err := validateCLIThresholdFlags(ciCmd); err != nil {
			return err
		}
		// The CI report is already printed by executeCICheck; suppress
		// cobra's default error output so an ErrCIFailed return doesn't
		// tack a redundant "Error: ..." line onto the report. Usage is
		// silenced at the start of runInspect; config failures print via
		// loadConfig before we reach this branch.
		cmd.SilenceErrors = true
		// Forward the root cobra command's signal-cancellable context to
		// runCICheckInner so image.AnalyzeWithOptions sees Ctrl+C. We pass
		// the context explicitly rather than mutating ciCmd.SetContext —
		// ciCmd is package-level state, and a leaked context outlives the
		// invocation.
		analysis, ciErr := executeCICheck(cmd.Context(), imageRef, cfg, ciCmd, noCache, true)
		if flagJSON != "" {
			var jsonErr error
			switch {
			case analysis != nil:
				jsonErr = runJSONExportFromAnalysis(analysis, flagJSON)
			case ciErr != nil:
				jsonErr = nil
			default:
				jsonErr = runJSONExport(cmd.Context(), imageRef, flagJSON, noCache)
			}
			return combineCIAndJSONErr(ciErr, jsonErr, os.Stderr)
		}
		return ciErr
	}

	if flagJSON != "" {
		// runJSONExport prints its own friendly stderr line; suppress
		// cobra's default error printer to avoid double-printing.
		cmd.SilenceErrors = true
		return runJSONExport(cmd.Context(), imageRef, flagJSON, noCache)
	}

	resolver, err := selectResolver(imageRef)
	if err != nil {
		return err
	}

	theme, err := resolveTheme(cfg.Theme, flagTheme)
	if err != nil {
		return err
	}

	return tui.Run(tui.Config{
		ImageRef: imageRef,
		Resolver: resolver,
		NoCache:  noCache,
		Platform: activePlatformDisplay(),
		Theme:    theme,
	})
}

// warnCIThresholdFlagsIgnored prints a warning to w when CI=true is active
// and a threshold flag name appears in the raw process arguments. Threshold
// flags are registered only on ciCmd, so Cobra rejects them as unknown flags
// before RunE on the root command — this warning is a belt-and-suspenders
// guard that fires if flag wiring ever changes.
func warnCIThresholdFlagsIgnored(w io.Writer) {
	thresholdFlags := []string{
		"--lowest-efficiency",
		"--highest-wasted-bytes",
		"--highest-user-wasted-percent",
	}
	raw := strings.Join(os.Args, " ")
	for _, f := range thresholdFlags {
		if strings.Contains(raw, f) {
			fmt.Fprintf(w,
				"warning: CI=true path does not accept --lowest-efficiency / --highest-wasted-bytes / --highest-user-wasted-percent\n         use `layerx ci --lowest-efficiency VALUE IMAGE` to pass thresholds directly\n",
			)
			return
		}
	}
}

// combineCIAndJSONErr decides which error wins when CI=true is paired with
// --json. The CI exit code wins on CI failure (a failing CI rule must stay
// the user-visible result), but a JSON write failure on a CI-pass run must
// not be silent — it becomes the returned error and is also surfaced as a
// warning on stderr regardless of which error wins.
func combineCIAndJSONErr(ciErr, jsonErr error, stderr io.Writer) error {
	if jsonErr != nil {
		fmt.Fprintf(stderr, "warning: JSON export failed: %v\n", jsonErr)
		if ciErr == nil {
			return jsonErr
		}
	}
	return ciErr
}
