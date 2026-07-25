package tui

import (
	"image/color"

	tea "charm.land/bubbletea/v2"
)

// finalizeView applies standard TUI options: alternate screen, mouse wheel
// capture, and the theme background color.
func finalizeView(v tea.View, bg color.Color) tea.View {
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.BackgroundColor = bg
	return v
}

// finalizeViewNoMouse is like finalizeView but disables mouse capture so the
// terminal handles mouse events natively (enabling text selection by click+drag).
func finalizeViewNoMouse(v tea.View, bg color.Color) tea.View {
	v.AltScreen = true
	v.MouseMode = tea.MouseModeNone
	v.BackgroundColor = bg
	return v
}
