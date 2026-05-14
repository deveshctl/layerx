package image

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/moby/moby/client"
)

const MaxViewSize = 1 << 20 // 1 MB

// FileContent holds the result of extracting a file from an image.
type FileContent struct {
	Path      string
	Data      []byte
	Size      int64 // actual file size (may exceed len(Data) if truncated)
	Truncated bool
	Binary    bool
}

// Extractor retrieves file contents from a container image.
type Extractor interface {
	Extract(ctx context.Context, imageRef string, filePath string) (*FileContent, error)
	ExtractRaw(ctx context.Context, imageRef string, filePath string) ([]byte, error)
}

// IsBinary reports whether data appears to be binary content.
// Uses net/http content detection plus null-byte scanning.
func IsBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	ct := http.DetectContentType(data)
	if strings.HasPrefix(ct, "text/") {
		return false
	}
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

// processContent applies binary detection and size limits to raw file data.
func processContent(path string, data []byte, totalSize int64) *FileContent {
	fc := &FileContent{
		Path: path,
		Size: totalSize,
	}

	if len(data) == 0 {
		return fc
	}

	sniff := data
	if len(sniff) > 512 {
		sniff = sniff[:512]
	}
	if IsBinary(sniff) {
		fc.Binary = true
		return fc
	}

	if len(data) > MaxViewSize {
		fc.Data = data[:MaxViewSize]
		fc.Truncated = true
	} else {
		fc.Data = data
	}
	return fc
}

// DockerExtractor extracts files from images via the Docker daemon.
type DockerExtractor struct {
	cli client.APIClient
}

// NewDockerExtractor creates an extractor using the provided Docker client.
func NewDockerExtractor(cli client.APIClient) *DockerExtractor {
	return &DockerExtractor{cli: cli}
}

// Extract creates a temporary container, copies the file out, and removes the container.
func (e *DockerExtractor) Extract(ctx context.Context, imageRef string, filePath string) (*FileContent, error) {
	createResult, err := e.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Image: imageRef,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create container for extraction: %w", err)
	}
	containerID := createResult.ID

	defer func() {
		removeCtx := context.WithoutCancel(ctx)
		_, _ = e.cli.ContainerRemove(removeCtx, containerID, client.ContainerRemoveOptions{Force: true})
	}()

	copyResult, err := e.cli.CopyFromContainer(ctx, containerID, client.CopyFromContainerOptions{
		SourcePath: filePath,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to copy %s from container: %w", filePath, err)
	}
	defer copyResult.Content.Close()

	totalSize := copyResult.Stat.Size

	data, err := readFirstFileFromTar(copyResult.Content, totalSize)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", filePath, err)
	}

	return processContent(filePath, data, totalSize), nil
}

// ExtractRaw extracts a file's raw bytes without truncation or binary detection.
func (e *DockerExtractor) ExtractRaw(ctx context.Context, imageRef string, filePath string) ([]byte, error) {
	createResult, err := e.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Image: imageRef,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create container for extraction: %w", err)
	}
	containerID := createResult.ID

	defer func() {
		removeCtx := context.WithoutCancel(ctx)
		_, _ = e.cli.ContainerRemove(removeCtx, containerID, client.ContainerRemoveOptions{Force: true})
	}()

	copyResult, err := e.cli.CopyFromContainer(ctx, containerID, client.CopyFromContainerOptions{
		SourcePath: filePath,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to copy %s from container: %w", filePath, err)
	}
	defer copyResult.Content.Close()

	return readFullFileFromTar(copyResult.Content)
}

// readFirstFileFromTar reads the first regular file from a tar stream.
// Docker's CopyFromContainer wraps the file in a single-entry tar.
func readFirstFileFromTar(r io.Reader, expectedSize int64) ([]byte, error) {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("no file found in tar stream")
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}

		limit := int64(MaxViewSize + 1)
		if expectedSize > 0 && expectedSize < limit {
			limit = expectedSize
		}
		data, err := io.ReadAll(io.LimitReader(tr, limit))
		if err != nil {
			return nil, err
		}
		return data, nil
	}
}

// readFullFileFromTar reads the first regular file from a tar stream without size limits.
func readFullFileFromTar(r io.Reader) ([]byte, error) {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("no file found in tar stream")
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		return io.ReadAll(tr)
	}
}
