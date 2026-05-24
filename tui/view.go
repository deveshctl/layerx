package tui

import tea "charm.land/bubbletea/v2"

// finalizeView applies standard TUI options: alternate screen and mouse wheel
// capture (cell motion — clicks and scroll without all-motion tracking).
func finalizeView(v tea.View) tea.View {
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}
