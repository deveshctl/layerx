package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// treePaneRows is shared by drawing and navigation. On short panes, omit
// the column header and divider to leave room for a file and the filter.
func treePaneRows(height int, showHeader, showFilter bool) (header, files int) {
	filter := 0
	if showFilter {
		filter = 1
	}
	if showHeader && height-filter >= 3 {
		header = 2
	}
	return header, max(height-header-filter, 0)
}

func splitTreeFits(height int, showFilter bool) bool {
	minimum := 7 // two headers, two file rows, and the split divider
	if showFilter {
		minimum = 9 // allow the filter in either half when focus changes
	}
	return height >= minimum
}

type statusHint struct{ key, desc string }

func renderStatusLine(st themeStyles, hints []statusHint, right string, width int) string {
	// Always leave room for help/exit hints, even during a long status message.
	right = ansi.Truncate(right, max(width*2/3, 0), "…")
	budget := max(width-lipgloss.Width(right)-1, 0)
	render := func(compact bool) string {
		parts := make([]string, 0, len(hints))
		for _, h := range hints {
			part := st.statusKey.Render(h.key)
			if !compact || h.key == "Enter" || h.key == "Esc" || h.key == "?" || h.key == "/" {
				part += " " + st.statusDim.Render(h.desc)
			}
			parts = append(parts, part)
		}
		sep := " "
		if !compact {
			sep = " " + st.headerSep.Render("│") + " "
		}
		return " " + strings.Join(parts, sep)
	}
	left := render(false)
	if lipgloss.Width(left) > budget {
		left = render(true)
		for i := len(hints) - 1; i >= 0 && lipgloss.Width(left) > budget; i-- {
			if hints[i].key == "?" || hints[i].key == "q" || hints[i].key == "Esc" {
				continue
			}
			hints = append(hints[:i:i], hints[i+1:]...)
			left = render(true)
		}
	}
	left = ansi.Truncate(left, budget, "…")
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 0)
	return st.bgOnly.Render(left + strings.Repeat(" ", gap) + right)
}

// renderQueryBar keeps the end of active input and its caret visible. The
// stored query is untouched; confirmed queries retain their leading context.
func renderQueryBar(st themeStyles, query string, active bool, counter, hint string, width int) string {
	prefix := st.accent.Render("/ ")
	caret := ""
	if active {
		caret = st.selected.Render("█")
		hint = ""
	}
	if lipgloss.Width(prefix+query+caret+counter+hint) > width {
		hint = ""
	}
	available := width - lipgloss.Width(prefix+caret+counter+hint)
	if available < 8 {
		counter = ""
		available = width - lipgloss.Width(prefix+caret)
	}
	available = max(available, 0)
	if active && ansi.StringWidth(query) > available {
		end := ansi.StringWidth(query)
		query = "…" + ansi.Cut(query, end-max(available-1, 0), end)
	} else {
		query = ansi.Truncate(query, available, "…")
	}
	return ansi.Truncate(prefix+st.selected.Render(query)+caret+counter+hint, max(width, 0), "")
}
