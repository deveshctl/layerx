package image

import (
	"archive/tar"
	"bytes"
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

// Exercise the real SDK so version negotiation and serialized platform
// options are covered, rather than just counting opaque option functions.
func TestResolve_DefaultPlatform(t *testing.T) {
	for _, tc := range []struct {
		name       string
		version    string
		nativeArch string
		localArch  string
		wantPull   int
		wantSave   string
		imageID    bool
	}{
		{"arm64 daemon with only amd64 cached", "1.55", "aarch64", "amd64", 1, "arm64", false},
		{"amd64 daemon with only arm64 cached", "1.48", "x86_64", "arm64", 1, "amd64", false},
		{"native image reused without pulling", "1.55", "x86_64", "amd64", 0, "amd64", false},
		{"native variant absent at inspect", "1.55", "x86_64", "", 1, "amd64", false},
		{"old API with successful info", "1.47", "x86_64", "amd64", 0, "", false},
		{"old API pulls missing native image", "1.47", "x86_64", "arm64", 1, "", false},
		{"explicit image ID keeps its platform", "1.55", "x86_64", "arm64", 0, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nativeArch, _ := normalizeArch(tc.nativeArch)
			imageRef := "alpine:3.20"
			if tc.imageID {
				imageRef = "sha256:" + strings.Repeat("a", 64)
			}
			var inner bytes.Buffer
			require.NoError(t, tar.NewWriter(&inner).Close())
			manifest, err := json.Marshal([]dockerManifest{{Config: "config.json", Layers: []string{"layer.tar"}}})
			require.NoError(t, err)
			archive := buildTar(t, map[string][]byte{
				"manifest.json": manifest,
				"config.json":   buildConfig(t, []string{"RUN test"}),
				"layer.tar":     inner.Bytes(),
			}).Bytes()

			var mu sync.Mutex
			localArch := tc.localArch
			pulls, saves := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if req.URL.Path == "/_ping" {
					w.Header().Set("API-Version", tc.version)
					w.WriteHeader(http.StatusOK)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch strings.TrimPrefix(req.URL.Path, "/v"+tc.version) {
				case "/info":
					fmt.Fprintf(w, `{"OSType":"linux","Architecture":%q}`, tc.nativeArch)
				case "/images/json":
					assert.False(t, tc.imageID, "image IDs must not use the reference filter")
					fmt.Fprint(w, `[{"Id":"sha256:cached"}]`)
				case "/images/" + imageRef + "/json":
					if localArch == "" {
						w.WriteHeader(http.StatusNotFound)
						fmt.Fprint(w, `{"message":"No such image"}`)
						return
					}
					fmt.Fprintf(w, `{"Id":"sha256:%s","Os":"linux","Architecture":%q}`, localArch, localArch)
				case "/images/create":
					pulls++
					assert.Equal(t, "linux/"+nativeArch, req.URL.Query().Get("platform"))
					localArch = nativeArch
					fmt.Fprintln(w, `{"status":"Download complete"}`)
				case "/images/get":
					saves++
					assert.Equal(t, []string{imageRef}, req.URL.Query()["names"])
					if tc.wantSave == "" {
						assert.Empty(t, req.URL.Query().Get("platform"))
					} else {
						assert.JSONEq(t, `{"os":"linux","architecture":"`+tc.wantSave+`"}`, req.URL.Query().Get("platform"))
						if localArch != nativeArch {
							http.Error(w, `{"message":"no suitable export target found for native platform"}`, http.StatusNotFound)
							return
						}
					}
					w.Header().Set("Content-Type", "application/x-tar")
					_, _ = w.Write(archive)
				default:
					t.Errorf("unexpected request: %s %s", req.Method, req.URL)
					http.NotFound(w, req)
				}
			}))
			defer server.Close()
			cli, err := client.New(client.WithHost(server.URL), client.WithHTTPClient(server.Client()))
			require.NoError(t, err)
			t.Cleanup(func() { _ = cli.Close() })
			resolver, err := NewDockerResolver(WithClient(cli))
			require.NoError(t, err)

			// A cache hit must not hide the missing native platform either.
			_, err = resolver.ImageID(context.Background(), imageRef)
			if tc.wantPull > 0 {
				assert.Error(t, err, "non-native image must not supply the default analysis cache key")
			} else {
				require.NoError(t, err)
			}
			layers, err := resolver.Resolve(context.Background(), imageRef)
			require.NoError(t, err)
			require.Len(t, layers, 1)
			_, err = resolver.ImageID(context.Background(), imageRef)
			require.NoError(t, err)
			mu.Lock()
			defer mu.Unlock()
			assert.Equal(t, tc.wantPull, pulls)
			assert.Equal(t, 1, saves)
		})
	}
}
