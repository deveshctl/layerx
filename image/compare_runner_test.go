package image

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pairResolver struct {
	resolve func(context.Context, chan<- ProgressEvent) ([]Layer, error)
	digest  string
	idErr   error
	calls   atomic.Int32
}

func (r *pairResolver) Resolve(ctx context.Context, ref string) ([]Layer, error) {
	return r.ResolveWithProgress(ctx, ref, nil)
}

func (r *pairResolver) ResolveWithProgress(ctx context.Context, _ string, progress chan<- ProgressEvent) ([]Layer, error) {
	r.calls.Add(1)
	return r.resolve(ctx, progress)
}

func (r *pairResolver) ImageID(context.Context, string) (string, error) {
	return r.digest, r.idErr
}

func (r *pairResolver) Inspect(context.Context, string) (*ImageMeta, error) {
	return &ImageMeta{Platform: "linux/amd64"}, nil
}

func awaitPairSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for worker")
	}
}

func awaitPairError(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not finish")
		return nil
	}
}

func TestAnalyzePairCancellationJoinsBothWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var inputs [2]CompareInput
	started := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
	cleaned := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
	for side := range inputs {
		inputs[side] = CompareInput{Ref: "image", Resolver: &pairResolver{
			resolve: func(ctx context.Context, progress chan<- ProgressEvent) ([]Layer, error) {
				defer close(cleaned[side])
				close(started[side])
				for range 1000 {
					emitProgress(progress, ProgressEvent{Phase: PhasePulling})
				}
				<-ctx.Done()
				return nil, ctx.Err()
			},
		}}
	}
	done := make(chan error, 1)
	root := t.TempDir()
	go func() {
		_, err := AnalyzePair(ctx, inputs, CompareRunOptions{CacheRoot: root})
		done <- err
	}()
	for _, signal := range started {
		awaitPairSignal(t, signal)
	}
	cancel()
	require.ErrorIs(t, awaitPairError(t, done), context.Canceled)
	for _, signal := range cleaned {
		select {
		case <-signal:
		default:
			t.Fatal("runner returned before resolver cleanup")
		}
	}
}

func TestAnalyzePairSiblingFailureWaitsForCleanup(t *testing.T) {
	for failedSide := range 2 {
		t.Run(string(rune('A'+failedSide)), func(t *testing.T) {
			failure := errors.New("registry denied")
			started := make(chan struct{})
			cancelled := make(chan struct{})
			release := make(chan struct{})
			defer close(release)
			var inputs [2]CompareInput
			inputs[failedSide] = CompareInput{Ref: "bad", Resolver: &pairResolver{
				resolve: func(ctx context.Context, _ chan<- ProgressEvent) ([]Layer, error) {
					select {
					case <-started:
						return nil, failure
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				},
			}}
			inputs[1-failedSide] = CompareInput{Ref: "large", Resolver: &pairResolver{
				resolve: func(ctx context.Context, _ chan<- ProgressEvent) ([]Layer, error) {
					close(started)
					<-ctx.Done()
					close(cancelled)
					<-release // Simulate closing a stream and deleting temporary files.
					return nil, ctx.Err()
				},
			}}
			done := make(chan error, 1)
			root := t.TempDir()
			go func() {
				_, err := AnalyzePair(t.Context(), inputs, CompareRunOptions{CacheRoot: root})
				done <- err
			}()
			awaitPairSignal(t, cancelled)
			select {
			case err := <-done:
				t.Fatalf("returned before sibling cleanup: %v", err)
			default:
			}
			// Send releases the worker; deferred close also releases it on failure.
			release <- struct{}{}
			err := awaitPairError(t, done)
			require.ErrorIs(t, err, failure)
			assert.Contains(t, err.Error(), "bad")
			assert.NoError(t, t.Context().Err(), "must not cancel the caller's context")
		})
	}
}

func TestAnalyzePairAlreadyCancelledDoesNotResolve(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	results, err := AnalyzePair(ctx, [2]CompareInput{}, CompareRunOptions{})
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, [2]CompareImage{}, results)
}

func TestAnalyzePairSameDigestConcurrentCache(t *testing.T) {
	root := t.TempDir()
	digest := "sha256:" + strings.Repeat("a", 64)
	started := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
	release := make(chan struct{})
	defer close(release)
	var inputs [2]CompareInput
	var resolvers [2]*pairResolver
	for side := range inputs {
		resolvers[side] = &pairResolver{digest: digest,
			resolve: func(ctx context.Context, progress chan<- ProgressEvent) ([]Layer, error) {
				close(started[side])
				select {
				case <-release:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				emitProgress(progress, ProgressEvent{Phase: PhaseParsing})
				return []Layer{{Index: 0, DiffID: digest, Size: 128, UncompressedSize: 128,
					Tree: makeTree(makeFile("file", "/file", 12))}}, nil
			},
		}
		inputs[side] = CompareInput{Ref: "alias:" + string(rune('A'+side)), Resolver: resolvers[side]}
	}
	progress := &CompareProgress{}
	done := make(chan error, 1)
	var results [2]CompareImage
	go func() {
		var err error
		results, err = AnalyzePair(t.Context(), inputs, CompareRunOptions{CacheRoot: root, Progress: progress})
		done <- err
	}()
	// Both cold resolves must enter before either is allowed to finish.
	for _, signal := range started {
		awaitPairSignal(t, signal)
	}
	release <- struct{}{}
	release <- struct{}{}
	require.NoError(t, awaitPairError(t, done))
	for side, result := range results {
		require.NotNil(t, result.Analysis)
		assert.Equal(t, inputs[side].Ref, result.Analysis.ImageRef)
		assert.Equal(t, digest, result.Digest)
		assert.True(t, progress.Snapshot()[side].Ready)
		assert.Empty(t, progress.Snapshot()[side].Warning)
	}
	// Cache reads must produce independent trees even when the digest is shared.
	cached, err := AnalyzePair(t.Context(), inputs, CompareRunOptions{CacheRoot: root})
	require.NoError(t, err)
	for _, resolver := range resolvers {
		assert.Equal(t, int32(1), resolver.calls.Load())
	}
	cached[0].Analysis.Layers[0].Tree.Root.Children[0].Size = 999
	assert.Equal(t, int64(12), cached[1].Analysis.Layers[0].Tree.Root.Children[0].Size)
	temps, err := filepath.Glob(filepath.Join(root, strings.TrimPrefix(digest, "sha256:"), "*.tmp-*"))
	require.NoError(t, err)
	assert.Empty(t, temps)
}

func TestAnalyzePairDigestWarningAndProgress(t *testing.T) {
	progress := &CompareProgress{}
	var inputs [2]CompareInput
	for side := range inputs {
		inputs[side] = CompareInput{Ref: "image", Resolver: &pairResolver{idErr: errors.New("digest unavailable"),
			resolve: func(_ context.Context, ch chan<- ProgressEvent) ([]Layer, error) {
				emitProgress(ch, ProgressEvent{Phase: PhasePulling, BytesCurr: 10, BytesTotal: 20})
				return nil, nil
			},
		}}
	}
	results, err := AnalyzePair(t.Context(), inputs, CompareRunOptions{CacheRoot: t.TempDir(), Progress: progress})
	require.NoError(t, err)
	for side, status := range progress.Snapshot() {
		assert.True(t, status.Ready)
		assert.Equal(t, int64(10), status.Event.BytesCurr)
		assert.Contains(t, status.Warning, "digest unavailable")
		assert.Empty(t, results[side].Digest)
	}
}

func TestAnalyzeCancellationAfterResolveSkipsFurtherWork(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	resolver := &pairResolver{resolve: func(context.Context, chan<- ProgressEvent) ([]Layer, error) {
		cancel()
		return []Layer{{Tree: makeTree(makeFile("file", "/file", 12))}}, nil
	}}
	analysis, err := AnalyzeWithOptions(ctx, resolver, "image", AnalyzeOptions{CacheRoot: t.TempDir()})
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, analysis)
}

func TestAnalyzeCacheWarningUsesProgress(t *testing.T) {
	badRoot := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(badRoot, nil, 0600))
	t.Setenv("LAYERX_CACHE_DIR", badRoot)
	progress := make(chan ProgressEvent, 8)
	resolver := &pairResolver{resolve: func(context.Context, chan<- ProgressEvent) ([]Layer, error) { return nil, nil }}
	_, err := AnalyzeWithOptions(t.Context(), resolver, "image", AnalyzeOptions{Progress: progress})
	require.NoError(t, err)
	close(progress)
	var warnings []string
	for event := range progress {
		if event.Phase == PhaseCacheWarn {
			warnings = append(warnings, event.Message)
		}
	}
	require.NotEmpty(t, warnings)
	assert.Contains(t, warnings[0], "ignoring LAYERX_CACHE_DIR")
}

func TestStackContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := stackContext(ctx, []Layer{{Tree: NewFileTree()}})
	require.ErrorIs(t, err, context.Canceled)
	_, err = aggregatedTreesContext(ctx, []Layer{{Tree: NewFileTree()}})
	require.ErrorIs(t, err, context.Canceled)
}

type replacingArchiveResolver struct {
	*ArchiveResolver
	replacement []byte
	initialID   string
	idCalls     int
}

func (r *replacingArchiveResolver) ImageID(ctx context.Context, ref string) (string, error) {
	r.idCalls++
	if r.idCalls == 1 && r.initialID != "" {
		return r.initialID, nil
	}
	return r.ArchiveResolver.ImageID(ctx, ref)
}

func (r *replacingArchiveResolver) ResolveWithProgress(ctx context.Context, ref string, progress chan<- ProgressEvent) ([]Layer, error) {
	layers, err := r.ArchiveResolver.ResolveWithProgress(ctx, ref, progress)
	if err == nil {
		err = os.WriteFile(r.path, r.replacement, 0600)
	}
	return layers, err
}

func TestAnalyzePairKeepsExportedIdentityWhenArchiveChanges(t *testing.T) {
	archive := func(command, content string) string {
		manifest, err := json.Marshal([]dockerManifest{{Config: "config.json", Layers: []string{"layer.tar"}}})
		require.NoError(t, err)
		return writeArchive(t, map[string][]byte{
			"manifest.json": manifest,
			"config.json":   buildConfig(t, []string{command}),
			"layer.tar":     buildSimpleLayerTar(t, map[string][]byte{"file": []byte(content)}),
		})
	}
	for _, hideChange := range []bool{false, true} {
		original := archive("old", "old\n")
		replacement := archive("new", "BAD\n")
		oldID, err := NewArchiveResolver(original).ImageID(t.Context(), original)
		require.NoError(t, err)
		newID, err := NewArchiveResolver(replacement).ImageID(t.Context(), replacement)
		require.NoError(t, err)
		data, err := os.ReadFile(replacement)
		require.NoError(t, err)
		resolver := &replacingArchiveResolver{ArchiveResolver: NewArchiveResolver(original), replacement: data}
		if hideChange {
			// Even matching pre/post lookups must not authenticate a different export.
			resolver.initialID = newID
		}
		cache := t.TempDir()
		pair, err := AnalyzePair(t.Context(), [2]CompareInput{
			{Ref: original, Resolver: resolver},
			{Ref: replacement, Resolver: NewArchiveResolver(replacement)},
		}, CompareRunOptions{NoCache: true, CacheRoot: cache})
		require.NoError(t, err)
		assert.Equal(t, oldID, pair[0].Digest)
		assert.Equal(t, oldID, pair[0].Analysis.Digest)
		assert.Equal(t, newID, pair[1].Digest)
		assert.Equal(t, "old", pair[0].Analysis.Layers[0].Command)
		cached, ok, err := loadCache(cache, newID)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "new", cached[0].Command, "the old export must not poison the replacement cache")
	}
}
