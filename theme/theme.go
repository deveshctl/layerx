// Package theme defines layerx's curated TUI color themes. It is a leaf
// package — depends only on stdlib image/color and lipgloss/v2 for the
// color.Color interface — so config/ can validate theme names without
// pulling tui/ into config-loading.
package theme

import "image/color"

// Name is the canonical, validated form of a theme identifier as it
// appears on the command line, in $LAYERX_THEME, or under "theme:" in
// .layerx.yaml. Typed alias rather than bare string so cmd/ can't
// accidentally cross-wire user-supplied strings into Theme fields.
type Name string

// Theme bundles a palette with display metadata. Description is shown by
// `layerx themes`; Name is the lookup key Get accepts.
type Theme struct {
	Name        Name
	Description string
	Palette     Palette
}

// Palette holds every color token consumed by the TUI. Field set is
// frozen to the 23 tokens tui/styles.go uses today; adding a token here
// requires updating BuildStyles in tui/styles.go in the same change
// (TestPaletteCompleteness + TestBuildStyles_AllThemes guard this).
type Palette struct {
	// Base is the background painted under every panel body, overlay,
	// and loading box. Without it, panel bodies inherit whatever
	// background the user's terminal is configured with — so a dark
	// theme on a light terminal (or vice versa) leaves panel text
	// rendered against a clashing background and the foreground colors
	// blend in. Set to lipgloss.NoColor{} on themes that intentionally
	// defer to the terminal palette (minimal).
	Base color.Color

	// Borders & focus
	Accent          color.Color
	FocusedBorder   color.Color
	UnfocusedBorder color.Color

	// Diff coloring
	Added     color.Color
	Modified  color.Color
	Removed   color.Color
	Unchanged color.Color

	// Selection
	SelectedFg color.Color
	SelectedBg color.Color

	// Status bar
	StatusBg  color.Color
	StatusKey color.Color
	StatusDim color.Color
	HeaderDim color.Color
	HeaderSep color.Color

	// Misc text
	Command   color.Color
	FileName  color.Color
	Separator color.Color

	// Tree / scroll dims
	MetaDim   color.Color
	TreeDim   color.Color
	ScrollDim color.Color

	// Search highlights
	SearchHighlightBg color.Color
	SearchCurrentBg   color.Color
	SearchCurrentFg   color.Color
}

// Get resolves name to a Theme. Returns *ErrUnknownTheme if name does
// not match any registered theme (case-sensitive). The empty string is
// rejected — callers handle "unset" before calling Get.
func Get(name string) (Theme, error) {
	for _, t := range registry {
		if string(t.Name) == name {
			return t, nil
		}
	}
	return Theme{}, &ErrUnknownTheme{Name: name}
}

// All returns every registered theme in stable display order
// (default, latte, frappe, macchiato, nord, minimal). The slice is a
// copy; callers may mutate freely.
func All() []Theme {
	out := make([]Theme, len(registry))
	copy(out, registry)
	return out
}

// Default returns the built-in default theme (Catppuccin Mocha). It is
// guaranteed to be in registry; the package init wiring makes Default
// non-failing so cmd/ can use it as a fallback when discovery hits a
// bad LAYERX_THEME value.
func Default() Theme {
	return registry[0]
}

// Names returns every registered theme's Name in stable display order.
// Used for shell completion and `layerx themes --json`.
func Names() []Name {
	out := make([]Name, len(registry))
	for i, t := range registry {
		out[i] = t.Name
	}
	return out
}
