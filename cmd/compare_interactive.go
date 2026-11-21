package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func validateInteractiveCompare(cmd *cobra.Command) error {
	interactive, _ := cmd.Flags().GetBool("interactive")
	if !interactive {
		return nil
	}
	for _, name := range []string{"mode", "top"} {
		if cmd.Flags().Changed(name) {
			return fmt.Errorf("--interactive cannot be combined with --%s", name)
		}
	}
	format, _ := cmd.Flags().GetString("format")
	if format != "text" {
		return fmt.Errorf("--interactive requires --format text")
	}
	out, ok := cmd.OutOrStdout().(*os.File)
	if !isTerminal(cmd.InOrStdin()) || !ok || !term.IsTerminal(int(out.Fd())) {
		return fmt.Errorf("--interactive requires terminal input and output; omit -i for redirected reports")
	}
	return nil
}
