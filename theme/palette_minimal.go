package theme

import "charm.land/lipgloss/v2"

// minimal uses only the 8 base ANSI colors (and their bright variants
// where contrast requires it), expressed as lipgloss.ANSIColor values.
// The ANSI palette is whatever the user's terminal has configured —
// minimal looks different on every terminal, and that is the contract.
//
// Layerx is regularly used over SSH, in tmux, and in CI log viewers
// where a fixed-hex palette can clash with the user's color scheme;
// minimal sidesteps that by deferring to the terminal entirely.
//
// ANSI indices:
//   0 black   1 red       2 green   3 yellow
//   4 blue    5 magenta   6 cyan    7 white
//   8-15 are the bright variants of the above.
//
// The Palette field type is color.Color; lipgloss.ANSIColor satisfies
// that interface, so no special-casing is needed in tui/.
//
// Base is intentionally lipgloss.NoColor{}: minimal defers panel
// backgrounds to the terminal so the user's shell theme remains
// unaltered. Every other theme paints its own panel base.
var minimal = Palette{
	Base: lipgloss.NoColor{},

	Accent:          lipgloss.ANSIColor(4), // blue
	FocusedBorder:   lipgloss.ANSIColor(4),
	UnfocusedBorder: lipgloss.ANSIColor(8), // bright black (gray)

	Added:     lipgloss.ANSIColor(2), // green
	Modified:  lipgloss.ANSIColor(3), // yellow
	Removed:   lipgloss.ANSIColor(1), // red
	Unchanged: lipgloss.ANSIColor(7), // white

	SelectedFg: lipgloss.ANSIColor(15), // bright white
	SelectedBg: lipgloss.ANSIColor(8),  // bright black

	StatusBg:  lipgloss.ANSIColor(0),  // black
	StatusKey: lipgloss.ANSIColor(12), // bright blue
	StatusDim: lipgloss.ANSIColor(8),
	HeaderDim: lipgloss.ANSIColor(7),
	HeaderSep: lipgloss.ANSIColor(8),

	Command:   lipgloss.ANSIColor(7),
	FileName:  lipgloss.ANSIColor(7),
	Separator: lipgloss.ANSIColor(8),

	MetaDim:   lipgloss.ANSIColor(8),
	TreeDim:   lipgloss.ANSIColor(8),
	ScrollDim: lipgloss.ANSIColor(8),

	SearchHighlightBg: lipgloss.ANSIColor(8),
	SearchCurrentBg:   lipgloss.ANSIColor(11), // bright yellow
	SearchCurrentFg:   lipgloss.ANSIColor(0),
}
