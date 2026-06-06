package theme

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNames_StableOrder locks the display order users see in
// `layerx themes`. Re-ordering this list is a UX change; this test
// makes sure it stays deliberate.
func TestNames_StableOrder(t *testing.T) {
	got := Names()
	want := []Name{"default", "latte", "frappe", "macchiato", "nord", "minimal"}
	require.Equal(t, want, got)
}

// TestGet_KnownNames asserts every registered theme resolves and the
// returned Theme.Name matches the lookup key.
func TestGet_KnownNames(t *testing.T) {
	for _, n := range Names() {
		got, err := Get(string(n))
		require.NoError(t, err, "Get(%q)", n)
		require.Equal(t, n, got.Name, "Theme.Name mismatch for %q", n)
		require.NotEmpty(t, got.Description, "%q missing description", n)
	}
}

// TestGet_Unknown returns *ErrUnknownTheme carrying the input name.
func TestGet_Unknown(t *testing.T) {
	_, err := Get("not-a-real-theme")
	require.Error(t, err)
	var typed *ErrUnknownTheme
	require.ErrorAs(t, err, &typed)
	require.Equal(t, "not-a-real-theme", typed.Name)
}

// TestGet_EmptyString rejects "". Callers handle "unset" before
// calling Get; a future regression where Get("") silently returned
// the default would mask config-load bugs.
func TestGet_EmptyString(t *testing.T) {
	_, err := Get("")
	require.Error(t, err)
}

// TestDefaultIsValid: Default() returns a theme also in All().
// Guards against registry[0] being deleted or reshuffled without
// updating Default().
func TestDefaultIsValid(t *testing.T) {
	def := Default()
	require.Equal(t, Name("default"), def.Name)
	got, err := Get("default")
	require.NoError(t, err)
	require.Equal(t, def, got)
}

// TestPaletteCompleteness: every Palette field on every registered
// theme is non-nil. Catches "added a Palette field but forgot to
// fill it in nord" — the most likely error class as the theme set
// grows.
func TestPaletteCompleteness(t *testing.T) {
	for _, th := range All() {
		v := reflect.ValueOf(th.Palette)
		typ := v.Type()
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			require.Falsef(t,
				f.Kind() == reflect.Interface && f.IsNil(),
				"theme %q: Palette.%s is nil", th.Name, typ.Field(i).Name)
		}
	}
}

// TestAllReturnsCopy: mutating the slice returned by All() must not
// affect subsequent calls. registry is package-internal state.
func TestAllReturnsCopy(t *testing.T) {
	a := All()
	a[0] = Theme{Name: "tampered"}
	b := All()
	require.NotEqual(t, Name("tampered"), b[0].Name)
}
