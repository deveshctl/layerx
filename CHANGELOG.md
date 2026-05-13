# Changelog

All notable changes to this project will be documented in this file.

## [M06+M07] — 2026-05-13

File tree filter, diff-only toggle, and sort-by-size.

### Added
- `/` opens substring filter input; `Esc` clears, `Enter` keeps filter active
- `d` toggles diff-only mode (hides unchanged files, shows only added/modified/removed)
- `s` cycles sort: default tree order → largest first → smallest first → default
- All three compose together (filter + diff + sort simultaneously)
- Status bar shows `[diff]` and `[↓size]`/`[↑size]` indicators when active
- Help overlay updated with new File Tree section

### Changed
- Esc key now has precedence: closes filter → closes help → quits
- Sort resets to default on layer switch; filter and diff-only persist
- File tree displays flat paths (no indentation) when sort is active
- Panel title shows active filter query: `FILE TREE [3/12] "nginx"`

### Technical
- Pure-function pipeline: `displayTree()` = `currentFlatTree()` → `applyDiffFilter()` → `applySubstringFilter()` → `applySortBySize()`
- No changes to `image/` package — all transformations are view-layer only
- Gate C pending: Verify filter/diff/sort interactions on nginx:latest and ubuntu:latest

## [M04+M05] — 2026-05-13

Live Docker data in TUI + file tree with layer stacking and whiteout handling.

### Added
- Real image layers shown in TUI (replacing hardcoded fake data)
- Async loading with spinner, friendly error messages for daemon/pull failures
- Full file tree parsing from layer tars (`ParseLayerTar`)
- Layer stacking with `.wh.<name>` and `.wh..wh..opq` whiteout support (`Stack`)
- DiffType colouring: green=Added, yellow=Modified, red=Removed, grey=Unchanged
- `Analyze()` orchestrator as single entry point for TUI and CI
- Structured error types: `ErrDaemonNotRunning`, `ErrPullFailed`, `ErrImageNotFound`
- `FileNode` helpers: `FindChild`, `AddChild`, `RemoveChild`, `Walk`, `NewFileTree`

### Changed
- `cmd/root.go`: creates resolver, passes image ref + resolver into TUI
- `tui/model.go`: state machine (loading/ready/error), async fetch via `tea.Cmd`
- `tui/layers.go`: renders real `image.Layer` data with `FormatBytes` sizes
- `tui/filetree.go`: renders real `image.FileNode` with depth-first flattening

### Technical
- Domain layer (`image/`) has zero UI imports — fully testable in isolation
- Whiteout handling: regular (`.wh.<name>`) and opaque (`.wh..wh..opq`) both implemented
- Non-fatal layer tar parsing: invalid/empty tars leave `Layer.Tree` as nil
- Gate C pending: Verify on second machine against nginx:latest, alpine:latest, ubuntu:latest

## [M03] — 2026-05-12

Bubbletea layout proof. `layerx <any-image>` launches interactive TUI with fake data.

### Added
- `tui/` package: bubbletea v2 Model with 2-panel layout (layers + file tree)
- Responsive layout: adapts to terminal resize, minimum 50x10 enforced
- Navigation: Tab switches panels, j/k moves cursor, g/G jump to top/bottom
- Adaptive styling: lipgloss v2 with rounded borders, diff colouring
- Hardcoded fake data: 6-layer Go multi-stage build with per-layer file trees
- Diff colouring: green (added), yellow (modified), red (removed), gray (unchanged)
- Command preview: bottom of left panel shows full Dockerfile instruction

### Changed
- `cmd/root.go`: launches TUI instead of printing table to stdout

### Technical
- Import: `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`
- Viewport scroll logic for file trees exceeding panel height
- Focus management with visual border colour indicator (purple focused, gray unfocused)

## [M02] — 2026-05-12

Layer metadata table. `layerx nginx:latest` prints layer table with index, ID, size, and command.

### Added
- Layer size parsing from tar entry headers
- Dockerfile command correlation from image config history
- Human-readable byte formatting (`FormatBytes`)
- Formatted table output with aligned columns
- OCI format support (Docker 25+ `blobs/sha256/` layout)

### Technical
- Single-pass tar scan: manifest + config + layer sizes in one read
- Empty layer filtering for correct command-to-layer correlation

## [M01] — 2026-05-12

Docker plumbing proof. `layerx nginx:latest` prints layer count to stdout.

### Added
- `image/` package: `Resolver` interface, `DockerResolver` implementation
- Docker image tar export and `manifest.json` parsing
- Automatic image pull if not available locally
- `cmd/` package: cobra CLI with `ExactArgs(1)` validation
- CI pipeline: build, vet, test, cross-compile (linux/darwin/windows)
- Unit tests for tar parsing logic

### Technical
- Uses `github.com/moby/moby/client` with `WithAPIVersionNegotiation()`
- `FileTree`, `FileNode`, `DiffType` types defined as placeholders for M05
- `Layer` struct includes all fields needed through M05 (Size, Command, Tree)
