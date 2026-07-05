# LayerX Image Inspector

> A terminal tool for inspecting, comparing, and CI-gating Docker, Podman, and OCI container images.

LayerX opens a container image in an interactive TUI and shows you exactly what each layer added, modified, or removed — layer by layer, byte by byte. It works with live Docker and Podman daemons, or reads `docker save` / `podman save` / OCI-layout archives directly from disk with no daemon required.

## What it does

- **Interactive layer browser** — navigate layers, browse the file tree with diff colouring, open any file in an in-place viewer with syntax highlighting and search, extract files to disk.
- **Efficiency analysis** — computes wasted bytes (files duplicated across layers) and an efficiency score (0.0–1.0). Waste navigator (`w`) jumps directly to the worst offenders.
- **CI mode** (`layerx ci`) — headless evaluation against configurable thresholds: lowest efficiency, highest wasted bytes, highest user-wasted percent, and glob-based path rules. Exit codes: 0 pass, 1 rule failure, 2 operational error.
- **Image comparison** (`layerx compare OLD NEW`) — deterministic delta report across size, efficiency, layers, files, and waste. Ends with a machine-parseable verdict line (`verdict: ok`, `verdict: regression reason=...`).
- **JSON export** (`--json PATH`) — full analysis as versioned JSON for scripts, dashboards, and `jq`.
- **Multi-platform support** (`--platform linux/amd64`) — inspect a specific variant of a multi-platform image.
- **Analysis cache** — results cached per image digest; repeat runs are instant. `layerx cache list/prune` manages entries.
- **Build wrapper** (`layerx build`) — build via Docker or Podman and open the result in the TUI immediately.

## Container engine support

- **Docker** — auto-detects the active Docker context (`docker context use`), `DOCKER_HOST`, or the platform default socket.
- **Podman** — auto-detects the active Podman connection (`podman system connection default`), `CONTAINER_HOST`, or the Podman socket. Works on Linux, macOS, and Windows.
- **Archives** — any argument that resolves to an existing file is read directly as a `docker save`, `podman save`, or OCI-layout tarball. No daemon, no network, no root.

## CLI

```
layerx IMAGE_OR_ARCHIVE               # interactive TUI
layerx ci IMAGE_OR_ARCHIVE            # headless CI gate
layerx compare OLD NEW                # image-to-image diff
layerx build [BUILD_ARGS...]          # build + inspect
layerx --json out.json IMAGE          # JSON export
layerx --platform linux/amd64 IMAGE   # specific platform variant
layerx --engine podman IMAGE          # explicit engine selection
layerx --no-cache IMAGE               # bypass analysis cache
layerx cache list                     # show cached analyses
layerx cache prune --older-than 30d   # evict old entries
layerx init --flavour python          # generate starter .layerx.yaml
```

## Configuration (`.layerx.yaml`)

```yaml
version: 1
rules:
  lowest-efficiency: 0.9
  highest-wasted-bytes: 52428800
  highest-user-wasted-percent: 0.1
path-rules:
  block:
    - "**/.git/**"
  deny-waste:
    - "**/*.pyc"
```

## Key TUI bindings

`Tab` switch panes · `j/k` up/down · `g/G` top/bottom · `Enter` open file viewer · `x` extract file · `/` filter or search · `d` diff-only · `s` sort by size · `w` waste navigator · `A` split-pane view · `y` copy path · `Y` copy content · `c` copy layer command · `?` help · `q` quit

## Install

```bash
brew install deveshctl/tap/layerx          # macOS / Linux
scoop install layerx                       # Windows
go install github.com/deveshctl/layerx@latest
```

Prebuilt binaries for Linux, macOS, Windows (amd64 + arm64): https://github.com/deveshctl/layerx/releases

## Supply chain

Every release ships a cosign-signed checksum file (keyless, GitHub OIDC), an SPDX SBOM per archive, and a SLSA Build Level 3 provenance attestation.

## Links

- Repository: https://github.com/deveshctl/layerx
- Releases: https://github.com/deveshctl/layerx/releases
- Configuration reference: https://github.com/deveshctl/layerx/blob/main/docs/configuration.md
- CI integration guide: https://github.com/deveshctl/layerx/blob/main/docs/ci-integration.md
- JSON export schema: https://github.com/deveshctl/layerx/blob/main/docs/json-export.md
- Migrating from Dive: https://github.com/deveshctl/layerx/blob/main/docs/migrating-from-dive.md
- Changelog: https://github.com/deveshctl/layerx/blob/main/CHANGELOG.md
