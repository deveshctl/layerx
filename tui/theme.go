package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Theme holds all colors used by the TUI. Add a constructor function per
// named theme; NewModel picks which one to use.
type Theme struct {
	// Accent / brand
	Accent color.Color

	// Panel borders
	FocusedBorder   color.Color
	UnfocusedBorder color.Color

	// Selection
	Selected   color.Color
	SelectedBg color.Color

	// Diff
	Added     color.Color
	Modified  color.Color
	Removed   color.Color
	Unchanged color.Color

	// Chrome (status bar, header)
	Separator color.Color
	Command   color.Color
	StatusKey color.Color
	StatusDim color.Color
	StatusBg  color.Color
	HeaderDim color.Color
	HeaderSep color.Color
	FileName  color.Color

	// Dim / tree
	MetaDim   color.Color
	TreeDim   color.Color
	ScrollDim color.Color

	// Search
	SearchHighlightBg color.Color
	SearchCurrentBg   color.Color
	SearchCurrentFg   color.Color

	// Overall terminal background (set via tea.WithBackgroundColor).
	// Gives the whole UI a consistent base so header/footer bars feel
	// grounded rather than floating on pure black.
	MainBg color.Color

	// Chroma syntax highlight theme name (passed to chroma styles.Get)
	ChromaStyle string
}

// CatppuccinMocha is the original built-in theme.
func CatppuccinMocha() Theme {
	return Theme{
		Accent:          lipgloss.Color("#89B4FA"),
		FocusedBorder:   lipgloss.Color("#89B4FA"),
		UnfocusedBorder: lipgloss.Color("#45475A"),

		Selected:   lipgloss.Color("#CDD6F4"),
		SelectedBg: lipgloss.Color("#313244"),

		Added:     lipgloss.Color("#A6E3A1"),
		Modified:  lipgloss.Color("#F9E2AF"),
		Removed:   lipgloss.Color("#F38BA8"),
		Unchanged: lipgloss.Color("#A6ADC8"),

		Separator: lipgloss.Color("#313244"),
		Command:   lipgloss.Color("#A6ADC8"),
		StatusKey: lipgloss.Color("#89B4FA"),
		StatusDim: lipgloss.Color("#6C7086"),
		StatusBg:  lipgloss.Color("#181825"),
		HeaderDim: lipgloss.Color("#9399B2"),
		HeaderSep: lipgloss.Color("#313244"),
		FileName:  lipgloss.Color("#BAC2DE"),

		MetaDim:   lipgloss.Color("#6C7086"),
		TreeDim:   lipgloss.Color("#45475A"),
		ScrollDim: lipgloss.Color("#6C7086"),

		SearchHighlightBg: lipgloss.Color("#585B70"),
		SearchCurrentBg:   lipgloss.Color("#F9E2AF"),
		SearchCurrentFg:   lipgloss.Color("#1E1E2E"),

		MainBg: lipgloss.Color("#1E1E2E"),

		ChromaStyle: "monokai",
	}
}

// TokyoNight is a cool deep-blue dark theme.
func TokyoNight() Theme {
	return Theme{
		Accent:          lipgloss.Color("#7AA2F7"),
		FocusedBorder:   lipgloss.Color("#7AA2F7"),
		UnfocusedBorder: lipgloss.Color("#3B4261"),

		Selected:   lipgloss.Color("#C0CAF5"),
		SelectedBg: lipgloss.Color("#283457"),

		Added:     lipgloss.Color("#9ECE6A"),
		Modified:  lipgloss.Color("#E0AF68"),
		Removed:   lipgloss.Color("#F7768E"),
		Unchanged: lipgloss.Color("#A9B1D6"),

		// #1F2335 is just above MainBg — subtle dividers that don't disappear
		Separator: lipgloss.Color("#1F2335"),
		Command:   lipgloss.Color("#A9B1D6"),
		StatusKey: lipgloss.Color("#7AA2F7"),
		// Slightly brighter than before so key labels stay legible on StatusBg
		StatusDim: lipgloss.Color("#6272A4"),
		// Darker than MainBg — header/footer feel grounded, not floating
		StatusBg:  lipgloss.Color("#13131A"),
		HeaderDim: lipgloss.Color("#737AA2"),
		HeaderSep: lipgloss.Color("#1F2335"),
		FileName:  lipgloss.Color("#C0CAF5"),

		MetaDim:   lipgloss.Color("#565F89"),
		TreeDim:   lipgloss.Color("#3B4261"),
		ScrollDim: lipgloss.Color("#565F89"),

		SearchHighlightBg: lipgloss.Color("#3D4C7A"),
		SearchCurrentBg:   lipgloss.Color("#E0AF68"),
		SearchCurrentFg:   lipgloss.Color("#1A1B26"),

		// Base surface — slightly above pure black, ties the whole UI together
		MainBg: lipgloss.Color("#1A1B26"),

		ChromaStyle: "tokyonight-dark",
	}
}
