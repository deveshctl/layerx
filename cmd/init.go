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
	Short: "Write a starter .layerx.yaml configuration",
	Long: `Write a starter .layerx.yaml configuration to the current directory.

Picks a configuration flavoured for your stack: Node, Python, Java, Go, or
generic. The starter file enables sensible path rules for that ecosystem
(e.g. block /root/.npm for Node, deny-waste **/*.pyc for Python). Edit
afterward to taste.

Refuses to overwrite an existing .layerx.yaml unless --force is set.`,
	Example: `  # Interactive flavour prompt (TTY) or generic (non-TTY)
  layerx init

  # Write the Node config without prompting
  layerx init --flavour node

  # Overwrite an existing .layerx.yaml with the generic config
  layerx init --flavour generic --force`,
	Args:         cobra.NoArgs,
	RunE:         runInitCmd,
	SilenceUsage: true,
}

func init() {
	initCmd.Flags().StringVar(&flagInitFlavour, "flavour", "",
		fmt.Sprintf("starter config flavour (%s)", flavourList()))
	initCmd.Flags().StringVar(&flagInitFlavour, "flavor", "",
		fmt.Sprintf("starter config flavour (%s)", flavourList()))
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
		return fmt.Errorf("unknown flavour %q (want %s)", flavour, flavourList())
	}

	return writeStarterConfig(initConfigPath, data, flagInitForce)
}

// resolveFlavour decides which flavour to use based on the flag, TTY state,
// and (if interactive) user input. Empty flag + non-TTY stdin defaults to
// "generic" with a stderr note. Empty flag + TTY prompts.
func resolveFlavour(flag string, in io.Reader, errw io.Writer) (string, error) {
	if flag != "" {
		if !isValidFlavour(flag) {
			return "", fmt.Errorf("unknown flavour %q (want %s)", flag, flavourList())
		}
		return flag, nil
	}

	if !isTerminal(in) {
		fmt.Fprintln(errw, "stdin is not a terminal; defaulting to flavour 'generic'. Use --flavour to override.")
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

// promptFlavour shows a numbered selector and reads the user's choice.
// 1) node, 2) python, 3) java, 4) go, 5) generic. Default (empty input)
// is generic.
func promptFlavour(in io.Reader, errw io.Writer) (string, error) {
	fmt.Fprintln(errw, "Pick a starter config flavour:")
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
