package tui

import (
	"image/color"

	tea "charm.land/bubbletea/v2"
)

// finalizeView applies standard TUI options: alternate screen, mouse wheel
// capture, and the theme background color. bg is skipped when nil (transparent
// background mode — the terminal's own background shows through).
func finalizeView(v tea.View, bg color.Color) tea.View {
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	if bg != nil {
		v.BackgroundColor = bg
	}
	return v
}

// finalizeViewNoMouse is like finalizeView but disables mouse capture so the
// terminal handles mouse events natively (enabling text selection by click+drag).
func finalizeViewNoMouse(v tea.View, bg color.Color) tea.View {
	v.AltScreen = true
	v.MouseMode = tea.MouseModeNone
	if bg != nil {
		v.BackgroundColor = bg
	}
	return v
}
