# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]
### Added
- Per-image-digest analysis cache: `Analyze*` writes parsed layers to `{LAYERX_CACHE_DIR or os.UserCacheDir()/layerx}/{digest}/layers.gob` and reads them back on the next run. Repeat runs against an unchanged image skip Docker `ImageSave` + tar parse and feel instant.
- `--no-cache` flag (alias `--refresh`) on the root command and `layerx ci`. Bypasses the cache for the current run; the run still writes the cache on success.
- New `image.PhaseCacheLoad` progress phase; TUI renders "loaded from cache" briefly on hit.

### Changed
- Release artifact names dropped the version segment: `layerx_linux_amd64.deb` (was `layerx_1.1.0_linux_amd64.deb`). Applies to v1.1.1+.
- README install snippets now use `/releases/latest/download/<name>` so they auto-track the latest release. Older releases keep their versioned filenames.

### Technical
- New `image/cache.go`, `image/cache_dto.go`, `image/cache_test.go`. Encoding is `encoding/gob` with an envelope holding `digest`, `schemaVersion`, `cachedAt`, and `[]cachedLayer`. Schema mismatch, digest mismatch, and decode errors all delete the offending file and fall through to a cold resolve.
- `Resolver` gains `ImageID(ctx, ref) (string, error)`. The cache is wired at `image.AnalyzeWithOptions`; existing `Analyze` and `AnalyzeWithProgress` are preserved as shims. Architectural laws unchanged: `image/` still has zero imports of `tui/`, `ci/`, `cmd/`, `config/`.
- Cached fields per layer: `Index`, `ID`, `Size`, `Command`, parsed `Tree`. NOT cached: `NetDelta`, `DiffType`, `IntroducedInLayer` — recomputed by `Stack()` + `assignNetDeltas()` on every load.

## [M19] — 2026-05-21
Layer net-delta column — surfaces per-layer change in merged-filesystem live byte size.

### Added
- `image.Layer.NetDelta` populated by `Analyze`; layer 0 = full live size, layer i>0 = stacked[i] − stacked[i−1]; negative on cleanup layers
- `image.TreeLiveFileBytes` walks a tree summing live file bytes (skips directories and `Removed` subtrees)
- `image.FormatSignedBytes` for "+12 MB" / "-3.0 MB" / "0 B"
- TUI: layer panel shows signed **Change** column by default (`S` cycles Change → Stored → both); panel title uses plain labels (`change` / `stored` / `stored+change`)
- TUI: **Change** colored green when negative, accent when ≥10% of final live size, dim otherwise; `both` mode falls back to Change on narrow panels
- TUI: help overlay uses multi-column layout on wide terminals; dedicated **Layers** section; footnote explains Change vs Stored
- TUI: status bar shows **change** or **stored** for the selected layer when the layers panel is focused
- JSON export: each layer entry gains a `netDelta` field

### Technical
- New `image/size.go` (domain helper) + `image/size_test.go`; `cmd/json.go` and `tui/layers.go` consume the new field; no changes cross existing architectural rules
- `tui/help.go` extracted from `tui/model.go`; column-rendering loop generalised to handle 1–N columns safely; help-popup width clamp simplified (no redundant floor that could exceed screen width)

## [M18] — 2026-05-20
Waste Navigator — interactive overlay surfacing duplicated files and jumping to the layer that introduced each one.

### Added
- `w` opens a centred "Wasted Files" overlay listing the worst offenders by total wasted bytes
- Columns: `path` | wasted bytes | `xN` (layer count) | `L{intro}` (introducing layer, 1-indexed); narrow terminals drop the `xN` column
- `Enter` jumps to the introducing layer with the cursor positioned on the file; clears any active filter and disables `diff-only` if needed to surface the path
- `a` toggles between the top 20 (default) and the full list (capped at 500)
- `y` copies the highlighted path to the clipboard; modal stays open
- `Esc` closes the overlay; `q`/`Ctrl+C` quit
- Empty-state message when efficiency is 100%
- Help overlay gains a "Wasted Files" section; status bar gains `w wasted`

### Technical
- New `tui/waste.go` (rendering + handler) and `tui/waste_test.go` (unit tests)
- Reuses existing `image.Efficiency()` output and `FileNode.IntroducedInLayer` (M16) — no changes to `image/`, `ci/`, `config/`, `cmd/`, or `mcp/`
- Esc cascade extended: viewer → waste → filter → help → quit
- Update routing precedence: filter → waste → help-toggle → help-swallow → viewer → main switch

## [M16] — 2026-05-17
Clipboard, viewer search, and layer origin annotations.

### Added
- **Clipboard (OSC52)**: `y` copies focused file path; `Y` copies file content (viewer) or layer command (layers panel)
- **Search in file viewer**: `/` opens search input, `n`/`N` navigate matches, inline highlighting with distinct current-match style
- **Layer origin annotation**: file tree shows `(LN)` suffix for files introduced in a different layer; viewer title shows origin layer and command
- `IntroducedInLayer` field on `FileNode` — propagated through `Stack()` for all clone/merge paths

### Changed
- Filter Enter behaviour: first Enter confirms the search query (closes input); second Enter opens the selected file (two-Enter flow)
- Backspace on empty filter input is the primary clear+dismiss gesture (Esc still exits search mode)
- Esc cascade extended: viewer search active → viewer search query → close viewer → filter → help → quit

### Technical
- `viewerParams` struct replaces parameter explosion in `renderFileView`
- `renderViewerLine` uses occurrence-counting for highlight correctness on truncated lines
- `renderViewerSearchBar` handles zero-match state without displaying invalid indices
- OSC52 clipboard via `tea.SetClipboard()` — works in tmux/SSH sessions
- 12 new unit tests covering clipboard, viewer search, origin tracking, and Esc cascade

## [UI Polish] — 2026-05-17
TUI visual refinement — modern palette, improved layout, and UX fixes.

### Changed
- Modern colour palette: muted blue/purple accents, subtle borders, high-contrast text
- Improved layer panel: right-aligned sizes, padded columns, cleaner command preview
- File tree: proper column alignment for permissions, UID:GID, and size
- Narrow terminal fallback: graceful degradation below 80 columns
- Status bar: updated key hints with consistent formatting

### Fixed
- Border width calculation causing off-by-one panel sizing
- Tree content overflowing panel bounds on resize
- Scroll offset not accounting for header row in file tree
- Rune-width handling for wide/combining characters in file paths
- Filter+Enter now opens file viewer correctly without clearing filter state

### Added
- README.md with full feature documentation, usage examples, and dive comparison
- LICENSE (MIT)

## [M13+M14] — 2026-05-15
Shell completion and JSON export.

### Fixed
- CI workflow: Go version updated from 1.24 to 1.26 to match go.mod requirement.
- Push hook: added explicit pattern for combined-milestone branches (`feat/mNN-mNN`).

### M13 — Shell Completion
- `layerx completion [bash|zsh|fish|powershell]` outputs shell completion script.
- Custom image argument completer suggests local Docker images via `docker images`.
- Sourcing the script enables tab-completion for subcommands and image references.

### M14 — JSON Export
- `--json <path>` flag on root command exports analysis to JSON file (skips TUI).
- Flat schema: imageRef, totalSize, layerCount, efficiency (score + wastedFiles), layers (each with files[]).
- Files include path, size, and diffType (added/modified/removed/unchanged).
- Gate C: `layerx --json /tmp/out.json nginx:latest` writes valid JSON; `jq '.layers | length'` returns correct count.

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
