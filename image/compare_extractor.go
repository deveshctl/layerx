package image

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/moby/moby/client"
)

// CompareExtractor retains a lazy, indexed export for one immutable image ID.
// Close releases the export after outstanding reads stop.
type CompareExtractor interface {
	Extractor
	BoundedLayerExtractor
	io.Closer
}

func (r *DockerResolver) NewCompareExtractor(sourceRef string) CompareExtractor {
	return &compareDockerExtractor{
		DockerExtractor: &DockerExtractor{cli: r.cli, saveOpts: func(ctx context.Context, _ string) []client.ImageSaveOption {
			// The immutable export ID must use the original reference's platform policy.
			return r.imageSaveOpts(ctx, sourceRef)
		}},
		gate: make(chan struct{}, 1),
	}
}

type compareDockerExtractor struct {
	*DockerExtractor
	gate     chan struct{}
	closed   bool
	ref      string
	spool    *os.File
	cleanup  func()
	metadata *imageArchiveMetadata
	offsets  map[string]int64
}

func (e *compareDockerExtractor) Close() error {
	e.gate <- struct{}{}
	defer func() { <-e.gate }()
	e.closed = true
	if e.cleanup != nil {
		e.cleanup()
		e.cleanup, e.spool, e.metadata, e.offsets = nil, nil, nil, nil
	}
	return nil
}

func (e *compareDockerExtractor) ExtractBoundedFromLayer(ctx context.Context, ref, path string, cursor int, maxBytes int64) ([]byte, error) {
	cleanPath := cleanTarPath(path)
	if cleanPath == "" || cursor < 0 || maxBytes < 0 || maxBytes > MaxSaveSize {
		return nil, fmt.Errorf("invalid bounded extraction request")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case e.gate <- struct{}{}:
	}
	defer func() { <-e.gate }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e.closed || (e.spool != nil && e.ref != ref) {
		return nil, fmt.Errorf("comparison export is unavailable for %q", ref)
	}
	if e.spool == nil {
		spool, cleanup, err := e.spoolImage(ctx, ref)
		if err != nil {
			return nil, err
		}
		offsets := make(map[string]int64)
		metadata, err := readImageArchiveMetadataContext(ctx, spool, offsets)
		if err != nil {
			cleanup()
			return nil, err
		}
		e.ref, e.spool, e.cleanup, e.metadata, e.offsets = ref, spool, cleanup, metadata, offsets
	}
	layers := e.metadata.manifest.Layers
	if cursor >= len(layers) {
		return nil, fmt.Errorf("layer index %d out of range (have %d)", cursor, len(layers))
	}
	load := func(name string) (io.Reader, error) {
		size, ok := e.metadata.headers[name]
		if !ok {
			return nil, fmt.Errorf("layer %q is missing from the comparison export", name)
		}
		if size > MaxLayerBlobSize {
			return nil, fmt.Errorf("layer blob %s too large: %d bytes (limit %d)", name, size, MaxLayerBlobSize)
		}
		if offset, ok := e.offsets[name]; ok {
			return io.NewSectionReader(e.spool, offset, size), nil
		}
		return openLayerBlobFromSpool(ctx, e.spool, name)
	}
	return walkBackForFileBounded(ctx, layers, load, cleanPath, path, cursor, maxBytes)
}
