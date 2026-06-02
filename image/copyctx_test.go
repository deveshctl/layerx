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

// blockingReader yields one chunk, then blocks on release until the test
// signals it. Used to deterministically interleave a cancel with the copy.
type blockingReader struct {
	first   []byte
	yielded bool
	release chan struct{}
}

func (b *blockingReader) Read(p []byte) (int, error) {
	if !b.yielded {
		b.yielded = true
		n := copy(p, b.first)
		return n, nil
	}
	<-b.release
	return 0, io.EOF
}

func TestCopyCtx_MidStreamCancel(t *testing.T) {
	first := bytes.Repeat([]byte("a"), 32*1024) // exactly one chunk
	br := &blockingReader{first: first, release: make(chan struct{})}

	ctx, cancel := context.WithCancel(context.Background())
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

	// Wait until the first chunk has been written, then cancel. closing
	// br.release after cancel unblocks the goroutine deterministically:
	// if it had already entered the second (blocking) Read, the close
	// returns it (0, io.EOF) and the next ctx.Err() check returns the
	// cancel; otherwise the cancel was observed first and the close is
	// a no-op for an already-returned goroutine.
	for {
		if dst.Len() == len(first) {
			break
		}
	}
	cancel()
	close(br.release)
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
