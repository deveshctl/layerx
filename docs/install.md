# Install LayerX

Prebuilt releases support Linux, macOS 13 Ventura or later, and Windows,
with amd64 and arm64 builds. Go is only needed when building from source.

## Homebrew (macOS)

```bash
brew install --cask deveshctl/tap/layerx
```

The published Homebrew package is a cask. If you installed the older formula,
run `brew uninstall --formula layerx` before installing the cask. For Linux,
use the packages or direct downloads below.

## Scoop (Windows)

```bash
scoop bucket add layerx https://github.com/deveshctl/scoop-bucket
scoop install layerx
```

## Debian / Ubuntu

```bash
# amd64
curl -LO https://github.com/deveshctl/layerx/releases/latest/download/layerx_linux_amd64.deb
sudo dpkg -i layerx_linux_amd64.deb

# arm64
curl -LO https://github.com/deveshctl/layerx/releases/latest/download/layerx_linux_arm64.deb
sudo dpkg -i layerx_linux_arm64.deb
```

## RHEL / Fedora

```bash
# amd64
curl -LO https://github.com/deveshctl/layerx/releases/latest/download/layerx_linux_amd64.rpm
sudo rpm -i layerx_linux_amd64.rpm

# arm64
curl -LO https://github.com/deveshctl/layerx/releases/latest/download/layerx_linux_arm64.rpm
sudo rpm -i layerx_linux_arm64.rpm
```

## Direct download

Prebuilt binaries for Linux, macOS, and Windows (amd64 + arm64) on the [Releases page](https://github.com/deveshctl/layerx/releases). Replace `latest` with a specific tag (e.g. `v1.6.3`) in any URL above.

## Container image (Docker, Podman, any OCI runtime)

Every tagged release also publishes a multi-arch image (linux/amd64 + linux/arm64) to GitHub Container Registry:

```
ghcr.io/deveshctl/layerx:latest
ghcr.io/deveshctl/layerx:v1.6.3     # pin a specific version
```

LayerX is a client to a running container engine, so the image needs the host engine's socket bind-mounted in. It cannot inspect live images without one — but archive mode (bind-mount a `docker save` / OCI tar) works without any socket at all.

**Docker on Linux** — the socket is owned by `root:docker` on the host. The image runs as `nonroot` (uid 65532) and needs the host's numeric `docker` GID passed in as a supplementary group (the distroless image has no `/etc/group`, so `--group-add docker` by name won't resolve inside the container):

```bash
DOCKER_GID=$(getent group docker 2>/dev/null | cut -d: -f3)
if [ -z "$DOCKER_GID" ]; then
    DOCKER_GID=$(awk -F: '$1 == "docker" {print $3; exit}' /etc/group)
fi
docker run --rm -it \
    --group-add "$DOCKER_GID" \
    -v /var/run/docker.sock:/var/run/docker.sock \
    ghcr.io/deveshctl/layerx:latest nginx:latest
```

The `awk` fallback handles hosts without `getent`. Confirm `DOCKER_GID` is
non-empty before running the container; the host needs a Docker group for this
recipe.

**Docker Desktop (macOS / Windows)** — the socket is proxied and no group is enforced, so the group flag is not needed:

```bash
docker run --rm -it \
    -v /var/run/docker.sock:/var/run/docker.sock \
    ghcr.io/deveshctl/layerx:latest nginx:latest
```

**Podman (rootless, Linux)** — mount the user's Podman socket at the container's Docker socket path. Podman's Docker-compat API means LayerX talks to it the same way as Docker:

```bash
podman run --rm -it \
    -v "${XDG_RUNTIME_DIR}/podman/podman.sock:/var/run/docker.sock" \
    ghcr.io/deveshctl/layerx:latest alpine:3
```

For rootful Podman on Linux the socket is at `/run/podman/podman.sock`; on macOS / Windows the Podman Machine socket path varies per install — resolve it with `podman info --format '{{.Host.RemoteSocket.Path}}'` and substitute it into the left side of the `-v` flag.

**Archive mode (no daemon)** — bind-mount the tarball read-only, no socket required:

```bash
docker run --rm -it \
    -v "$PWD/alpine.tar:/tmp/alpine.tar:ro" \
    ghcr.io/deveshctl/layerx:latest /tmp/alpine.tar
```

Trade-offs vs. the native binary: the image is a distroless base plus the layerx binary (a few MB of overhead — check the release page for the exact compressed size), it needs the `--group-add` step on Linux for Docker's `root:docker`-owned socket, and file-extraction (`x` key) writes into the container's filesystem — bind-mount an output directory if you want the file on the host. Native binaries are the smoother path for daily use; the container image shines in CI runners that already have an engine but no package manager, and for pinning the exact LayerX version alongside your other build tooling.

If GHCR returns `401`, check the package visibility and your registry credentials.
For interactive use, start with the native binary for your platform.

## Install with Go

Requires Go 1.27.2+:

```bash
go install github.com/deveshctl/layerx@latest
```

`@latest` installs the latest tagged release. To try changes described under
`Unreleased` in [CHANGELOG.md](../CHANGELOG.md), build the current main branch:

```bash
git clone https://github.com/deveshctl/layerx.git
cd layerx
go build -o layerx .
./layerx --version
```

On Windows, use `go build -o layerx.exe .` and `.\layerx.exe --version`.
See [CONTRIBUTING.md](../CONTRIBUTING.md) for development checks.


[Back to the quick start](../README.md#quick-start) · [Engine setup](engines.md) · [Verify releases](verifying-releases.md)
