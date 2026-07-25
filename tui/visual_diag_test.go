package tui

// Diagnostic-only: renders every theme's ready-state View() to a PNG so colour,
// contrast, alignment, and border problems are visible to a human (and to a
// reviewer reading the image) — something the ANSI-stripped golden test cannot
// show. Run with -visual to write testdata/visual/*.png; the test is a no-op
// otherwise so it never gates CI.
//
// Pipeline: View() -> ANSI frame -> cell grid (rune+fg+bg) -> image.RGBA drawn
// with an embedded DejaVu Sans Mono TTF -> PNG. The font is test-only (go:embed
// here); it never ships in the binary.
//
// This file and its testdata are safe to delete once the theming visuals are
// dialed in.

import (
	_ "embed"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	dimg "github.com/deveshctl/layerx/image"
)

//go:embed testdata/fonts/DejaVuSansMono.ttf
var monoTTF []byte

var writeVisual = flag.Bool("visual", false, "write tui visual diagnostic PNGs to testdata/visual")

// cell is one terminal grid position: its rune and the colours active when it
// printed. Unset fg/bg (nil) means "terminal default" and is filled with the
// theme's canvas colour so the PNG matches what a real terminal shows.
type cell struct {
	r  rune
	fg color.Color
	bg color.Color
}

type gridState struct {
	fg, bg color.Color
}

// parseANSIGrid turns an ANSI frame into a rectangular cell grid, interpreting
// truecolor SGR (38;2 / 48;2) and reset (0). Rows are padded to the widest row
// so the image is rectangular.
func parseANSIGrid(frame string) [][]cell {
	var grid [][]cell
	for _, line := range strings.Split(frame, "\n") {
		st := gridState{}
		var row []cell
		i := 0
		for i < len(line) {
			if line[i] == 0x1b && i+1 < len(line) && line[i+1] == '[' {
				j := i + 2
				for j < len(line) && line[j] != 'm' {
					j++
				}
				if j < len(line) {
					applyGridSGR(&st, line[i+2:j])
					i = j + 1
					continue
				}
			}
			// Decode one UTF-8 rune so box-drawing glyphs map to a single cell.
			r, size := utf8.DecodeRuneInString(line[i:])
			if size == 0 {
				size = 1
			}
			row = append(row, cell{r: r, fg: st.fg, bg: st.bg})
			i += size
		}
		grid = append(grid, row)
	}
	maxW := 0
	for _, row := range grid {
		if len(row) > maxW {
			maxW = len(row)
		}
	}
	for i := range grid {
		for len(grid[i]) < maxW {
			grid[i] = append(grid[i], cell{r: ' '})
		}
	}
	return grid
}

func applyGridSGR(st *gridState, params string) {
	fields := strings.Split(params, ";")
	for k := 0; k < len(fields); k++ {
		switch fields[k] {
		case "0", "":
			*st = gridState{}
		case "38", "48":
			if k+4 < len(fields) && fields[k+1] == "2" {
				c := color.RGBA{atoiByte(fields[k+2]), atoiByte(fields[k+3]), atoiByte(fields[k+4]), 0xff}
				if fields[k] == "38" {
					st.fg = c
				} else {
					st.bg = c
				}
				k += 4
			}
		}
	}
}

func atoiByte(s string) uint8 {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	if n > 255 {
		n = 255
	}
	return uint8(n)
}

// renderGridPNG rasterizes the cell grid at cellW x cellH per cell. defaultBg/
// defaultFg fill cells whose SGR left the colour unset.
func renderGridPNG(grid [][]cell, face font.Face, cellW, cellH int, ascent fixed.Int26_6, defaultBg, defaultFg color.Color) *image.RGBA {
	rows := len(grid)
	cols := 0
	if rows > 0 {
		cols = len(grid[0])
	}
	img := image.NewRGBA(image.Rect(0, 0, cols*cellW, rows*cellH))
	fillRect(img, img.Bounds(), defaultBg)

	drawer := &font.Drawer{Dst: img, Face: face}
	for y, row := range grid {
		for x, c := range row {
			bg := c.bg
			if bg == nil {
				bg = defaultBg
			}
			fillRect(img, image.Rect(x*cellW, y*cellH, (x+1)*cellW, (y+1)*cellH), bg)

			if c.r == ' ' || c.r == 0 {
				continue
			}
			fg := c.fg
			if fg == nil {
				fg = defaultFg
			}
			drawer.Src = image.NewUniform(fg)
			drawer.Dot = fixed.Point26_6{X: fixed.I(x * cellW), Y: fixed.I(y*cellH) + ascent}
			drawer.DrawString(string(c.r))
		}
	}
	return img
}

func fillRect(img *image.RGBA, r image.Rectangle, c color.Color) {
	rc := color.RGBAModel.Convert(c).(color.RGBA)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, rc)
		}
	}
}

// TestVisualDiagnostic writes several PNG frames per theme so colour, contrast,
// and alignment can be reviewed across the main screens (list+selection,
// file viewer, split view) and at a narrow width. No-op without -visual.
func TestVisualDiagnostic(t *testing.T) {
	if !*writeVisual {
		t.Skip("run with -visual to write testdata/visual PNGs")
	}

	ft, err := opentype.Parse(monoTTF)
	if err != nil {
		t.Fatalf("parse font: %v", err)
	}
	face, err := opentype.NewFace(ft, &opentype.FaceOptions{Size: 16, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		t.Fatalf("new face: %v", err)
	}
	metrics := face.Metrics()
	cellH := (metrics.Ascent + metrics.Descent).Ceil()
	advance, ok := face.GlyphAdvance('M')
	if !ok {
		t.Fatal("font has no 'M' advance")
	}
	cellW := advance.Ceil()
	ascent := metrics.Ascent

	dir := filepath.Join("testdata", "visual")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	write := func(name string, m model) {
		grid := parseANSIGrid(m.View().Content)
		img := renderGridPNG(grid, face, cellW, cellH, ascent, m.theme.RootBg, m.theme.TextPrimary)
		path := filepath.Join(dir, name+".png")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			t.Fatal(err)
		}
		f.Close()
		fmt.Printf("wrote %s (%dx%d px)\n", path, img.Bounds().Dx(), img.Bounds().Dy())
	}

	names := make([]string, 0)
	for n := range themeRegistry() {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		th, _ := ResolveTheme(n)

		// Frame 1: tree focused, selection + diff glyphs, layers list.
		m1 := goldenModelWithTheme(t, th)
		m1.analysis = testAnalysisWithDiffs()
		m1.focus = focusTree
		m1.layerCursor = len(m1.analysis.Layers) - 1
		m1.clampCursors()
		write("theme_"+n, m1)

		// Frame 2: layers panel focused (selection on the left list).
		m2 := goldenModelWithTheme(t, th)
		m2.analysis = testAnalysisWithDiffs()
		m2.focus = focusLayers
		m2.layerCursor = 1
		m2.clampCursors()
		write("theme_"+n+"_layers", m2)

		// Frame 3: split / aggregated view (both sub-panes + divider).
		m3 := goldenModelWithTheme(t, th)
		m3.analysis = testAnalysisWithDiffs()
		m3.aggregated = true
		m3.focus = focusTree
		m3.layerCursor = len(m3.analysis.Layers) - 1
		m3.clampCursors()
		write("theme_"+n+"_split", m3)

		// Frame 4: file viewer open on a source file (syntax highlighting +
		// viewer status bar).
		m4 := goldenModelWithTheme(t, th)
		m4.analysis = testAnalysisWithDiffs()
		m4.viewState = viewReady
		m4.viewContent = &dimg.FileContent{
			Path: "/etc/nginx.conf",
			Data: []byte("user nginx;\nworker_processes auto;\n\nevents {\n    worker_connections 1024;\n}\n\nhttp {\n    include /etc/nginx/mime.types;\n    sendfile on;\n    server {\n        listen 80;\n        server_name localhost;\n        location / {\n            root /usr/share/nginx/html;\n        }\n    }\n}\n"),
		}
		m4.viewOriginLayer = 1
		m4.viewOriginCmd = "RUN apt-get install -y nginx"
		write("theme_"+n+"_viewer", m4)

		// Frame 5: narrow width, to catch status-bar overflow / truncation.
		m5 := goldenModelWithTheme(t, th)
		m5.analysis = testAnalysisWithDiffs()
		m5.width = 80
		m5.focus = focusTree
		m5.layerCursor = len(m5.analysis.Layers) - 1
		m5.clampCursors()
		write("theme_"+n+"_narrow", m5)
	}
}
