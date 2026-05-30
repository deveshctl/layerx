# layerx

Interactive Docker image layer inspector with CI-friendly efficiency checks. Single binary; no daemon required when reading saved image archives.

![CI](https://github.com/deveshctl/layerx/actions/workflows/ci.yml/badge.svg)
![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-blue)
![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey)

<!-- Demo: a GIF will go here. -->

---

## What is layerx?

A terminal tool for understanding what's inside a Docker image — which layer added each file, where the wasted bytes are, and what your `RUN` steps actually produced.

Use it to:

- Debug bloated images and find the layer responsible
- Review the filesystem impact of a Dockerfile change before merging
- Gate CI on layer waste or efficiency thresholds

---

## Quick Start

```bash
# macOS / Linux
brew install deveshctl/tap/layerx

# Inspect a Docker image (daemon required)
layerx nginx:latest

# Or inspect a local docker-save / OCI archive (no daemon needed)
layerx ./build/app.tar
```

Other platforms: see [Install](#install).

---

## What you can do

| Mode        | Command                                              | Best for                                                |
|-------------|------------------------------------------------------|---------------------------------------------------------|
| Interactive | `layerx IMAGE_OR_ARCHIVE`                            | Exploring layers, diffs, file contents, wasted bytes    |
| CI          | `layerx ci IMAGE_OR_ARCHIVE` or `CI=true layerx ...` | Pipeline gates on efficiency / wasted bytes             |
| Compare     | `layerx compare OLD NEW`                             | Side-by-side deltas between two builds; CI regression gate |
| Export      | `layerx --json out.json IMAGE_OR_ARCHIVE`            | Scripts, dashboards, `jq`                               |

`IMAGE_OR_ARCHIVE` is auto-detected: an existing file is read directly without contacting any container runtime, anything else is resolved through the Docker daemon. All three modes accept either form.

Deeper guides live in [`docs/`](docs/):

- [Configuration reference](docs/configuration.md) — every `.layerx.yaml` field, both path-rules forms, starter flavours
- [CI integration](docs/ci-integration.md) — GitHub Actions and GitLab CI recipes, threshold recommendations, exit codes
- [JSON export](docs/json-export.md) — full schema, `jq` one-liners, scripting use cases

### Interactive explorer

- Browse layers with vim keys; see Dockerfile command, size, short digest
- Per-layer file tree with diff colouring (green = added, yellow = modified, red = removed)
- Open files inline with line numbers, scrolling, and in-viewer search
- Sort by size, filter by name, hide unchanged files
- Extract a file to disk, copy a path, or copy file contents to your clipboard (works over SSH and tmux)
- Efficiency score and wasted bytes always visible in the status bar

See [TUI keybindings](#tui-keybindings) for the full shortcut list.

### CI mode

- Three configurable rules: lowest efficiency, highest wasted bytes, highest user-wasted percent
- Exits `0` on pass, `1` on rule failure, `2` on internal error
- Configurable via `.layerx.yaml` or CLI flags

### Compare two images

```bash
# Compare a release tag against the previous one
layerx compare myapp:1.4.0 myapp:1.5.0

# Mix archives and refs freely
layerx compare ./build/prev.tar myapp:next

# Show every diff entry instead of the top-N summary
layerx compare --mode full myapp:old myapp:new
```

- Reports size, efficiency, layer, file, and waste deltas in a deterministic text report with consistently aligned columns
- Default compact mode shows the largest deltas per section with a `... and N more` counter; `--mode full` prints everything; `--mode summary` keeps only the header and verdict
- Last line is always machine-parseable: `verdict: ok`, `verdict: regression reason=efficiency,waste`, `verdict: noop digest=<sha256>` when both sides resolve to the same image, or `verdict: noop reason=path-equal` when both arguments are the same archive path and no digest is observable
- Exit codes: `0` no regression, `1` regression detected, `2` operational error (daemon down, archive missing, etc.)
- Live progress on stderr while resolving remote images — pulling, exporting, parsing, and the resolved digest are all surfaced per side. Pipe `2>/dev/null` to silence; stdout stays grep-clean for CI gating
- Running `layerx compare` with no arguments prints a short usage hint with concrete examples instead of an opaque error

### JSON export

Full analysis (layers, files, efficiency) as JSON — pipe through `jq` for scripted checks. See [docs/json-export.md](docs/json-export.md) for the full schema, jq one-liners, and scripting recipes.

---

## Prerequisites

Docker is required when inspecting Docker image references (`nginx:latest`, `myregistry/app:tag`). It is **not** required when inspecting a local archive file (`docker save` output or OCI layout tarball) — archive mode reads the file directly.

| Platform | Install                                                                                  |
|----------|------------------------------------------------------------------------------------------|
| Linux    | [Docker Engine](https://docs.docker.com/engine/install/)                                 |
| macOS    | [Docker Desktop](https://docs.docker.com/desktop/setup/install/mac-install/)             |
| Windows  | [Docker Desktop](https://docs.docker.com/desktop/setup/install/windows-install/)         |

---

## Install

### Homebrew (macOS & Linux)

```bash
brew install deveshctl/tap/layerx
```

### Scoop (Windows)

```bash
scoop bucket add layerx https://github.com/deveshctl/scoop-bucket
scoop install layerx
```

### Debian / Ubuntu

```bash
# amd64
curl -LO https://github.com/deveshctl/layerx/releases/latest/download/layerx_linux_amd64.deb
sudo dpkg -i layerx_linux_amd64.deb

# arm64
curl -LO https://github.com/deveshctl/layerx/releases/latest/download/layerx_linux_arm64.deb
sudo dpkg -i layerx_linux_arm64.deb
```

### RHEL / Fedora

```bash
# amd64
curl -LO https://github.com/deveshctl/layerx/releases/latest/download/layerx_linux_amd64.rpm
sudo rpm -i layerx_linux_amd64.rpm

# arm64
curl -LO https://github.com/deveshctl/layerx/releases/latest/download/layerx_linux_arm64.rpm
sudo rpm -i layerx_linux_arm64.rpm
```

### Direct download

Prebuilt binaries for Linux, macOS, and Windows (amd64 + arm64) on the [Releases page](https://github.com/deveshctl/layerx/releases). For a specific version, replace `latest` with the tag (e.g. `v1.3.0`) in the URLs above.

### Build from source

Requires Go 1.26+:

```bash
go install github.com/deveshctl/layerx@latest
```

---

## Usage

```bash
# Interactive TUI — Docker reference
layerx nginx:latest

# Interactive TUI — local archive (no daemon required)
layerx ./build/app.tar

# Force a fresh analysis (bypass the cache)
layerx --no-cache nginx:latest

# CI mode — exit 1 if efficiency < 95% (works for both inputs)
layerx ci --lowest-efficiency 0.95 nginx:latest
layerx ci --lowest-efficiency 0.95 ./build/app.tar

# JSON export
layerx --json analysis.json nginx:latest

# Shell completion (bash)
source <(layerx completion bash)
```

### TUI keybindings

| Key         | Action                                                       |
|-------------|--------------------------------------------------------------|
| `Tab`       | Switch panel (layers ↔ file tree)                            |
| `j` / `k`   | Move up / down                                               |
| `g` / `G`   | Jump to top / bottom                                         |
| `Enter`     | Open file viewer; expand or collapse a folder                |
| `Esc`       | Dismiss (close search → close viewer → close waste → clear filter → close help). Quits only on the loading and error screens. |
| `/`         | Filter file tree (tree) / search in viewer (viewer)          |
| `n` / `N`   | Next / previous search match (viewer)                        |
| `y`         | Copy file path to clipboard                                  |
| `Y`         | Copy file content (viewer) or layer command (layers)         |
| `d`         | Toggle diff-only mode (hide unchanged files)                 |
| `s`         | Cycle sort: default → largest → smallest                     |
| `x`         | Extract focused file to disk                                 |
| `?`         | Toggle help overlay                                          |
| `q`         | Quit                                                         |

---

## CI mode

### GitHub Actions

```yaml
- name: Install layerx
  run: |
    curl -LO https://github.com/deveshctl/layerx/releases/latest/download/layerx_linux_amd64.deb
    sudo dpkg -i layerx_linux_amd64.deb

- name: Check image efficiency
  run: layerx ci --lowest-efficiency 0.95 myapp:${{ github.sha }}
```

### Configuration

Drop a `.layerx.yaml` in your project root:

```yaml
version: 1

rules:
  lowest-efficiency: 0.9
  highest-wasted-bytes: 52428800    # 50MB
  highest-user-wasted-percent: 0.1

path-rules:
  block:
    - "**/.git/**"
    - /tmp/**
  deny-waste:
    - "**/*.pyc"
```

See `layerx init` (below) for ready-made configs by language.

CLI flags override config-file values. Setting a threshold to `0` or negative disables that rule.

For the full field reference, path-rule semantics, and worked examples, see [docs/configuration.md](docs/configuration.md). For end-to-end CI/CD recipes (GitHub Actions, GitLab CI, threshold recommendations, exit-code reference), see [docs/ci-integration.md](docs/ci-integration.md).

### Starter configs

Run `layerx init` to drop a ready-made `.layerx.yaml` in your repo:

```bash
layerx init --flavour node       # Node.js / npm / yarn / pnpm
layerx init --flavour python     # CPython, .pyc and __pycache__ rules
layerx init --flavour java       # Maven, Gradle, multi-stage targets
layerx init --flavour go         # tighter thresholds for Go images
layerx init --flavour generic    # baseline — works for any stack
```

Each starter blocks build-time caches (`/root/.npm/...`, `/root/.cache/pip/...`,
etc.) and version-control metadata, and flags wasteful layer patterns
(`node_modules` reinstalled per layer, `.pyc` files duplicated). Edit the
file after init to tune for your repo.

The starter configs live in [`cmd/examples/`](cmd/examples/) for browsing
or copy-paste.

---

## Caching & environment

| Variable           | Purpose                                                        |
|--------------------|----------------------------------------------------------------|
| `CI=true`          | Treat `layerx IMAGE` (no subcommand) as `layerx ci IMAGE`      |
| `LAYERX_CACHE_DIR` | Override the default analysis cache directory                  |

Repeat runs against an unchanged image digest reuse the cache and skip the tar export and parse. `--no-cache` (alias `--refresh`) bypasses the cache for a single run; the run still refreshes the cache on success.

---

## Troubleshooting

- **"Cannot connect to the Docker daemon"** — Docker isn't running. Start Docker Desktop, or `sudo systemctl start docker` on Linux. (Tip: if your image is already saved as a tarball, pass the file path instead — no daemon required.)
- **"Archive not found"** — the path you passed doesn't exist or isn't a regular file. Check spelling and that you're not pointing at a directory.
- **"Not a valid image archive"** — the file exists but isn't a `docker save` or OCI layout tarball. Re-export with `docker save -o image.tar IMAGE` or build with `--output type=oci,dest=image.tar`.
- **"image not found"** — layerx pulls images on demand. Check the reference and that you can `docker pull` it manually.
- **Cache permission errors** — point `LAYERX_CACHE_DIR` somewhere writable, e.g. `LAYERX_CACHE_DIR=$HOME/.cache/layerx`.

---

## Contributing

Issues and PRs welcome. For larger changes, please open an issue first to discuss the approach. See [CHANGELOG](CHANGELOG.md) for release notes.

---

## Architecture

```
image/    Domain layer — Docker SDK, tar parsing, file tree, efficiency
tui/      Bubbletea v2 TUI — consumes image/ interfaces only
ci/       CI evaluator — consumes image/ interfaces only
cmd/      Cobra CLI — wires packages together
config/   .layerx.yaml loader
```

Design rules:

- `image/` has zero imports from `tui/`, `ci/`, or `config/`
- TUI and CI consume interfaces, never concrete Docker SDK types
- All Docker client calls negotiate the API version (no breakage on Docker Engine upgrades)
- Both whiteout conventions handled correctly (regular and opaque)

---

## Tech stack

| Concern  | Choice                                                                                         |
|----------|------------------------------------------------------------------------------------------------|
| Language | Go 1.26+                                                                                       |
| CLI      | [cobra](https://github.com/spf13/cobra)                                                        |
| TUI      | [bubbletea v2](https://github.com/charmbracelet/bubbletea) + [lipgloss v2](https://github.com/charmbracelet/lipgloss) + [bubbles v2](https://github.com/charmbracelet/bubbles) |
| Docker   | [moby/moby client](https://github.com/moby/moby)                                               |
| Config   | [goccy/go-yaml](https://github.com/goccy/go-yaml)                                              |
| Testing  | [testify](https://github.com/stretchr/testify)                                                 |

---

## License

MIT
