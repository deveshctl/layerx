package tui

import (
	"fmt"
	"image/color"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	colorful "github.com/lucasb-eyer/go-colorful"
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

// statusKeyFg / statusDescFg / statusSepFg / statusBrandFg are the status-bar
// text roles, derived from StatusBg rather than reused from the panel palette.
// The old code drew descriptions in TextDim2 and separators in Separator —
// both tuned against the panel background — so on the darker StatusBg the
// descriptions were barely legible and the separator pipes rendered near-black.
// Deriving them from StatusBg guarantees a readable contrast on every theme.
func (t Theme) statusKeyFg() color.Color  { return t.Accent }
func (t Theme) statusDescFg() color.Color { return blend(t.StatusBg, statusInk(t.StatusBg), 0.62) }
func (t Theme) statusSepFg() color.Color  { return blend(t.StatusBg, statusInk(t.StatusBg), 0.38) }
func (t Theme) statusBrandFg() color.Color {
	return blend(t.StatusBg, statusInk(t.StatusBg), 0.92)
}

// statusInk returns the ink direction (white on dark bars, black on light)
// used to derive readable status-bar text.
func statusInk(bg color.Color) color.Color {
	if isLightBg(bg) {
		return black()
	}
	return white()
}

// defaultTheme is Catppuccin Mocha, reproducing the exact palette shipped
// before theming existed. Changing any value here changes the default look.
func defaultTheme() Theme {
	return Theme{
		Name:            "mocha",
		RootBg:          lipgloss.Color("#1E1E2E"),
		PanelBg:         lipgloss.Color("#313244"),
		BorderFocus:     lipgloss.Color("#89B4FA"),
		BorderBlur:      lipgloss.Color("#A6ADC8"),
		SelectFg:        lipgloss.Color("#CDD6F4"),
		SelectBg:        lipgloss.Color("#45475A"),
		DiffAdd:         lipgloss.Color("#A6E3A1"),
		DiffModify:      lipgloss.Color("#F9E2AF"),
		DiffRemove:      lipgloss.Color("#F38BA8"),
		TextPrimary:     lipgloss.Color("#BAC2DE"),
		TextNeutral:     lipgloss.Color("#A6ADC8"),
		TextDim1:        lipgloss.Color("#9399B2"),
		TextDim2:        lipgloss.Color("#6C7086"),
		TreeGlyph:       lipgloss.Color("#6C7086"),
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
		"mocha":          refine(defaultTheme()),
		"latte":          refine(fromCatppuccin("latte", catppuccinLatte)),
		"frappe":         refine(fromCatppuccin("frappe", catppuccinFrappe)),
		"macchiato":      refine(fromCatppuccin("macchiato", catppuccinMacchiato)),
		"dracula":        refine(fromChroma("dracula", "dracula")),
		"gruvbox":        refine(fromChroma("gruvbox", "gruvbox")),
		"solarized-dark": refine(fromChroma("solarized-dark", "solarized-dark")),
	}
}

// refine derives the interaction colours every theme kept getting wrong when
// authored by hand or from a syntax style: a selection bar that actually reads
// as selected, and a status bar whose text is legible on its own background.
// Applied uniformly so all themes share the same visual grammar.
//
//   - SelectBg becomes an accent-tinted surface (blend of panel→accent), the
//     lazygit convention — a saturated selection reads instantly, where a grey
//     one perceptual step above the panel disappears.
//   - SelectFg is forced to whichever of the theme's brightest text or a
//     contrast colour is more legible on the new SelectBg.
//   - StatusBg is pulled to a clear offset from PanelBg (darker on dark themes,
//     lighter on light) so the bar is a distinct band, not a muddy continuation.
//
// isLight decides direction from the root canvas luminance so light themes
// (latte) get the mirror treatment instead of the dark-tuned one.
func refine(t Theme) Theme {
	light := isLightBg(t.RootBg)

	// Accent-tinted selection. 0.42 keeps the panel's character while pushing
	// far enough toward the accent to be unmistakable; the guard below bumps it
	// if a low-chroma accent still lands too close to the panel.
	sel := blend(t.PanelBg, t.Accent, 0.42)
	if labDistance(sel, t.PanelBg) < 0.14 {
		// Accent too close to the panel (grey/low-sat theme): step the surface
		// itself instead so selection never collapses into the fill.
		if light {
			sel = blend(t.PanelBg, black(), 0.16)
		} else {
			sel = blend(t.PanelBg, white(), 0.20)
		}
	}
	t.SelectBg = sel

	// High-contrast selection text: prefer the theme's bright text, fall back to
	// pure white/black when it isn't legible enough on the tinted selection.
	if labDistance(t.SelectFg, sel) < 0.45 {
		if isLightBg(sel) {
			t.SelectFg = blend(sel, black(), 0.85)
		} else {
			t.SelectFg = blend(sel, white(), 0.90)
		}
	}

	// Status bar as a distinct band from the panels.
	if labDistance(t.StatusBg, t.PanelBg) < 0.06 {
		if light {
			t.StatusBg = blend(t.PanelBg, black(), 0.12)
		} else {
			t.StatusBg = blend(t.PanelBg, black(), 0.40)
		}
	}

	return t
}

func white() color.Color { return lipgloss.Color("#ffffff") }
func black() color.Color { return lipgloss.Color("#000000") }

// isLightBg reports whether c is a light colour (perceptual L* > 0.5), used to
// mirror the dark-tuned derivations for light themes.
func isLightBg(c color.Color) bool {
	cf, _ := colorful.MakeColor(c)
	l, _, _ := cf.Lab()
	return l > 0.5
}


// ResolveTheme returns the named theme. "" yields the default. An unknown name
// is an error listing valid names — we never silently fall back, because a
// silent fallback is exactly how a user's typo produces the wrong look.
func ResolveTheme(name string) (Theme, error) {
	if name == "" {
		return refine(defaultTheme()), nil
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
