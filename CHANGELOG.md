# Changelog

All notable changes to this project will be documented in this file.

## [M11+M12] — 2026-05-15
CI mode and config file — automated efficiency checks for pipelines.

### M11 — CI Mode
- `layerx ci <image>`: evaluates efficiency against configurable thresholds, exits 0/1.
- Three independent rules: `lowest-efficiency`, `highest-wasted-bytes`, `highest-user-wasted-percent`.
- `CI=true layerx <image>` triggers CI mode from root command (no TUI).
- Human-readable report with aligned columns, wasted file listing.
- All three rules independently configurable via CLI flags.

### M12 — Config File
- `.layerx.yaml` in working directory sets CI rule thresholds.
- Missing config silently uses defaults; invalid YAML produces clear error.
- Partial config merges with defaults (unspecified fields keep default values).
- CLI flags override config file values.
- Config example documented in `layerx ci --help`.

### Fixed
- OCI layer blobs: handle gzip-compressed blobs from Docker 25+ (`decompressIfGzip`).
- Removed keybinding override feature (caused status bar/help mismatch with actual keys).

## [M09+M10] — 2026-05-14
Efficiency analysis and file extraction to disk.

### M09 — Efficiency Score + Wasted Bytes
- `image/efficiency.go`: Detects files duplicated across layers.
- Algorithm: file at same path in N layers → N-1 occurrences are waste.
- Score displayed as percentage badge in status bar.
- Wasted bytes shown alongside score when > 0.
- Gate C: Test against image with known waste (apt-get install in separate RUN steps).

### M10 — File Extraction to Disk
- `x` key saves focused file to current working directory.
- Reuses Docker CopyFromContainer via `ExtractRaw` (no 1MB truncation).
- Status bar confirmation: "Saved: filename" or error message.
- Works for binary files (raw bytes, no truncation).
- Gate C: Extract a file, verify it appears on disk with correct bytes.

## [M08] — 2026-05-14

File content viewer — the #1 most-requested feature dive has never shipped.

### Added
- `Enter` on any file in the tree opens an inline content viewer
- Scrollable text with line numbers (j/k, g/G navigation)
- Binary file detection with notice (ELF, images, archives, etc.)
- Empty file notice for 0-byte files
- 1 MB size limit with truncation notice for large files
- Async extraction with loading spinner (no TUI freeze)
- `Esc` returns to file tree from viewer
- Status bar shows line position and scroll percentage in viewer mode
- Help overlay updated with File Viewer section

### Technical
- `image/extractor.go`: Extractor interface + DockerExtractor (container create → CopyFromContainer → remove)
- `tui/fileview.go`: viewer panel with line numbers, notices, scrolling
- Binary detection: net/http.DetectContentType + null-byte scan (matches Git's heuristic)
- Container cleanup guaranteed via defer (no orphan containers on error)

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
