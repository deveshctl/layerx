package theme

// registry is the ordered list of every built-in theme. Order is the
// display order users see in `layerx themes`. Index 0 is the default —
// Default() relies on this. Palette literals live in their own files
// (palette_*.go) and are wired in here.
//
// Adding a theme: append a Theme literal here, add its Palette in a
// new palette_<name>.go (or extend palettes.go), and update the test
// TestPaletteCompleteness expectation if needed.
var registry = []Theme{
	{
		Name:        "default",
		Description: "Catppuccin Mocha (dark) — built-in default",
		Palette:     mocha,
	},
	{
		Name:        "latte",
		Description: "Catppuccin Latte (light)",
		Palette:     latte,
	},
	{
		Name:        "frappe",
		Description: "Catppuccin Frappé (dark)",
		Palette:     frappe,
	},
	{
		Name:        "macchiato",
		Description: "Catppuccin Macchiato (dark)",
		Palette:     macchiato,
	},
	{
		Name:        "nord",
		Description: "Nord (dark, blue-leaning)",
		Palette:     nord,
	},
	{
		Name:        "minimal",
		Description: "Terminal ANSI-16 colors (respects your shell theme)",
		Palette:     minimal,
	},
}
