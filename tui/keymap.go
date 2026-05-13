package tui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Quit     key.Binding
	Up       key.Binding
	Down     key.Binding
	Top      key.Binding
	Bottom   key.Binding
	Switch   key.Binding
	Copy     key.Binding
	Help     key.Binding
	Filter   key.Binding
	DiffOnly key.Binding
	Sort     key.Binding
}

var keys = keyMap{
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	Up: key.NewBinding(
		key.WithKeys("k", "up"),
		key.WithHelp("k/↑", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("j", "down"),
		key.WithHelp("j/↓", "down"),
	),
	Top: key.NewBinding(
		key.WithKeys("g"),
		key.WithHelp("g", "top"),
	),
	Bottom: key.NewBinding(
		key.WithKeys("G"),
		key.WithHelp("G", "bottom"),
	),
	Switch: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("Tab", "switch"),
	),
	Copy: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "copy cmd"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	Filter: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "filter"),
	),
	DiffOnly: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "diff only"),
	),
	Sort: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "sort size"),
	),
}
