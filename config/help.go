package config

// SectionHelp returns a short reference excerpt for section. When section is
// empty or unknown, a general .layerx.yaml hint is returned so every config
// failure surfaces actionable next steps.
func SectionHelp(section string) string {
	if h, ok := sectionHelp[section]; ok && h != "" {
		return h
	}
	return sectionHelp[SectionGeneral]
}

// sectionHelp maps config sections to concise examples. Keep excerpts short —
// full reference lives in docs/configuration.md.
var sectionHelp = map[string]string{
	SectionGeneral: `.layerx.yaml — configuration file (read from the working directory):

  rules:           CI efficiency thresholds (layerx ci, CI=true)
  path-rules:      path-scoped CI rules (block, deny-waste, max-layer-count)
  version:         schema version (currently 1)
  theme:           TUI colour theme (see below)

See docs/configuration.md for the full reference.
Run "layerx init --flavour generic" to write a starter file.`,
	SectionTheme: `theme — TUI colour theme selection:

  theme: tokyo-night       # default

Available themes: tokyo-night, catppuccin-mocha, kanagawa, gruvbox-dark,
                  rose-pine, dracula, oxocarbon, cyberdream

Precedence (highest to lowest):
  1. --theme flag on the command line
  2. theme: in .layerx.yaml
  3. built-in default (tokyo-night)`,
	SectionRules: `rules — global CI efficiency thresholds (used by "layerx ci" and CI=true):

  rules:
    lowest-efficiency: 0.9              # minimum score, 0.0–1.0 (0 disables)
    highest-wasted-bytes: 0             # max wasted bytes (0 disables)
    highest-user-wasted-percent: 0.1    # max waste fraction (0 disables)

Run "layerx init --flavour generic" for a starter file.`,
	SectionPathRules: `path-rules — path-scoped CI rules (block, deny-waste, max-layer-count):

  path-rules:
    block:
      - "**/.git/**"
    deny-waste:
      - "**/*.log"
    max-layer-count: 5

See docs/configuration.md for flat vs list form.`,
	SectionVersion: `version — schema version for .layerx.yaml (currently only 1 is supported):

  version: 1

Omit the field to use schema version 1.`,
	SectionKeybindings: `keybindings — reserved for a future TUI override feature (M12):

  keybindings:
    quit: q

The key is accepted today but not yet wired; remove it if you do not need it.`,
}
