# Changelog

All notable changes to this project will be documented in this file.

Format based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `--platform OS/ARCH[/VARIANT]` flag for selecting a specific variant of a
  multi-platform image. Available on `layerx`, `layerx ci`, `layerx compare`,
  and `layerx --json`. Accepts the same shapes Docker CLI does:
  `linux/amd64`, `linux/arm64`, `linux/arm/v7`, `linux/arm64/v8`,
  `windows/amd64`, and the bare-arch shortcut (`amd64` → `linux/amd64`).
  The platform flows into the daemon's pull, save, and inspect calls, so
  layerx fetches and analyzes exactly the requested manifest. Without
  `--platform`, the historic behaviour is preserved (the daemon's default
  platform is used).
- Shell completion for `--platform` lists the canonical Docker / OCI
  variants out of the box.
- Helpful error when the requested platform is not part of the image's
  manifest list. The error names what was asked for and lists the
  variants the image actually carries when the daemon exposes them
  (Docker 25+ with the containerd image store), e.g.
  `platform linux/ppc64le not found / Available platforms: -
  linux/amd64 - linux/arm64`.
- Archive mode (`layerx ./image.tar`) tolerates `--platform` when it
  matches the archive's recorded variant and rejects it with the same
  `ErrPlatformNotInImage` shape otherwise — so a typo never silently
  inspects the wrong content.
- `layerx build --platform LIST .` continues to forward `--platform` to
  the engine's build (the engine governs what is built); the help text
  now spells out how the build-side flag relates to the top-level
  `layerx --platform` (which selects an existing image's variant).
- The TUI header now appends the active `--platform` after the image
  name (e.g. `layerx │ nginx:latest │ linux/arm64`) so multi-platform
  images give a visual cue which variant is on screen.
- `layerx --json` now emits an optional `platform` field alongside
  `imageRef` whenever `--platform` is set, so a downstream consumer can
  disambiguate two exports of the same multi-platform image without
  re-running layerx. JSON schema bumped to **1.0.1** (additive,
  backwards-compatible).

### Fixed
- `ArchiveResolver` rewinds the archive file handle after the
  `--platform` compatibility check, so a successful platform match no
  longer trips `parseLayers` into reporting "manifest.json not found"
  (the platform scan had advanced the file offset past the tar entries).

## [v1.4.1] - 2026-06-12

`layerx build` thin wrapper plus a Docker-resolver fix for image-digest refs.

### Added
- `layerx build [BUILD_ARGS...]` thin wrapper around `docker build` /
  `podman build`. Forwards every argument verbatim (including `-t`,
  `--build-arg`, `--platform`, `--target`, `--file`, the build-context
  path, and BuildKit flags), streams the engine's native progress output
  to the terminal, and on success automatically opens the built image in
  the layerx TUI. When `-t` / `--tag` is supplied, the first tag is used
  as the TUI image reference (matching `docker build` UX); the engine's
  `--iidfile` is only consulted as a fallback for untagged builds. The
  engine binary follows the existing `--engine` flag (`docker` /
  `podman` / `auto`); auto mirrors the same socket-probing order layerx
  already uses to pick a resolver. On build failure layerx exits with
  the engine's exit code and does not launch the TUI.
- File viewer: `h` / `l` (and `←` / `→`) scroll horizontally for inspecting
  lines wider than the panel. `g` / `G` reset horizontal scroll along with
  vertical, matching vim's line-jump semantics.
- File viewer status bar now shows `h/l` horizontal-scroll hint alongside
  `j/k` so the navigation controls are self-documenting.
- File viewer releases mouse capture while open, allowing text to be selected
  by click-dragging in the terminal. Mouse wheel scrolling is still available
  via keyboard (`j`/`k`, `h`/`l`). Mouse capture resumes when the viewer closes.

### Fixed
- DockerResolver no longer tries to pull image content digests
  (`sha256:<64hex>`) as if they were registry references. The `reference`
  filter on the Docker daemon does not match content digests, so callers
  that passed a raw image ID — e.g. `layerx build` falling back to
  `--iidfile` for an untagged build — would hit "image not found" while
  the image was sitting in the daemon. The resolver now detects ID-shaped
  refs and uses `ImageInspect` directly.
- File viewer `h` / `l` are now vi-style cursor moves: each keystroke
  advances the cursor by one column within the visible area, and the
  viewport only scrolls horizontally when the cursor would cross the
  left or right edge. Previously every keystroke shifted the entire
  viewport four columns at a time, which made inspecting long lines
  feel like tabbing through an indent and undid scroll progress on `h`.
- File-viewer search no longer reports a "match" that is invisible. When a
  search hit lies past the right edge of a long line, the viewer now
  horizontally scrolls so the match is centered in view, matching vim's
  `sidescroll` behavior. A `«` marker on the left edge signals that text
  continues off-screen.

## [v1.4.0] - 2026-06-06

Podman support and explicit cache management.

### Added
- `layerx cache list` now shows an IMAGE column with the original image
  reference each entry was written with (e.g. `nginx:latest`,
  `./build/app.tar`). Backed by a small `meta.json` sidecar written
  next to `layers.gob` on every successful cache save; entries from
  older versions of layerx without a sidecar render as `<unknown>`
  until they are re-cached. The sidecar is display-only — `loadCache`
  and `PruneCache` ignore it. Best-effort: a sidecar write failure
  does not invalidate the cache.
- `--engine docker|podman|auto` flag selects which container engine to talk
  to. `auto` (the default) uses `DOCKER_HOST` if set, otherwise tries the
  Docker socket then falls back to the Podman rootless socket on Linux.
- New typed errors `ErrPodmanSocketNotSet` and `ErrNoEngineFound` for the
  `--engine podman` and `--engine auto` failure paths.
- `layerx cache list` and `layerx cache prune` subcommands. `list` prints
  every cached digest with its size on disk and cached-at time, plus a
  totals footer. `prune --older-than DURATION` (e.g. `7d`, `12h`, `2w`)
  evicts entries older than the cutoff; `prune --all` empties the cache;
  bare `prune` is a dry run that lists what would be removed without
  touching disk and prints a hint pointing at `--all` / `--older-than`
  so it isn't mistaken for an actual eviction. `--dry-run` previews any
  of the above. `--older-than` and `--all` are mutually exclusive. Cache
  directory and override env vars (`LAYERX_CACHE_DIR`,
  `LAYERX_CACHE_TTL_DAYS`, `LAYERX_CACHE_MAX_BYTES`) are documented in
  `layerx cache --help`. The root command's `--help` `Cache:` section
  also points at `layerx cache list` / `prune`. (I-04)
- Analysis cache now self-prunes by age (default 30 days) and total size
  (default 1 GiB). Configurable via `LAYERX_CACHE_TTL_DAYS` and
  `LAYERX_CACHE_MAX_BYTES`; set either to `0` to disable that limit.
  TTL values above 100000 days are rejected with a warn (overflow guard).
  Pruning runs opportunistically at the end of every successful cache
  write. Failures are best-effort and surface as `cache prune ...`
  warnings on stderr. (I-03)
- `saveCache` now invokes the prune helper at the tail of every
  successful write, threading the analyze run's progress channel for
  warnings. The signature gained a trailing `progress` parameter
  (unexported; no API change). (I-03)

### Changed
- `layerx cache list` now displays rows newest-first (most recently
  cached at the top) so a freshly-written entry is visible at a glance
  without scrolling. The underlying `image.ListCache` order is unchanged
  (oldest-first, which is what eviction needs); only the renderer walks
  in reverse.
- `isDaemonUnreachable` now classifies low-level connection errors
  (`no such file or directory`, `connection refused`, `connect: permission
  denied`, `file does not exist`) as daemon-unreachable. Previously these
  passed through as raw transport errors regardless of engine.
- `cmd.selectResolver` is now a swappable package-level var (test-only
  seam; production behaviour unchanged). Unblocks two previously
  skipped cancellation tests (TestRunCICheckInner_ContextCancelled,
  TestRunJSONExport_ContextCancelled) and adds happy-path coverage
  for `runInspect`'s CI=true and --json routes. (I-02)

### Documentation
- New "Container Engines" section in README covering Docker, Podman
  (Linux auto, macOS/Windows manual), and archive mode.
- `layerx --help` reorganised: top-level Long is now ~20 lines (was ~40)
  with a "Common usage" synopsis listing the three primary commands
  (`layerx IMAGE`, `layerx ci IMAGE`, `layerx compare A B`) so the bare
  form's role is obvious. Engines prose collapsed to a one-line pointer
  at `--engine`; cache prose kept the explicit `cache list` / `cache prune`
  surfacing from #f742493 in a tighter form.
- `layerx ci --help`: example `.layerx.yaml` block now has a
  `Example .layerx.yaml:` header so it isn't misread as flag prose.
  Threshold flags (`--lowest-efficiency`, `--highest-wasted-bytes`,
  `--highest-user-wasted-percent`) now render as
  `(default: from config (built-in 0.9))` etc. instead of the misleading
  `(default -1)` sentinel.
- `layerx compare --help`: added an example for `--no-cache` to surface
  the inherited persistent flag.
- `layerx ci --help` and `layerx compare --help` now end with a one-line
  pointer to `layerx --help` for `--engine`, `--json`, and `--no-cache`
  details (the persistent flags inherited from root).

### Removed
- The hidden `--refresh` alias for `--no-cache`. `--no-cache` was the
  documented form everywhere except the alias's own registration; the
  hidden flag had no discoverability path. Pass `--no-cache` instead.

### Fixed
- `TestCopyCtx_MidStreamCancel` was racy and failed intermittently on
  `main` once #39 and #40 landed together: the test's blocking reader
  returned `io.EOF` after `close(br.release)`, but `copyCtx` returns
  `nil` on EOF before its next `ctx.Err()` check, so the cancel could
  be missed. The reader now blocks on `<-ctx.Done()` and returns
  `ctx.Err()` instead, making the test deterministic without changing
  production code. Test-only.
- `stderrProgress` `stop()` is now idempotent. The first call closes the
  channel and waits for the drain goroutine; subsequent calls are no-ops.
  Latent — all current callers defer `stop` exactly once; this guards
  against a panic if a future refactor or test ever double-calls it. (B-08)
- `layerx ci` and `layerx --json` now cancel within a single 32 KiB chunk
  during the image-spool stage on Ctrl+C, instead of waiting for the full
  Docker export to land on temp disk. Closes the gap left by B-05 where
  pass 2 honoured `ctx.Done()` but the initial spool copy didn't. (B-06)
- `layerx ci` and `layerx --json` now stream analyze progress to stderr
  (phase transitions plus a throttled byte/layer heartbeat) and cancel
  cleanly on Ctrl+C with a friendly `Error: interrupted`. `parseLayers`
  now honours `ctx.Done()` between layer iterations. (B-05, D-03)
- `layerx ci` and `layerx --json` now classify "image not found", "Docker
  daemon not reachable", and "pull failed" as distinct, friendly one-line
  messages on stderr. Previously these all surfaced as raw daemon text.
  TUI behaviour is unchanged.
- TUI now shows a clear "Could not <op>: <cause>. Free up disk space or set
  TMPDIR to a writable location and try again." message for archive infra
  failures (temp spool create, disk full while spooling) instead of a raw
  OS error string.
- `layerx ci --help` Example header listed only `lowest-efficiency: 0.9`
  as the default; it now also names `highest-user-wasted-percent: 0.1`,
  matching `config.Default()` and what an unconfigured `layerx ci` actually
  enforces.
- File tree size column and `s` size-sort no longer count Removed
  (whiteout) bytes. On layers that mostly delete files, directories full
  of deletions used to appear large and float to the top of the sort,
  pushing real live-byte hotspots down. The sort now reflects what is
  actually in the image at the selected layer.
- Bare `layerx` (no image argument) shows the usage block again. v1.3.0
  silenced usage on the root command for all error paths, which also
  suppressed help when args were missing.
- Bare `layerx ci` (no image argument) now prints a one-line synopsis,
  the Usage line, and three concrete examples to stderr instead of just
  cobra's terse `accepts 1 arg(s), received 0` with no actionable help.
  The same hint appears for too-many-args invocations. Exit code stays 2.
- Malformed `.layerx.yaml` errors print a section-specific reference
  excerpt (rules, path-rules, version) when the failing section is known,
  or a general config hint otherwise — never the full command usage block.
  `rules: null` is now rejected rather than silently zeroing thresholds.
- `findFileInLayer` no longer reports a directory as removed by its own
  opaque-whiteout marker (`dir/.wh..wh..opq` clears contents only, not the
  directory itself). Latent — no current user impact; tightens the
  contract for future API consumers.

## [v1.3.0] - 2026-05-30

Compare two builds, declare path rules, and pin the config and JSON schemas.
Two breaking changes — see Changed.

### Added
- `layerx compare OLD NEW` reports size, efficiency, layer, file, and waste
  deltas between two images side-by-side. Both arguments accept the same
  inputs as `layerx` itself (image refs and local archives, in any mix), so
  you can compare a registry tag against a freshly-built tarball without
  pushing first.
- Output ends with a one-line `verdict:` summary (`ok`, `regression
  reason=...`, or `noop digest=...`) that is easy to grep from CI logs.
  `layerx compare` exits 1 on regression (efficiency dropped or wasted bytes
  grew) and 2 on operational errors (daemon down, archive missing), so it
  drops into a build pipeline as a gate without extra wrapping.
- `--mode compact|full|summary` and `--top N` control verbosity: compact
  (default) shows the largest deltas with a "... and N more" counter, full
  prints every entry, summary keeps only the header and verdict.
- `.layerx.yaml` accepts a new `path-rules` section with three rule kinds:
  `block` (fail if any layer wrote matching paths, even if a later layer
  deleted them), `deny-waste` (fail if matching paths appear in more than
  one layer), and `max-layer-count` (fail if any one path appears in more
  than N layers). Globs use `**` for recursive matches.
- `layerx init --flavour <node|python|java|go|generic>` writes a starter
  `.layerx.yaml` tuned for that stack. Path rules ship off by default —
  running `init` is the explicit opt-in.
- `--json` export gains a top-level `schemaVersion` field (currently
  `"1.0.0"`) so downstream tools can pin against the format.
- `.layerx.yaml` accepts an optional `version: 1` field at the root.
  Unset means 1; future schema changes will rev this.

### Changed
- `layerx compare` with no arguments now prints a short usage hint with
  three concrete examples instead of an opaque "Error" line, so the
  command is self-explanatory on first use.
- `layerx compare` shows live progress on stderr while resolving remote
  images — pulling, exporting, parsing, and the resolved digest are all
  surfaced per side, so a slow registry pull is visibly active rather than
  appearing stuck. Pipe `2>/dev/null` to silence; stdout stays grep-clean.
- `layerx compare` no-op message (when both inputs resolve to the same
  image content) is rewritten in plain language and shows both the short
  and full digest. The machine-parseable `verdict: noop digest=...` line
  is unchanged.
- `layerx compare` table columns now align consistently across long paths
  and commands, so the LAYERS, FILE CHANGES, and WASTE CHANGES sections
  stay readable on real diffs.
- **Breaking:** Esc in the main TUI no longer quits the app when nothing
  is dismissable. Esc still closes the file viewer, the wasted-files
  overlay, the help overlay, and clears an active filter, but mashing it
  past those layers is now a no-op instead of a silent quit. Quit
  remains on `q` and `Ctrl+C`. On the loading and error screens Esc
  continues to exit, matching the on-screen "Press q or Esc to exit"
  hint.
- **Breaking:** the CI report now groups results into `Global Rules:` and
  `Path Rules:` sections. Log scrapers that grep the previous flat-list
  format need to update their patterns. Exit codes and JSON output are
  unchanged.

### Fixed
- Ctrl+C now cancels `layerx ci`, `--json`, and `layerx compare` while a
  slow image pull, export, or parse is in progress. Previously the process
  hung until the operation completed even after the interrupt was sent.
- CI mode no longer silently passes when every threshold is disabled.
  Setting all of `lowest-efficiency`, `highest-wasted-bytes`, and
  `highest-user-wasted-percent` to 0 (or omitting them all) now exits 2
  with a clear message naming each flag and the config key.
- `.layerx.yaml` files containing a `keybindings:` block now load
  successfully. Strict mode previously rejected the entire config — rules
  and all — when this section was present.
- A malformed `.layerx.yaml` no longer drowns the parse error under the
  full `layerx --help` output. The error line ("parsing .layerx.yaml:
  line 4 column 5: ...") is now the last thing printed, so the failure
  is visible at a glance instead of buried in 60 lines of usage text.
- Efficiency scores no longer charge waste for files that were deleted
  between layers and a different file with the same path was added later
  (`apt-get install` → `apt-get clean` → reinstall). Each delete-then-readd
  cycle is now treated as a separate run, so only true duplicates within a
  single run count toward wasted bytes.
- `wasted %` rule output now reads as a percent. The rule used to print
  the raw fraction (e.g. `0.10`) labelled as a percent; it now renders as
  `10.0%` in both Actual and Threshold columns. The `efficiency` rule
  matches the same format (`92.5%`) so all three rules agree.
- `layerx compare ./app.tar ./app.tar` is now recognized as a no-op
  immediately, even when the resolver cannot return a content digest. The
  short-circuit emits `verdict: noop reason=path-equal` so the verdict
  line stays well-formed for parsers that scan for `digest=` or `reason=`.
- `--top` is now ignored in `--mode summary` and `--mode full` instead of
  rejecting `--top 0` with an error. Compact mode keeps the existing
  range check.
- Filenames with CJK or emoji characters no longer overflow the selected
  filetree row. Padding now measures display width to match the unselected
  branch.
- Multi-GB layers (ML model images, dataset bundles) no longer load the
  entire compressed blob into memory before parsing. Peak memory during
  analysis stays bounded by the largest single layer's parsed file tree
  rather than the largest compressed layer.
- Long Dockerfile commands wrap on the nearest space within the panel
  width, eliminating an off-by-one that could force a mid-word cut on
  odd-width panels.
- File extraction (`x` save, content viewer) on multi-GB images no longer
  loads every layer blob into memory at once. The per-extract memory
  ceiling is now one layer blob — the one currently being scanned — so
  ML / dataset images that previously OOMed the process now work, and
  TUI key-mash sequences that fired concurrent extracts are bounded.
- `x` save now writes via temp-file + rename instead of in-place. A
  process kill (Ctrl+C, OOM, power loss) mid-write no longer leaves a
  truncated file at the user's chosen path; the target either holds the
  complete pre-write content or the complete new content. The save
  resolves symlinks before writing (so saving over a symlink updates the
  link's target, not the link itself) and applies the process umask to
  the chosen mode bits, matching the prior `os.WriteFile` semantics.
- Opening a large source file in the viewer no longer freezes the TUI
  while syntax highlighting runs. The Chroma tokenize/format pass moved
  off the input goroutine; the file shows immediately in plain text and
  the colored version swaps in when ready.
- Filenames with CJK or wide-emoji characters no longer overflow the
  Waste panel's path column. Truncation now measures display columns
  rather than rune count, keeping the surrounding table aligned.
- Layer blob loading is now bounded by an explicit per-blob size cap.
  A malformed image archive that declares a single layer is petabytes
  no longer causes runaway allocation.
- Image pulls now fail loudly when the registry rejects the request.
  Authentication failures, "manifest not found", and registry 5xx
  responses arrive from the daemon as in-band JSON `errorDetail`
  events on a 200 stream; previously both the progress and
  non-progress paths swallowed those events and reported the pull as
  successful, then surfaced as a confusing "manifest.json not found"
  during export. The error message from the registry is now returned
  as the pull failure cause.
- An empty, whitespace-only, or comments-only `.layerx.yaml` no longer
  blocks startup. The M12 contract treats such files identically to a
  missing config — fall back to defaults — and a stub placeholder file
  in a repository will load cleanly.
- Replacing a regular file with a hardlink at the same path is now
  charged as wasted bytes. The original file's bytes still ship in the
  earlier layer's tar even though the live filesystem now points
  elsewhere, so the prior contents are dead weight in the image. The
  efficiency walk previously skipped the replacement node entirely and
  the prior occurrence dropped out of the waste total.

## [v1.2.3] - 2026-05-28

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
