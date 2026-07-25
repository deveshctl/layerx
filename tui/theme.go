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

// Kanagawa is a warm dark theme inspired by the colours of Katsushika
// Hokusai's "The Great Wave" — deep indigo base with gold, sakura, and
// jade accents.
func Kanagawa() Theme {
	return Theme{
		Accent:          lipgloss.Color("#7E9CD8"), // wave blue
		FocusedBorder:   lipgloss.Color("#7E9CD8"),
		UnfocusedBorder: lipgloss.Color("#2A2A37"),

		Selected:   lipgloss.Color("#DCD7BA"), // fuji white
		SelectedBg: lipgloss.Color("#2D4F67"), // wave teal selection

		Added:     lipgloss.Color("#76946A"), // spring green
		Modified:  lipgloss.Color("#DCA561"), // carp yellow
		Removed:   lipgloss.Color("#C34043"), // samurai red
		Unchanged: lipgloss.Color("#727169"), // fuji grey

		Separator: lipgloss.Color("#1F1F28"),
		Command:   lipgloss.Color("#938AA9"), // spring violet
		StatusKey: lipgloss.Color("#7E9CD8"),
		StatusDim: lipgloss.Color("#54546D"), // wave grey
		StatusBg:  lipgloss.Color("#12121A"), // darker than MainBg
		HeaderDim: lipgloss.Color("#717C7C"), // autumn grey
		HeaderSep: lipgloss.Color("#1F1F28"),
		FileName:  lipgloss.Color("#DCD7BA"),

		MetaDim:   lipgloss.Color("#54546D"),
		TreeDim:   lipgloss.Color("#2A2A37"),
		ScrollDim: lipgloss.Color("#54546D"),

		SearchHighlightBg: lipgloss.Color("#2D4F67"),
		SearchCurrentBg:   lipgloss.Color("#DCA561"), // carp yellow
		SearchCurrentFg:   lipgloss.Color("#1F1F28"),

		MainBg: lipgloss.Color("#1F1F28"), // sumi ink

		ChromaStyle: "monokai",
	}
}

// GruvboxDark is a retro earthy theme with warm amber, orange, and green
// accents on a dark brown base.
func GruvboxDark() Theme {
	return Theme{
		Accent:          lipgloss.Color("#83A598"),
		FocusedBorder:   lipgloss.Color("#83A598"),
		UnfocusedBorder: lipgloss.Color("#3C3836"),

		Selected:   lipgloss.Color("#EBDBB2"),
		SelectedBg: lipgloss.Color("#504945"),

		Added:     lipgloss.Color("#B8BB26"),
		Modified:  lipgloss.Color("#FABD2F"),
		Removed:   lipgloss.Color("#FB4934"),
		Unchanged: lipgloss.Color("#A89984"),

		Separator: lipgloss.Color("#282828"),
		Command:   lipgloss.Color("#D5C4A1"),
		StatusKey: lipgloss.Color("#83A598"),
		StatusDim: lipgloss.Color("#665C54"),
		StatusBg:  lipgloss.Color("#1C1C1C"),
		HeaderDim: lipgloss.Color("#928374"),
		HeaderSep: lipgloss.Color("#282828"),
		FileName:  lipgloss.Color("#EBDBB2"),

		MetaDim:   lipgloss.Color("#665C54"),
		TreeDim:   lipgloss.Color("#3C3836"),
		ScrollDim: lipgloss.Color("#665C54"),

		SearchHighlightBg: lipgloss.Color("#504945"),
		SearchCurrentBg:   lipgloss.Color("#FABD2F"),
		SearchCurrentFg:   lipgloss.Color("#282828"),

		MainBg: lipgloss.Color("#282828"),

		ChromaStyle: "gruvbox",
	}
}

// RosePine is a soft, muted dark theme with dusty rose, mauve, and pine
// green on a deep midnight base.
func RosePine() Theme {
	return Theme{
		Accent:          lipgloss.Color("#C4A7E7"), // iris
		FocusedBorder:   lipgloss.Color("#C4A7E7"),
		UnfocusedBorder: lipgloss.Color("#26233A"),

		Selected:   lipgloss.Color("#E0DEF4"), // text
		SelectedBg: lipgloss.Color("#403D52"), // highlight med

		Added:     lipgloss.Color("#31748F"), // pine
		Modified:  lipgloss.Color("#F6C177"), // gold
		Removed:   lipgloss.Color("#EB6F92"), // love
		Unchanged: lipgloss.Color("#6E6A86"), // subtle

		Separator: lipgloss.Color("#1F1D2E"),
		Command:   lipgloss.Color("#908CAA"), // muted
		StatusKey: lipgloss.Color("#C4A7E7"),
		StatusDim: lipgloss.Color("#6E6A86"),
		StatusBg:  lipgloss.Color("#16131E"),
		HeaderDim: lipgloss.Color("#817C9C"),
		HeaderSep: lipgloss.Color("#1F1D2E"),
		FileName:  lipgloss.Color("#E0DEF4"),

		MetaDim:   lipgloss.Color("#6E6A86"),
		TreeDim:   lipgloss.Color("#26233A"),
		ScrollDim: lipgloss.Color("#6E6A86"),

		SearchHighlightBg: lipgloss.Color("#403D52"),
		SearchCurrentBg:   lipgloss.Color("#F6C177"),
		SearchCurrentFg:   lipgloss.Color("#1F1D2E"),

		MainBg: lipgloss.Color("#191724"), // base

		ChromaStyle: "monokai",
	}
}

// Dracula is a high-contrast purple/pink theme with vibrant cyan and green
// accents on a near-black background.
func Dracula() Theme {
	return Theme{
		Accent:          lipgloss.Color("#BD93F9"), // purple
		FocusedBorder:   lipgloss.Color("#BD93F9"),
		UnfocusedBorder: lipgloss.Color("#44475A"),

		Selected:   lipgloss.Color("#F8F8F2"), // foreground
		SelectedBg: lipgloss.Color("#44475A"), // selection

		Added:     lipgloss.Color("#50FA7B"), // green
		Modified:  lipgloss.Color("#FFB86C"), // orange
		Removed:   lipgloss.Color("#FF5555"), // red
		Unchanged: lipgloss.Color("#6272A4"), // comment

		Separator: lipgloss.Color("#21222C"),
		Command:   lipgloss.Color("#8BE9FD"), // cyan
		StatusKey: lipgloss.Color("#BD93F9"),
		StatusDim: lipgloss.Color("#6272A4"),
		StatusBg:  lipgloss.Color("#191A21"),
		HeaderDim: lipgloss.Color("#6272A4"),
		HeaderSep: lipgloss.Color("#21222C"),
		FileName:  lipgloss.Color("#F8F8F2"),

		MetaDim:   lipgloss.Color("#6272A4"),
		TreeDim:   lipgloss.Color("#44475A"),
		ScrollDim: lipgloss.Color("#6272A4"),

		SearchHighlightBg: lipgloss.Color("#44475A"),
		SearchCurrentBg:   lipgloss.Color("#FFB86C"),
		SearchCurrentFg:   lipgloss.Color("#282A36"),

		MainBg: lipgloss.Color("#282A36"),

		ChromaStyle: "dracula",
	}
}

// Oxocarbon is an IBM Carbon-inspired theme — cool charcoal base with
// cyan, teal, and lilac accents.
func Oxocarbon() Theme {
	return Theme{
		Accent:          lipgloss.Color("#78A9FF"), // blue
		FocusedBorder:   lipgloss.Color("#78A9FF"),
		UnfocusedBorder: lipgloss.Color("#393939"),

		Selected:   lipgloss.Color("#F2F4F8"), // text
		SelectedBg: lipgloss.Color("#353535"), // selection

		Added:     lipgloss.Color("#42BE65"), // green
		Modified:  lipgloss.Color("#FFD700"), // yellow
		Removed:   lipgloss.Color("#FF7EB6"), // pink/red
		Unchanged: lipgloss.Color("#8D8D8D"), // subtle

		Separator: lipgloss.Color("#1E1E1E"),
		Command:   lipgloss.Color("#3DDBD9"), // teal/cyan
		StatusKey: lipgloss.Color("#78A9FF"),
		StatusDim: lipgloss.Color("#525252"),
		StatusBg:  lipgloss.Color("#161616"),
		HeaderDim: lipgloss.Color("#6F6F6F"),
		HeaderSep: lipgloss.Color("#1E1E1E"),
		FileName:  lipgloss.Color("#F2F4F8"),

		MetaDim:   lipgloss.Color("#525252"),
		TreeDim:   lipgloss.Color("#393939"),
		ScrollDim: lipgloss.Color("#525252"),

		SearchHighlightBg: lipgloss.Color("#353535"),
		SearchCurrentBg:   lipgloss.Color("#FFD700"),
		SearchCurrentFg:   lipgloss.Color("#1E1E1E"),

		MainBg: lipgloss.Color("#1E1E1E"),

		ChromaStyle: "monokai",
	}
}

// Everforest is a calm, muted green forest theme — warm dark base with
// soft greens, yellows, and reds that are easy on the eyes.
// Nord is an arctic, blue-grey theme — cool polar night base with frost
// blue accents and aurora green, yellow, and red for diff states.
// OneDark is the classic Atom One Dark theme — balanced warm/cool dark
// base with blue, cyan, green, and orange accents.
// Cyberdream is a high-contrast neon synthwave theme — near-black base
// with electric cyan, magenta, and lime accents.
func Cyberdream() Theme {
	return Theme{
		Accent:          lipgloss.Color("#00BFFF"), // electric cyan
		FocusedBorder:   lipgloss.Color("#00BFFF"),
		UnfocusedBorder: lipgloss.Color("#2A2A3D"),

		Selected:   lipgloss.Color("#FFFFFF"),
		SelectedBg: lipgloss.Color("#2A2A3D"),

		Added:     lipgloss.Color("#00FF9C"), // neon green
		Modified:  lipgloss.Color("#FFD700"), // electric gold
		Removed:   lipgloss.Color("#FF355E"), // neon red
		Unchanged: lipgloss.Color("#5A5A7A"),

		Separator: lipgloss.Color("#16161F"),
		Command:   lipgloss.Color("#BD00FF"), // neon purple
		StatusKey: lipgloss.Color("#00BFFF"),
		StatusDim: lipgloss.Color("#3D3D5C"),
		StatusBg:  lipgloss.Color("#0D0D14"),
		HeaderDim: lipgloss.Color("#4A4A6A"),
		HeaderSep: lipgloss.Color("#16161F"),
		FileName:  lipgloss.Color("#E0E0FF"),

		MetaDim:   lipgloss.Color("#3D3D5C"),
		TreeDim:   lipgloss.Color("#2A2A3D"),
		ScrollDim: lipgloss.Color("#3D3D5C"),

		SearchHighlightBg: lipgloss.Color("#2A2A3D"),
		SearchCurrentBg:   lipgloss.Color("#FFD700"),
		SearchCurrentFg:   lipgloss.Color("#16161F"),

		MainBg: lipgloss.Color("#16161F"),

		ChromaStyle: "monokai",
	}
}

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
