package image

import (
	"fmt"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// ParsePlatform converts a Docker-CLI-style platform string into an
// ocispec.Platform suitable for the moby client's pull / save / inspect
// options. Accepts the same shapes the Docker CLI does:
//
//	"amd64"                 → os=linux, arch=amd64
//	"arm64"                 → os=linux, arch=arm64
//	"linux/amd64"           → os=linux, arch=amd64
//	"linux/arm64"           → os=linux, arch=arm64
//	"linux/arm/v7"          → os=linux, arch=arm, variant=v7
//	"linux/arm64/v8"        → os=linux, arch=arm64, variant=v8
//	"windows/amd64"         → os=windows, arch=amd64
//
// The OS defaults to "linux" when only an architecture is given, mirroring
// the Docker CLI. The function does not contact any registry — it parses
// the string syntactically; a returned platform may still fail at lookup
// time if the image does not contain that variant.
//
// The empty string returns a nil platform with no error: callers use that
// to mean "use the daemon's default platform" and skip the per-call API
// option entirely (preserving existing behaviour for users who never pass
// --platform).
func ParsePlatform(s string) (*ocispec.Platform, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, "/")
	for _, p := range parts {
		if p == "" {
			return nil, &ErrPlatformInvalid{Spec: s, Reason: "empty component"}
		}
	}
	var p ocispec.Platform
	switch len(parts) {
	case 1:
		p.OS = "linux"
		p.Architecture = parts[0]
	case 2:
		p.OS = parts[0]
		p.Architecture = parts[1]
	case 3:
		p.OS = parts[0]
		p.Architecture = parts[1]
		p.Variant = parts[2]
	default:
		return nil, &ErrPlatformInvalid{
			Spec:   s,
			Reason: "expected ARCH, OS/ARCH, or OS/ARCH/VARIANT",
		}
	}
	return &p, nil
}

// FormatPlatform renders an ocispec.Platform back into the canonical
// "os/arch[/variant]" form used by the Docker CLI. Returns "" for a nil
// or zero-value platform so error messages can elide it cleanly.
func FormatPlatform(p *ocispec.Platform) string {
	if p == nil {
		return ""
	}
	if p.OS == "" && p.Architecture == "" && p.Variant == "" {
		return ""
	}
	if p.Variant != "" {
		return fmt.Sprintf("%s/%s/%s", p.OS, p.Architecture, p.Variant)
	}
	return fmt.Sprintf("%s/%s", p.OS, p.Architecture)
}

// PlatformsEqual reports whether two platforms refer to the same image
// variant. Empty fields on either side compare equal to any value (the
// CLI commonly omits the variant for arches that only have one). nil on
// either side means "no preference" and matches anything.
func PlatformsEqual(want, got *ocispec.Platform) bool {
	if want == nil || got == nil {
		return true
	}
	if want.OS != "" && got.OS != "" && !strings.EqualFold(want.OS, got.OS) {
		return false
	}
	if want.Architecture != "" && got.Architecture != "" &&
		!strings.EqualFold(want.Architecture, got.Architecture) {
		return false
	}
	if want.Variant != "" && got.Variant != "" &&
		!strings.EqualFold(want.Variant, got.Variant) {
		return false
	}
	return true
}
