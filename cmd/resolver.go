package cmd

import (
	"fmt"
	"os"

	"github.com/deveshctl/layerx/image"
)

// selectResolver is a package-level var so tests can swap in a fake.
var selectResolver = selectResolverDefault

// selectResolverDefault picks the right Resolver implementation based on imageRef.
//
// If imageRef is the path of a regular file (after symlink resolution),
// returns an ArchiveResolver that reads the file directly — no Docker daemon
// required. Otherwise returns a DockerResolver bound to the local daemon.
//
// Detection is purely path-based: anything that is not a regular file (does
// not exist, is a directory, is a special file) falls through to the Docker
// path. A user-supplied ref that happens to be both a valid Docker tag and an
// existing file is resolved as the file (auto-detect prefers the local
// artifact, since the user typed an existing path).
func selectResolverDefault(imageRef string) (image.Resolver, error) {
	if isRegularFilePath(imageRef) {
		return image.NewArchiveResolver(imageRef), nil
	}
	r, err := image.NewDockerResolver()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize: %w", err)
	}
	return r, nil
}

// isRegularFilePath returns true if path resolves (through any symlinks) to
// an existing regular file. Returns false for missing paths, directories,
// and special files; the caller falls back to Docker daemon resolution.
func isRegularFilePath(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}
