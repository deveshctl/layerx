# layerx — AI Session Context

Read this entire file before touching any code or asking questions. It contains everything needed to work on this project.

---

## What is layerx?

A terminal-based Docker image layer inspector — a CLI tool written in Go. You point it at a Docker image and get an interactive TUI where you can browse layers, explore filesystem changes per layer, view file contents, and run CI efficiency checks.

Single binary. Zero runtime dependencies beyond a running Docker daemon.

**Inspired by:** `wagoodman/dive` (53k+ stars). layerx is NOT a fork. It solves the same problem with a better architecture and a superset of dive's features — specifically the features dive users have been requesting since 2018 that dive has never shipped.

**Purpose:** Portfolio piece targeting senior backend/platform/DevOps roles. Signals: Docker internals knowledge, TUI engineering, product judgment (identified a real gap, built exactly that).

---

## Development Constraints (READ FIRST)

| Constraint | Detail |
|---|---|
| Dev machine | Windows 11. Cannot run `.exe` or Docker. |
| `go test` | Blocked by Windows Defender admin policy. Do NOT run locally. |
| Verification on dev machine | `go build ./...` and `go vet ./...` only. |
| Test execution | GitHub Actions CI only (Linux runner). |
| Binary testing | Push to GitHub → pull on second machine → test there. |
| Docker | Required at runtime. All testing against local images on second machine. |
| No paid cloud | All testing against local Docker images only. |

**Never suggest running `go test`, `.exe` files, or Docker commands on the dev machine.**

---

## Tech Stack (locked — do not change without explicit instruction)

| Concern | Choice | Import Path | Reason |
|---|---|---|---|
| Language | Go 1.24+ | — | Docker SDK, single binary, TUI ecosystem |
| CLI framework | cobra v1.10.x | `github.com/spf13/cobra` | Subcommands needed (`ci`, `mcp`, `completion`) |
| TUI framework | bubbletea v2 | `charm.land/bubbletea/v2` | Elm architecture, declarative view, cursed renderer |
| TUI styling | lipgloss v2 | `charm.land/lipgloss/v2` | Pairs with bubbletea v2 |
| TUI components | bubbles v2 | `charm.land/bubbles/v2` | viewport for scrolling, list, textinput |
| Docker SDK | moby client | `github.com/moby/moby/client` | `docker/docker` deprecated Nov 2025 (Docker v29) |
| Config | goccy/go-yaml | `github.com/goccy/go-yaml` | `gopkg.in/yaml.v3` archived Apr 2025 |
| Testing | testify v1.11.x | `github.com/stretchr/testify` | Standard |

**All Docker client calls must use `client.WithAPIVersionNegotiation()`** — prevents breakage on Docker Engine upgrades (dive broke on v26, v27, v29).

**Migration notes (May 2026):**
- `github.com/docker/docker` is deprecated as of Docker v29. Use `github.com/moby/moby/client` — same API, same negotiation option, actively maintained.
- `gopkg.in/yaml.v3` (go-yaml/yaml) was archived by its maintainer Apr 2025. `goccy/go-yaml` (v1.19.x, 2.2k stars) is the actively maintained replacement.
- bubbletea v2 stable since Feb 2024. New import domain `charm.land/` replaces `github.com/charmbracelet/`. Elm architecture preserved (Init/Update/View).

---

## Repository Layout

```
layerx/
├── main.go              entry point only — no logic
├── cmd/
│   └── root.go          cobra root + subcommands (inspect, ci, mcp, completion)
├── image/               domain layer — ZERO UI imports
│   ├── resolver.go      Resolver interface
│   ├── docker.go        Docker SDK implementation of Resolver
│   ├── layer.go         Layer struct + tar parsing
│   ├── filetree.go      FileTree, FileNode, Stack(), CompareAndMark()
│   ├── efficiency.go    Efficiency() → score + wasted bytes
│   ├── extractor.go     ExtractFile() → raw bytes via CopyFromContainer
│   └── analysis.go      Analyze([]Layer) → Analysis
├── tui/
│   ├── model.go         root bubbletea model
│   ├── layers.go        layer list panel
│   ├── filetree.go      file tree panel + filter
│   ├── fileview.go      file content viewer
│   ├── statusbar.go     bottom key hints bar
│   ├── keymap.go        all key bindings
│   └── styles.go        all lipgloss styles
├── ci/
│   ├── evaluator.go     run rules, print report, exit code
│   └── rules.go         LowestEfficiency, HighestWastedBytes, HighestUserWastedPercent
├── mcp/
│   └── server.go        MCP stdio server exposing image analysis as tools
└── config/
    └── config.go        .layerx.yaml loader
```

---

## Architectural Laws (non-negotiable)

```
1. image/ has zero imports from tui/, ci/, config/, or mcp/
2. tui/ imports only image/ interfaces — never concrete Docker SDK types
3. ci/ imports only image/ interfaces — never tui/
4. mcp/ imports only image/ interfaces — never tui/
5. cmd/ is the only package allowed to wire packages together
6. No package-level mutable vars in tui/ — everything through bubbletea Model
7. Every public type in image/ is an interface or plain value struct
```

Breaking rule 1 or 2 means the feature is in the wrong layer — fix the architecture, not the rule.

---

## Core image/ Interfaces (the contracts TUI and CI consume)

```go
// image/resolver.go
type Resolver interface {
    Resolve(imageRef string) ([]Layer, error)
}

// image/layer.go
type Layer struct {
    Index   int
    ID      string    // short digest
    Size    int64
    Command string    // Dockerfile instruction that created this layer
    Tree    *FileTree // populated after parsing
}

// image/filetree.go
type DiffType int
const (
    Unchanged DiffType = iota
    Added
    Modified
    Removed
)

type FileNode struct {
    Path     string
    Size     int64
    DiffType DiffType
    Children []*FileNode
    IsDir    bool
}

type FileTree struct {
    Root *FileNode
}

// image/efficiency.go
type EfficiencyResult struct {
    Score       float64       // 0.0–1.0
    WastedBytes int64
    WastedFiles []WastedFile
}

type WastedFile struct {
    Path        string
    TotalWasted int64
    LayerCount  int
}

// image/extractor.go
type Extractor interface {
    ExtractFile(imageRef string, path string) ([]byte, error)
}
```

---

## Whiteout File Handling (implement correctly from day one)

Docker overlay filesystem uses two whiteout conventions:
- `.wh.<name>` — delete the file `<name>` in the same directory
- `.wh..wh..opq` — opaque whiteout: delete ALL contents of the directory below this layer

Both must be handled in `Stack()`. dive has had bugs with opaque whiteouts. Get it right from M01.

---

## The Build Framework

### Governing Rule
Every milestone is a vertical slice: domain code + TUI wiring + quality gate. The gate must pass before the next milestone starts. No exceptions.

### The Hard Gate (3 parts, all required)

**Gate A — Dev machine (before push):**
```
[ ] go build ./...   — clean compile, zero errors
[ ] go vet ./...     — zero warnings
[ ] CHANGELOG.md updated with ## [MXX] — YYYY-MM-DD section
[ ] No TODO/FIXME in new code
```

**Gate B — CI (after push, before tagging):**
```
[ ] go test ./... passes on GitHub Actions Linux runner
[ ] Builds for linux/amd64, darwin/amd64, windows/amd64
[ ] go vet passes
```

**Gate C — Second machine (before tagging the release):**
```
[ ] Binary runs against nginx:latest (standard multi-layer image)
[ ] Binary runs against alpine:latest (thin image)
[ ] q / Ctrl+C exits cleanly from every screen
[ ] Docker daemon not running → clear error message, no panic
[ ] New milestone feature works as described
[ ] Previous milestone features still work (regression check)
```

**Tagging rule:** Tag only after Gate C passes. Gate B green is necessary but not sufficient.

### Branch Strategy
```
main         — always tagged stable. Never broken.
feat/m01     — Milestone 1 work
feat/m02     — branches from main after m01 tag exists
...
```

One branch per milestone. Branch from `main` only after the previous milestone's tag exists on `main`.

### CHANGELOG.md Format
Each milestone gets one section before merge:
```markdown
## [M01] — 2026-05-12
Docker plumbing proof. `layerx nginx:latest` prints layer count to stdout.
Gate C: Verified against nginx:latest and alpine:latest on second machine.
```

---

## The 15 Milestones

Every milestone produces a testable binary. No domain-only milestones.

### Phase 0 — Risk Elimination

**M01 — Docker plumbing proof**
- Build: `image/docker.go` (connect + pull + tar export), `cmd/root.go` (print layer count)
- Done: `layerx nginx:latest` prints "Found N layers". No TUI.
- Gate C extra: Test against an image not yet pulled locally (tests pull path).

**M02 — Layer metadata table**
- Build: `image/layer.go` (parse tar → []Layer with ID, size, command), plain text table to stdout
- Done: Prints table with index, short ID, size, command for every layer.
- Gate C extra: Run against `ubuntu:latest` (many layers, long commands). Verify no truncation.

**M03 — Bubbletea layout proof**
- Build: `tui/model.go`, `tui/styles.go`, `tui/keymap.go` — hardcoded fake data, layout + navigation only
- Done: TUI launches. Tab switches panels. j/k navigates. q quits. Resize works. 80-col narrow fallback works.
- Gate C: Layout and resize checks only — data is fake, that is expected for this milestone.

### Phase 1 — Core Value

**M04 — Live layer list in TUI**
- Build: Wire `image/` into `tui/model.go`. Loading state while fetching. Error state if daemon is down.
- Done: Real layers in left panel. Selecting highlights. Clean error (not panic) if daemon down.
- Gate C extra: Kill Docker daemon mid-run — verify readable error message.

**M05 — File tree in TUI**
- Build: `image/filetree.go` (parse tar → FileTree, CompareAndMark), `image/stack.go` (Stack() with both whiteout types), `tui/filetree.go` (colour-coded, virtual scrolling via bubbles/viewport)
- Done: Selecting a layer shows correct file tree. Colours match diff type (green=added, red=removed, yellow=modified, grey=unchanged). No lag on `ubuntu:latest`.
- Gate C extra: Navigate all layers on ubuntu:latest. Verify correct colouring and no lag.

**M06 — File tree filter + toggle**
- Build: `/` opens filter input (substring match), `Esc` clears. `c` key toggles show-only-changed. Both persist correctly on layer switch.
- Done: Filter reduces visible files live. `c` hides unchanged files. Both combine correctly.
- Gate C extra: Filter → switch layer → clear filter — verify state throughout.

**M07 — Sort by size**
- Build: `s` key toggles sort-by-size (largest first) in file tree panel. Sort applied after stacking, before render.
- Done: `s` reorders with largest files at top. Combines with filter and changed-only toggle. Resets correctly on layer switch.
- Gate C extra: Sort a fat layer (ubuntu apt layer) — verify largest file is first.

### Phase 2 — The Headline Feature

**M08 — File content viewer**
- Build: `image/extractor.go` (ExtractFile via Docker SDK CopyFromContainer — create temp container, copy, remove), `tui/fileview.go` (inline pane, scrollable, async with loading spinner, Esc returns to tree). Binary file notice. 1MB size limit with truncation notice.
- Done: Enter on any text file shows content. Esc returns to tree. Binary → notice. >1MB → truncation notice. No TUI freeze during extraction. Works for OCI-format images (Docker 25+).
- Gate C extra: Open /etc/os-release (text). Open /bin/sh (binary). Open a large file. Mash Esc — no state corruption.

### Phase 3 — Reliability

**M09 — Efficiency score + wasted bytes**
- Build: `image/efficiency.go` (Efficiency() — path appears in >1 layer → all but first is waste). Score shown in status bar. Wasted bytes in layer panel footer.
- Done: Score shown as percentage. Wasted bytes shown. Algorithm: `score = 1 - (wastedBytes / totalBytes)`.
- Gate C extra: Test against an image with known waste (apt-get install + separate apt-get clean in different RUN steps).

**M10 — File extraction to disk**
- Build: `x` key saves focused file to current working directory. Reuses `image/extractor.go`. Confirmation in status bar. Error shown on permission denied.
- Done: `x` saves file to `./filename`. Correct content. Works for OCI-format images. Confirmation visible. No crash on error.
- Gate C extra: Extract a file, verify it appears on disk with correct bytes.

### Phase 4 — CI Mode and Config

**M11 — CI mode**
- Build: `ci/evaluator.go`, `ci/rules.go`, `cmd/ci.go` (cobra subcommand). Three rules: lowest-efficiency, highest-wasted-bytes, highest-user-wasted-percent. `CI=true layerx <image>` also triggers.
- Done: Clean image exits 0. Wasteful image exits 1 with human-readable report. All three rules independently configurable via flags.
- Gate C extra: `CI=true layerx nginx:latest; echo $?` — verify exit code.

**M12 — Config file**
- Build: `config/config.go` (load `.layerx.yaml`, merge with defaults). Wire into `cmd/root.go`. Config: CI thresholds, keybinding overrides, display preferences.
- Done: `.layerx.yaml` with `rules.lowest-efficiency: 0.95` changes CI threshold. Keybinding override works. Missing config silently ignored.
- Gate C extra: Override one keybinding, verify it works in TUI.

### Phase 5 — Distribution and Tooling

**M13 — Shell completion**
- Build: Register cobra's built-in completion command. Custom completer for image argument (suggests from `docker images` output).
- Done: `layerx completion bash` outputs valid completion script. Sourcing it makes subcommands tab-complete.
- Gate C extra: Source completion script, tab-complete `layerx ` — verify subcommands appear.

**M14 — JSON export**
- Build: `--json <path>` flag on root command. Skips TUI, marshals Analysis to JSON (layers + file lists + efficiency data). Required as prerequisite for M15 MCP.
- Done: `layerx --json /tmp/out.json nginx:latest` writes valid JSON. `jq '.layers | length'` returns correct layer count. Schema documented in CHANGELOG.
- Gate C extra: Pipe output through jq, verify layer count and file list structure.

**M15 — MCP server**
- Build: `cmd/mcp.go`, `mcp/server.go` (stdio transport). Tools: `analyze_image`, `list_layers`, `inspect_layer`, `read_file`, `get_efficiency`. Reuses image/ interfaces and extractor.go.
- Done: `layerx mcp` launches MCP server on stdio. Claude Desktop can connect. `analyze_image nginx:latest` returns structured layer data. `read_file nginx:latest /etc/nginx/nginx.conf` returns file contents.
- Gate C extra: Wire into Claude Desktop on second machine. Run analyze_image and read_file from a Claude conversation.

---

## Process Rules (enforced every milestone)

```
1. image/ never imports tui/, ci/, config/, mcp/
2. tui/ imports only image/ interfaces, not concrete types
3. One branch per milestone: feat/mNN
4. Branch from main only after previous milestone tag exists
5. Never merge to main with failing CI
6. Gate C (manual test) before tagging — not after
7. CHANGELOG.md updated before merge, not after
8. All Docker client calls use client.WithAPIVersionNegotiation()
9. Both whiteout types handled: .wh.<name> and .wh..wh..opq
10. No go test on Windows dev machine — CI only
11. No package-level mutable state in tui/
12. Error handling belongs in the milestone where the failure mode is introduced
```

---

## What dive Has vs What layerx Adds

### dive's current feature set (as of v0.13.1, March 2025)

| Feature | Status |
|---|---|
| Layer browser with diff view | ✅ |
| File tree (Add/Modify/Remove colouring) | ✅ |
| Current-layer vs aggregated toggle (Ctrl+L / Ctrl+A) | ✅ |
| Filter by change type (hide unchanged etc.) | ✅ |
| File name filter (Ctrl+F) | ✅ |
| Efficiency score + wasted bytes | ✅ |
| CI mode with configurable thresholds | ✅ |
| JSON export (incl. layer file data since v0.13) | ✅ |
| `dive build` wrapper (build + immediately inspect) | ✅ |
| Docker engine support | ✅ |
| Podman support (Linux/macOS) | ✅ |
| Docker archive support | ✅ |
| Config file (keybindings, CI thresholds) | ✅ |
| Vim navigation (h/j/k/l) added v0.13 | ✅ |
| File attributes (perms, uid/gid, setuid) added v0.13 | ✅ |
| File content viewer | ❌ |
| Sort file tree by size | ❌ |
| Multi-arch image selection | ❌ |
| nerdctl / containerd support | ❌ (PR #561 stalled Jan 2025) |
| File extraction (working, OCI-compatible) | ⚠️ broken for Docker 25+ OCI format |
| Shell completion | ❌ |
| MCP / AI agent integration | ❌ (PR #688 unmerged, March 2026) |
| Performance on large layers | ⚠️ recomputes full tree on every keypress |

### dive's most-requested open issues (by reaction count)

| Issue | Reactions | Open since |
|---|---|---|
| #224 — View/download files from UI | 153 | 2019 |
| #336 — See contents of file | 110 | 2021 |
| #89 — Order tree by size | 89 | 2018 |
| #568 — Is this project still alive? | 85 | 2025 |
| #341 — Sort by size in current layer | 47 | 2021 |
| #356 — Wrap long commands in layer details | 39 | 2021 |
| #367 — containerd / nerdctl support | 34 | 2021 |
| #442 — Specify arch for multi-arch image | 26 | 2023 |
| #323 — Sort leaves by size | 26 | 2020 |
| #525 — Show content of selected file | 19 | 2024 |
| #288 — Slows to crawl on large images | 18 | 2020 |
| #347 — Ignore patterns for CI calculations | 9 | 2021 |
| #239 — Bash completion | 8 | 2019 |

### dive's notable open PRs (long-unmerged)

| PR | What it adds | Open since |
|---|---|---|
| #472 | File extraction (original — has bugs) | Sep 2023 |
| #561 | nerdctl / containerd support | Jan 2025 |
| #555 | Track which layer introduced each file | Sep 2024 |
| #688 | Full MCP server mode | Mar 2026 |
| #693 | Fix file extraction for OCI images (Docker 25+) | Apr 2026 |
| #615 | `--export-metadata` flag (per-layer text export) | Jun 2025 |

### What layerx ships that dive never has

- File content viewer (M08) — 282 combined reactions across #224, #336, #525
- Sort by size (M07) — 162 combined reactions across #89, #341, #323
- Working file extraction for OCI images (M10) — dive's is broken
- Shell completion (M13) — cobra gives this almost for free
- MCP server (M15) — no competing tool has shipped this from a stable release

---

## What we are NOT building (v1 scope boundary)

- Web UI or HTTP server
- Daemon or background process
- Cloud integration of any kind
- Podman support (Docker only for v1)
- nerdctl / containerd support (Docker only for v1)
- Multi-arch image selection
- Image diff mode (`layerx diff img1 img2`)
- Plugin system
- `dive build` equivalent wrapper

---

## Reference Links

- dive repo: https://github.com/wagoodman/dive
- dive open issues (by reactions): https://github.com/wagoodman/dive/issues?q=is%3Aissue+is%3Aopen+sort%3Areactions-desc
- dive open PRs: https://github.com/wagoodman/dive/pulls?q=is%3Apr+is%3Aopen
- Bubbletea v2: https://github.com/charmbracelet/bubbletea (import: charm.land/bubbletea/v2)
- bubbles v2: https://github.com/charmbracelet/bubbles (import: charm.land/bubbles/v2)
- lipgloss v2: https://github.com/charmbracelet/lipgloss (import: charm.land/lipgloss/v2)
- Moby client (Docker SDK): https://github.com/moby/moby/tree/master/client
- goccy/go-yaml: https://github.com/goccy/go-yaml
- cobra: https://github.com/spf13/cobra
