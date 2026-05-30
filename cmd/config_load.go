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

// printConfigSectionHint walks the error chain for *config.LoadError and
// prints SectionHelp when available.
func printConfigSectionHint(w io.Writer, err error) {
	for err != nil {
		var loadErr *config.LoadError
		if errors.As(err, &loadErr) {
			if snippet := config.SectionHelp(loadErr.Section); snippet != "" {
				fmt.Fprintln(w)
				fmt.Fprintln(w, snippet)
			}
			return
		}
		err = errors.Unwrap(err)
	}
}
