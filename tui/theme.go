package tui

import (
	"fmt"
	"image/color"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
)

// Theme is the complete set of colour roles the TUI renders with. Each field
// is one semantic role, not a raw palette colour: fields that share a hex in
// one theme (e.g. Accent and BorderFocus in Mocha) are kept distinct so a
// different theme can diverge them. ChromaStyle names the chroma syntax style
// that visually matches this theme so the file viewer and the chrome agree.
type Theme struct {
	Name string

	// RootBg is the outermost canvas the whole frame is painted on. PanelBg is
	// the raised panel-body fill, one perceptual step lighter than RootBg so
	// each pane reads as a card floating above the canvas. SelectBg (below)
	// steps up again above PanelBg so a selected row stays distinct from the
	// panel it sits on. Before backgrounds were themed these did not exist and
	// the terminal's own default background bled through everywhere.
	RootBg  color.Color
	PanelBg color.Color

	BorderFocus color.Color
	BorderBlur  color.Color

	SelectFg color.Color
	SelectBg color.Color

	DiffAdd    color.Color
	DiffModify color.Color
	DiffRemove color.Color

	TextPrimary color.Color
	TextNeutral color.Color
	TextDim1    color.Color
	TextDim2    color.Color
	TreeGlyph   color.Color

	Accent    color.Color
	Separator color.Color
	StatusBg  color.Color

	SearchMatchBg   color.Color
	SearchMatchFg   color.Color
	SearchCurrentBg color.Color
	SearchCurrentFg color.Color

	ChromaStyle string
}

func sprintfHex(r, g, b uint8) string { return fmt.Sprintf("#%02x%02x%02x", r, g, b) }

// defaultTheme is Catppuccin Mocha, reproducing the exact palette shipped
// before theming existed. Changing any value here changes the default look.
func defaultTheme() Theme {
	return Theme{
		Name:            "mocha",
		RootBg:          lipgloss.Color("#1E1E2E"),
		PanelBg:         lipgloss.Color("#313244"),
		BorderFocus:     lipgloss.Color("#89B4FA"),
		BorderBlur:      lipgloss.Color("#45475A"),
		SelectFg:        lipgloss.Color("#CDD6F4"),
		SelectBg:        lipgloss.Color("#45475A"),
		DiffAdd:         lipgloss.Color("#A6E3A1"),
		DiffModify:      lipgloss.Color("#F9E2AF"),
		DiffRemove:      lipgloss.Color("#F38BA8"),
		TextPrimary:     lipgloss.Color("#BAC2DE"),
		TextNeutral:     lipgloss.Color("#A6ADC8"),
		TextDim1:        lipgloss.Color("#9399B2"),
		TextDim2:        lipgloss.Color("#6C7086"),
		TreeGlyph:       lipgloss.Color("#45475A"),
		Accent:          lipgloss.Color("#89B4FA"),
		Separator:       lipgloss.Color("#313244"),
		StatusBg:        lipgloss.Color("#181825"),
		SearchMatchBg:   lipgloss.Color("#5A4A3A"),
		SearchMatchFg:   lipgloss.Color("#F5E0DC"),
		SearchCurrentBg: lipgloss.Color("#F9E2AF"),
		SearchCurrentFg: lipgloss.Color("#1E1E2E"),
		ChromaStyle:     "catppuccin-mocha",
	}
}

// Styles holds the theme every renderer reads its colours from. Built once by
// newStyles(theme) and stored on the model — no package-level mutable state.
type Styles struct {
	Theme Theme
}

func newStyles(t Theme) Styles { return Styles{Theme: t} }

// panelText builds a style whose foreground is fg and whose background is the
// theme's raised-panel colour. Every cell of panel-body content routes through
// this so no terminal-default background bleeds through — matching the
// header/status-bar pattern where each segment carries its own background.
func panelText(t Theme, fg color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(fg).Background(t.PanelBg)
}

// rootText is the RootBg counterpart of panelText, for content rendered
// directly on the root canvas rather than inside a panel (the command bar and
// the separator row live in that band between the panels and the status bar).
func rootText(t Theme, fg color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(fg).Background(t.RootBg)
}

// themeRegistry maps a theme name to its complete palette. mocha stays
// hand-pinned to defaultTheme() so the exact legacy hex is guaranteed; the
// other Catppuccin flavors are built from catppuccin/go (see
// theme_catppuccin.go) and dracula/gruvbox/solarized-dark are derived from
// chroma syntax styles (see theme_chroma.go).
func themeRegistry() map[string]Theme {
	return map[string]Theme{
		"mocha":          defaultTheme(),
		"latte":          fromCatppuccin("latte", catppuccinLatte),
		"frappe":         fromCatppuccin("frappe", catppuccinFrappe),
		"macchiato":      fromCatppuccin("macchiato", catppuccinMacchiato),
		"dracula":        fromChroma("dracula", "dracula"),
		"gruvbox":        fromChroma("gruvbox", "gruvbox"),
		"solarized-dark": fromChroma("solarized-dark", "solarized-dark"),
	}
}

// ResolveTheme returns the named theme. "" yields the default. An unknown name
// is an error listing valid names — we never silently fall back, because a
// silent fallback is exactly how a user's typo produces the wrong look.
func ResolveTheme(name string) (Theme, error) {
	if name == "" {
		return defaultTheme(), nil
	}
	reg := themeRegistry()
	if th, ok := reg[name]; ok {
		return th, nil
	}
	return Theme{}, fmt.Errorf("unknown theme %q; valid themes: %s", name, strings.Join(ThemeNames(), ", "))
}

// ThemeNames returns the registered theme names, sorted, for help text and
// error messages.
func ThemeNames() []string {
	reg := themeRegistry()
	names := make([]string, 0, len(reg))
	for k := range reg {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
