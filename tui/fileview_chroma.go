package tui

import (
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
)

var (
	chromaFmtOnce sync.Once
	chromaFmt     chroma.Formatter
)

// chromaStyleFor resolves a chroma style by name, falling back to "monokai"
// when the name is not registered. The formatter is initialized once (it is
// stateless); the style is resolved per-call so theme changes are honoured.
func chromaStyleFor(styleName string) *chroma.Style {
	chromaFmtOnce.Do(func() {
		chromaFmt = formatters.Get("terminal256")
	})
	if s := chromastyles.Get(styleName); s != nil {
		return s
	}
	return chromastyles.Get("monokai")
}

// highlightFileCmd wraps highlightFileLines in a tea.Cmd so the bubbletea
// runtime can run tokenisation off the Update goroutine. The returned
// message carries requestID so the receiver can discard a stale highlight
// when the user has navigated to a different file in the meantime.
func highlightFileCmd(requestID uint64, path string, data []byte, chromaStyleName string) tea.Cmd {
	// Snapshot the input — Cmds run after Update returns and the underlying
	// FileContent.Data slice is part of model state that another extract
	// could swap out before this Cmd executes.
	pathCopy := path
	dataCopy := append([]byte(nil), data...)
	return func() tea.Msg {
		return highlightedMsg{
			requestID: requestID,
			lines:     highlightFileLines(pathCopy, dataCopy, chromaStyleName),
		}
	}
}

func highlightFileLines(path string, data []byte, styleName string) []string {
	style := chromaStyleFor(styleName)
	if chromaFmt == nil || style == nil {
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
	if err := chromaFmt.Format(&buf, style, it); err != nil {
		return nil
	}

	// Match splitFileLines' empty-input contract: whitespace-only data
	// (or empty data) splits to a nil slice, not []string{""}. Returning
	// the latter would desync line counts against the non-highlighted
	// renderer the moment the file viewer hits an empty/whitespace blob.
	plain := splitFileLines(data)
	if len(plain) == 0 {
		return nil
	}

	out := strings.TrimSuffix(buf.String(), "\n")
	var highlighted []string
	if out != "" {
		highlighted = strings.Split(out, "\n")
	}
	if len(highlighted) != len(plain) {
		// Length mismatch would desync search/jump indexing against
		// splitFileLines. Drop highlighting; the non-highlighted path
		// will render the same lines without color.
		return nil
	}
	return highlighted
}
