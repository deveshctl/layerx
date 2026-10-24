package image

import (
	"context"
	"errors"
	"testing"

	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPinnedLocalPlatformIsVerifiedBeforeReuse(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64", ""} {
		t.Run(arch, func(t *testing.T) {
			pulls := 0
			fake := &fakeAPIClient{
				imageInspect: func(context.Context, string) (client.ImageInspectResult, error) {
					result := client.ImageInspectResult{}
					result.Os = "linux"
					result.Architecture = arch
					return result, nil
				},
				imagePull: func(context.Context, string, client.ImagePullOptions) (client.ImagePullResponse, error) {
					pulls++
					return nil, errors.New("test pull attempted")
				},
			}
			resolver, err := NewDockerResolver(WithClient(fake), WithPlatform(&ocispec.Platform{OS: "linux", Architecture: "amd64"}))
			require.NoError(t, err)
			err = resolver.(*DockerResolver).ensureImageWithProgress(t.Context(), "app:local", nil)
			if arch == "amd64" {
				require.NoError(t, err)
				assert.Zero(t, pulls)
			} else {
				require.ErrorContains(t, err, "test pull attempted")
				assert.Equal(t, 1, pulls)
			}
		})
	}
}
