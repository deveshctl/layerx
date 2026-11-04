package tui

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/deveshctl/layerx/image"
	"github.com/deveshctl/layerx/internal/sizebudget"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type compareTestResolver struct {
	image.Resolver
	started chan struct{}
	cleaned chan struct{}
	block   bool
}

func (r *compareTestResolver) ImageID(context.Context, string) (string, error) { return "", nil }
func (r *compareTestResolver) Inspect(context.Context, string) (*image.ImageMeta, error) {
	return &image.ImageMeta{}, nil
}
func (r *compareTestResolver) ResolveWithProgress(ctx context.Context, _ string, _ chan<- image.ProgressEvent) ([]image.Layer, error) {
	defer close(r.cleaned)
	close(r.started)
	if r.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return nil, nil
}

func compareTestConfig(t *testing.T, block bool) (CompareConfig, [2]*compareTestResolver) {
	t.Helper()
	cfg := CompareConfig{Options: image.CompareRunOptions{CacheRoot: t.TempDir()}}
	var resolvers [2]*compareTestResolver
	for side := range resolvers {
		resolvers[side] = &compareTestResolver{started: make(chan struct{}), cleaned: make(chan struct{}), block: block}
		cfg.Inputs[side] = image.CompareInput{Ref: "image", Resolver: resolvers[side]}
	}
	return cfg, resolvers
}

func TestCompareSessionQuitCancelsAndJoins(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: 'q'}, {Code: 'c', Mod: tea.ModCtrl}} {
		cfg, resolvers := compareTestConfig(t, true)
		session := newCompareSession(t.Context(), cfg)
		t.Cleanup(session.close)
		session.start()
		for _, resolver := range resolvers {
			select {
			case <-resolver.started:
			case <-time.After(5 * time.Second):
				t.Fatal("worker did not start")
			}
		}
		m := compareModel{session: session, keys: defaultKeys()}
		_, cmd := m.Update(key)
		require.NotNil(t, cmd)
		assert.IsType(t, tea.QuitMsg{}, cmd())
		require.ErrorIs(t, session.ctx.Err(), context.Canceled)
		session.close()
		for _, resolver := range resolvers {
			select {
			case <-resolver.cleaned:
			default:
				t.Fatal("session closed before cleanup")
			}
		}
	}
}

func TestCompareSessionCloseBeforeInit(t *testing.T) {
	cfg, resolvers := compareTestConfig(t, true)
	session := newCompareSession(t.Context(), cfg)
	session.close()
	session.start() // A late command cannot restart analysis after cleanup.
	require.ErrorIs(t, session.err, context.Canceled)
	for _, resolver := range resolvers {
		select {
		case <-resolver.started:
			t.Fatal("closed session started resolver")
		default:
		}
	}
}

func TestRunCompareAnalysisCancelledBeforeProgram(t *testing.T) {
	cfg, resolvers := compareTestConfig(t, true)
	cfg.Output = io.Discard
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	results, err := RunCompareAnalysis(ctx, cfg)
	require.Error(t, err)
	assert.Equal(t, [2]image.CompareImage{}, results)
	for _, resolver := range resolvers {
		select {
		case <-resolver.started:
			t.Fatal("cancelled program started resolver")
		default:
		}
	}
}

func TestCompareLoadingProgramCompletes(t *testing.T) {
	cfg, _ := compareTestConfig(t, false)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	session := newCompareSession(ctx, cfg)
	defer session.close()
	m := compareModel{session: session, keys: defaultKeys(), theme: themeFor(""), width: 80, height: 24}
	input, output := io.Pipe()
	defer input.Close()
	go func() { <-session.done; _, _ = output.Write([]byte("q")); _ = output.Close() }()
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(io.Discard),
		tea.WithoutRenderer(), tea.WithoutSignalHandler())
	_, err := p.Run()
	require.NoError(t, err)
	<-session.done
	require.NoError(t, session.err)
	for _, result := range session.results {
		require.NotNil(t, result.Analysis)
	}
}

func TestCompareProgressRendering(t *testing.T) {
	assert.Equal(t, "pulling (total unknown)", compareProgressText(image.CompareSideProgress{
		Event: image.ProgressEvent{Phase: image.PhasePulling},
	}))
	assert.Contains(t, compareProgressText(image.CompareSideProgress{
		Event: image.ProgressEvent{Phase: image.PhasePulling, BytesCurr: 20, BytesTotal: 10},
	}), "100%")
	assert.Equal(t, "ready", compareProgressText(image.CompareSideProgress{Ready: true}))
	cfg, _ := compareTestConfig(t, false)
	cfg.Inputs[0].Ref = "hostile\x1b[2J\nname"
	session := newCompareSession(t.Context(), cfg)
	defer session.close()
	m := compareModel{session: session, theme: themeFor(""), width: 48, height: 8}
	out := m.View().Content
	assert.NotContains(t, out, "\x1b[2J")
	for _, line := range strings.Split(out, "\n") {
		assert.LessOrEqual(t, ansi.StringWidth(line), 48)
	}
	// Tiny or not-yet-sized terminals must not panic.
	m.width, m.height = 0, 0
	assert.NotPanics(t, func() { m.View() })
}

func TestCompareBrowserPairsAndNavigation(t *testing.T) {
	before := &image.Analysis{Layers: []image.Layer{
		{Index: 0, DiffID: "a", Command: "base"},
		{Index: 1, DiffID: "b", Command: "build"},
		{Index: 2, Command: "only A"},
	}}
	after := &image.Analysis{Layers: []image.Layer{
		{Index: 0, DiffID: "b", Command: "build"},
		{Index: 1, DiffID: "a", Command: "base"},
		{Index: 2, Command: "only B"},
	}}
	m := compareModel{result: image.CompareAnalysis(before, after), keys: defaultKeys(), theme: themeFor(""), width: 100, height: 12}
	out := m.View().Content
	assert.Contains(t, out, "shared DiffID")
	assert.Contains(t, out, "#2 \"build\"")
	assert.Contains(t, out, "— absent")
	assert.Equal(t, 1, m.result.LayerDiffs[0].BeforeIndex)
	m.height = 8
	for range 30 {
		next, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
		m = next.(compareModel)
	}
	assert.Equal(t, len(m.result.LayerDiffs)-1, m.cursor)
	assert.Greater(t, m.offset, 0)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 30, Height: 5})
	m = next.(compareModel)
	for _, line := range strings.Split(m.View().Content, "\n") {
		assert.LessOrEqual(t, ansi.StringWidth(line), 30)
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: 'g'})
	m = next.(compareModel)
	assert.Zero(t, m.cursor)
	assert.Zero(t, m.offset)
}

func TestCompareBrowserEmptyAndAsymmetric(t *testing.T) {
	m := compareModel{result: image.CompareAnalysis(&image.Analysis{}, &image.Analysis{}), theme: themeFor(""), width: 40, height: 10}
	assert.Contains(t, m.View().Content, "No layers")
	before := &image.Analysis{Layers: []image.Layer{{Command: "old"}}}
	after := &image.Analysis{Layers: make([]image.Layer, 25)}
	for i := range after.Layers {
		after.Layers[i].Command = "new"
	}
	m.result = image.CompareAnalysis(before, after)
	assert.Len(t, m.result.LayerDiffs, 26)
	assert.Equal(t, "— absent", compareLayerNumber(m.result.LayerDiffs[0].BeforeIndex))
	assert.Equal(t, "command match?", compareMatch("command"))
}

func TestCompareBrowserShowsBudgetAndKeepsFooter(t *testing.T) {
	m := compareModel{
		result: image.CompareAnalysis(&image.Analysis{}, &image.Analysis{}),
		checks: []sizebudget.Result{{Name: "image size", ActualBytes: 20, LimitBytes: 10, Limit: "10B"}},
		keys:   defaultKeys(), theme: themeFor(""), width: 80, height: 12,
	}
	out := m.View().Content
	assert.Contains(t, ansi.Strip(out), "Verdict: REGRESSION")
	assert.Contains(t, out, "FAIL image size")
	next, _ := m.Update(tea.KeyPressMsg{Code: '?'})
	m = next.(compareModel)
	assert.Contains(t, m.View().Content, "Ctrl+u/d")
	m.height = 3
	assert.Contains(t, m.View().Content, "back")
}

func TestCompareFrameKeepsIdentityAndSelectionVisible(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		m := compareModel{result: image.CompareAnalysis(&image.Analysis{ImageRef: "old:1"}, &image.Analysis{ImageRef: "new:2", Layers: make([]image.Layer, 30)}), keys: defaultKeys(), theme: themeFor(""), width: width, height: 24}
		next, _ := m.Update(tea.KeyPressMsg{Code: 'G'})
		m = next.(compareModel)
		out := ansi.Strip(m.View().Content)
		assert.Contains(t, out, "layerx")
		assert.Contains(t, out, "old:1")
		assert.Contains(t, out, "new:2")
		assert.Contains(t, out, "#30")
		assert.Len(t, strings.Split(out, "\n"), 24)
		for _, line := range strings.Split(out, "\n") {
			assert.LessOrEqual(t, ansi.StringWidth(line), width)
		}
	}
}
