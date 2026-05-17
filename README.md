# layerx

A terminal-based Docker image layer inspector. Point it at any Docker image and get an interactive TUI to browse layers, explore filesystem changes, view file contents, and run CI efficiency checks.

Single binary. Zero runtime dependencies beyond a running Docker daemon.

**Inspired by [dive](https://github.com/wagoodman/dive)** — layerx ships the features dive users have requested since 2018 but never received: file content viewing, sort-by-size, working OCI file extraction, and CI automation.

![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-blue)
![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey)

---

## Features

### Interactive TUI
- **Layer browser** — navigate layers with vim keys (j/k), see Dockerfile command, size, and short digest
- **File tree** — columnar display with permissions, UID:GID, size, and diff colouring (green=added, yellow=modified, red=removed)
- **Layer stacking** — correct filesystem view with `.wh.<name>` and `.wh..wh..opq` whiteout handling
- **File content viewer** — press Enter on any file to view its contents inline with line numbers and scrolling
- **Viewer search** — `/` in viewer opens search input with inline highlighting, `n`/`N` navigate matches
- **Layer origin annotations** — file tree shows `(LN)` suffix for files introduced in a different layer; viewer title shows origin layer and command
- **Clipboard (OSC52)** — `y` copies file path, `Y` copies file content (viewer) or layer command (layers panel); works over SSH/tmux
- **Filter** — `/` opens substring filter, Enter confirms, Backspace-on-empty clears
- **Diff-only mode** — `d` hides unchanged files, shows only added/modified/removed
- **Sort by size** — `s` cycles: default → largest first → smallest first
- **File extraction** — `x` saves the focused file to your working directory
- **Efficiency badge** — score percentage and wasted bytes in the status bar

### CI Mode
- `layerx ci <image>` — evaluates image efficiency against configurable thresholds
- Three rules: `lowest-efficiency`, `highest-wasted-bytes`, `highest-user-wasted-percent`
- Exits 0 (pass) or 1 (fail) with human-readable report
- `CI=true layerx <image>` triggers CI mode from root command
- Configurable via `.layerx.yaml` or CLI flags

### JSON Export
- `layerx --json <path> <image>` — full analysis to JSON (layers, files, efficiency)
- Pipe through `jq` for scripted analysis

### Shell Completion
- `layerx completion [bash|zsh|fish|powershell]`
- Custom completer suggests local Docker images

---

## Install

### From source

```bash
go install github.com/deveshpharswan/layerx@latest
```

### From release binary

Download from [Releases](https://github.com/deveshpharswan/layerx/releases) — prebuilt for Linux, macOS, and Windows (amd64).

---

## Usage

```bash
# Interactive TUI
layerx nginx:latest

# CI mode — exit 1 if efficiency < 95%
layerx ci --lowest-efficiency 0.95 nginx:latest

# JSON export
layerx --json analysis.json nginx:latest

# Shell completion (bash)
source <(layerx completion bash)
```

### TUI Keybindings

| Key | Action |
|-----|--------|
| `Tab` | Switch panel (layers ↔ file tree) |
| `j` / `k` | Navigate up/down |
| `g` / `G` | Jump to top/bottom |
| `Enter` | Open file content viewer |
| `Esc` | Back (close search → close viewer → clear filter → quit) |
| `/` | Filter file tree (tree) / search in viewer (viewer) |
| `n` / `N` | Next / previous search match (viewer) |
| `y` | Copy file path to clipboard |
| `Y` | Copy file content (viewer) / layer command (layers) |
| `d` | Toggle diff-only mode |
| `s` | Cycle sort (default → largest → smallest) |
| `x` | Extract file to disk |
| `?` | Toggle help overlay |
| `q` | Quit |

### Configuration

Create `.layerx.yaml` in your project root:

```yaml
rules:
  lowest-efficiency: 0.9
  highest-wasted-bytes: 52428800    # 50MB
  highest-user-wasted-percent: 0.1
```

---

## How It Compares to dive

| Feature | dive v0.13 | layerx |
|---------|:----------:|:------:|
| Layer browser with diff view | ✅ | ✅ |
| File tree with Add/Modify/Remove colouring | ✅ | ✅ |
| Filter by name | ✅ | ✅ |
| Efficiency score + CI mode | ✅ | ✅ |
| JSON export | ✅ | ✅ |
| Config file | ✅ | ✅ |
| Vim navigation | ✅ | ✅ |
| File permissions + UID:GID display | ✅ | ✅ |
| **File content viewer** | ❌ | ✅ |
| **Viewer search with highlighting** | ❌ | ✅ |
| **Clipboard (OSC52)** | ❌ | ✅ |
| **Layer origin tracking** | ❌ | ✅ |
| **Sort by size** | ❌ | ✅ |
| **File extraction (OCI-compatible)** | ⚠️ broken | ✅ |
| **Shell completion** | ❌ | ✅ |
| Docker 25+ OCI format support | ⚠️ partial | ✅ |

The features marked **bold** represent 450+ combined reactions on dive's issue tracker (issues [#224](https://github.com/wagoodman/dive/issues/224), [#336](https://github.com/wagoodman/dive/issues/336), [#89](https://github.com/wagoodman/dive/issues/89), [#341](https://github.com/wagoodman/dive/issues/341), [#525](https://github.com/wagoodman/dive/issues/525)).

---

## Architecture

```
image/         Domain layer — Docker SDK, tar parsing, file tree, efficiency analysis
tui/           Bubbletea v2 TUI — consumes image/ interfaces only
ci/            CI evaluator — consumes image/ interfaces only
cmd/           Cobra CLI — wires packages together
config/        .layerx.yaml loader
```

Key design rules:
- `image/` has zero imports from `tui/`, `ci/`, or `config/`
- TUI and CI consume interfaces, never concrete Docker SDK types
- All Docker client calls use `client.WithAPIVersionNegotiation()` (prevents breakage on Docker Engine upgrades)
- Both whiteout conventions handled correctly (regular + opaque)

---

## Tech Stack

| Concern | Choice |
|---------|--------|
| Language | Go 1.26+ |
| CLI | [cobra](https://github.com/spf13/cobra) |
| TUI | [bubbletea v2](https://github.com/charmbracelet/bubbletea) + [lipgloss v2](https://github.com/charmbracelet/lipgloss) + [bubbles v2](https://github.com/charmbracelet/bubbles) |
| Docker | [moby/moby client](https://github.com/moby/moby) |
| Config | [goccy/go-yaml](https://github.com/goccy/go-yaml) |
| Testing | [testify](https://github.com/stretchr/testify) |

---

## Development Status

layerx is under active development. Current milestone progress:

- [x] M01 — Docker plumbing proof
- [x] M02 — Layer metadata table
- [x] M03 — Bubbletea layout proof
- [x] M04 — Live layer list in TUI
- [x] M05 — File tree in TUI
- [x] M06 — File tree filter + toggle
- [x] M07 — Sort by size
- [x] M08 — File content viewer
- [x] M09 — Efficiency score + wasted bytes
- [x] M10 — File extraction to disk
- [x] M11 — CI mode
- [x] M12 — Config file
- [x] M13 — Shell completion
- [x] M14 — JSON export
- [ ] M15 — MCP server *(deferred)*
- [x] M16 — Clipboard, viewer search, layer origin annotations

---

## License

MIT
