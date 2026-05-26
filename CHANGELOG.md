# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

Three correctness fixes in waste overlay, file viewer, and efficiency calculation. Plus two reliability fixes in the pull-progress and CI paths. Two extractor and analysis hardening fixes. One Windows-specific cache eviction fix. Ten low-priority correctness, robustness, and test-coverage fixes. Five further bug-scan fixes covering tar-path normalization, CI-rule disable semantics, pull-error propagation, file-viewer chroma cost, and extractor goroutine cancellation. Plus a tree-wide gopls cleanup: deprecated `tar.TypeRegA` removed, `client.NewClientWithOpts` migrated to `client.New`, and Go 1.22+ modernizations applied. A round-2 deep scan added 11 more fixes spanning hardlink metadata propagation, directory-to-file replacement diff emission, JSON-export tmp-file collisions, deterministic waste ordering, gzip reader leaks, file-viewer trailing-newline rendering, and version-string formatting. A round-3 review added six more, covering tar-path normalization in the extractor, silent JSON-export failures under `CI=true`, a duplicate Enter hint, the filetree below-indicator overwriting the filter-bar border, version-string built-suffix on missing dates, and the CI cross-compile matrix missing `arm64`. A round-4 deep scan adds 13 more, covering memory-bounded resolver parsing, end-to-end TUI quit cancellation, and a sweep of CRLF / title-truncation / CI-JSON-error-handling correctness fixes.

### bug-batch — 2026-05-25 (round 4)

Round 4 deep scan: 5 parallel reviewers + manual source verification of every candidate finding. 13 bugs verified by direct source inspection.

- fix(image): `parseLayers` now spools the image archive to a temp file and walks it in two passes — pass 1 reads `manifest.json` and the legacy root-level config, pass 2 streams each layer through decompress + `ParseLayerTar` and drops the buffer before reading the next entry. Previously the whole image was buffered into a `map[string][]byte`, so an 8 GB image peaked at 8 GB resident heap during `layerx <image>`. Peak heap is now bounded by the largest single layer, mirroring the spool pattern already in `extractor.loadLayerTars`.
- fix(image): `insertNode` now correctly handles a tar entry that replaces a directory with a regular file within the same layer (legal in tar; appears in unusual rename-into-self / build-tooling traces). The previous code left `IsDir=true` and dropped the file's size, so the tree rendered an expandable directory with no children and the file's content was unreachable from the tree. The same change folds in trusting tar's `size==0` header on update — useful for truncate-to-zero / hardlink-replacement cases that were silently keeping a stale non-zero size.
- fix(image): `ensureImageWithProgress` no longer mislabels `context.Canceled` as `ErrDaemonNotRunning`. A user pressing `q` during pull had no way to surface a clean cancellation; the transient error read "Docker is not running." Cancellation now propagates as the bare context error, matching the `context.DeadlineExceeded` guard.
- fix(cmd): the `layerx ci --json` subcommand path now routes JSON-export failures through the same `combineCIAndJSONErr` helper as the `CI=true` path. The previous `_ = runJSONExport(...)` swallowed permission-denied, missing-parent-dir, and disk-full failures on a CI-rule failure — pipelines silently consumed a stale or missing JSON file. CI exit code still wins on rule failure; JSON errors now show on stderr regardless.
- fix(cmd): `CI=true layerx --json` and `layerx ci --json` no longer analyze the image twice. `executeCICheck` now returns the `*image.Analysis`; the JSON path consumes it via a new `runJSONExportFromAnalysis` rather than re-resolving and re-parsing. Halves the wall-clock time of CI-with-JSON on `--no-cache` runs of large images.
- fix(cmd): `cmd/completion.go` now strips trailing `\r` from each line of `docker images` output. Under Git Bash / WSL talking to Windows `docker.exe` (which emits CRLF), tab-completion candidates were `nginx:latest\r` — shell either rejected them or echoed the CR into the next layerx invocation, where Docker returned "no such image."
- fix(config): `LoadFrom` now wraps the YAML parse error with the file path. `cmd/ci.go` and `cmd/root.go` further wrap with `loading config: %w`, so the user sees the full chain (`loading config: parsing /path/to/.layerx.yaml: yaml: line 12: ...`) instead of having to guess which of multiple config locations was malformed.
- fix(tui): the file viewer now normalizes CRLF to LF before splitting and rendering. CRLF-encoded files (Windows-built images, Windows-edited config files in any image) previously left a `\r` on every rendered line, jumping the cursor to column 0 mid-row and smashing the panel border for the rest of the frame. Both `renderFileView` and `recomputeViewerMatches` now go through a shared `splitFileLines` helper so renderer and search agree on line indices and the off-by-one in `recomputeViewerMatches` (it indexed an extra phantom trailing line) is eliminated.
- fix(tui): `renderPanel` now truncates panel titles wider than the available width with `ansi.Truncate(..., "…")`. The previous code clamped `fillCount` to 0 but rendered the title at full length; a deep file path plus the `← L<n>: <cmd>` suffix on a narrow terminal made the top border longer than the side borders, mis-aligning subsequent rows with the closing `╮`. Affects every `renderPanel` caller (file viewer, file tree, layers list, waste overlay).
- fix(tui): the waste overlay title now uses the visible-row count as the denominator and surfaces the cap when it bites. The previous title divided the cursor (capped at 500) by the uncapped `len(efficiency.WastedFiles)`, so a `12483`-file image displayed `Wasted Files 500/12483` at the bottom of the list — implying 11983 more rows that didn't exist. Title now reads `Wasted Files N/500 (capped, 12483 total)` when truncated; the underlying `12483` is still surfaced in the body header.
- fix(tui): the TUI now plumbs a cancellable context through both fetch goroutines (`fetchInspect`, `fetchAnalysisWithProgress`). On `q` / Ctrl-C during a multi-GB pull, the goroutines previously kept the Docker daemon busy until the syscalls returned on their own — the process appeared to hang for tens of seconds post-quit. Each quit transition now calls `m.fetchCancel()` before `tea.Quit`, and the underlying Docker SDK calls (`ImageList`, `ImagePull`, `ImageSave`, `ImageInspect`) honor cancellation by closing the HTTP request. Ctrl-C during pull now exits within ~1 second.

### bug-batch — 2026-05-25

Fixes from a deep scan of all `.go` files. Each finding was verified by direct code inspection before fix.

- fix(ci): a `lowest-efficiency` threshold of 0 or negative now disables the rule, matching the disable-on-zero semantics of the other two rules.
- fix(cmd): JSON export writes to a tmp file and atomically renames — no more half-written files on crash.
- fix(cmd): `--json` is now a persistent flag and composes with the `ci` subcommand. CI rule evaluation still runs; JSON is written either way.
- fix(cmd): `CI=true` + `--json` now runs CI evaluation first and then exports JSON, instead of silently skipping CI.
- fix(cli): exit 2 on internal errors (Docker down, config parse, etc.); exit 1 is reserved for CI rule failure. Pipelines can now distinguish.
- fix(completion): 1s timeout on `docker images` probe — a hung daemon no longer hangs the user's shell.
- fix(image): tree node correctly clears `Size` when a path is promoted from file to directory across layers.
- chore: `errors.As(&v)` modernized to `errors.AsType[T]` (Go 1.26) at the three sites flagged by gopls.

#### Round 2 — deeper scan

A second pass over the tree surfaced 11 more findings, each verified by direct code inspection.

- fix(image): `Stack` now propagates `Linkname` and `IsHardlink` through every clone path (`mergeLayer` merged/Modified branches, `cloneAsUnchanged`, `cloneAsRemoved`, `cloneAsAdded`, `cloneStructure`). Previously the hardlink metadata was set only on first parse and silently dropped at every subsequent stacking, so efficiency and viewer logic that depends on `IsHardlink` saw zero hardlinks for any layer past the first.
- fix(image): when a directory is replaced by a regular file in a later layer (`RUN rm -rf /etc && touch /etc`), `Stack` now emits `Removed` nodes for each child of the gone directory before constructing the replacement node. Without this, the children stayed `Unchanged` in the stacked tree and the waste/diff views silently dropped them.
- fix(cmd): `writeJSONAtomic` now creates its temp file via `os.CreateTemp(filepath.Dir(targetPath), ".layerx-json-*.tmp")` instead of a deterministic `<target>.tmp` name. Two concurrent `layerx --json out.json …` runs in the same directory could clobber each other's tmp file and produce a corrupt output.
- fix(image): `Efficiency` now uses a stable total ordering (`TotalWasted` desc, then `Path` asc). With only `TotalWasted` as the key, two files with equal waste landed in nondeterministic order across runs — making JSON exports and CI reports flap between identical inputs.
- fix(tui): waste-overlay jump no longer wipes `filterActive`/`filterQuery` when the intro layer is unknown (`IntroLayer == -1`). The clear is now scoped to the success branch, matching the cursor-jump.
- fix(tui): file viewer no longer renders a phantom blank line at the end of files that end in `\n` (which is most of them). `renderFileView` now trims one trailing newline before splitting; `fileViewLineCount` mirrors the trim with a single-newline guard so an all-`"\n"` file still reports 1 line.
- fix(ci): `report.TopWaste` is now a defensive copy of `efficiency.WastedFiles[:limit]`, not a slice into the same backing array. Future code mutating either side can no longer leak through the shared header.
- fix(image): `decompressIfGzip` now returns an `io.ReadCloser` and callers (`parseLayers`, `findFileInLayer`) close the reader after use. Gzipped layer blobs (the OCI Docker 25+ default) previously leaked the gzip reader's internal buffer on every layer parse and every file extraction.
- fix(tui): waste-overlay expand toggle now accepts both `a` and `A`, matching every other case-insensitive shortcut in the TUI.
- fix(tui): file viewer title truncation now uses `lipgloss.Width` + `ansi.Truncate` instead of rune count. A 30-CJK-character `originCmd` (display width 60) was rendered untruncated and overflowed the title bar; truncation now happens at display-width 40 with the existing `…` suffix.
- fix(cmd): `SetVersionInfo` no longer splices `(commit none, built unknown)` into the version string. `main.go`'s defaults (`commit="none"`, `date="unknown"` when not built with `-ldflags`) are now suppressed; only a real commit produces the parenthesized suffix.

#### Round 3 — verified findings

A third pass with four parallel reviewers (one per package, plus cross-cutting) surfaced six more findings. Every one was re-verified against the working tree before fix.

- fix(image): `findFileInLayer` and the `ExtractFromLayer` / `ExtractRawFromLayer` callers now route both the entry name and the requested path through `cleanTarPath`. Previously they only trimmed a leading `./` or `/`, so a tar with an entry like `usr/./bin/sh` (busybox tar, some BuildKit edge cases) — which `cleanTarPath` collapses to `usr/bin/sh` during tree parsing — was visible in the tree but failed extraction with "not found in any layer." Both sides now share the same normalization.
- fix(cmd): `CI=true` paired with `--json PATH` no longer swallows JSON-export errors. The previous `_ = runJSONExport(…)` discarded permission-denied, missing-parent-dir, and disk-full failures; on a CI-pass run the user got exit 0 with no JSON file and no diagnostic. The CI exit code still wins on CI failure, but a JSON write failure is now warned on stderr and (when CI itself passed) becomes the returned error. The decision lives in a small `combineCIAndJSONErr` helper for unit testability.
- fix(tui): the status bar's Enter hint is now a single context-aware entry. The previous code emitted both `{"Enter","view"}` (always) and `{"Enter","toggle"}` (appended on directories in collapse-eligible mode), so on a ≥90-column terminal the bar read `… Enter view │ x save │ y copy path │ ? help │ Enter toggle` — with the "view" hint actively wrong on directories.
- fix(tui): the filetree panel no longer paints a `▾` scroll indicator on the filter bar's right border. `renderPanel` paints the indicator at row `height-1`, which is exactly where the filter bar lands when it's shown, leaving the impression that scrolling was "below the filter bar" rather than "below the file list." The below-indicator is now suppressed when the filter bar occupies that row; the title's match-count and natural cursor advancement convey the overflow signal.
- fix(cmd): `SetVersionInfo` now omits the `, built …` suffix when the date is empty or `"unknown"`. A common dev-CI pattern injects only commit (`go build -ldflags "-X main.commit=$(git rev-parse HEAD)"`), leaving date at `main.go`'s `unknown` default — round 2 fixed the commit-default case, but the date placeholder still surfaced as `dev (commit abc1234, built unknown)`.
- ci: cross-compile matrix extended to `arm64`. `.goreleaser.yaml` ships `linux/darwin/windows × amd64/arm64` and the README advertises `linux/arm64` `.deb` and `.rpm` artifacts, but CI only built `amd64`. An arm64-specific build break would have sailed through CI and produced broken release binaries. Pure-Go project with `CGO_ENABLED=0`, so the existing `go build -o /dev/null` step covers all six combinations at near-zero cost.

### Fixed
- `cleanTarPath` now returns the cleaned path instead of the original. The validator computed `path.Clean(p)` and then returned `p`, so a tar entry like `usr/./bin/sh` (legal output of busybox tar and some `--transform` rules) was inserted into the tree under a phantom `.` node. Whiteout matching missed `.wh.bin` entries against such paths, the TUI rendered ugly `/usr/./bin/sh` rows, and efficiency comparisons silently under-reported waste when one layer used the clean form and another the dotted form. One-character fix at `image/tree_parser.go:68` plus a `TestParseLayerTar_EmbeddedDotSegmentNormalized` regression.
- `layerx ci --highest-user-wasted-percent 0` now disables the rule, matching its own help text. `cmd/ci.go` guarded `HighestWastedBytes` with `if hwb > 0` but appended `HighestUserWastedPercent` unconditionally, and the rule's `Evaluate` had no internal bypass — so passing `0` (or setting it in `.layerx.yaml`) failed any image with any waste at all. `cmd/ci.go` now mirrors the existing guard, and `HighestUserWastedPercent.Evaluate` itself bypasses on `Threshold <= 0` for belt-and-suspenders coverage of direct callers (e.g. future MCP).
- `streamPullProgress` now returns its stream error instead of swallowing it. `ensureImageWithProgress` ignored the result, so a network failure or invalid manifest mid-pull would surface to the user as the misleading downstream `failed to export image: No such image` instead of the actual pull failure. The function signature now returns `error`; the caller wraps the error in `*ErrPullFailed{Ref, Cause}`, matching the non-progress branch.
- File viewer no longer re-runs chroma syntax highlighting on every TUI frame. `renderFileView` called `highlightFileLines(path, data)` unconditionally inside the View pipeline, so each keystroke, spinner tick, or resize re-tokenized the entire file from scratch — a 500 KB file (within the 1 MB viewer cap) added 200–400 ms of latency per frame. Highlighting is now computed once when `fileContentMsg` arrives and cached on the model as `viewHighlightedLines`; `renderFileView` accepts the slice via `viewerParams` and skips chroma in the render hot path. The cache is invalidated on Esc and on each new content load.
- Pressing Esc during a slow file extraction now cancels the in-flight Docker container work. `fetchFileContent` and `fetchFileRaw` previously used `context.Background()`, so the temp-container create/copy/remove round-trip kept running even after the user moved on; rapid Enter→Esc→Enter sequences could race two extractions at once. The model now carries `viewerCancel` and `saveCancel` `context.CancelFunc`s that are wired through `ExtractFromLayer`/`ExtractRawFromLayer`. Each new fetch cancels any prior context; Esc on the viewer also cancels; arrival of the matching message clears the func.
- Waste overlay no longer silently jumps to layer 1 when a wasted file's intro layer is unknown. `buildIntroIndex` only walks the *last* stacked tree, so files whited out before the final layer were missing from the index — the bare map lookup returned Go's zero value (0), which the jump path then treated as "introduced at layer 1". The lookup now uses comma-ok and stores `-1` for unknowns; the row renders `L?`, and Enter shows `Intro layer unknown for <path>` instead of jumping.
- File viewer no longer pops back open after Esc. A slow extract whose user pressed Esc mid-load would re-set `viewState = viewReady` when the goroutine finally delivered, overlaying the user's current screen. The model now carries a monotonic `viewRequestID` captured by each load goroutine; Esc bumps the ID, and any in-flight `fileContentMsg` whose ID doesn't match is dropped. Same pattern applied to `fileSaveMsg` (`x` save-to-disk) so a second `x` invalidates the first.
- Efficiency calculation no longer counts hardlinks as 0-byte files. Tar `TypeLink` entries carry `hdr.Size == 0` by convention (the real bytes are at the link target), but the parser was inserting them as zero-byte file nodes that `Efficiency` then walked, inflating the file count without adding bytes. `FileNode` now carries `IsHardlink` / `Linkname`; `walkFiles` skips hardlinks, so per-layer waste reflects actual content bytes only.
- Pull-progress sends in `image/docker.go` no longer deadlock the analyze pipeline on a busy TUI. `ResolveWithProgress`, `ensureImageWithProgress`, and `streamPullProgress` previously sent on the bounded `progress` channel with a bare `<-`. On a multi-arch pull (hundreds of JSON events) a brief Update-loop pause could fill the buffer and hang the entire pull. All four sends now go through the existing non-blocking `emitProgress` helper, matching the pattern in `image/analysis.go`. Dropped events for `Exporting`/`Parsing`/`Pulling` phase markers are a cosmetic loss only.
- `layerx ci` no longer calls `os.Exit(1)` from inside the Cobra `RunE` handler. The bare `os.Exit` skipped deferred cleanup and made the failure path untestable. `executeCICheck` now returns a sentinel `*ErrCIFailed` on rule failure; `main.go` already exits with status 1 on any non-nil error from `cmd.Execute()`. The `ci` subcommand and the `CI=true` root path both set `SilenceErrors`/`SilenceUsage` so the returned error doesn't tack a redundant "Error: ..." line and usage block onto the already-printed report.
- `loadLayerTars` no longer buffers the entire image archive in memory per extraction. The previous implementation read every blob in `manifest.json` into a `map[string][]byte` regardless of which layer the user requested — on an 8 GB ML image that meant 8 GB of heap per `Enter`/`x` keypress, with mashing keys producing concurrent multi-gigabyte allocations. The function now spools the `ImageSave` stream to a temp file, reads `manifest.json` in a first pass, then reads only the blobs in `manifest.Layers[0..layerCursor]` in a second pass. Peak heap is now bounded by the largest needed blob, not the whole image. Both `ExtractFromLayer` and `ExtractRawFromLayer` pass `layerCursor+1` as the cap.
- Cache write in `AnalyzeWithOptions` no longer fires with an empty `cacheRoot`. The `digestErr != nil` branch (image was not local pre-resolve) called `saveCache(cacheRoot, …)` without the `cacheRoot != ""` guard that the default branch had, so a failure of `CacheDir()` at startup would produce a confusing `MkdirAll(<digest>)` against the current working directory. The post-resolve cache-write switch is now wrapped in a single `if cacheRoot != ""` outer guard covering all branches.
- `loadCache` no longer leaves bad cache files on disk on Windows. The previous implementation called `os.Remove(path)` on schema/digest/gob mismatches while a `defer f.Close()` on the same path was still pending. Go's `os.Open` on Windows uses only `FILE_SHARE_READ | FILE_SHARE_WRITE` (no `FILE_SHARE_DELETE`), so the remove failed with `ERROR_SHARING_VIOLATION`, the `_ =` swallowed the error, and the bad file persisted — every subsequent run hit the same mismatch and re-paid the full cold cost. The open+decode is now in a separate `readCacheFile` helper that closes the handle on return, so the caller's `os.Remove` runs against a closed handle and succeeds on Windows. Linux/macOS behavior is unchanged.
- File viewer search highlight no longer renders mojibake when `strings.ToLower` changes a rune's byte length (e.g. Turkish `İ`). `renderViewerLine` now indexes lines, lowercased lines, and the query as `[]rune` slices, matching the existing `renderNameWithHighlight` in the file tree. ASCII paths (the common case for Dockerfiles, configs, scripts) are unchanged.
- Whiteout entries (`.wh.<name>`, `.wh..wh..opq`) no longer leak into `EfficiencyResult.WastedFiles`. `walkFiles` skipped them via `IsHardlink` for size, but two layers deleting the same path produced a phantom `WastedFile{TotalWasted:0, LayerCount:2}` in the user-visible waste overlay and JSON export. `walkFiles` now skips any node whose name satisfies `isWhiteoutName` before recursing.
- `LAYERX_CACHE_DIR=~/foo` now expands to `$HOME/foo` instead of creating a literal `~` directory in the current working directory. `expandHome` resolves a leading `~` (or `~/...`) via `os.UserHomeDir`; `~user/...` forms are rejected with a stderr warning. Bare paths without `~` are unchanged.
- `saveCache` now sweeps stale `layers.gob.tmp-*` files older than one hour from the digest directory before creating a new temp file. A SIGKILL during a previous write could orphan temp files; without the sweep, repeated crashes would accumulate them indefinitely.
- `image/tree_parser.go` now whitelists tar typeflags (`TypeReg`, `TypeDir`, `TypeSymlink`, `TypeLink`, `TypeChar`, `TypeBlock`, `TypeFifo`). A tar carrying `TypeXGlobalHeader` (rare but legal) would previously surface as a phantom `PaxHeaders.0/` directory in the rendered tree.
- `IsBinary` now trusts `http.DetectContentType` for non-`octet-stream` results. UTF-16 text files (which carry null bytes in their high-byte half) were correctly classified as `text/plain; charset=utf-16le` by detection, then mis-overridden as binary by the unconditional null-byte scan. The scan now runs only when detection returns `application/octet-stream`.
- `cleanTarPath` now rejects `..` segments via `path.Clean`. Defense-in-depth: the cache and extractor paths already validate independently, but a future consumer using tree paths to write to disk would have inherited the gap.
- `loadCache` no longer evicts the cache file on a transient `os.Open` failure. `readCacheFile` only flagged decode-time errors as transient; an open-time failure (EACCES from a permission flip, EBUSY from a network share, anything other than ENOENT) was returned with `transient=false`, which caused `loadCache` to treat the still-good file as confirmed corruption and remove it. `os.Open` errors that aren't `os.ErrNotExist` are now flagged transient too, so a recoverable I/O glitch surfaces as an error and keeps the cache intact for the next run. Caught when `TestLoadCache_TransientIOError_KeepsFile` failed on the GH Actions ubuntu runner — the runner's filesystem enforces 0o000 against the owner correctly, but `loadCache` was discarding the EACCES instead of treating it as transient.

### Test coverage
- `TestJSONExport_SchemaRoundTrip` now marshals + unmarshals through a literal-named schema struct, locking the public JSON shape against silent tag renames or misplaced `omitempty`.
- `TestLoadCache_TransientIOError_KeepsFile` no longer passes vacuously on Windows. `runtime.GOOS == "windows"` and root-uid checks now skip explicitly at the top; the assertion path runs unconditionally on Linux/macOS non-root. Tightening the assertion surfaced the open-time-EACCES eviction bug above on the GH Actions ubuntu runner.
- `TestParseLayers_HistoryMismatch` now feeds 1024 zero bytes per fake layer.tar instead of 100/200/300. After `parseLayers` started propagating `ParseLayerTar` errors, the short buffers caused `archive/tar` to error mid-header; the larger size lands on a clean two-block end-of-archive. The test's intent (history shorter than layer list → blank commands for excess layers) is unchanged.
- `TestExtractFromLayer_SingleLayerImage` covers the cursor=0 walk-back boundary on a one-layer image, both for the "found" and "not found" branches.
- `TestParseLayerTar_EmbeddedDotSegmentNormalized` covers `cleanTarPath` collapsing embedded `./` segments.
- `TestHighestUserWastedPercent_DisabledWhenZero` covers `Threshold == 0` bypass.
- `TestFileContentMsgPopulatesHighlightCache` and `TestEscClearsHighlightCache` cover the lifecycle of the new chroma cache.

### Technical
- New `FileNode` fields: `IsHardlink bool`, `Linkname string`. Populated at parse time from `tar.TypeLink` and `hdr.Linkname`.
- `model.viewRequestID` and `model.saveRequestID` are bumped on dispatch (and on Esc for the viewer); message handlers drop any message with a stale ID.
- New exported `cmd.ErrCIFailed` sentinel error; CI failure path is now reachable from tests without killing the test process.
- New `image.expandHome` and `image.sweepOrphanTempFiles` helpers in `image/cache.go`.
- New `viewerParams.highlightedLines` field; `renderFileView` consumes pre-computed chroma output instead of computing it inline.
- New model fields `viewHighlightedLines []string`, `viewerCancel context.CancelFunc`, and `saveCancel context.CancelFunc`.
- `fetchFileContent` and `fetchFileRaw` now take `ctx context.Context` as a parameter.
- Cleared all gopls modernization hints across the tree (Go 1.22+ idioms): `min`/`max` for clamp patterns, `for i := range n` integer loops, `strings.SplitSeq` for range loops, `errors.AsType[T]` in place of `errors.As`, `slices.Contains` for the binary-byte null check, and tagged `switch` on `m.viewState` / `msg.Code`. Behavior preserved.
- Replaced `client.NewClientWithOpts` with `client.New` in `image/docker.go` (the former is scheduled for removal in the next moby release). `WithAPIVersionNegotiation` is retained per the project rule, even though it is now a no-op in the moby client.
- Removed the deprecated `tar.TypeRegA` alias from the typeflag whitelist and the extractor's regular-file check (`TypeRegA` is an alias for `TypeReg`).

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
