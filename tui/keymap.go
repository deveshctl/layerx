package tui

import (
	"charm.land/bubbles/v2/key"

	"github.com/deveshpharswan/layerx/config"
)

type keyMap struct {
	Quit        key.Binding
	Up          key.Binding
	Down        key.Binding
	Top         key.Binding
	Bottom      key.Binding
	Switch      key.Binding
	Copy        key.Binding
	Help        key.Binding
	Filter      key.Binding
	DiffOnly    key.Binding
	Sort        key.Binding
	ExtractFile key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
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
		ExtractFile: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "save file"),
		),
	}
}

func applyOverrides(km *keyMap, kb config.KeybindingsConfig) {
	if kb.Quit != "" {
		km.Quit = key.NewBinding(key.WithKeys(kb.Quit), key.WithHelp(kb.Quit, "quit"))
	}
	if kb.Up != "" {
		km.Up = key.NewBinding(key.WithKeys(kb.Up), key.WithHelp(kb.Up, "up"))
	}
	if kb.Down != "" {
		km.Down = key.NewBinding(key.WithKeys(kb.Down), key.WithHelp(kb.Down, "down"))
	}
	if kb.Filter != "" {
		km.Filter = key.NewBinding(key.WithKeys(kb.Filter), key.WithHelp(kb.Filter, "filter"))
	}
	if kb.Sort != "" {
		km.Sort = key.NewBinding(key.WithKeys(kb.Sort), key.WithHelp(kb.Sort, "sort size"))
	}
	if kb.DiffOnly != "" {
		km.DiffOnly = key.NewBinding(key.WithKeys(kb.DiffOnly), key.WithHelp(kb.DiffOnly, "diff only"))
	}
	if kb.Extract != "" {
		km.ExtractFile = key.NewBinding(key.WithKeys(kb.Extract), key.WithHelp(kb.Extract, "save file"))
	}
	if kb.Help != "" {
		km.Help = key.NewBinding(key.WithKeys(kb.Help), key.WithHelp(kb.Help, "help"))
	}
	if kb.Switch != "" {
		km.Switch = key.NewBinding(key.WithKeys(kb.Switch), key.WithHelp(kb.Switch, "switch"))
	}
}
