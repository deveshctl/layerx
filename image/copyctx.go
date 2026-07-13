package image

import (
	"context"
	"fmt"
	"io"
)

func copyCtx(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 32*1024)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		n, rerr := src.Read(buf)
		if n > 0 {
			nw, werr := dst.Write(buf[:n])
			written += int64(nw)
			if werr != nil {
				return written, werr
			}
			if nw < n {
				return written, io.ErrShortWrite
			}
		}
		if rerr == io.EOF {
			return written, nil
		}
		if rerr != nil {
			return written, rerr
		}
	}
}

// MaxArchiveSize bounds the total bytes spooled from a daemon's ImageSave
// response to a temp file. A rogue or compromised daemon (a hostile
// DOCKER_HOST=tcp://…, an SSH-tunnelled Podman) can stream an unbounded
// chunked response and fill the local disk before parsing ever begins. The
// per-blob MaxLayerBlobSize cap only fires later, during the tar walk, so the
// spool itself needs its own ceiling. 64 GiB comfortably exceeds any real
// multi-layer image while stopping an endless stream from exhausting /tmp.
const MaxArchiveSize = 64 << 30 // 64 GiB

// spoolFromDaemon copies an ImageSave stream to dst, aborting with a clear
// error once the source exceeds MaxArchiveSize rather than filling the disk.
// Use this for daemon-sourced streams (untrusted DOCKER_HOST); it is not for
// on-disk archives, whose size is already bounded by the filesystem.
func spoolFromDaemon(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	n, err := copyCtx(ctx, dst, io.LimitReader(src, MaxArchiveSize+1))
	if err != nil {
		return n, err
	}
	if n > MaxArchiveSize {
		return n, fmt.Errorf("image archive exceeds %d bytes: refusing to spool an unbounded daemon response", MaxArchiveSize)
	}
	return n, nil
}
