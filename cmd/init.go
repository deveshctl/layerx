package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const initConfigPath = ".layerx.yaml"

var (
	flagInitFlavour string
	flagInitForce   bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a starter .layerx.yaml configuration",
	Long: `Create .layerx.yaml in the current directory. Choose a generic, Node,
Python, Java, or Go template, then adjust its rules for your image.

Without --flavour, prompts in an interactive terminal and otherwise uses
the generic template. Existing files are kept unless --force is set.`,
	Example: `  layerx init
  layerx init --flavour node
  layerx init --flavour generic --force`,
	Args:         cobra.NoArgs,
	RunE:         runInitCmd,
	SilenceUsage: true,
}

func init() {
	initCmd.Flags().StringVar(&flagInitFlavour, "flavour", "",
		fmt.Sprintf("configuration template (%s)", flavourList()))
	initCmd.Flags().StringVar(&flagInitFlavour, "flavor", "",
		fmt.Sprintf("configuration template (%s)", flavourList()))
	_ = initCmd.Flags().MarkHidden("flavor")
	initCmd.Flags().BoolVar(&flagInitForce, "force", false,
		"overwrite an existing .layerx.yaml")
	rootCmd.AddCommand(initCmd)
}

func runInitCmd(cmd *cobra.Command, _ []string) error {
	flavour, err := resolveFlavour(flagInitFlavour, cmd.InOrStdin(), cmd.ErrOrStderr())
	if err != nil {
		return err
	}

	data, ok := StarterConfig(flavour)
	if !ok {
		return fmt.Errorf("unknown template %q (choose %s)", flavour, flavourList())
	}

	if err := writeStarterConfig(initConfigPath, data, flagInitForce); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Created %s (template: %s). Review its rules before running CI.\n", initConfigPath, flavour)
	return nil
}

// resolveFlavour decides which flavour to use based on the flag, TTY state,
// and (if interactive) user input. Empty flag + non-TTY stdin defaults to
// "generic" with a stderr note. Empty flag + TTY prompts.
func resolveFlavour(flag string, in io.Reader, errw io.Writer) (string, error) {
	if flag != "" {
		if !isValidFlavour(flag) {
			return "", fmt.Errorf("unknown template %q (choose %s)", flag, flavourList())
		}
		return flag, nil
	}

	if !isTerminal(in) {
		fmt.Fprintln(errw, "Using the generic template. Set --flavour to choose another.")
		return "generic", nil
	}

	return promptFlavour(in, errw)
}

// isTerminal reports whether r is an interactive terminal. Returns false
// for pipes, files, and any reader that isn't an *os.File.
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func promptFlavour(in io.Reader, errw io.Writer) (string, error) {
	fmt.Fprintln(errw, "Choose a configuration template:")
	for i, f := range validFlavours {
		fmt.Fprintf(errw, "  %d) %s\n", i+1, f)
	}
	fmt.Fprintf(errw, "Choice [1-%d, default=%d (generic)]: ", len(validFlavours), len(validFlavours))

	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return "generic", nil
	}
	choice := strings.TrimSpace(scanner.Text())
	if choice == "" {
		return "generic", nil
	}
	n, err := strconv.Atoi(choice)
	if err != nil || n < 1 || n > len(validFlavours) {
		return "", fmt.Errorf("invalid choice %q (want 1-%d)", choice, len(validFlavours))
	}
	return validFlavours[n-1], nil
}

// writeStarterConfig writes data to path. Refuses to overwrite an existing
// file unless force is true. Empty/missing destination directory is an
// error — we don't create parent directories (init is a per-repo command;
// the user should be running it from the repo root).
func writeStarterConfig(path string, data []byte, force bool) error {
	if _, err := os.Stat(path); err == nil {
		if !force {
			return fmt.Errorf("refusing to overwrite existing %s; pass --force to replace", path)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("checking %s: %w", path, err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
