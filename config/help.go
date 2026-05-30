package config

// SectionHelp returns a short reference excerpt for section, or "" when the
// section is unknown. Add new top-level config keys here as they ship so the
// CLI can surface section-specific guidance without calling cobra.Usage().
func SectionHelp(section string) string {
	return sectionHelp[section]
}

// sectionHelp maps config sections to concise examples. Keep excerpts short —
// full reference lives in docs/configuration.md.
var sectionHelp = map[string]string{
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
