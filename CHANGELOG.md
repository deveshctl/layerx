# Changelog

All notable changes to this project will be documented in this file.

Format based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Inspect local image archives without a Docker daemon: pass a path to a
  `docker save` tarball or OCI layout tarball (e.g. `layerx ./build/app.tar`)
  and LayerX reads the file directly. Works for the TUI, `--json` export,
  and `layerx ci`. Useful in CI runners and air-gapped environments where
  the image is already on disk and starting a daemon is unwanted.
- Auto-detection: any argument that resolves to an existing regular file
  is opened as an archive; everything else still goes through the Docker
  daemon. No new flags; existing `layerx nginx:latest` workflows are
  unchanged.
- File content viewer and save-to-disk (`x`) work in archive mode too —
  full feature parity with daemon-backed inspection.

### Changed
- Manually-built binaries (`go build` from source) now include the git commit and build date in `--version` output, picked up automatically from the metadata Go embeds at build time. Release builds and source-tarball checkouts without git history are unchanged.

### Fixed
- Clearer errors for archive mode: a missing path reports "Archive not
  found" instead of a generic Docker error; a malformed tarball reports
  "Not a valid image archive" instead of a low-level parse error. A
  permission-denied archive now reports "Permission denied" instead of
  being mislabelled as "not found".
- Saving or viewing a file from a layer enforces the same 2 GiB cap as
  the daemon-backed copy path. A crafted tar entry with an inflated
  size header can no longer make LayerX exhaust memory.
- Archive image-size readout (shown during loading) now sums only the
  layer blobs declared in the manifest, not unrelated tar entries like
  `manifest.json` and the config blob.

## [v1.2.2] - 2026-05-26

A correctness and reliability release across the TUI, CI mode, config, and
large-image handling.

### Added
- Background saves with auto-rename — slow disks no longer freeze the TUI,
  and existing files get `name.1`, `name.2`, … instead of being overwritten.

### Changed
- **Breaking — CI:** `layerx ci` / `CI=true layerx` exit `1` only on rule
  failure; internal errors (daemon down, bad config, JSON write failure)
  exit `2`. Threshold flags live only on `layerx ci`; `.layerx.yaml` is
  decoded strictly; out-of-range / `NaN` / `±Inf` thresholds error up front;
  threshold `0` or negative disables that rule (previously failed every
  image).

### Fixed
- TUI: `q` / `Ctrl+C` exits within ~1 s during pulls, extraction, and saves;
  `q` types normally inside the filter and viewer search. CRLF/CR endings,
  CJK and wide characters, narrow-terminal pull totals, and trailing `\n`
  no longer smash panel borders. Waste overlay shows accurate counts,
  handles unknown intro layers, excludes whiteouts, and preserves filter
  state on failed jumps.
- Analyzer: file viewer and extraction read from the *selected* layer (not
  the final image); non-regular files return a clear error. Strict
  whiteout handling for re-add and reserved prefixes; directory↔file type
  flips across layers; hardlinks count toward efficiency. Search handles
  non-ASCII case changes; filter input capped at 256 chars.
- Scale: 8 GB images no longer peak at 8 GB RAM (layer parsing spools to
  disk); 100 GB tar entries cap at 2 GiB instead of allocating upfront.
  Efficiency report and JSON export are deterministic across runs.
- Plumbing: `LAYERX_CACHE_DIR=~/…` expands `~`; transient I/O doesn’t evict
  good cache files; concurrent `--json` runs use unique temp names; no
  `.tmp-*` orphans after crashes; multi-arch pulls don’t deadlock. Shell
  completion under Git Bash / WSL strips CRLF; hung daemon times out after
  1 s. `--version` no longer prints `(commit none, built unknown)` for
  binaries built without `-ldflags`.

## [v1.2.1] - 2026-05-24

Correctness fixes for layer stacking and the file viewer.

### Fixed
- Directory metadata changes (mode, UID, GID) introduced by a later layer
  (e.g. `RUN chmod 0777 /app`) are now correctly attributed to that layer
  instead of being silently dropped from the stacked tree.
- Pressing `Enter` (view) or `x` (save) now extracts the file as it exists
  at the *selected* layer, not the final image. Previously, viewing
  `/etc/config` at layer 2 always showed layer 5's contents.
- The pull-progress line stops being truncated on large images. The
  loading-panel width adapts so the bytes total (`4.7 GB`) is never clipped.
- Waste overlay navigation: pressing `j` past the collapsed top-20 list now
  auto-expands and continues scrolling. `G` jumps to the true last row,
  expanding when needed. Manual `a` toggle is unchanged.
- The waste overlay panel title now reads `Wasted Files 14/30`, matching
  the `Layers 14/30` and `File Tree 14/30` convention. The body header is
  simplified to `5.6 MB wasted across 31 files`.

## [v1.2.0] - 2026-05-23

Per-image-digest analysis cache — repeat runs against an unchanged image
feel instant.

### Added
- Analysis cache. Parsed layer trees are written to
  `${LAYERX_CACHE_DIR or os.UserCacheDir()/layerx}/<digest>/layers.gob` and
  re-used on the next run. A cache hit skips the Docker `ImageSave` and tar
  parse entirely.
- `--no-cache` (alias `--refresh`) on the root command and `layerx ci`.
  Bypasses the cache for the current run; the run still writes the cache
  on success.
- The TUI briefly shows "loaded from cache" on hit, and surfaces non-fatal
  cache I/O failures as a transient status message rather than silently
  swallowing them.

### Fixed
- Typing `q` inside the file-tree filter or viewer search no longer quits
  the TUI. Filter queries containing `q` (e.g. `jquery`, `graphql`) now
  type normally; `Ctrl+C` continues to quit unconditionally.
- A concurrent `docker pull` mid-run that retags the image no longer
  poisons the cache — the digest is re-checked after resolve.
- Empty or unsafe digests are rejected before they reach the cache key,
  so a misbehaving Resolver can't write to an arbitrary cache slot.
- Transient I/O during cache decode (permission flip, EBUSY, etc.) no
  longer evicts an otherwise-valid cache file.
- Cache writes survive power loss between rename and writeback — the temp
  file is fsynced before the atomic rename.
- The TUI no longer flashes "Pulling …" before any actual progress event;
  cache hits jump straight to "loaded from cache".
- Bad `LAYERX_CACHE_DIR` overrides print one stderr warning and fall back
  to the OS default cache dir.

### Changed
- Release artifact filenames dropped the version segment:
  `layerx_linux_amd64.deb` (was `layerx_1.1.0_linux_amd64.deb`).
- README install snippets use `/releases/latest/download/<name>` so they
  auto-track the latest release. Older releases keep their versioned names.

## [v1.1.0] - 2026-05-21

Layer net-delta column and the Waste Navigator.

### Added
- **Waste Navigator (`w`).** Centred overlay listing the worst duplicate
  files by total wasted bytes. Columns: path | wasted bytes | `xN` (layer
  count) | `L<n>` (introducing layer). `Enter` jumps to the introducing
  layer with the cursor on the file. `a` toggles between the top 20 and
  the full list (capped at 500). `y` copies the highlighted path. `Esc`
  closes; `q` / `Ctrl+C` quit. Empty state when efficiency is 100%.
- **Layer net-delta column.** The layers panel now shows per-layer change
  in live filesystem size — green when negative (cleanup layers), accent
  when ≥10% of final size, dim otherwise. `S` cycles
  Change → Stored → both. The status bar surfaces the same number for the
  selected layer.
- JSON export: each layer entry gains a `netDelta` field.
- Help overlay reorganised: dedicated **Layers** section, multi-column
  layout on wide terminals, footnote explaining Change vs Stored.
- **Clipboard (OSC 52).** `y` copies the focused file path; `Y` copies
  file content (in the viewer) or the layer command (in the layers panel).
  Works inside tmux/SSH sessions.
- **Search in the file viewer.** `/` opens search; `n` / `N` navigate
  matches; the current match is rendered with a distinct style.
- **Layer-origin annotations.** The file tree shows `(LN)` for files
  introduced in a different layer. The viewer title surfaces the
  introducing layer and the Dockerfile command.

### Changed
- Filter Enter is a two-step: first Enter confirms and closes the input,
  second Enter opens the selected file.
- Backspace on an empty filter input clears + dismisses (Esc still works).
- `Esc` cascade: viewer search → close viewer search → close viewer →
  close filter → close help → quit.

## [v1.0.0] - 2026-05-18

Initial public release. Interactive TUI for inspecting Docker image layers.

### Added
- **Layer browser** with diff colouring (green Added, yellow Modified, red
  Removed, grey Unchanged) and per-layer file trees.
- **File tree** with whiteout-aware layer stacking — handles both
  `.wh.<name>` and `.wh..wh..opq` opaque whiteouts correctly.
- **File content viewer** (`Enter`) — the headline feature dive has never
  shipped. Scrollable text with line numbers, binary detection, and a
  1 MB truncation notice for large files. `Esc` returns to the tree.
- **File extraction to disk** (`x`) — saves the focused file to the
  current working directory at full size (no 1 MB cap).
- **Filter, diff-only, and sort.** `/` opens substring filter; `d` hides
  unchanged files; `s` cycles default → largest first → smallest first.
  All three compose. Filter and diff persist across layer switches.
- **Efficiency score and wasted bytes**, computed by detecting files that
  appear in more than one layer (all but the first occurrence is waste).
- **CI mode** — `layerx ci <image>` (or `CI=true layerx <image>`)
  evaluates against three configurable rules: `lowest-efficiency`,
  `highest-wasted-bytes`, `highest-user-wasted-percent`. Exits 1 on
  failure, 0 on pass, with a human-readable report.
- **`.layerx.yaml`** for CI thresholds. Missing config silently uses
  defaults; CLI flags override config.
- **JSON export** — `--json <path>` skips the TUI and writes a flat
  schema with image metadata, layers, files, diff types, and efficiency
  data. Schema is documented and tested for round-trip stability.
- **Shell completion** — `layerx completion [bash|zsh|fish|powershell]`.
  Custom completer suggests local Docker images for the image argument.
- **Help overlay** (`?`) with a key reference for every panel.
- README and MIT LICENSE.

### Technical
- Single static binary, no runtime dependencies beyond a running Docker
  daemon.
- Built on `github.com/moby/moby/client` (the actively maintained Docker
  SDK) with API-version negotiation.
- TUI on bubbletea v2 / lipgloss v2 / bubbles v2.
- Cross-compiled for `linux/{amd64,arm64}`, `darwin/{amd64,arm64}`,
  `windows/{amd64,arm64}`. `.deb` and `.rpm` artifacts for Linux.
