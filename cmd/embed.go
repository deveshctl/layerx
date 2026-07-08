package cmd

import (
	"embed"
	"fmt"
	"slices"
	"strings"
)

//go:embed examples/*.yaml
var starterFS embed.FS

// validFlavours is the canonical, ordered list of supported starter flavours.
// Order is the prompt order in interactive mode and the help-text order.
var validFlavours = []string{"node", "python", "java", "go", "generic"}

// StarterConfig returns the embedded starter config bytes for the named
// flavour. The boolean is false if the flavour isn't one of the supported
// values. Callers should validate flavour before calling, but the boolean
// keeps a typo from causing a nil-byte write to .layerx.yaml.
func StarterConfig(flavour string) ([]byte, bool) {
	if !isValidFlavour(flavour) {
		return nil, false
	}
	data, err := starterFS.ReadFile(fmt.Sprintf("examples/.layerx.%s.yaml", flavour))
	if err != nil {
		return nil, false
	}
	return data, true
}

func isValidFlavour(s string) bool {
	return slices.Contains(validFlavours, s)
}

func flavourList() string {
	if len(validFlavours) == 0 {
		return ""
	}
	if len(validFlavours) == 1 {
		return validFlavours[0]
	}
	return strings.Join(validFlavours[:len(validFlavours)-1], ", ") + ", or " + validFlavours[len(validFlavours)-1]
}
