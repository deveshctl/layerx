package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/deveshctl/layerx/tui"
	"github.com/spf13/cobra"
)

type ErrBuildFailed struct {
	ExitCode int
}

func (e *ErrBuildFailed) Error() string {
	return fmt.Sprintf("build failed (exit %d)", e.ExitCode)
}

var runBuildCommand = func(c *exec.Cmd) error { return c.Run() }

var lookPath = exec.LookPath

var buildCmd = &cobra.Command{
	Use:   "build [BUILD_ARGS...]",
	Short: "Build an image and open it in the layerx TUI",
	Long: `Run the active container engine's build command and, on success, open
the resulting image in the layerx TUI.

This is a thin wrapper. Every argument after "build" is forwarded verbatim
to "docker build" or "podman build" — including -t, --build-arg, --platform,
--target, --file, the build-context path, and BuildKit flags. The engine
streams its own progress output (BuildKit, classic builder, or buildah)
directly to your terminal; layerx does not interpret it.

The engine is selected the same way as the rest of layerx: --engine
docker|podman|auto (default auto). Auto-detection probes the same sockets
"layerx" itself uses to pick docker or podman.

To recover the built image's identifier without parsing terminal output,
layerx asks the engine to write the image ID to a temporary file via
--iidfile. If you pass --iidfile yourself, your value is used and respected.

On a successful build, layerx hands the image ID off to the TUI exactly as
"layerx IMAGE" would. On a failed build, layerx exits with the engine's
exit code and does not launch the TUI.`,
	Example: `  # Build the current directory and inspect the result
  layerx build -t myimage .

  # Multiple tags — layerx inspects the image ID, which all tags share
  layerx build -t myimage:latest -t myimage:1.2.3 .

  # Forward arbitrary build flags
  layerx build --build-arg VERSION=1.2.3 --platform linux/amd64 -t myimage .

  # Force the podman CLI even if docker is also installed
  layerx --engine podman build -t myimage .`,
	DisableFlagParsing: true,
	Args:               cobra.ArbitraryArgs,
	SilenceUsage:       true,
	SilenceErrors:      true,
	RunE:               runBuild,
}

func init() {
	rootCmd.AddCommand(buildCmd)
}

func runBuild(cmd *cobra.Command, args []string) error {
	engineArgs, err := extractLayerxFlags(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		return err
	}

	for _, a := range engineArgs {
		if a == "-h" || a == "--help" {
			return cmd.Help()
		}
	}

	binary, err := pickEngineBinary(engineFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		return err
	}

	firstTag := firstTagFromArgs(engineArgs)

	var (
		iidPath     string
		ownsIIDFile bool
	)
	if firstTag == "" {
		iidPath, ownsIIDFile, err = ensureIIDFile(&engineArgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			return err
		}
		if ownsIIDFile {
			defer os.Remove(iidPath)
		}
	}

	fullArgs := append([]string{"build"}, engineArgs...)
	c := exec.CommandContext(cmd.Context(), binary, fullArgs...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	if err := runBuildCommand(c); err != nil {
		if errors.Is(cmd.Context().Err(), context.Canceled) {
			return cmd.Context().Err()
		}
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			return &ErrBuildFailed{ExitCode: exitErr.ExitCode()}
		}
		return fmt.Errorf("failed to invoke %s build: %w", binary, err)
	}

	imageRef := firstTag
	if imageRef == "" {
		id, readErr := readIIDFile(iidPath)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "warning: build succeeded but could not determine image to inspect (%v); pass -t TAG to layerx build to inspect by tag\n", readErr)
			return nil
		}
		imageRef = id
	}

	resolver, err := selectResolver(imageRef)
	if err != nil {
		return err
	}
	return tui.Run(tui.Config{
		ImageRef: imageRef,
		Resolver: resolver,
		NoCache:  noCacheRequested(),
	})
}

// firstTagFromArgs returns the first -t / --tag value in args, or "" if none.
// Both Docker and Podman accept -t and --tag with either a space or = form;
// only the first occurrence is used because all tags resolve to the same
// image and "the first one" matches what the engine itself would surface as
// the canonical reference for output.
func firstTagFromArgs(args []string) string {
	for i := range len(args) {
		a := args[i]
		if a == "--" {
			return ""
		}
		if a == "-t" || a == "--tag" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if v, ok := strings.CutPrefix(a, "--tag="); ok {
			return v
		}
		if v, ok := strings.CutPrefix(a, "-t="); ok {
			return v
		}
	}
	return ""
}

func extractLayerxFlags(args []string) ([]string, error) {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			out = append(out, args[i:]...)
			return out, nil
		}
		if a == "--engine" {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--engine requires a value")
			}
			if err := setEngine(args[i+1]); err != nil { //nolint:gosec // bound-checked above
				return nil, err
			}
			i++
			continue
		}
		if v, ok := strings.CutPrefix(a, "--engine="); ok {
			if err := setEngine(v); err != nil {
				return nil, err
			}
			continue
		}
		if a == "--no-cache" {
			flagNoCacheFl = true
			out = append(out, a)
			continue
		}
		if a == "--json" {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--json requires a value")
			}
			// gosec G602 cannot see that the i+1 < len(args) guard above
			// makes args[i+1] safe; we already returned on the failure
			// branch, so the access is unconditionally in-bounds.
			flagJSON = args[i+1] //nolint:gosec // bound-checked above
			i++
			continue
		}
		if v, ok := strings.CutPrefix(a, "--json="); ok {
			flagJSON = v
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

func setEngine(v string) error {
	switch v {
	case "docker", "podman", "auto":
		engineFlag = v
		return nil
	default:
		return fmt.Errorf("invalid engine %q (expected docker, podman, or auto)", v)
	}
}

func pickEngineBinary(engine string) (string, error) {
	switch engine {
	case "docker":
		return resolveBinary("docker")
	case "podman":
		return resolveBinary("podman")
	case "auto":
		if hasDockerSocket() {
			if path, err := lookPath("docker"); err == nil {
				return path, nil
			}
		}
		if runtime.GOOS == "linux" && hasPodmanSocket() {
			if path, err := lookPath("podman"); err == nil {
				return path, nil
			}
		}
		if path, err := lookPath("docker"); err == nil {
			return path, nil
		}
		if path, err := lookPath("podman"); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("neither docker nor podman found on PATH")
	default:
		return "", fmt.Errorf("unknown engine %q (expected docker, podman, or auto)", engine)
	}
}

func resolveBinary(name string) (string, error) {
	path, err := lookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s not found on PATH: %w", name, err)
	}
	return path, nil
}

func hasDockerSocket() bool {
	_, ok := probeFirst(dockerSocketCandidates())
	return ok
}

func hasPodmanSocket() bool {
	_, ok := probeFirst(podmanSocketCandidates())
	return ok
}

func ensureIIDFile(args *[]string) (path string, ownsFile bool, err error) {
	for i, a := range *args {
		if a == "--iidfile" {
			if i+1 >= len(*args) {
				return "", false, fmt.Errorf("--iidfile requires a value")
			}
			return (*args)[i+1], false, nil
		}
		if v, ok := strings.CutPrefix(a, "--iidfile="); ok {
			return v, false, nil
		}
	}
	f, err := os.CreateTemp("", "layerx-build-iid-*")
	if err != nil {
		return "", false, fmt.Errorf("could not create iidfile: %w", err)
	}
	p := f.Name()
	_ = f.Close()
	_ = os.Remove(p)
	*args = append(*args, "--iidfile", p)
	return p, true, nil
}

func readIIDFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(b))
	if id == "" {
		return "", fmt.Errorf("iidfile %s is empty", filepath.Base(path))
	}
	return id, nil
}
