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
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(io.Discard),
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
