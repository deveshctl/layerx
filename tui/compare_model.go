package tui

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/image"
)

// CompareConfig supplies streams explicitly so the comparison UI never writes
// progress to a process-global stderr. After loading, the caller receives the
// analyses for the comparison report.
type CompareConfig struct {
	Inputs  [2]image.CompareInput
	Options image.CompareRunOptions
	Theme   string
	Input   io.Reader
	Output  io.Writer
}

// compareSession owns synchronization outside the value-based Bubble Tea model.
// start is also called during cleanup: if the program exits before Init's
// command executes, the cancelled runner still completes and is joined safely.
type compareSession struct {
	ctx     context.Context
	cancel  context.CancelFunc
	once    sync.Once
	done    chan struct{}
	inputs  [2]image.CompareInput
	opts    image.CompareRunOptions
	results [2]image.CompareImage
	err     error
}

func newCompareSession(ctx context.Context, cfg CompareConfig) *compareSession {
	ctx, cancel := context.WithCancel(ctx)
	opts := cfg.Options
	opts.Progress = &image.CompareProgress{}
	return &compareSession{ctx: ctx, cancel: cancel, done: make(chan struct{}), inputs: cfg.Inputs, opts: opts}
}

func (s *compareSession) start() {
	s.once.Do(func() {
		go func() {
			defer close(s.done)
			s.results, s.err = image.AnalyzePair(s.ctx, s.inputs, s.opts)
		}()
	})
}

func (s *compareSession) close() {
	s.cancel()
	s.start()
	<-s.done
}

type compareLoadedMsg struct{}
type compareTickMsg struct{}

type compareModel struct {
	session       *compareSession
	keys          keyMap
	theme         Theme
	progress      [2]image.CompareSideProgress
	width, height int
	frame         int
}

func (m compareModel) Init() tea.Cmd {
	return tea.Batch(func() tea.Msg {
		m.session.start()
		<-m.session.done
		return compareLoadedMsg{}
	}, compareTick())
}

func compareTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return compareTickMsg{} })
}

func (m compareModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		if key.Matches(msg, m.keys.Quit) {
			m.session.cancel()
			return m, tea.Quit
		}
	case compareLoadedMsg:
		return m, tea.Quit
	case compareTickMsg:
		m.progress = m.session.opts.Progress.Snapshot()
		m.frame++
		return m, compareTick()
	}
	return m, nil
}

func (m compareModel) View() tea.View {
	lines := []string{styleWithFg(m.theme.FocusedBorder).Bold(true).Render("Compare images"), ""}
	for side, input := range m.session.inputs {
		status := compareProgressText(m.progress[side])
		spinner := string("|/-\\"[m.frame%4])
		if m.progress[side].Ready {
			spinner = "✓"
		}
		lines = append(lines, fmt.Sprintf("Image %c %s %s: %s", 'A'+side, spinner, strconv.Quote(input.Ref), status))
		if warning := m.progress[side].Warning; warning != "" {
			lines = append(lines, "  Warning: "+strconv.Quote(warning))
		}
	}
	lines = append(lines, "", "q / Ctrl+C cancel")
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(0, m.width), "…")
	}
	lines = lines[:min(len(lines), max(0, m.height))]
	return finalizeViewNoMouse(tea.NewView(strings.Join(lines, "\n")), m.theme.MainBg)
}

func compareProgressText(p image.CompareSideProgress) string {
	if p.Ready {
		return "ready"
	}
	switch p.Event.Phase {
	case image.PhasePulling:
		if p.Event.BytesTotal > 0 {
			percent := min(100.0, max(0.0, 100*float64(p.Event.BytesCurr)/float64(p.Event.BytesTotal)))
			return fmt.Sprintf("pulling %.0f%% (%s / %s)", percent, image.FormatBytes(p.Event.BytesCurr), image.FormatBytes(p.Event.BytesTotal))
		}
		return "pulling (total unknown)"
	case image.PhaseExporting:
		return "exporting"
	case image.PhaseParsing:
		return "parsing / indexing"
	case image.PhaseCacheLoad:
		return "indexing cached layers"
	default:
		return "resolving"
	}
}

// RunCompareAnalysis cancels and joins analysis even if terminal setup fails or
// the program exits before its first command runs. The parent context controls
// the program; the child work context can be cancelled independently by keys.
func RunCompareAnalysis(ctx context.Context, cfg CompareConfig) ([2]image.CompareImage, error) {
	session := newCompareSession(ctx, cfg)
	defer session.close()
	m := compareModel{session: session, keys: defaultKeys(), theme: themeFor(cfg.Theme), width: 80, height: 24}
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(cfg.Input), tea.WithOutput(cfg.Output))
	if _, err := p.Run(); err != nil {
		return [2]image.CompareImage{}, err
	}
	if err := session.ctx.Err(); err != nil {
		return [2]image.CompareImage{}, err
	}
	// The normal completion message is published only after done is closed.
	// Joining also covers external Quit messages without that completion message.
	select {
	case <-session.done:
		return session.results, session.err
	default:
		return [2]image.CompareImage{}, context.Canceled
	}
}
