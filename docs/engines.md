# Container engines and archives

LayerX talks to any daemon that implements the Docker Engine REST API. Docker and Podman are both first-class. LayerX honours each engine's *native* notion of "which daemon am I currently talking to", so once you've configured your engine the normal way — `docker context use my-remote`, `podman system connection default staging` — `layerx` inspects the same daemon your engine's own CLI would.

## Docker (default)

LayerX resolves the Docker endpoint in this order:

1. `DOCKER_HOST` env if set — for scripted or CI workflows.
2. `DOCKER_CONTEXT` env if set — overrides the active context per-shell.
3. The active Docker context from `config.json` (`docker context use <name>`).
4. The platform default socket:
   - Linux: `/var/run/docker.sock`
   - macOS: `~/.docker/run/docker.sock`
   - Windows: `\\.\pipe\docker_engine`

`DOCKER_CONFIG` selects the configuration directory; the default is
`~/.docker`. LayerX reads context metadata and Docker credentials there.

## Podman

LayerX resolves the Podman endpoint in this order:

1. `CONTAINER_HOST` env if set.
2. The connection named by `CONTAINER_CONNECTION`.
3. The default connection in `podman-connections.json` or `containers.conf`
   (`podman system connection default <name>`).
4. `DOCKER_HOST` for compatibility, when no Podman connection was selected.
5. On Linux, an available local rootless or rootful Podman socket.

`PODMAN_CONNECTIONS_CONF` and `CONTAINERS_CONF` override the configuration file
paths. Otherwise LayerX uses the operating system's user configuration
directory. When both files exist, `podman-connections.json` takes precedence.
A malformed configuration or a named connection that cannot be found is an
error; `DOCKER_HOST` does not bypass it.

The upshot: once you've run `podman system connection add` / `podman system connection default`, `layerx --engine podman <image>` just works. No `DOCKER_HOST=$(podman system connection list …)` shim required.

**Linux quick start** — no connections configured, just the local socket:

```bash
systemctl --user enable --now podman.socket
layerx --engine podman alpine:3
```

If Podman is your only engine, `--engine auto` (the default) falls back to the Podman socket when no Docker socket is found, so the flag is optional.

**macOS / Windows** — Podman Machine ships with a preconfigured connection. Once `podman machine start` is running, `layerx --engine podman alpine:3` picks up the machine's connection automatically. To point at a different remote, use Podman's own tooling:

```bash
podman system connection add prod ssh://user@prod-host/run/user/1000/podman/podman.sock
podman system connection default prod
layerx --engine podman alpine:3        # inspects on prod, no env plumbing
```

## Archive mode (no daemon required)

Skip the engine entirely by passing a `docker save` or `podman save` tar, or an OCI-layout archive:

```bash
podman save -o alpine.tar alpine:3
layerx ./alpine.tar
```

Useful for airgapped machines, CI runners without docker-in-docker, and anyone who prefers not to run a daemon. Archive mode never reads Docker contexts or Podman connections — the file on disk *is* the source of truth.

## Multi-platform images (`--platform`)

Most images on public registries today are multi-platform: `nginx:latest` resolves to a manifest list with separate manifests for `linux/amd64`, `linux/arm64`, and more. By default LayerX inspects whichever variant your engine picks for the host (Apple Silicon → arm64; typical CI runner → amd64). Pass `--platform` to pick one explicitly:

```bash
# Inspect the arm64 variant on an amd64 host (or anywhere)
layerx --platform linux/arm64 nginx:latest

# Variant suffixes are supported (e.g. arm/v7 for older Pis)
layerx --platform linux/arm/v7 alpine:3

# Gate CI against the variant your service actually runs
layerx ci --platform linux/amd64 --lowest-efficiency 0.9 myapp:${GIT_SHA}

# Compare the same image across architectures using two separate inspect runs
layerx --platform linux/amd64 myapp:1.5.0
layerx --platform linux/arm64 myapp:1.5.0
```

Accepted shapes (same as `docker --platform`): `OS/ARCH`, `OS/ARCH/VARIANT`, or the bare arch shortcut (`amd64` is treated as `linux/amd64`). When the requested platform is not in the image's manifest list, LayerX prints the variants the image actually carries — no silent mismatch.

`--platform` works in archive mode too: LayerX sanity-checks the archive against the requested variant and refuses to inspect a mismatched tarball.

[Back to the README](../README.md) · [Install](install.md)
