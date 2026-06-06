package config

import "fmt"

// Well-known .layerx.yaml sections. Used by LoadError and SectionHelp so the
// CLI can print targeted hints without dumping the full command usage block.
const (
	SectionRules       = "rules"
	SectionPathRules   = "path-rules"
	SectionVersion     = "version"
	SectionKeybindings = "keybindings"
	SectionTheme       = "theme"
	// SectionGeneral is used when the loader cannot map a failure to one
	// section (unknown top-level key, root syntax error, IO failure).
	SectionGeneral = "config"
)

// LoadError wraps a config parse or validation failure with the file path and
// the config section the user should fix. cmd/ uses SectionHelp(Section) to
// print a short excerpt after the one-line error.
type LoadError struct {
	Path    string
	Section string
	Err     error
}

func (e *LoadError) Error() string {
	if e.Section != "" {
		return fmt.Sprintf("%s (%s): %v", e.Path, e.Section, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Path, e.Err)
}

func (e *LoadError) Unwrap() error { return e.Err }

func newLoadError(path, section string, err error) error {
	if err == nil {
		return nil
	}
	return &LoadError{Path: path, Section: section, Err: err}
}
