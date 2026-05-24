# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

Three correctness fixes in waste overlay, file viewer, and efficiency calculation. Plus two reliability fixes in the pull-progress and CI paths.

### Fixed
- Waste overlay no longer silently jumps to layer 1 when a wasted file's intro layer is unknown. `buildIntroIndex` only walks the *last* stacked tree, so files whited out before the final layer were missing from the index — the bare map lookup returned Go's zero value (0), which the jump path then treated as "introduced at layer 1". The lookup now uses comma-ok and stores `-1` for unknowns; the row renders `L?`, and Enter shows `Intro layer unknown for <path>` instead of jumping.
- File viewer no longer pops back open after Esc. A slow extract whose user pressed Esc mid-load would re-set `viewState = viewReady` when the goroutine finally delivered, overlaying the user's current screen. The model now carries a monotonic `viewRequestID` captured by each load goroutine; Esc bumps the ID, and any in-flight `fileContentMsg` whose ID doesn't match is dropped. Same pattern applied to `fileSaveMsg` (`x` save-to-disk) so a second `x` invalidates the first.
- Efficiency calculation no longer counts hardlinks as 0-byte files. Tar `TypeLink` entries carry `hdr.Size == 0` by convention (the real bytes are at the link target), but the parser was inserting them as zero-byte file nodes that `Efficiency` then walked, inflating the file count without adding bytes. `FileNode` now carries `IsHardlink` / `Linkname`; `walkFiles` skips hardlinks, so per-layer waste reflects actual content bytes only.
- Pull-progress sends in `image/docker.go` no longer deadlock the analyze pipeline on a busy TUI. `ResolveWithProgress`, `ensureImageWithProgress`, and `streamPullProgress` previously sent on the bounded `progress` channel with a bare `<-`. On a multi-arch pull (hundreds of JSON events) a brief Update-loop pause could fill the buffer and hang the entire pull. All four sends now go through the existing non-blocking `emitProgress` helper, matching the pattern in `image/analysis.go`. Dropped events for `Exporting`/`Parsing`/`Pulling` phase markers are a cosmetic loss only.
- `layerx ci` no longer calls `os.Exit(1)` from inside the Cobra `RunE` handler. The bare `os.Exit` skipped deferred cleanup and made the failure path untestable. `executeCICheck` now returns a sentinel `*ErrCIFailed` on rule failure; `main.go` already exits with status 1 on any non-nil error from `cmd.Execute()`. The `ci` subcommand and the `CI=true` root path both set `SilenceErrors`/`SilenceUsage` so the returned error doesn't tack a redundant "Error: ..." line and usage block onto the already-printed report.

### Technical
- New `FileNode` fields: `IsHardlink bool`, `Linkname string`. Populated at parse time from `tar.TypeLink` and `hdr.Linkname`.
- `model.viewRequestID` and `model.saveRequestID` are bumped on dispatch (and on Esc for the viewer); message handlers drop any message with a stale ID.
- New exported `cmd.ErrCIFailed` sentinel error; CI failure path is now reachable from tests without killing the test process.

## [v1.2.1] — 2026-05-24

Correctness fixes in the layer-stacking and file-viewer paths, plus loading-panel and waste-overlay polish.

### Fixed
- Directory metadata changes (mode, UID, GID) are now correctly attributed to the layer that introduced them. Previously, `RUN chmod 0777 /app` in a later layer was silently dropped from the stacked tree — the merged node always took its metadata from the cumulative state, never re-consulting the new layer.
- File viewer (`Enter`) and save-to-disk (`x`) now extract the file as it exists at the selected layer, not as it exists in the final image. Previously both spawned a temporary container from the final image, so viewing `/etc/config` at Layer 2 always showed Layer 5's content. New `Extractor.ExtractFromLayer` and `ExtractRawFromLayer` methods walk back from the selected layer's tar to find the most recent version of the file.
- Loading panel no longer truncates the pull-progress line on large images. The box width is sized to fit the longest content line; on narrow terminals the progress bar shrinks to whatever space remains after the layer counter and bytes total, and is dropped entirely below 4 cells so the bytes total (e.g. `4.7 GB`) is never clipped.
- Waste overlay: pressing `j` past the collapsed top-20 list now silently auto-expands to the full list and continues scrolling, instead of silently stopping at row 20. `G` from a collapsed view now jumps to the true last row, auto-expanding when needed. Manual `a` toggle is unchanged.
- Waste overlay panel border title now reads `Wasted Files 14/30` to match the `Layers 14/30` and `File Tree 14/30` convention. Body header simplified to `5.6 MB wasted across 31 files`; the redundant `Top 20 of N` framing is gone now that the title carries the count. Empty state shows `Wasted Files 0/0`.

### Technical
- New methods on `image.Extractor` interface; existing `Extract` / `ExtractRaw` retained for backward compatibility.
- `image/stack.go` `mergeLayer` now compares `layerRoot.Mode/UID/GID` against `cumulative` and propagates changes through `IntroducedInLayer`. The trailing `hasChangedChildren` block now OR's metadata changes so a metadata-only change is not silently reverted to `Unchanged`.
- No new caching, no `Analysis`-level state. `ExtractFromLayer` performs one `ImageSave` per call and discards bytes after return.

## [v1.2.0] — 2026-05-23

Per-image-digest analysis cache — repeat runs against an unchanged image feel instant.

### Added
- Per-image-digest analysis cache: `Analyze*` writes parsed layers to `{LAYERX_CACHE_DIR or os.UserCacheDir()/layerx}/{digest}/layers.gob` and reads them back on the next run. Repeat runs against an unchanged image skip Docker `ImageSave` + tar parse.
- `--no-cache` flag (alias `--refresh`) on the root command and `layerx ci`. Bypasses the cache for the current run; the run still writes the cache on success.
- New `image.PhaseCacheLoad` progress phase; TUI renders "loaded from cache" briefly on hit.
- New `image.PhaseCacheWarn` progress phase; non-fatal cache I/O failures (load or save) surface as a transient TUI status message instead of being silently dropped.

### Fixed
- TUI no longer quits when `q` is typed inside the file-tree filter or the file-viewer search. The global quit binding now defers to the active text-input handler, so queries containing `q` (e.g. `jquery`, `graphql`) can be typed normally. `ctrl+c` continues to quit unconditionally.
- TOCTOU cache poisoning: `AnalyzeWithOptions` now re-reads `ImageID` after `ResolveWithProgress` and refuses to write the cache if the digest changed mid-run (concurrent `docker pull` retag).
- Empty / unsafe digests no longer reach the cache key. `normalizeDigest` rejects empty strings, `.`/`..`, and digests containing path separators, so a Resolver returning `("", nil)` cannot poison a shared cache slot and a hostile digest cannot escape the cache root.
- Transient I/O during `gob.Decode` (permission flip, EBUSY) no longer evicts an otherwise-valid cache file. Only confirmed corruption deletes.
- `saveCache` fsyncs the temp file before atomic rename so a power loss between rename and writeback can no longer leave a zero-length `layers.gob`.
- TUI no longer flashes "Pulling …" before the first progress event arrives. New `image.PhaseUnknown` zero value falls through to a generic "Loading …" line; cache hits jump straight to "loaded from cache".
- `--refresh` is now a separate flag bound to its own variable. `--no-cache` and `--refresh` are OR'd at read time, so command-line ordering can no longer reverse the bypass intent.
- Bad `LAYERX_CACHE_DIR` overrides print one stderr warning before falling back to `os.UserCacheDir()`.

### Changed
- Release artifact names dropped the version segment: `layerx_linux_amd64.deb` (was `layerx_1.1.0_linux_amd64.deb`).
- README install snippets now use `/releases/latest/download/<name>` so they auto-track the latest release. Older releases keep their versioned filenames.

### Technical
- New `image/cache.go`, `image/cache_dto.go`, `image/cache_test.go`. Encoding is `encoding/gob` with an envelope holding `digest`, `schemaVersion`, `cachedAt`, and `[]cachedLayer`. Schema mismatch, digest mismatch, and confirmed corruption all delete the offending file and fall through to a cold resolve; transient I/O does not.
- `Resolver` gains `ImageID(ctx, ref) (string, error)`. The cache is wired at `image.AnalyzeWithOptions`; existing `Analyze` and `AnalyzeWithProgress` are preserved as shims. Architectural laws unchanged: `image/` still has zero imports of `tui/`, `ci/`, `cmd/`, `config/`.
- Cached fields per layer: `Index`, `ID`, `Size`, `Command`, parsed `Tree`. NOT cached: `NetDelta`, `DiffType`, `IntroducedInLayer` — recomputed by `Stack()` + `assignNetDeltas()` on every load.
- `TestCacheDTO_RoundTrip_AllPersistableFields` now exercises the on-disk gob round trip via `saveCache` + `loadCache` so a future field with a gob-incompatible shape fails the test rather than silently shipping.
- All `Progress` channel sends in `AnalyzeWithOptions` are non-blocking (`select … default`) — a caller passing an unbuffered or full channel can no longer deadlock the analyze pipeline.

## [v1.1.0] — 2026-05-21

Layer net-delta column and the Waste Navigator overlay — surfaces per-layer change in live byte size and lets you jump from duplicated files to the layer that introduced them.

### M19 — Layer net-delta column
Surfaces per-layer change in merged-filesystem live byte size.

#### Added
- `image.Layer.NetDelta` populated by `Analyze`; layer 0 = full live size, layer i>0 = stacked[i] − stacked[i−1]; negative on cleanup layers
- `image.TreeLiveFileBytes` walks a tree summing live file bytes (skips directories and `Removed` subtrees)
- `image.FormatSignedBytes` for "+12 MB" / "-3.0 MB" / "0 B"
- TUI: layer panel shows signed **Change** column by default (`S` cycles Change → Stored → both); panel title uses plain labels (`change` / `stored` / `stored+change`)
- TUI: **Change** colored green when negative, accent when ≥10% of final live size, dim otherwise; `both` mode falls back to Change on narrow panels
- TUI: help overlay uses multi-column layout on wide terminals; dedicated **Layers** section; footnote explains Change vs Stored
- TUI: status bar shows **change** or **stored** for the selected layer when the layers panel is focused
- JSON export: each layer entry gains a `netDelta` field

#### Technical
- New `image/size.go` (domain helper) + `image/size_test.go`; `cmd/json.go` and `tui/layers.go` consume the new field; no changes cross existing architectural rules
- `tui/help.go` extracted from `tui/model.go`; column-rendering loop generalised to handle 1–N columns safely; help-popup width clamp simplified (no redundant floor that could exceed screen width)

### M18 — Waste Navigator
Interactive overlay surfacing duplicated files and jumping to the layer that introduced each one.

#### Added
- `w` opens a centred "Wasted Files" overlay listing the worst offenders by total wasted bytes
- Columns: `path` | wasted bytes | `xN` (layer count) | `L{intro}` (introducing layer, 1-indexed); narrow terminals drop the `xN` column
- `Enter` jumps to the introducing layer with the cursor positioned on the file; clears any active filter and disables `diff-only` if needed to surface the path
- `a` toggles between the top 20 (default) and the full list (capped at 500)
- `y` copies the highlighted path to the clipboard; modal stays open
- `Esc` closes the overlay; `q`/`Ctrl+C` quit
- Empty-state message when efficiency is 100%
- Help overlay gains a "Wasted Files" section; status bar gains `w wasted`

#### Technical
- New `tui/waste.go` (rendering + handler) and `tui/waste_test.go` (unit tests)
- Reuses existing `image.Efficiency()` output and `FileNode.IntroducedInLayer` (M16) — no changes to `image/`, `ci/`, `config/`, `cmd/`, or `mcp/`
- Esc cascade extended: viewer → waste → filter → help → quit
- Update routing precedence: filter → waste → help-toggle → help-swallow → viewer → main switch

### M16 — Clipboard, viewer search, layer origin annotations

#### Added
- **Clipboard (OSC52)**: `y` copies focused file path; `Y` copies file content (viewer) or layer command (layers panel)
- **Search in file viewer**: `/` opens search input, `n`/`N` navigate matches, inline highlighting with distinct current-match style
- **Layer origin annotation**: file tree shows `(LN)` suffix for files introduced in a different layer; viewer title shows origin layer and command
- `IntroducedInLayer` field on `FileNode` — propagated through `Stack()` for all clone/merge paths

#### Changed
- Filter Enter behaviour: first Enter confirms the search query (closes input); second Enter opens the selected file (two-Enter flow)
- Backspace on empty filter input is the primary clear+dismiss gesture (Esc still exits search mode)
- Esc cascade extended: viewer search active → viewer search query → close viewer → filter → help → quit

#### Technical
- `viewerParams` struct replaces parameter explosion in `renderFileView`
- `renderViewerLine` uses occurrence-counting for highlight correctness on truncated lines
- `renderViewerSearchBar` handles zero-match state without displaying invalid indices
- OSC52 clipboard via `tea.SetClipboard()` — works in tmux/SSH sessions
- 12 new unit tests covering clipboard, viewer search, origin tracking, and Esc cascade

## [v1.0.0] — 2026-05-18

Initial public release. Interactive TUI for Docker image layer inspection: layer browser, file tree with whiteout-aware stacking, file content viewer, efficiency analysis, file extraction, CI mode, config file, shell completion, and JSON export.

### UI Polish
TUI visual refinement — modern palette, improved layout, and UX fixes.

#### Changed
- Modern colour palette: muted blue/purple accents, subtle borders, high-contrast text
- Improved layer panel: right-aligned sizes, padded columns, cleaner command preview
- File tree: proper column alignment for permissions, UID:GID, and size
- Narrow terminal fallback: graceful degradation below 80 columns
- Status bar: updated key hints with consistent formatting

#### Fixed
- Border width calculation causing off-by-one panel sizing
- Tree content overflowing panel bounds on resize
- Scroll offset not accounting for header row in file tree
- Rune-width handling for wide/combining characters in file paths
- Filter+Enter now opens file viewer correctly without clearing filter state

#### Added
- README.md with full feature documentation, usage examples, and dive comparison
- LICENSE (MIT)

### M13+M14 — Shell completion and JSON export

#### Fixed
- CI workflow: Go version updated from 1.24 to 1.26 to match go.mod requirement.
- Push hook: added explicit pattern for combined-milestone branches (`feat/mNN-mNN`).

#### M13 — Shell Completion
- `layerx completion [bash|zsh|fish|powershell]` outputs shell completion script.
- Custom image argument completer suggests local Docker images via `docker images`.
- Sourcing the script enables tab-completion for subcommands and image references.

#### M14 — JSON Export
- `--json <path>` flag on root command exports analysis to JSON file (skips TUI).
- Flat schema: imageRef, totalSize, layerCount, efficiency (score + wastedFiles), layers (each with files[]).
- Files include path, size, and diffType (added/modified/removed/unchanged).
- Gate C: `layerx --json /tmp/out.json nginx:latest` writes valid JSON; `jq '.layers | length'` returns correct count.

### M11+M12 — CI mode and config file

#### M11 — CI Mode
- `layerx ci <image>`: evaluates efficiency against configurable thresholds, exits 0/1.
- Three independent rules: `lowest-efficiency`, `highest-wasted-bytes`, `highest-user-wasted-percent`.
- `CI=true layerx <image>` triggers CI mode from root command (no TUI).
- Human-readable report with aligned columns, wasted file listing.
- All three rules independently configurable via CLI flags.

#### M12 — Config File
- `.layerx.yaml` in working directory sets CI rule thresholds.
- Missing config silently uses defaults; invalid YAML produces clear error.
- Partial config merges with defaults (unspecified fields keep default values).
- CLI flags override config file values.
- Config example documented in `layerx ci --help`.

#### Fixed
- OCI layer blobs: handle gzip-compressed blobs from Docker 25+ (`decompressIfGzip`).
- Removed keybinding override feature (caused status bar/help mismatch with actual keys).

### M09+M10 — Efficiency analysis and file extraction to disk

#### M09 — Efficiency Score + Wasted Bytes
- `image/efficiency.go`: Detects files duplicated across layers.
- Algorithm: file at same path in N layers → N-1 occurrences are waste.
- Score displayed as percentage badge in status bar.
- Wasted bytes shown alongside score when > 0.
- Gate C: Test against image with known waste (apt-get install in separate RUN steps).

#### M10 — File Extraction to Disk
- `x` key saves focused file to current working directory.
- Reuses Docker CopyFromContainer via `ExtractRaw` (no 1MB truncation).
- Status bar confirmation: "Saved: filename" or error message.
- Works for binary files (raw bytes, no truncation).
- Gate C: Extract a file, verify it appears on disk with correct bytes.

### M08 — File content viewer

The #1 most-requested feature dive has never shipped.

#### Added
- `Enter` on any file in the tree opens an inline content viewer
- Scrollable text with line numbers (j/k, g/G navigation)
- Binary file detection with notice (ELF, images, archives, etc.)
- Empty file notice for 0-byte files
- 1 MB size limit with truncation notice for large files
- Async extraction with loading spinner (no TUI freeze)
- `Esc` returns to file tree from viewer
- Status bar shows line position and scroll percentage in viewer mode
- Help overlay updated with File Viewer section

#### Technical
- `image/extractor.go`: Extractor interface + DockerExtractor (container create → CopyFromContainer → remove)
- `tui/fileview.go`: viewer panel with line numbers, notices, scrolling
- Binary detection: net/http.DetectContentType + null-byte scan (matches Git's heuristic)
- Container cleanup guaranteed via defer (no orphan containers on error)

### M06+M07 — File tree filter, diff-only toggle, and sort-by-size

#### Added
- `/` opens substring filter input; `Esc` clears, `Enter` keeps filter active
- `d` toggles diff-only mode (hides unchanged files, shows only added/modified/removed)
- `s` cycles sort: default tree order → largest first → smallest first → default
- All three compose together (filter + diff + sort simultaneously)
- Status bar shows `[diff]` and `[↓size]`/`[↑size]` indicators when active
- Help overlay updated with new File Tree section

#### Changed
- Esc key now has precedence: closes filter → closes help → quits
- Sort resets to default on layer switch; filter and diff-only persist
- File tree displays flat paths (no indentation) when sort is active
- Panel title shows active filter query: `FILE TREE [3/12] "nginx"`

#### Technical
- Pure-function pipeline: `displayTree()` = `currentFlatTree()` → `applyDiffFilter()` → `applySubstringFilter()` → `applySortBySize()`
- No changes to `image/` package — all transformations are view-layer only
- Gate C pending: Verify filter/diff/sort interactions on nginx:latest and ubuntu:latest

### M04+M05 — Live Docker data in TUI + file tree with layer stacking and whiteout handling

#### Added
- Real image layers shown in TUI (replacing hardcoded fake data)
- Async loading with spinner, friendly error messages for daemon/pull failures
- Full file tree parsing from layer tars (`ParseLayerTar`)
- Layer stacking with `.wh.<name>` and `.wh..wh..opq` whiteout support (`Stack`)
- DiffType colouring: green=Added, yellow=Modified, red=Removed, grey=Unchanged
- `Analyze()` orchestrator as single entry point for TUI and CI
- Structured error types: `ErrDaemonNotRunning`, `ErrPullFailed`, `ErrImageNotFound`
- `FileNode` helpers: `FindChild`, `AddChild`, `RemoveChild`, `Walk`, `NewFileTree`

#### Changed
- `cmd/root.go`: creates resolver, passes image ref + resolver into TUI
- `tui/model.go`: state machine (loading/ready/error), async fetch via `tea.Cmd`
- `tui/layers.go`: renders real `image.Layer` data with `FormatBytes` sizes
- `tui/filetree.go`: renders real `image.FileNode` with depth-first flattening

#### Technical
- Domain layer (`image/`) has zero UI imports — fully testable in isolation
- Whiteout handling: regular (`.wh.<name>`) and opaque (`.wh..wh..opq`) both implemented
- Non-fatal layer tar parsing: invalid/empty tars leave `Layer.Tree` as nil
- Gate C pending: Verify on second machine against nginx:latest, alpine:latest, ubuntu:latest

### M03 — Bubbletea layout proof

`layerx <any-image>` launches interactive TUI with fake data.

#### Added
- `tui/` package: bubbletea v2 Model with 2-panel layout (layers + file tree)
- Responsive layout: adapts to terminal resize, minimum 50x10 enforced
- Navigation: Tab switches panels, j/k moves cursor, g/G jump to top/bottom
- Adaptive styling: lipgloss v2 with rounded borders, diff colouring
- Hardcoded fake data: 6-layer Go multi-stage build with per-layer file trees
- Diff colouring: green (added), yellow (modified), red (removed), gray (unchanged)
- Command preview: bottom of left panel shows full Dockerfile instruction

#### Changed
- `cmd/root.go`: launches TUI instead of printing table to stdout

#### Technical
- Import: `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`
- Viewport scroll logic for file trees exceeding panel height
- Focus management with visual border colour indicator (purple focused, gray unfocused)

### M02 — Layer metadata table

`layerx nginx:latest` prints layer table with index, ID, size, and command.

#### Added
- Layer size parsing from tar entry headers
- Dockerfile command correlation from image config history
- Human-readable byte formatting (`FormatBytes`)
- Formatted table output with aligned columns
- OCI format support (Docker 25+ `blobs/sha256/` layout)

#### Technical
- Single-pass tar scan: manifest + config + layer sizes in one read
- Empty layer filtering for correct command-to-layer correlation

### M01 — Docker plumbing proof

`layerx nginx:latest` prints layer count to stdout.

#### Added
- `image/` package: `Resolver` interface, `DockerResolver` implementation
- Docker image tar export and `manifest.json` parsing
- Automatic image pull if not available locally
- `cmd/` package: cobra CLI with `ExactArgs(1)` validation
- CI pipeline: build, vet, test, cross-compile (linux/darwin/windows)
- Unit tests for tar parsing logic

#### Technical
- Uses `github.com/moby/moby/client` with `WithAPIVersionNegotiation()`
- `FileTree`, `FileNode`, `DiffType` types defined as placeholders for M05
- `Layer` struct includes all fields needed through M05 (Size, Command, Tree)
