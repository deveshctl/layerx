package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/image"
)

func (m model) handleWasteDetail(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Copy before mutation: Update's value receiver must not mutate an older
	// model's detail state through a shared pointer.
	detail := *m.wasteDetail
	m.wasteDetail = &detail
	count := len(detail.explanation.Events)
	switch {
	case key.Matches(msg, m.keys.Down):
		detail.cursor = min(detail.cursor+1, max(count-1, 0))
	case key.Matches(msg, m.keys.Up):
		detail.cursor = max(detail.cursor-1, 0)
	case key.Matches(msg, m.keys.Top):
		detail.cursor = 0
	case key.Matches(msg, m.keys.Bottom):
		detail.cursor = max(count-1, 0)
	case key.Matches(msg, m.keys.PageDown), key.Matches(msg, m.keys.HalfPageDown):
		detail.cursor = min(detail.cursor+max(m.height-12, 1), max(count-1, 0))
	case key.Matches(msg, m.keys.PageUp), key.Matches(msg, m.keys.HalfPageUp):
		detail.cursor = max(detail.cursor-max(m.height-12, 1), 0)
	case msg.Code == tea.KeyEnter:
		row := detail.row
		if count > 0 {
			row.IntroLayer = detail.explanation.Events[detail.cursor].Layer
		}
		return m.wasteJump(row)
	case key.Matches(msg, m.keys.CopyPath):
		m.copyConfirm = true
		return m, tea.Batch(tea.SetClipboard(detail.row.Path),
			tea.Tick(2*time.Second, func(time.Time) tea.Msg { return clearCopyMsg{} }))
	case key.Matches(msg, m.keys.Copy):
		if count > 0 {
			m.copyConfirm = true
			return m, tea.Batch(tea.SetClipboard(detail.explanation.Events[detail.cursor].Command),
				tea.Tick(2*time.Second, func(time.Time) tea.Msg { return clearCopyMsg{} }))
		}
	}
	return m, nil
}

func (m model) renderWasteDetail() string {
	detail := m.wasteDetail
	width := max(min(m.width-6, 88), 0)
	dim := m.styles.metaDim
	lines := []string{m.styles.accent.Render(truncateMid(detail.row.Path, width))}
	footer := "j/k history · Enter jump · c cmd · Esc back"
	title := "Explain layer waste"
	if detail.row.CleanupReason != "" {
		title = "Potential cleanup"
		lines = append(lines,
			image.FormatBytes(detail.row.Wasted)+" live bytes (uncompressed)",
			detail.row.CleanupReason,
			dim.Render("Review whether this cache is needed at runtime."),
			dim.Render("Clean it in the same build step, or use a cache mount."),
			dim.Render("Deleting it in a later layer does not shrink stored layers."))
		footer = "Enter jump · y copy · Esc back"
	} else {
		lines = append(lines, image.FormatBytes(detail.explanation.TotalWasted)+" wasted (uncompressed payload)")
		events := detail.explanation.Events
		if len(events) == 0 {
			lines = append(lines, dim.Render("Layer history unavailable for this path."))
		} else {
			// Reserve space for the path, total, selected command/reason and
			// persistent footer. Derive the window each render so resize is safe.
			commandRows := min(max(m.height-13, 1), 3)
			visible := max(m.height-10-commandRows, 1)
			start := max(detail.cursor-visible+1, 0)
			end := min(start+visible, len(events))
			for i := start; i < end; i++ {
				event := events[i]
				change := "written"
				switch event.Change {
				case image.Added:
					change = "added"
				case image.Modified:
					change = "modified"
				case image.Removed:
					change = "deleted"
				}
				line := fmt.Sprintf("  L%d %-8s %s · waste %s", event.Layer+1,
					change, image.FormatBytes(event.Size), image.FormatBytes(event.WastedBytes))
				if i == detail.cursor {
					line = ">" + line[1:]
					line = m.styles.selectedTreeBg.Render(ansi.Truncate(line, width, "…"))
				}
				lines = append(lines, line)
			}
			selected := events[detail.cursor]
			command := strings.Join(strings.Fields(ansi.Strip(selected.Command)), " ")
			if command == "" {
				command = "(command unavailable)"
			}
			lines = append(lines, dim.Render(fmt.Sprintf("History %d/%d · %s", detail.cursor+1, len(events), selected.Reason)))
			lines = append(lines, wrapCommandLines(command, width, commandRows)...)
		}
	}
	if m.copyConfirm {
		footer = "Copied! · Esc back"
	}
	lines = append(lines, footer)
	panel := renderPanel(m.theme, strings.Join(lines, "\n"), title, true,
		width, min(len(lines), max(m.height-4, 0)), false, false)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}
