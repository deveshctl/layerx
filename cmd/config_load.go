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
//
// If --config was passed it is used directly. Otherwise config.Load() walks
// up from CWD looking for .layerx.yaml, then falls back to the OS user-config
// directory.
func loadConfig(cmd *cobra.Command) (*config.Config, error) {
	var (
		cfg *config.Config
		err error
	)
	if flagConfig != "" {
		cfg, err = config.LoadFrom(flagConfig)
	} else {
		cfg, err = config.Load()
	}
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
	// errors.AsType walks the full error chain itself — no manual unwrap
	// loop needed. A nil section falls through to SectionHelp's general
	// fallback. Matches the AsType pattern used elsewhere in cmd/ for
	// sentinel-error checks (cmd/ci.go, cmd/compare.go).
	section := ""
	if loadErr, ok := errors.AsType[*config.LoadError](err); ok {
		section = loadErr.Section
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, config.SectionHelp(section))
}
