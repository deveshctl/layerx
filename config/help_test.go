package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSectionHelp_KnownSection(t *testing.T) {
	assert.Contains(t, SectionHelp(SectionRules), "lowest-efficiency")
	assert.Contains(t, SectionHelp(SectionPathRules), "path-rules")
}

func TestSectionHelp_UnknownSection_FallsBackToGeneral(t *testing.T) {
	h := SectionHelp("")
	assert.Contains(t, h, "docs/configuration.md")
	assert.Contains(t, h, "layerx init")
}

func TestSectionHelp_AlwaysNonEmpty(t *testing.T) {
	for _, section := range []string{"", "typo-section", SectionRules, SectionGeneral} {
		assert.NotEmpty(t, SectionHelp(section), "section %q", section)
	}
}
