package image

import (
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePlatform_EmptyReturnsNil(t *testing.T) {
	got, err := ParsePlatform("")
	require.NoError(t, err)
	assert.Nil(t, got, "empty string must mean 'no platform pinned' — propagate as nil")

	got, err = ParsePlatform("   ")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestParsePlatform_AcceptedShapes(t *testing.T) {
	cases := []struct {
		spec     string
		wantOS   string
		wantArch string
		wantVar  string
	}{
		{"amd64", "linux", "amd64", ""},
		{"arm64", "linux", "arm64", ""},
		{"linux/amd64", "linux", "amd64", ""},
		{"linux/arm64", "linux", "arm64", ""},
		{"linux/arm/v7", "linux", "arm", "v7"},
		{"linux/arm64/v8", "linux", "arm64", "v8"},
		{"windows/amd64", "windows", "amd64", ""},
		// trims whitespace
		{"  linux/arm64  ", "linux", "arm64", ""},
	}
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			p, err := ParsePlatform(tc.spec)
			require.NoError(t, err)
			require.NotNil(t, p)
			assert.Equal(t, tc.wantOS, p.OS)
			assert.Equal(t, tc.wantArch, p.Architecture)
			assert.Equal(t, tc.wantVar, p.Variant)
		})
	}
}

func TestParsePlatform_RejectsMalformed(t *testing.T) {
	bad := []string{
		"linux/",
		"/amd64",
		"linux//amd64",
		"linux/amd64/v8/extra",
	}
	for _, s := range bad {
		t.Run(s, func(t *testing.T) {
			_, err := ParsePlatform(s)
			require.Error(t, err)
			var inv *ErrPlatformInvalid
			require.ErrorAs(t, err, &inv)
			assert.Equal(t, s, inv.Spec)
		})
	}
}

func TestFormatPlatform_RoundTrip(t *testing.T) {
	cases := []string{"linux/amd64", "linux/arm64", "linux/arm/v7", "linux/arm64/v8", "windows/amd64"}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			p, err := ParsePlatform(s)
			require.NoError(t, err)
			assert.Equal(t, s, FormatPlatform(p))
		})
	}
}

func TestFormatPlatform_NilAndZero(t *testing.T) {
	assert.Empty(t, FormatPlatform(nil))
	assert.Empty(t, FormatPlatform(&ocispec.Platform{}))
}

func TestPlatformsEqual(t *testing.T) {
	mk := func(s string) *ocispec.Platform {
		p, err := ParsePlatform(s)
		require.NoError(t, err)
		return p
	}
	// nil on either side matches anything (no preference)
	assert.True(t, PlatformsEqual(nil, mk("linux/amd64")))
	assert.True(t, PlatformsEqual(mk("linux/amd64"), nil))
	// exact match
	assert.True(t, PlatformsEqual(mk("linux/amd64"), mk("linux/amd64")))
	// missing variant on one side is treated as a match — Docker writes
	// arm64 images without a variant in some pipelines; we don't want a
	// spurious mismatch on a single-arch archive that recorded variant="".
	assert.True(t, PlatformsEqual(mk("linux/arm64"), mk("linux/arm64/v8")))
	// case-insensitive on os/arch (registries are inconsistent)
	assert.True(t, PlatformsEqual(mk("Linux/AMD64"), mk("linux/amd64")))
	// genuine mismatch
	assert.False(t, PlatformsEqual(mk("linux/amd64"), mk("linux/arm64")))
	assert.False(t, PlatformsEqual(mk("linux/amd64"), mk("windows/amd64")))
}

func TestErrPlatformNotInImage_Format(t *testing.T) {
	// With available platforms, the error lists them.
	e := &ErrPlatformNotInImage{
		Ref:       "nginx:latest",
		Requested: "linux/ppc64le",
		Available: []string{"linux/amd64", "linux/arm64"},
	}
	msg := e.Error()
	assert.Contains(t, msg, "platform linux/ppc64le not found")
	assert.Contains(t, msg, "linux/amd64")
	assert.Contains(t, msg, "linux/arm64")
	assert.Contains(t, msg, "Available platforms")
	// The image ref is intentionally NOT in the rendered message — the user
	// already typed it on the command line and the goal is a tight, scannable
	// "X not found / try Y" hint. Field is still readable on the struct.
	assert.NotContains(t, msg, "nginx:latest")

	// Without an available list (older daemon), the error stays terse.
	e2 := &ErrPlatformNotInImage{Ref: "nginx:latest", Requested: "linux/ppc64le"}
	msg2 := e2.Error()
	assert.Contains(t, msg2, "platform linux/ppc64le not found")
	assert.NotContains(t, msg2, "Available platforms")
}

func TestErrPlatformInvalid_Format(t *testing.T) {
	e := &ErrPlatformInvalid{Spec: "linux//amd64", Reason: "empty component"}
	msg := e.Error()
	assert.Contains(t, msg, "linux//amd64")
	assert.Contains(t, msg, "empty component")
	assert.Contains(t, msg, "OS/ARCH")
}
