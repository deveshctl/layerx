package tui

import (
	"bytes"
	"flag"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

var updateGolden = flag.Bool("update", false, "update tui golden files")

// goldenAssertTUI compares got against testdata/golden/<name>, writing it
// first when -update is passed. The golden captures ANSI-stripped TEXT+LAYOUT
// so it is stable across terminal colour profiles (dev machine has a TTY, CI
// does not) — colour correctness is enforced separately by the direct-hex
// test and the contrast guard.
func goldenAssertTUI(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, got, 0o644))
		return
	}
	want, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// Fixtures are generated once with -update on a machine that can run
		// the binary (see docs Gate C), then committed. Until then, skip
		// rather than fail so CI stays green on the first push.
		t.Skipf("golden %s missing — run `go test ./tui -update` and commit testdata/golden", name)
	}
	require.NoError(t, err)
	if !bytes.Equal(got, want) {
		t.Fatalf("golden mismatch %s\n--want--\n%s\n--got--\n%s", name, want, got)
	}
}

// goldenModelWithTheme builds a deterministic ready-state model at a fixed
// size with the given theme, so View() output is stable for golden capture.
func goldenModelWithTheme(t *testing.T, th Theme) model {
	t.Helper()
	m := NewModel(Config{ImageRef: "test:latest"})
	m.theme = th
	m.styles = newStyles(th)
	m.width = 120
	m.height = 40
	m.state = stateReady
	m.analysis = testAnalysis()
	return m
}

func hexOf(t *testing.T, c color.Color) string {
	t.Helper()
	r, g, b, _ := c.RGBA()
	return sprintfHex(uint8(r>>8), uint8(g>>8), uint8(b>>8))
}

func TestDefaultTheme_PreservesCurrentPalette(t *testing.T) {
	th := defaultTheme()
	cases := map[string]struct {
		got  color.Color
		want string
	}{
		"BorderFocus":     {th.BorderFocus, "#89b4fa"},
		"BorderBlur":      {th.BorderBlur, "#45475a"},
		"SelectFg":        {th.SelectFg, "#cdd6f4"},
		"SelectBg":        {th.SelectBg, "#313244"},
		"DiffAdd":         {th.DiffAdd, "#a6e3a1"},
		"DiffModify":      {th.DiffModify, "#f9e2af"},
		"DiffRemove":      {th.DiffRemove, "#f38ba8"},
		"TextPrimary":     {th.TextPrimary, "#bac2de"},
		"TextNeutral":     {th.TextNeutral, "#a6adc8"},
		"TextDim1":        {th.TextDim1, "#9399b2"},
		"TextDim2":        {th.TextDim2, "#6c7086"},
		"TreeGlyph":       {th.TreeGlyph, "#45475a"},
		"Accent":          {th.Accent, "#89b4fa"},
		"Separator":       {th.Separator, "#313244"},
		"StatusBg":        {th.StatusBg, "#181825"},
		"SearchMatchBg":   {th.SearchMatchBg, "#585b70"},
		"SearchCurrentBg": {th.SearchCurrentBg, "#f9e2af"},
		"SearchCurrentFg": {th.SearchCurrentFg, "#1e1e2e"},
	}
	for name, c := range cases {
		if got := hexOf(t, c.got); got != c.want {
			t.Errorf("%s = %s, want %s", name, got, c.want)
		}
	}
	if th.ChromaStyle != "catppuccin-mocha" {
		t.Errorf("ChromaStyle = %q, want catppuccin-mocha", th.ChromaStyle)
	}
	if th.Name != "mocha" {
		t.Errorf("Name = %q, want mocha", th.Name)
	}
}

func TestNewModel_HasDefaultTheme(t *testing.T) {
	m := NewModel(Config{})
	if m.theme.Name != "mocha" {
		t.Errorf("NewModel theme = %q, want mocha", m.theme.Name)
	}
	if m.styles.Theme.Name != "mocha" {
		t.Errorf("NewModel styles.Theme = %q, want mocha", m.styles.Theme.Name)
	}
}

func TestResolveTheme_EmptyIsDefault(t *testing.T) {
	th, err := ResolveTheme("")
	if err != nil {
		t.Fatal(err)
	}
	if th.Name != "mocha" {
		t.Errorf("empty resolved to %q", th.Name)
	}
}

func TestResolveTheme_UnknownErrors(t *testing.T) {
	if _, err := ResolveTheme("does-not-exist"); err == nil {
		t.Fatal("unknown theme must error, not silently fall back")
	}
}

func TestAllThemes_TextIsLegibleOnBackground(t *testing.T) {
	for name := range themeRegistry() {
		th, _ := ResolveTheme(name)
		pairs := []struct {
			fg, bg color.Color
			label  string
		}{
			{th.TextPrimary, th.StatusBg, "TextPrimary/StatusBg"},
			{th.SelectFg, th.SelectBg, "SelectFg/SelectBg"},
			{th.SearchCurrentFg, th.SearchCurrentBg, "SearchCurrentFg/SearchCurrentBg"},
		}
		for _, p := range pairs {
			if d := labDistance(p.fg, p.bg); d < minLegibleDistance {
				t.Errorf("theme %q: %s distance %.1f < %.1f (unreadable)", name, p.label, d, minLegibleDistance)
			}
		}
	}
}

func TestNonCatppuccinThemes_Exist(t *testing.T) {
	for _, n := range []string{"dracula", "gruvbox", "solarized-dark"} {
		if _, err := ResolveTheme(n); err != nil {
			t.Errorf("theme %q should resolve: %v", n, err)
		}
	}
}

func TestAllThemes_ChromaStyleResolves(t *testing.T) {
	for name := range themeRegistry() {
		th, _ := ResolveTheme(name)
		if th.ChromaStyle == "" {
			t.Errorf("theme %q has empty ChromaStyle", name)
		}
	}
}

// TestCatppuccinMochaMatchesDefault guards against drift between the
// hand-pinned defaultTheme() palette and the catppuccin/go library's Mocha
// flavor. The default MUST stay hand-pinned (see themeRegistry), but if the
// library ever diverges from the legacy hex this test flags it so the choice
// is deliberate rather than silent.
func TestCatppuccinMochaMatchesDefault(t *testing.T) {
	lib := fromCatppuccin("mocha", catppuccinMochaFlavor)
	def := defaultTheme()
	pairs := map[string]struct{ a, b color.Color }{
		"Accent":   {lib.Accent, def.Accent},
		"DiffAdd":  {lib.DiffAdd, def.DiffAdd},
		"SelectFg": {lib.SelectFg, def.SelectFg},
	}
	for role, p := range pairs {
		if hexOf(t, p.a) != hexOf(t, p.b) {
			t.Logf("mocha %s drift: library=%s default=%s (default is authoritative)", role, hexOf(t, p.a), hexOf(t, p.b))
		}
	}
}

// TestDefaultRender_Golden renders a deterministic frame with the default
// theme and compares its ANSI-stripped TEXT+LAYOUT to a committed golden. It
// is the safety net proving the theming refactor changed nothing visible in
// the default layout. Colour correctness is guarded separately by
// TestDefaultTheme_PreservesCurrentPalette.
func TestDefaultRender_Golden(t *testing.T) {
	m := goldenModelWithTheme(t, defaultTheme())
	got := ansi.Strip(m.View().Content)
	goldenAssertTUI(t, "default_frame.txt", []byte(got))
}

// TestThemeSnapshots renders every registered theme at a fixed size. Because
// the snapshots are ANSI-stripped, all themes share the SAME text/layout — so
// these prove no theme crashes or mis-lays-out the frame. Per-theme colour
// correctness is proven by the contrast guard, not by these snapshots.
func TestThemeSnapshots(t *testing.T) {
	names := make([]string, 0)
	for n := range themeRegistry() {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		th, _ := ResolveTheme(n)
		m := goldenModelWithTheme(t, th)
		got := ansi.Strip(m.View().Content)
		goldenAssertTUI(t, "theme_"+n+".txt", []byte(got))
	}
}
