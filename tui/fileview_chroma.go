package tui

import (
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
)

var (
	chromaInit  sync.Once
	chromaFmt   chroma.Formatter
	chromaStyle *chroma.Style
)

func initChroma() {
	chromaInit.Do(func() {
		chromaFmt = formatters.Get("terminal256")
		chromaStyle = chromastyles.Get("monokai")
	})
}

// highlightFileLines returns syntax-highlighted lines for path/data, or nil if
// highlighting is unavailable.
func highlightFileLines(path string, data []byte) []string {
	initChroma()
	if chromaFmt == nil || chromaStyle == nil {
		return nil
	}

	lexer := lexers.Match(path)
	if lexer == nil {
		lexer = lexers.Analyse(string(data))
	}
	if lexer == nil {
		return nil
	}

	lexer = chroma.Coalesce(lexer)
	src := strings.ReplaceAll(string(data), "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "")
	it, err := lexer.Tokenise(nil, src)
	if err != nil {
		return nil
	}

	var buf strings.Builder
	if err := chromaFmt.Format(&buf, chromaStyle, it); err != nil {
		return nil
	}

	out := strings.TrimSuffix(buf.String(), "\n")
	var highlighted []string
	if out != "" {
		highlighted = strings.Split(out, "\n")
	}
	if len(highlighted) != len(splitFileLines(data)) {
		// Length mismatch would desync search/jump indexing against
		// splitFileLines. Drop highlighting; the non-highlighted path
		// will render the same lines without color.
		return nil
	}
	if highlighted == nil {
		return []string{""}
	}
	return highlighted
}
