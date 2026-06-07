package theme

import "charm.land/lipgloss/v2"

// Nord — MIT, https://www.nordtheme.com
//
// Token mapping:
//   Base                               -> nord1 (polar second-darkest, panel bg)
//   Accent / FocusedBorder / StatusKey -> nord8 (frost cyan)
//   UnfocusedBorder / TreeDim          -> nord3 (polar gray)
//   Added                              -> nord14 (aurora green)
//   Modified                           -> nord13 (aurora yellow)
//   Removed                            -> nord11 (aurora red)
//   Unchanged / FileName               -> nord4 (snow storm light)
//   SelectedFg                         -> nord6 (snow storm lightest)
//   SelectedBg / Separator / HeaderSep -> nord1 (polar dark)
//   StatusBg                           -> nord0 (polar darkest, status strip)
//   StatusDim / MetaDim / ScrollDim    -> nord3
//   HeaderDim                          -> nord4
//   Command                            -> nord4
//   SearchHighlightBg                  -> nord2
//   SearchCurrentBg / SearchCurrentFg  -> nord13 / nord0
var nord = Palette{
	Base: lipgloss.Color("#3B4252"), // nord1 — panel bg, slightly lighter than status

	Accent:          lipgloss.Color("#88C0D0"),
	FocusedBorder:   lipgloss.Color("#88C0D0"),
	UnfocusedBorder: lipgloss.Color("#4C566A"),

	Added:     lipgloss.Color("#A3BE8C"),
	Modified:  lipgloss.Color("#EBCB8B"),
	Removed:   lipgloss.Color("#BF616A"),
	Unchanged: lipgloss.Color("#D8DEE9"),

	SelectedFg: lipgloss.Color("#ECEFF4"),
	SelectedBg: lipgloss.Color("#3B4252"),

	StatusBg:  lipgloss.Color("#2E3440"),
	StatusKey: lipgloss.Color("#88C0D0"),
	StatusDim: lipgloss.Color("#4C566A"),
	HeaderDim: lipgloss.Color("#D8DEE9"),
	HeaderSep: lipgloss.Color("#3B4252"),

	Command:   lipgloss.Color("#D8DEE9"),
	FileName:  lipgloss.Color("#D8DEE9"),
	Separator: lipgloss.Color("#3B4252"),

	MetaDim:   lipgloss.Color("#4C566A"),
	TreeDim:   lipgloss.Color("#4C566A"),
	ScrollDim: lipgloss.Color("#4C566A"),

	SearchHighlightBg: lipgloss.Color("#434C5E"),
	SearchCurrentBg:   lipgloss.Color("#EBCB8B"),
	SearchCurrentFg:   lipgloss.Color("#2E3440"),
}
