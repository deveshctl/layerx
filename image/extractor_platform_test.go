package image

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildPlatformExtractionArchive(t *testing.T, armLayers int, arches ...string) []byte {
	t.Helper()
	files := make(map[string][]byte)
	var manifests []dockerManifest
	for _, arch := range arches {
		count := 1
		if arch == "arm64" {
			count = armLayers
		}
		manifest := dockerManifest{Config: arch + ".json"}
		var commands []string
		for i := 0; i < count; i++ {
			path := fmt.Sprintf("%s/%d/layer.tar", arch, i)
			files[path] = buildTar(t, map[string][]byte{"etc/arch": []byte(fmt.Sprintf("%s-%d", arch, i))}).Bytes()
			manifest.Layers = append(manifest.Layers, path)
			commands = append(commands, "ADD "+arch)
		}
		files[manifest.Config] = buildConfigWithPlatform(t, "linux", arch, "", commands)
		manifests = append(manifests, manifest)
	}
	data, err := json.Marshal(manifests)
	require.NoError(t, err)
	files["manifest.json"] = data
	return buildTar(t, files).Bytes()
}

// Use the real SDK to verify both serialized export options and file bytes.
// An unscoped multi-platform export deliberately puts amd64 first.
func TestDockerExtractor_PlatformParity(t *testing.T) {
	for _, tc := range []struct {
		name           string
		version        string
		nativeArch     string
		pin            string
		imageID        string
		wantArch       string
		wantPlatform   string
		armLayers      int
		infoFails      bool
		rejectUnscoped bool
	}{
		{name: "explicit foreign platform", pin: "arm64", nativeArch: "x86_64", wantArch: "arm64", wantPlatform: "arm64"},
		{name: "inferred arm64", nativeArch: "aarch64", wantArch: "arm64", wantPlatform: "arm64"},
		{name: "inferred amd64 on API 1.48", version: "1.48", nativeArch: "x86_64", wantArch: "amd64", wantPlatform: "amd64"},
		{name: "explicit platform with different layer counts", pin: "arm64", nativeArch: "x86_64", wantArch: "arm64", wantPlatform: "arm64", armLayers: 2},
		{name: "inferred platform with different layer counts", nativeArch: "aarch64", wantArch: "arm64", wantPlatform: "arm64", armLayers: 2},
		{name: "explicit platform rejects unscoped export", pin: "arm64", nativeArch: "x86_64", wantArch: "arm64", wantPlatform: "arm64", rejectUnscoped: true},
		{name: "inferred platform rejects unscoped export", nativeArch: "aarch64", wantArch: "arm64", wantPlatform: "arm64", rejectUnscoped: true},
		{name: "old API keeps unscoped export", version: "1.47", nativeArch: "x86_64", wantArch: "amd64"},
		{name: "info failure keeps unscoped export", nativeArch: "x86_64", wantArch: "amd64", infoFails: true},
		{name: "foreign image ID", nativeArch: "x86_64", imageID: "sha256:" + strings.Repeat("a", 64), wantArch: "arm64"},
		{name: "image ID with explicit platform", nativeArch: "x86_64", imageID: "sha256:" + strings.Repeat("a", 64), pin: "arm64", wantArch: "arm64", wantPlatform: "arm64"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.version == "" {
				tc.version = "1.55"
			}
			if tc.armLayers == 0 {
				tc.armLayers = 1
			}
			ref := "fixture:test"
			if tc.imageID != "" {
				ref = tc.imageID
			}
			scoped := buildPlatformExtractionArchive(t, tc.armLayers, tc.wantArch)
			unscoped := buildPlatformExtractionArchive(t, tc.armLayers, "amd64", "arm64")
			if tc.wantPlatform == "" {
				// Old stores and exact image IDs export a single selected image.
				unscoped = scoped
			}
			var mu sync.Mutex
			var saveQueries []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/_ping" {
					w.Header().Set("API-Version", tc.version)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch strings.TrimPrefix(req.URL.Path, "/v"+tc.version) {
				case "/info":
					if tc.infoFails {
						http.Error(w, `{"message":"info unavailable"}`, http.StatusInternalServerError)
						return
					}
					fmt.Fprintf(w, `{"OSType":"linux","Architecture":%q}`, tc.nativeArch)
				case "/images/json":
					fmt.Fprint(w, `[{"Id":"sha256:fixture"}]`)
				case "/images/" + ref + "/json":
					fmt.Fprintf(w, `{"Id":"sha256:%s","Os":"linux","Architecture":%q}`, strings.Repeat("a", 64), tc.wantArch)
				case "/images/create":
					assert.Equal(t, "linux/"+tc.pin, req.URL.Query().Get("platform"))
					fmt.Fprintln(w, `{"status":"Download complete"}`)
				case "/images/get":
					assert.Equal(t, []string{ref}, req.URL.Query()["names"])
					platform := req.URL.Query().Get("platform")
					mu.Lock()
					saveQueries = append(saveQueries, platform)
					mu.Unlock()
					if tc.rejectUnscoped && platform == "" {
						http.Error(w, `{"message":"unscoped export unavailable"}`, http.StatusNotFound)
						return
					}
					w.Header().Set("Content-Type", "application/x-tar")
					if platform == "" {
						_, _ = w.Write(unscoped)
					} else {
						_, _ = w.Write(scoped)
					}
				default:
					t.Errorf("unexpected request: %s %s", req.Method, req.URL)
					http.NotFound(w, req)
				}
			}))
			defer server.Close()
			cli, err := client.New(client.WithHost(server.URL), client.WithHTTPClient(server.Client()))
			require.NoError(t, err)
			t.Cleanup(func() { _ = cli.Close() })
			opts := []Option{WithClient(cli)}
			if tc.pin != "" {
				platform, err := ParsePlatform("linux/" + tc.pin)
				require.NoError(t, err)
				opts = append(opts, WithPlatform(platform))
			}
			resolver, err := NewDockerResolver(opts...)
			require.NoError(t, err)
			ctx := context.Background()
			analysisOpts := AnalyzeOptions{CacheRoot: t.TempDir()}
			if tc.pin != "" {
				analysisOpts.Platform = "linux/" + tc.pin
			}
			analysis, err := AnalyzeWithOptions(ctx, resolver, ref, analysisOpts)
			require.NoError(t, err)
			layers := analysis.Layers
			count := 1
			if tc.wantArch == "arm64" {
				count = tc.armLayers
			}
			require.Len(t, layers, count)
			assert.Equal(t, "ADD "+tc.wantArch, layers[count-1].Command)
			extractor := resolver.(ExtractorSource).NewExtractor()
			for i := range layers {
				want := fmt.Sprintf("%s-%d", tc.wantArch, i)
				content, err := extractor.ExtractFromLayer(ctx, ref, "/etc/arch", i)
				if assert.NoError(t, err) {
					assert.Equal(t, want, string(content.Data))
				}
				data, err := extractor.ExtractRawFromLayer(ctx, ref, "/etc/arch", i)
				if assert.NoError(t, err) {
					assert.Equal(t, want, string(data))
				}
			}
			// A cache hit skips Resolve; a fresh resolver must still pass its
			// platform selection to the extractor without a prior export.
			cachedResolver, err := NewDockerResolver(opts...)
			require.NoError(t, err)
			progress := make(chan ProgressEvent, 10)
			analysisOpts.Progress = progress
			cached, err := AnalyzeWithOptions(ctx, cachedResolver, ref, analysisOpts)
			require.NoError(t, err)
			close(progress)
			hit := false
			for event := range progress {
				hit = hit || event.Phase == PhaseCacheLoad
			}
			require.True(t, hit, "second analysis must use the cache")
			require.Len(t, cached.Layers, count)
			cachedExtractor := cachedResolver.(ExtractorSource).NewExtractor()
			data, err := cachedExtractor.ExtractRawFromLayer(ctx, ref, "/etc/arch", count-1)
			require.NoError(t, err)
			assert.Equal(t, fmt.Sprintf("%s-%d", tc.wantArch, count-1), string(data))
			mu.Lock()
			defer mu.Unlock()
			require.Len(t, saveQueries, 2+2*count)
			for _, query := range saveQueries {
				if tc.wantPlatform == "" {
					assert.Empty(t, query)
				} else {
					assert.JSONEq(t, `{"os":"linux","architecture":"`+tc.wantPlatform+`"}`, query)
				}
			}
		})
	}
}

func TestDockerExtractor_ExplicitPlatformOnOldAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.47")
			return
		}
		t.Errorf("unsupported platform must not send an unscoped export: %s", req.URL)
		http.NotFound(w, req)
	}))
	defer server.Close()
	cli, err := client.New(client.WithHost(server.URL), client.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })
	platform, err := ParsePlatform("linux/arm64")
	require.NoError(t, err)
	resolver, err := NewDockerResolver(WithClient(cli), WithPlatform(platform))
	require.NoError(t, err)
	extractor := resolver.(ExtractorSource).NewExtractor()
	_, err = extractor.ExtractFromLayer(context.Background(), "fixture:test", "/etc/arch", 0)
	require.ErrorContains(t, err, "1.48")
	_, err = extractor.ExtractRawFromLayer(context.Background(), "fixture:test", "/etc/arch", 0)
	require.ErrorContains(t, err, "1.48")
}
