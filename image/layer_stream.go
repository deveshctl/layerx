package image

import (
	"context"
	"hash"
	"io"
	"strings"
)

// layerStream counts/hashes through the gzip trailer and tar padding, with
// cancellation checked on every read. Callers must drain it after parsing.
type layerStream struct {
	ctx    context.Context
	reader io.Reader
	hash   hash.Hash
	bytes  int64
}

func (r *layerStream) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	r.bytes += int64(n)
	_, _ = r.hash.Write(p[:n])
	return n, err
}

func (c imageConfig) platformName() string {
	if c.OS == "" || c.Architecture == "" {
		return ""
	}
	parts := []string{c.OS, c.Architecture}
	if c.Variant != "" {
		parts = append(parts, c.Variant)
	}
	return strings.Join(parts, "/")
}
