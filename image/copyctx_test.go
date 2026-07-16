package image

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
)

func TestCopyCtx_HappyPath(t *testing.T) {
	src := bytes.NewReader(bytes.Repeat([]byte("layerx"), 200_000)) // ~1.2 MB
	want := src.Len()
	var dst bytes.Buffer

	n, err := copyCtx(context.Background(), &dst, src)
	if err != nil {
		t.Fatalf("copyCtx: unexpected error: %v", err)
	}
	if int(n) != want {
		t.Fatalf("copyCtx returned %d bytes, want %d", n, want)
	}
	if dst.Len() != want {
		t.Fatalf("dst received %d bytes, want %d", dst.Len(), want)
	}
}

func TestCopyCtx_PreCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	src := bytes.NewReader([]byte("never read"))
	var dst bytes.Buffer

	n, err := copyCtx(ctx, &dst, src)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("copyCtx error = %v, want context.Canceled", err)
	}
	if n != 0 {
		t.Fatalf("copyCtx returned %d bytes, want 0", n)
	}
	if dst.Len() != 0 {
		t.Fatalf("dst received %d bytes, want 0", dst.Len())
	}
}

// blockingReader yields one chunk, signals served once that chunk has been
// returned, then on the second call blocks until ctx is done and returns
// ctx.Err(). The served channel lets a test wait for the first chunk to be
// written without polling the destination buffer (a polling read while
// copyCtx is still writing trips the race detector).
type blockingReader struct {
	ctx     context.Context
	first   []byte
	yielded bool
	served  chan struct{}
}

func (b *blockingReader) Read(p []byte) (int, error) {
	if !b.yielded {
		b.yielded = true
		n := copy(p, b.first)
		close(b.served)
		return n, nil
	}
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func TestCopyCtx_MidStreamCancel(t *testing.T) {
	first := bytes.Repeat([]byte("a"), 32*1024) // exactly one chunk
	ctx, cancel := context.WithCancel(context.Background())
	br := &blockingReader{ctx: ctx, first: first, served: make(chan struct{})}
	var dst bytes.Buffer

	var (
		wg   sync.WaitGroup
		n    int64
		cerr error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		n, cerr = copyCtx(ctx, &dst, br)
	}()

	// Wait until the reader has handed back the first chunk, then cancel.
	// copyCtx may still be inside its dst.Write at this point, so we don't
	// inspect dst until wg.Wait — the synchronisation comes from the
	// goroutine completing, not from a polling read on the buffer.
	// Whichever side observes the cancel first wins deterministically:
	// copyCtx's top-of-loop ctx.Err() check returns context.Canceled, or
	// the reader's <-ctx.Done() unblocks and returns ctx.Err() which
	// copyCtx propagates via the rerr branch. Either way, cerr ==
	// context.Canceled.
	<-br.served
	cancel()
	wg.Wait()

	if !errors.Is(cerr, context.Canceled) {
		t.Fatalf("copyCtx error = %v, want context.Canceled", cerr)
	}
	if int(n) != len(first) {
		t.Fatalf("copyCtx returned %d bytes, want %d", n, len(first))
	}
	if dst.Len() != len(first) {
		t.Fatalf("dst received %d bytes, want %d", dst.Len(), len(first))
	}
}

var errBoom = errors.New("boom")

// failingReader returns some bytes then a sentinel error.
type failingReader struct {
	chunk []byte
	done  bool
}

func (f *failingReader) Read(p []byte) (int, error) {
	if f.done {
		return 0, errBoom
	}
	f.done = true
	n := copy(p, f.chunk)
	return n, nil
}

func TestCopyCtx_ReadError(t *testing.T) {
	chunk := []byte("partial")
	src := &failingReader{chunk: chunk}
	var dst bytes.Buffer

	n, err := copyCtx(context.Background(), &dst, src)
	if !errors.Is(err, errBoom) {
		t.Fatalf("copyCtx error = %v, want errBoom", err)
	}
	if int(n) != len(chunk) {
		t.Fatalf("copyCtx returned %d bytes, want %d", n, len(chunk))
	}
	if dst.Len() != len(chunk) {
		t.Fatalf("dst received %d bytes, want %d", dst.Len(), len(chunk))
	}
}

func TestSpoolFromDaemon_HappyPath(t *testing.T) {
	src := bytes.NewReader(bytes.Repeat([]byte("layerx"), 200_000)) // ~1.2 MB
	want := src.Len()
	var dst bytes.Buffer

	n, err := spoolFromDaemon(context.Background(), &dst, src)
	if err != nil {
		t.Fatalf("spoolFromDaemon: unexpected error: %v", err)
	}
	if int(n) != want {
		t.Fatalf("spoolFromDaemon returned %d bytes, want %d", n, want)
	}
	if dst.Len() != want {
		t.Fatalf("dst received %d bytes, want %d", dst.Len(), want)
	}
}

// infiniteReader models a rogue daemon that never stops streaming. It fills
// every buffer and never returns EOF; spoolFromDaemon's internal LimitReader
// is what must bound it. Bytes are discarded, so the test allocates nothing
// beyond the copy buffer regardless of MaxArchiveSize.
type infiniteReader struct{}

func (infiniteReader) Read(p []byte) (int, error) { return len(p), nil }

func TestSpoolFromDaemon_RejectsUnboundedStream(t *testing.T) {
	n, err := spoolFromDaemon(context.Background(), io.Discard, infiniteReader{})
	if err == nil {
		t.Fatalf("spoolFromDaemon accepted an unbounded stream; want cap error")
	}
	if n <= MaxArchiveSize {
		t.Fatalf("spoolFromDaemon returned %d bytes, want > MaxArchiveSize (%d)", n, int64(MaxArchiveSize))
	}
}
