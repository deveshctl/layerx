package image

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Use real content digests and an OCI config with rootfs/history so these
// tests exercise an image-layout archive, not Docker's manifest.json format.
func standaloneOCIArchive(t *testing.T, algorithm string, multi bool) (path, configID string, layerSize int64) {
	t.Helper()
	digest := func(data []byte) string {
		if algorithm == "sha512" {
			return fmt.Sprintf("sha512:%x", sha512.Sum512(data))
		}
		return fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	}
	blobPath := func(d string) string { return "blobs/" + strings.ReplaceAll(d, ":", "/") }
	marshal := func(value any) []byte {
		data, err := json.Marshal(value)
		require.NoError(t, err)
		return data
	}
	rawLayer := buildSimpleLayerTar(t, map[string][]byte{"etc/hostname": []byte("oci-test\n")})
	layer := gzipBytes(t, rawLayer)
	layerDigest := digest(layer)
	files := map[string][]byte{
		"oci-layout":          []byte(`{"imageLayoutVersion":"1.0.0"}`),
		blobPath(layerDigest): layer,
	}
	arches := []string{"amd64"}
	if multi {
		arches = append(arches, "arm64")
	}
	var descriptors []ociDescriptor
	for i, arch := range arches {
		config := marshal(map[string]any{
			"os": "linux", "architecture": arch,
			"rootfs":  map[string]any{"type": "layers", "diff_ids": []string{fmt.Sprintf("sha256:%x", sha256.Sum256(rawLayer))}},
			"history": []configHistoryEntry{{CreatedBy: "RUN echo oci"}},
		})
		configDigest := digest(config)
		files[blobPath(configDigest)] = config
		if i == 0 {
			configID = fmt.Sprintf("sha256:%x", sha256.Sum256(config))
		}
		manifest := marshal(map[string]any{
			"schemaVersion": 2,
			"mediaType":     "application/vnd.oci.image.manifest.v1+json",
			"config": ociDescriptor{
				MediaType: "application/vnd.oci.image.config.v1+json", Digest: configDigest, Size: int64(len(config)),
			},
			"layers": []ociDescriptor{{
				MediaType: "application/vnd.oci.image.layer.v1.tar+gzip", Digest: layerDigest, Size: int64(len(layer)),
			}},
		})
		manifestDigest := digest(manifest)
		files[blobPath(manifestDigest)] = manifest
		descriptors = append(descriptors, ociDescriptor{
			MediaType: "application/vnd.oci.image.manifest.v1+json", Digest: manifestDigest, Size: int64(len(manifest)),
		})
	}
	files["index.json"] = marshal(map[string]any{"schemaVersion": 2, "manifests": descriptors})
	return writeArchive(t, files), configID, int64(len(layer))
}

func TestArchiveResolver_StandaloneOCI_AllPaths(t *testing.T) {
	for _, algorithm := range []string{"sha256", "sha512"} {
		t.Run(algorithm, func(t *testing.T) {
			path, wantID, wantSize := standaloneOCIArchive(t, algorithm, false)
			r := NewArchiveResolver(path)
			ctx := context.Background()

			id, err := r.ImageID(ctx, path)
			require.NoError(t, err)
			assert.Equal(t, wantID, id)
			meta, err := r.Inspect(ctx, path)
			require.NoError(t, err)
			assert.Equal(t, wantSize, meta.Size)
			progress := make(chan ProgressEvent, 10)
			layers, err := r.ResolveWithProgress(ctx, path, progress)
			require.NoError(t, err)
			require.Len(t, layers, 1)
			assert.Equal(t, "RUN echo oci", layers[0].Command)
			assert.NotNil(t, layers[0].Tree)
			for _, event := range drainProgress(progress) {
				assert.NotEqual(t, PhaseCacheWarn, event.Phase, "single-image archives must not warn")
			}

			ex := r.NewExtractor()
			for _, extract := range []func() ([]byte, error){
				func() ([]byte, error) { return ex.ExtractRaw(ctx, path, "/etc/hostname") },
				func() ([]byte, error) { return ex.ExtractRawFromLayer(ctx, path, "/etc/hostname", 0) },
			} {
				data, err := extract()
				require.NoError(t, err)
				assert.Equal(t, "oci-test\n", string(data))
			}
			view, err := ex.ExtractFromLayer(ctx, path, "/etc/hostname", 0)
			require.NoError(t, err)
			assert.Equal(t, "oci-test\n", string(view.Data))
			view, err = ex.Extract(ctx, path, "/etc/hostname")
			require.NoError(t, err)
			assert.Equal(t, "oci-test\n", string(view.Data))

			// ImageID must permit a warm analysis cache, not just parsing.
			cacheRoot := filepath.Join(t.TempDir(), "cache")
			_, err = AnalyzeWithOptions(ctx, r, path, AnalyzeOptions{CacheRoot: cacheRoot})
			require.NoError(t, err)
			cachedProgress := make(chan ProgressEvent, 10)
			_, err = AnalyzeWithOptions(ctx, r, path, AnalyzeOptions{CacheRoot: cacheRoot, Progress: cachedProgress})
			require.NoError(t, err)
			foundCacheHit := false
			for _, event := range drainProgress(cachedProgress) {
				if event.Phase == PhaseCacheLoad {
					foundCacheHit = true
				}
			}
			assert.True(t, foundCacheHit, "second analysis must reuse the cached OCI image")

			// The daemon extractor may also receive an OCI-layout export.
			archive, err := os.ReadFile(path)
			require.NoError(t, err)
			daemonExtractor := NewDockerExtractor(&fakeImageSaveClient{saveData: archive})
			data, err := daemonExtractor.ExtractRawFromLayer(ctx, "test:latest", "/etc/hostname", 0)
			require.NoError(t, err)
			assert.Equal(t, "oci-test\n", string(data))
		})
	}
}

func TestArchiveResolver_StandaloneOCI_PlatformAndWarning(t *testing.T) {
	for _, multi := range []bool{false, true} {
		t.Run(fmt.Sprintf("multi=%t", multi), func(t *testing.T) {
			path, wantID, _ := standaloneOCIArchive(t, "sha256", multi)
			platform, err := ParsePlatform("linux/amd64")
			require.NoError(t, err)
			r := NewArchiveResolverWithPlatform(path, platform)
			progress := make(chan ProgressEvent, 10)
			layers, err := r.ResolveWithProgress(context.Background(), path, progress)
			require.NoError(t, err)
			require.Len(t, layers, 1)
			id, err := r.ImageID(context.Background(), path)
			require.NoError(t, err)
			assert.Equal(t, wantID, id, "metadata must identify the image used by Resolve")
			var warnings []string
			for _, event := range drainProgress(progress) {
				if event.Phase == PhaseCacheWarn {
					warnings = append(warnings, event.Message)
				}
			}
			if multi {
				require.Len(t, warnings, 1)
				assert.Contains(t, warnings[0], "2 manifest entries")
				assert.Contains(t, warnings[0], "export a single-image archive")
				assert.NotContains(t, warnings[0], "--platform")
			} else {
				assert.Empty(t, warnings)
			}
			// Even in a multi-image archive, the pin validates the first
			// image; it must never silently analyse a different variant.
			platform, err = ParsePlatform("linux/arm64")
			require.NoError(t, err)
			_, err = NewArchiveResolverWithPlatform(path, platform).Resolve(context.Background(), path)
			var mismatch *ErrPlatformNotInImage
			require.ErrorAs(t, err, &mismatch)
			assert.Equal(t, []string{"linux/amd64"}, mismatch.Available)
		})
	}
}
