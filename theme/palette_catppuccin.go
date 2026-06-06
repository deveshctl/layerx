package theme

import "charm.land/lipgloss/v2"

// Catppuccin (mocha/latte/frappe/macchiato) — MIT
// https://github.com/catppuccin/catppuccin
//
// Hex values are reproduced directly rather than imported via
// catppuccin/go to avoid pulling a module dependency for ~80 strings.
// Token mapping (same across all four flavors):
//   Accent / FocusedBorder / StatusKey -> blue
//   UnfocusedBorder                    -> surface1
//   Added                              -> green
//   Modified                           -> yellow
//   Removed                            -> red
//   Unchanged / Command                -> subtext0 / subtext1
//   SelectedFg                         -> text
//   SelectedBg / Separator / HeaderSep -> surface0
//   StatusBg                           -> mantle (one step darker than base)
//   StatusDim / MetaDim / ScrollDim    -> overlay0
//   TreeDim                            -> surface1
//   HeaderDim                          -> overlay2
//   FileName                           -> subtext0
//   SearchHighlightBg                  -> surface2
//   SearchCurrentBg / SearchCurrentFg  -> yellow / base (inverted hit)

// mocha is the default theme. Catppuccin Mocha (dark).
var mocha = Palette{
	Accent:          lipgloss.Color("#89B4FA"), // blue
	FocusedBorder:   lipgloss.Color("#89B4FA"),
	UnfocusedBorder: lipgloss.Color("#45475A"), // surface1

	Added:     lipgloss.Color("#A6E3A1"), // green
	Modified:  lipgloss.Color("#F9E2AF"), // yellow
	Removed:   lipgloss.Color("#F38BA8"), // red
	Unchanged: lipgloss.Color("#A6ADC8"), // subtext0

	SelectedFg: lipgloss.Color("#CDD6F4"), // text
	SelectedBg: lipgloss.Color("#313244"), // surface0

	StatusBg:  lipgloss.Color("#181825"), // mantle
	StatusKey: lipgloss.Color("#89B4FA"),
	StatusDim: lipgloss.Color("#6C7086"), // overlay0
	HeaderDim: lipgloss.Color("#9399B2"), // overlay2
	HeaderSep: lipgloss.Color("#313244"),

	Command:   lipgloss.Color("#A6ADC8"),
	FileName:  lipgloss.Color("#BAC2DE"), // subtext1
	Separator: lipgloss.Color("#313244"),

	MetaDim:   lipgloss.Color("#6C7086"),
	TreeDim:   lipgloss.Color("#45475A"),
	ScrollDim: lipgloss.Color("#6C7086"),

	SearchHighlightBg: lipgloss.Color("#585B70"), // surface2
	SearchCurrentBg:   lipgloss.Color("#F9E2AF"),
	SearchCurrentFg:   lipgloss.Color("#1E1E2E"), // base
}

// latte is the Catppuccin light flavor. Same token mapping as mocha
// against the Latte palette. Dark text on light surfaces.
var latte = Palette{
	Accent:          lipgloss.Color("#1E66F5"),
	FocusedBorder:   lipgloss.Color("#1E66F5"),
	UnfocusedBorder: lipgloss.Color("#BCC0CC"),

	Added:     lipgloss.Color("#40A02B"),
	Modified:  lipgloss.Color("#DF8E1D"),
	Removed:   lipgloss.Color("#D20F39"),
	Unchanged: lipgloss.Color("#6C6F85"),

	SelectedFg: lipgloss.Color("#4C4F69"),
	SelectedBg: lipgloss.Color("#CCD0DA"),

	StatusBg:  lipgloss.Color("#E6E9EF"),
	StatusKey: lipgloss.Color("#1E66F5"),
	StatusDim: lipgloss.Color("#9CA0B0"),
	HeaderDim: lipgloss.Color("#7C7F93"),
	HeaderSep: lipgloss.Color("#CCD0DA"),

	Command:   lipgloss.Color("#6C6F85"),
	FileName:  lipgloss.Color("#5C5F77"),
	Separator: lipgloss.Color("#CCD0DA"),

	MetaDim:   lipgloss.Color("#9CA0B0"),
	TreeDim:   lipgloss.Color("#BCC0CC"),
	ScrollDim: lipgloss.Color("#9CA0B0"),

	SearchHighlightBg: lipgloss.Color("#ACB0BE"),
	SearchCurrentBg:   lipgloss.Color("#DF8E1D"),
	SearchCurrentFg:   lipgloss.Color("#EFF1F5"),
}

// frappe is the Catppuccin medium-dark flavor.
var frappe = Palette{
	Accent:          lipgloss.Color("#8CAAEE"),
	FocusedBorder:   lipgloss.Color("#8CAAEE"),
	UnfocusedBorder: lipgloss.Color("#414559"),

	Added:     lipgloss.Color("#A6D189"),
	Modified:  lipgloss.Color("#E5C890"),
	Removed:   lipgloss.Color("#E78284"),
	Unchanged: lipgloss.Color("#A5ADCE"),

	SelectedFg: lipgloss.Color("#C6D0F5"),
	SelectedBg: lipgloss.Color("#292C3C"),

	StatusBg:  lipgloss.Color("#232634"),
	StatusKey: lipgloss.Color("#8CAAEE"),
	StatusDim: lipgloss.Color("#737994"),
	HeaderDim: lipgloss.Color("#949CBB"),
	HeaderSep: lipgloss.Color("#292C3C"),

	Command:   lipgloss.Color("#A5ADCE"),
	FileName:  lipgloss.Color("#B5BFE2"),
	Separator: lipgloss.Color("#292C3C"),

	MetaDim:   lipgloss.Color("#737994"),
	TreeDim:   lipgloss.Color("#414559"),
	ScrollDim: lipgloss.Color("#737994"),

	SearchHighlightBg: lipgloss.Color("#51576D"),
	SearchCurrentBg:   lipgloss.Color("#E5C890"),
	SearchCurrentFg:   lipgloss.Color("#303446"),
}

// macchiato is the Catppuccin dark flavor (one step lighter than mocha).
var macchiato = Palette{
	Accent:          lipgloss.Color("#8AADF4"),
	FocusedBorder:   lipgloss.Color("#8AADF4"),
	UnfocusedBorder: lipgloss.Color("#363A4F"),

	Added:     lipgloss.Color("#A6DA95"),
	Modified:  lipgloss.Color("#EED49F"),
	Removed:   lipgloss.Color("#ED8796"),
	Unchanged: lipgloss.Color("#A5ADCB"),

	SelectedFg: lipgloss.Color("#CAD3F5"),
	SelectedBg: lipgloss.Color("#1E2030"),

	StatusBg:  lipgloss.Color("#181926"),
	StatusKey: lipgloss.Color("#8AADF4"),
	StatusDim: lipgloss.Color("#6E738D"),
	HeaderDim: lipgloss.Color("#939AB7"),
	HeaderSep: lipgloss.Color("#1E2030"),

	Command:   lipgloss.Color("#A5ADCB"),
	FileName:  lipgloss.Color("#B8C0E0"),
	Separator: lipgloss.Color("#1E2030"),

	MetaDim:   lipgloss.Color("#6E738D"),
	TreeDim:   lipgloss.Color("#363A4F"),
	ScrollDim: lipgloss.Color("#6E738D"),

	SearchHighlightBg: lipgloss.Color("#5B6078"),
	SearchCurrentBg:   lipgloss.Color("#EED49F"),
	SearchCurrentFg:   lipgloss.Color("#24273A"),
}
