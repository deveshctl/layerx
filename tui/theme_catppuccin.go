package tui

import (
	catppuccin "github.com/catppuccin/go"
)

// Catppuccin flavor handles used by themeRegistry. Named here so the registry
// reads as a flat list. catppuccin.Color implements color.Color natively, so
// each accessor is used as a role colour without conversion.
var (
	catppuccinLatte     = catppuccin.Latte
	catppuccinFrappe    = catppuccin.Frappe
	catppuccinMacchiato = catppuccin.Macchiato
	// catppuccinMochaFlavor is used only by the drift test — mocha ships as
	// the hand-pinned defaultTheme(), not fromCatppuccin, so this handle
	// exists to compare the two.
	catppuccinMochaFlavor = catppuccin.Mocha
)

// fromCatppuccin builds a Theme from a Catppuccin flavor. The role→swatch
// mapping mirrors defaultTheme (Mocha) so every flavor is internally
// consistent: Blue drives accents/focus, the Surface ladder drives
// selection/separators, Subtext/Overlay drive the text-dim ladder, and the
// diff triad maps to Green/Yellow/Red. ChromaStyle points at the matching
// "catppuccin-<flavor>" chroma style so the file viewer agrees with the chrome.
func fromCatppuccin(name string, f catppuccin.Flavor) Theme {
	return Theme{
		Name:            name,
		BorderFocus:     f.Blue(),
		BorderBlur:      f.Surface1(),
		SelectFg:        f.Text(),
		SelectBg:        f.Surface0(),
		DiffAdd:         f.Green(),
		DiffModify:      f.Yellow(),
		DiffRemove:      f.Red(),
		TextPrimary:     f.Subtext1(),
		TextNeutral:     f.Subtext0(),
		TextDim1:        f.Overlay2(),
		TextDim2:        f.Overlay0(),
		TreeGlyph:       f.Surface1(),
		Accent:          f.Blue(),
		Separator:       f.Surface0(),
		StatusBg:        f.Mantle(),
		SearchMatchBg:   f.Surface2(),
		SearchCurrentBg: f.Yellow(),
		SearchCurrentFg: f.Base(),
		ChromaStyle:     "catppuccin-" + name,
	}
}
