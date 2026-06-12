package tui

import (
	"charm.land/bubbles/v2/key"
)

type keyMap struct {
	Quit         key.Binding
	Up           key.Binding
	Down         key.Binding
	Top          key.Binding
	Bottom       key.Binding
	Left         key.Binding
	Right        key.Binding
	Switch       key.Binding
	Copy         key.Binding
	CopyPath     key.Binding
	CopyContent  key.Binding
	Help         key.Binding
	Filter       key.Binding
	DiffOnly     key.Binding
	Sort         key.Binding
	ExtractFile  key.Binding
	ViewerSearch key.Binding
	NextMatch    key.Binding
	PrevMatch    key.Binding
	Waste        key.Binding
	SizeColumn   key.Binding
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
		Left: key.NewBinding(
			key.WithKeys("h", "left"),
			key.WithHelp("h/←", "scroll left"),
		),
		Right: key.NewBinding(
			key.WithKeys("l", "right"),
			key.WithHelp("l/→", "scroll right"),
		),
		Switch: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("Tab", "switch"),
		),
		Copy: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "copy cmd"),
		),
		CopyPath: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "copy path"),
		),
		CopyContent: key.NewBinding(
			key.WithKeys("Y"),
			key.WithHelp("Y", "copy content"),
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
		ViewerSearch: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		NextMatch: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "next match"),
		),
		PrevMatch: key.NewBinding(
			key.WithKeys("N"),
			key.WithHelp("N", "prev match"),
		),
		Waste: key.NewBinding(
			key.WithKeys("w"),
			key.WithHelp("w", "wasted files"),
		),
		SizeColumn: key.NewBinding(
			key.WithKeys("S"),
			key.WithHelp("S", "size display"),
		),
	}
}
