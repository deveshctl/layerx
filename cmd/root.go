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
	Short: "Inspect container image layers and files",
	Long: `Browse image layers, filesystem changes, and wasted space interactively.

IMAGE_OR_ARCHIVE is a Docker or Podman image reference, a saved-image tar
archive, or an OCI layout tarball. Archives need no running engine.

Use --json to export analysis instead of opening the interactive viewer.
With CI=true, inspection runs CI checks using config or default thresholds.
Use "layerx ci" to set thresholds on the command line.

Configuration: --config overrides automatic lookup in the current directory,
parent directories, then the user configuration directory. Run "layerx init"
to create .layerx.yaml.

Engines use the active Docker context or Podman connection. Use --engine to
choose one and --platform OS/ARCH to select an image variant. Without
--platform, the engine's default platform is used.

Themes: tokyo-night (default), catppuccin-mocha, kanagawa, gruvbox-dark,
rose-pine, dracula, oxocarbon, cyberdream.

Documentation: https://github.com/deveshctl/layerx#readme`,
	Example: `  layerx nginx:stable
  layerx ./build/app.tar
  layerx --platform linux/arm64 nginx:stable
  layerx --json analysis.json ./build/app.tar
  layerx --engine podman alpine:3`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return nil
		}
		cmd.SilenceUsage = true
		return fmt.Errorf("expected 1 image or archive, got %d\nUsage: layerx [flags] IMAGE_OR_ARCHIVE\nExample: layerx nginx:stable\nRun layerx --help for options", len(args))
	},
	RunE: runInspect,
}

func init() {
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		cmd.SilenceUsage = true
		if cmd.SilenceErrors || rootCmd.SilenceErrors {
			return presentCLIError(cmd.ErrOrStderr(), err)
		}
		return err
	})
	rootCmd.Flags().StringVar(&flagJSON, "json", "", "write analysis to `FILE` as JSON instead of opening the viewer")
	rootCmd.PersistentFlags().BoolVar(&flagNoCacheFl, "no-cache", false, "run a fresh analysis and update the cache")
	rootCmd.PersistentFlags().StringVar(&flagConfig, "config", "", "read configuration from `FILE` instead of automatic lookup")
	rootCmd.Flags().StringVar(&flagTheme, "theme", "", "viewer colour theme (default: tokyo-night; see above for choices)")
	rootCmd.PersistentFlags().Var(&engineValue{v: &engineFlag}, "engine",
		`container engine: docker, podman, or auto`)
	rootCmd.PersistentFlags().StringVar(&platformFlag, "platform", "",
		`image platform: OS/ARCH[/VARIANT] (e.g. linux/arm64)`)
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
	// Runtime errors need a message, not the help page.
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

	// selectResolver produces the typed engine/resolver errors (no engine
	// found, Podman connection not configured, malformed connection config)
	// synchronously, before the TUI starts. Route them through presentCLIError
	// so the user sees the friendly, actionable message rather than the raw
	// Error() string cobra would otherwise print — and silence cobra's own
	// printer so it does not double-print. Mirrors the --json branch above.
	cmd.SilenceErrors = true

	resolver, err := selectResolver(imageRef)
	if err != nil {
		return presentCLIError(os.Stderr, err)
	}

	theme, err := resolveTheme(cfg.Theme, flagTheme)
	if err != nil {
		return presentCLIError(os.Stderr, err)
	}

	if err := tui.Run(tui.Config{
		ImageRef:      imageRef,
		Resolver:      resolver,
		NoCache:       noCache,
		Platform:      activePlatformDisplay(),
		Theme:         theme,
		TransparentBg: cfg.TransparentBackground,
	}); err != nil {
		return presentCLIError(os.Stderr, err)
	}
	return nil
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
