package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/deveshctl/layerx/theme"
)

// Styles holds every pre-built lipgloss.Style the TUI renders with.
// Constructed once per model from a theme.Palette via BuildStyles;
// never mutated afterwards. Render code reads m.styles.X — never
// constructs styles inline. Centralizing here gives the codebase a
// single registry of every styled UI element, and avoids per-render
// allocation in hot paths.
type Styles struct {
	// Borders
	FocusedBorder   lipgloss.Style
	UnfocusedBorder lipgloss.Style

	// Diff coloring
	Added     lipgloss.Style
	Modified  lipgloss.Style
	Removed   lipgloss.Style
	Unchanged lipgloss.Style

	// Selection
	Selected     lipgloss.Style
	SelectedBold lipgloss.Style

	// Status bar — many usages need a styled foreground on the status bg.
	StatusBg          lipgloss.Style // bg only, for fillers
	StatusKey         lipgloss.Style // key glyph, bold
	StatusDim         lipgloss.Style // dim caption text
	StatusBrand       lipgloss.Style // " layerx", bold accent
	StatusBrandGlyph  lipgloss.Style // ◆ glyph
	StatusSep         lipgloss.Style // " │ "
	StatusImageName   lipgloss.Style
	StatusBadgeAccent lipgloss.Style // [eff], [↓size], [↑size]
	StatusBadgeWarn   lipgloss.Style // [diff]
	StatusCopied      lipgloss.Style // "Copied!"
	StatusError       lipgloss.Style
	StatusMatch       lipgloss.Style
	StatusRightHi     lipgloss.Style // bold "Layer N" right-side highlight
	StatusRightDim    lipgloss.Style // dim layer-counter trailer

	// Header
	HeaderDim         lipgloss.Style
	HeaderDimOnStatus lipgloss.Style // header-dim foreground on the status bar's bg
	HeaderSep         lipgloss.Style

	// File tree / file view
	FileName        lipgloss.Style
	SearchCurrent   lipgloss.Style
	SearchHighlight lipgloss.Style // bg only; per-line fg layered on at render time

	// Help panel
	HelpTitle   lipgloss.Style
	HelpSection lipgloss.Style
	HelpKey     lipgloss.Style
	HelpDesc    lipgloss.Style
	HelpDim     lipgloss.Style
	HelpNote    lipgloss.Style

	// Layers panel
	LayerArrow       lipgloss.Style
	LayerInstruction lipgloss.Style
	LayerCursor      lipgloss.Style

	// Waste panel
	WasteTitle lipgloss.Style
	WasteDim   lipgloss.Style
	WasteKey   lipgloss.Style
	WasteDesc  lipgloss.Style

	// Loading screen / progress bar
	BarFilled lipgloss.Style
	BarEmpty  lipgloss.Style
	LoadHint  lipgloss.Style
	LoadError lipgloss.Style

	// Misc
	Separator  lipgloss.Style
	Command    lipgloss.Style
	MetaDim    lipgloss.Style
	TreeDim    lipgloss.Style
	ScrollDim  lipgloss.Style
	Accent     lipgloss.Style
	AccentBold lipgloss.Style

	// palette is retained on Styles for the small number of render sites
	// that combine a palette color with a runtime-computed counterpart
	// (per-line search highlight foreground, diff-color selection in
	// filetree). Render code uses palette directly only in those cases.
	palette theme.Palette

	// Base is the panel-content background. Painted under every panel
	// rectangle (borders, body lines, filler pads, blank rows) so a
	// theme's foreground colors meet the background it was designed
	// for, regardless of the user's terminal default. Themes that opt
	// out of this (minimal) set Palette.Base to lipgloss.NoColor{}, in
	// which case lipgloss emits no background SGR and the terminal
	// default shows through.
	Base lipgloss.Style
}

// Palette returns the underlying theme palette this Styles was built
// from. Used by render sites that need a raw color.Color (search
// highlight foregrounds, diff coloring per file).
func (s Styles) Palette() theme.Palette { return s.palette }

// Pad returns n spaces painted with the panel Base background. Use
// this for column gaps and trailing pads inside a panel body — the
// SGR reset at the end of a styled segment clears the background, so
// a literal " " between two styled segments would otherwise show as
// terminal-default cells. n <= 0 returns "".
func (s Styles) Pad(n int) string {
	if n <= 0 {
		return ""
	}
	return s.Base.Render(strings.Repeat(" ", n))
}

// BuildStyles constructs every Style from p once, at model creation
// time. ~40 lipgloss.Style values cost less to allocate once at
// startup than reconstructing them per Render call.
//
// fgOnBase pairs a foreground with the panel base background so any
// styled text rendered inside a panel paints its own background and
// the panel rectangle reads as a single solid color. Styles that
// already set their own background (status bar, selection, search
// highlight) skip this — their explicit bg is the intent.
func BuildStyles(p theme.Palette) Styles {
	fgOnBase := func(c color.Color) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(c).Background(p.Base)
	}
	on := func(fgc, bgc color.Color) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(fgc).Background(bgc)
	}
	return Styles{
		FocusedBorder:   fgOnBase(p.FocusedBorder),
		UnfocusedBorder: fgOnBase(p.UnfocusedBorder),

		Added:     fgOnBase(p.Added),
		Modified:  fgOnBase(p.Modified),
		Removed:   fgOnBase(p.Removed),
		Unchanged: fgOnBase(p.Unchanged),

		Selected:     on(p.SelectedFg, p.SelectedBg),
		SelectedBold: on(p.SelectedFg, p.SelectedBg).Bold(true),

		StatusBg:          lipgloss.NewStyle().Background(p.StatusBg),
		StatusKey:         on(p.StatusKey, p.StatusBg).Bold(true),
		StatusDim:         on(p.StatusDim, p.StatusBg),
		StatusBrand:       on(p.Accent, p.StatusBg).Bold(true),
		StatusBrandGlyph:  on(p.Accent, p.StatusBg),
		StatusSep:         on(p.HeaderSep, p.StatusBg),
		StatusImageName:   on(p.SelectedFg, p.StatusBg),
		StatusBadgeAccent: on(p.Accent, p.StatusBg),
		StatusBadgeWarn:   on(p.Modified, p.StatusBg),
		StatusCopied:      on(p.Added, p.StatusBg).Bold(true),
		StatusError:       on(p.Removed, p.StatusBg).Bold(true),
		StatusMatch:       on(p.SearchCurrentBg, p.StatusBg).Bold(true),
		StatusRightHi:     on(p.SelectedFg, p.StatusBg).Bold(true),
		StatusRightDim:    on(p.StatusDim, p.StatusBg),

		HeaderDim:         fgOnBase(p.HeaderDim),
		HeaderDimOnStatus: on(p.HeaderDim, p.StatusBg),
		HeaderSep:         fgOnBase(p.HeaderSep),

		FileName:        fgOnBase(p.FileName),
		SearchCurrent:   on(p.SearchCurrentFg, p.SearchCurrentBg),
		SearchHighlight: lipgloss.NewStyle().Background(p.SearchHighlightBg),

		HelpTitle:   fgOnBase(p.Accent).Bold(true),
		HelpSection: fgOnBase(p.Modified).Bold(true),
		HelpKey:     fgOnBase(p.StatusKey),
		HelpDesc:    fgOnBase(p.FileName),
		HelpDim:     fgOnBase(p.StatusDim),
		HelpNote:    fgOnBase(p.StatusDim).Italic(true),

		LayerArrow:       fgOnBase(p.Accent).Bold(true),
		LayerInstruction: fgOnBase(p.Accent).Bold(true),
		LayerCursor:      fgOnBase(p.Accent),

		WasteTitle: fgOnBase(p.Accent).Bold(true),
		WasteDim:   fgOnBase(p.StatusDim),
		WasteKey:   fgOnBase(p.StatusKey).Bold(true),
		WasteDesc:  fgOnBase(p.FileName),

		BarFilled: fgOnBase(p.Accent),
		BarEmpty:  fgOnBase(p.Separator),
		LoadHint:  fgOnBase(p.StatusDim),
		LoadError: fgOnBase(p.Removed).Bold(true),

		Separator:  fgOnBase(p.Separator),
		Command:    fgOnBase(p.Command),
		MetaDim:    fgOnBase(p.MetaDim),
		TreeDim:    fgOnBase(p.TreeDim),
		ScrollDim:  fgOnBase(p.ScrollDim),
		Accent:     fgOnBase(p.Accent),
		AccentBold: fgOnBase(p.Accent).Bold(true),

		Base: lipgloss.NewStyle().Background(p.Base),

		palette: p,
	}
}

// largeStepGrowthFraction is the threshold above which a positive Δfs
// is colored as a notable size increase. 0.10 = 10% of final live size.
const largeStepGrowthFraction = 0.10

// renderPanel draws the framed panel used by the layers, tree, file,
// help, and waste sub-views. Takes Styles instead of reading
// package-level vars so the panel respects the active theme.
//
// Every cell in the panel rectangle — borders, title row, content
// lines, trailing filler pads, scroll arrows, and any rows past the
// end of the supplied content — is painted with the theme's Base
// background. Without this, panel bodies inherit the user's terminal
// default background and a light theme renders dark text on a dark
// terminal (and vice versa). Inner content that sets its own bg
// (Selected, SearchCurrent, SearchHighlight) overrides Base in the
// cells it covers; trailing pads continue the Base bg from where the
// inner SGR's reset left off.
func renderPanel(s Styles, content, title string, focused bool, contentWidth, height int, hasAbove, hasBelow bool) string {
	if contentWidth < 0 {
		contentWidth = 0
	}
	if height < 0 {
		height = 0
	}
	border := s.UnfocusedBorder
	if focused {
		border = s.FocusedBorder
	}

	maxTitle := max(contentWidth-3, 0)
	if ansi.StringWidth(title) > maxTitle {
		title = ansi.Truncate(title, maxTitle, "…")
	}
	titleRendered := border.Bold(true).Render(title)

	topLeft := border.Render("╭")
	topRight := border.Render("╮")
	bottomLeft := border.Render("╰")
	bottomRight := border.Render("╯")
	vLine := border.Render("│")

	titleLen := lipgloss.Width(title)
	fillCount := max(contentWidth-titleLen-3, 0)
	topBorder := topLeft + border.Render("─ ") + titleRendered + border.Render(" "+strings.Repeat("─", fillCount)) + topRight

	bottomBorder := bottomLeft + border.Render(strings.Repeat("─", contentWidth)) + bottomRight

	lines := strings.Split(content, "\n")
	var sb strings.Builder
	sb.WriteString(topBorder)
	sb.WriteString("\n")

	for i := range height {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		lineWidth := ansi.StringWidth(line)
		if lineWidth > contentWidth {
			line = ansi.Truncate(line, contentWidth, "")
			lineWidth = ansi.StringWidth(line)
		}
		pad := max(contentWidth-lineWidth, 0)

		sb.WriteString(vLine)
		sb.WriteString(line)
		// Pad spaces are explicitly Base-styled so any inner SGR reset at
		// the end of `line` (Selected, SearchCurrent, etc.) is followed by
		// Base bg, not a stripe of terminal-default cells.
		sb.WriteString(s.Pad(pad))

		rightBorder := vLine
		if hasAbove && i == 0 {
			rightBorder = s.ScrollDim.Render("▴")
		} else if hasBelow && i == height-1 {
			rightBorder = s.ScrollDim.Render("▾")
		}
		sb.WriteString(rightBorder)

		if i < height-1 {
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(bottomBorder)

	return sb.String()
}
