package cmd

import (
	"errors"
	"fmt"
	"io"

	"github.com/deveshctl/layerx/config"
	"github.com/spf13/cobra"
)

// loadConfig reads .layerx.yaml and presents failures without invoking
// cobra's full usage block. On error the one-line message is printed to
// stderr along with a section-specific hint when the failure is a
// *config.LoadError.
func loadConfig(cmd *cobra.Command) (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		wrapped := fmt.Errorf("loading config: %w", err)
		presentConfigLoadFailure(cmd, wrapped)
		return nil, wrapped
	}
	return cfg, nil
}

// presentConfigLoadFailure prints the config error and an optional section
// hint. When the command silences cobra errors (ci/compare), this is the
// only user-visible output. For root inspect, SilenceErrors is flipped so
// cobra does not double-print after we write the message ourselves.
func presentConfigLoadFailure(cmd *cobra.Command, err error) {
	w := cmd.ErrOrStderr()
	fmt.Fprintf(w, "Error: %v\n", err)
	printConfigSectionHint(w, err)
	cmd.SilenceErrors = true
}

// printConfigSectionHint prints section-specific guidance when the error
// chain contains a tagged *config.LoadError; otherwise the general config
// hint. A hint is always printed so the user has a next step.
func printConfigSectionHint(w io.Writer, err error) {
	section := ""
	for err != nil {
		var loadErr *config.LoadError
		if errors.As(err, &loadErr) {
			section = loadErr.Section
			break
		}
		err = errors.Unwrap(err)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, config.SectionHelp(section))
}
