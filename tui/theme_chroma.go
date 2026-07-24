package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	colorful "github.com/lucasb-eyer/go-colorful"
)

// minLegibleDistance is the smallest perceptual distance a foreground/
// background pair may have and still be considered readable. The scale is
// go-colorful's DistanceLab, whose Lab L channel is normalised to [0,1] — so
// black↔white is ~1.0, NOT the 0..100 CIE76 range. Every shipped theme's
// text/background pairs score ~0.4–0.9 on this scale (mocha, the long-shipped
// default, included), so 0.20 admits all legible pairs while still rejecting a
// near-identical fg/bg (distance ≈ 0).
const minLegibleDistance = 0.20

// labDistance returns the perceptual distance between two colours in CIE Lab
// space. Used by the contrast guard to reject any theme whose text would be
// unreadable on its own background.
func labDistance(a, b color.Color) float64 {
	ca, _ := colorful.MakeColor(a)
	cb, _ := colorful.MakeColor(b)
	return ca.DistanceLab(cb)
}

func chromaColor(c chroma.Colour) color.Color { return lipgloss.Color(c.String()) }

// chromaColorOr bridges a chroma.Colour to color.Color, falling back to `alt`
// when the style leaves the token unset. An unset chroma.Colour is 0 and its
// String() does not yield a usable hex, so IsSet() must be checked first —
// verified against chroma v2.24.1 (colour.go: IsSet reports c != 0).
func chromaColorOr(c chroma.Colour, alt color.Color) color.Color {
	if !c.IsSet() {
		return alt
	}
	return lipgloss.Color(c.String())
}

// blend nudges base toward target by amt (0..1) in perceptual Lab space, used
// to derive surface/selection tiers a chroma syntax style doesn't itself
// define. Clamped() keeps the result in-gamut so no channel overflows.
func blend(base, target color.Color, amt float64) color.Color {
	b, _ := colorful.MakeColor(base)
	tg, _ := colorful.MakeColor(target)
	return lipgloss.Color(b.BlendLab(tg, amt).Clamped().Hex())
}

// fromChroma derives a full Theme from a chroma syntax style. A chroma style
// defines only token colours (background, text, keywords, strings, …); the
// TUI needs surface/selection/dim tiers that syntax styles don't carry, so
// those are derived by blending the background toward white/black in Lab
// space. Diff/accent roles read specific syntax tokens, falling back to the
// base text colour when a given style leaves that token unset. ChromaStyle is
// the same style name so the file viewer's highlighting matches the chrome.
func fromChroma(name, styleName string) Theme {
	st := chromastyles.Get(styleName)
	bg := chromaColor(st.Get(chroma.Background).Background)
	fg := chromaColor(st.Get(chroma.Text).Colour)
	white := lipgloss.Color("#ffffff")
	black := lipgloss.Color("#000000")
	// Background hierarchy: the syntax style gives only one background (bg),
	// so the raised-panel and selection tiers are derived by nudging bg toward
	// white in Lab space. panel sits just above the root canvas; selection
	// steps up again so a selected row reads distinct from the panel.
	panel := blend(bg, white, 0.06)
	sel := blend(bg, white, 0.16)
	accent := chromaColorOr(st.Get(chroma.KeywordType).Colour, fg)
	return Theme{
		Name:            name,
		RootBg:          bg,
		PanelBg:         panel,
		BorderFocus:     accent,
		BorderBlur:      blend(bg, white, 0.18),
		SelectFg:        fg,
		SelectBg:        sel,
		DiffAdd:         chromaColorOr(st.Get(chroma.NameFunction).Colour, fg),
		DiffModify:      chromaColorOr(st.Get(chroma.LiteralString).Colour, fg),
		DiffRemove:      chromaColorOr(st.Get(chroma.Keyword).Colour, fg),
		TextPrimary:     fg,
		TextNeutral:     blend(fg, bg, 0.20),
		TextDim1:        blend(fg, bg, 0.40),
		TextDim2:        blend(fg, bg, 0.55),
		TreeGlyph:       blend(bg, white, 0.22),
		Accent:          accent,
		Separator:       blend(bg, white, 0.12),
		StatusBg:        blend(bg, black, 0.30),
		// Accent-tinted so a match reads distinct from the grey selection
		// tier; fg stays the style's text colour, kept legible by the blend
		// staying close to bg. The contrast guard asserts both.
		SearchMatchBg:   blend(bg, accent, 0.30),
		SearchMatchFg:   fg,
		SearchCurrentBg: chromaColorOr(st.Get(chroma.LiteralString).Colour, accent),
		SearchCurrentFg: bg,
		ChromaStyle:     styleName,
	}
}
