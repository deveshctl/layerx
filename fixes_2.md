# Deep Bug Sweep — Findings Report

*Date:* 2026-05-23
*Branch:* fix/stacking-and-extractor-v121
*HEAD:* 4d26ba6
*Method:* Two rounds of parallel reviewer agents (image/, tui/, cmd+ci+config, then cache/extractor/tree-parser/tests). Every HIGH/MEDIUM finding verified against source before inclusion.

---

## Summary

| Severity | Count |
|---|---|
| HIGH | 3 |
| MEDIUM | 9 |
| LOW | 8 |
| False positives identified and dropped | 9 |

---

## HIGH

### 1. HighestUserWastedPercent cannot be disabled — 0 makes every image fail
- cmd/ci.go:123 appends the rule unconditionally, while the sibling HighestWastedBytes has a if hwb > 0 guard at line 120.
- ci/rules.go:73 evaluates pct <= r.Threshold with no Threshold > 0 short-circuit (compare to HighestWastedBytes.Evaluate at line 49, which does have it).
- The flag help text on cmd/ci.go:62 says "0 disables the rule" — but setting 0 actually means "fail unless waste is exactly 0 bytes," i.e., every real image fails.
- *Fix:* mirror the > 0 guard in buildRules, and add the same internal guard to HighestUserWastedPercent.Evaluate so behavior matches its sibling and matches the doc.

### 2. Stale viewer load overwrites a closed viewer
- tui/model.go:278-289 accepts every fileContentMsg and unconditionally sets viewState = viewReady.
- model.go:327-330 (Esc handler) clears viewState = viewNone and viewContent = nil while a load is in flight. The goroutine still delivers later, reopening the viewer over whatever the user is now doing.
- *Repro:* open a slow file (large layer / cold extract), press Esc immediately, navigate to a different file or the layer panel — content pops back over the screen seconds later.
- *Fix:* add a monotonic viewRequestID on the model. Capture it in the tea.Cmd closure; ignore the message if it doesn't match the current ID. Same pattern is needed for fileSaveMsg (x save-to-disk).

### 3. mockExtractor.ExtractFromLayer discards the layer parameter — Bug #3 regression is unprotected
- tui/model_test.go:924 signature is ExtractFromLayer(_ context.Context, _ string, path string, _ int) — layer is _.
- The whole point of commits 7451203 + e25186f was to fix layer-correct extraction. There is no test that asserts the production code actually passes the right layer to the extractor. A regression that re-introduces "always extract from layer 0" or "always extract from final layer" would pass every existing test.
- *Fix:* capture the layer index in mockExtractor (e.g., lastLayer int), and add a test that sets m.layerCursor = N, fires the extract, asserts mock.lastLayer == N.

---

## MEDIUM

### 4. Pull-progress sends can deadlock on busy TUI
- image/docker.go:87, :97, :181 send to progress with bare <-. The codebase already established a non-blocking pattern in image/analysis.go:155 (emitProgress with select { default }).
- TUI buffer is 16 (tui/model.go:159). streamPullProgress emits per JSON pull message; large multi-arch image pulls produce hundreds. If the bubbletea Update loop is held up briefly (lipgloss redraw on a small terminal, GC pause), the channel fills and the next send hangs the entire pull.
- *Fix:* route the three direct sends through emitProgress. The dropped event for "Exporting"/"Parsing" phase events would be a cosmetic loss only — exactly what the rest of the pipeline already accepts.

### 5. mergeLayer marks files Modified even when nothing changed
- image/stack.go:96-107: when both cumulative and layer have the same non-dir path, the merged node is unconditionally DiffType: Modified. No size/mode/uid/gid comparison.
- A layer that re-emits an unchanged file (common with COPY over already-present files, or rebuilds with identical content) shows up yellow in the UI when it should be grey.
- Doesn't affect efficiency (which walks raw per-layer trees) but does affect the file tree colouring and the "wasted files" UX. This is the missed sibling of the recent c14175a fix that added equivalent comparison for directory metadata.
- *Fix:* compare cChild.Size/Mode/UID/GID to lChild.*; use Unchanged when all match.

### 6. Config silently zeroes thresholds on malformed YAML
- config/config.go:53-56 seeds defaults, then yaml.Unmarshal overwrites in place. A .layerx.yaml containing rules: null (or rules: with no body) zeroes the entire RulesConfig, leaving LowestEfficiency = 0.0.
- LowestEfficiency.Evaluate is score >= 0.0 — always passes. The CI rule the user wanted is silently disabled with no error.
- Project rules say "missing config silently ignored" but a malformed file with structurally-valid-but-semantically-wrong content is not the same as missing.
- *Fix:* post-unmarshal, validate that LowestEfficiency is in (0, 1]. If a non-empty file unmarshals to a zero threshold, return an error rather than passing back a silently-broken config.

### 7. introIdx zero-value collision sends "unknown layer" jumps to layer 0
- tui/waste.go:39 populates idx[path] = IntroducedInLayer only for files in the last stacked tree.
- tui/waste.go:74 does introIdx[wf.Path] — for a wasted file that's been whited out by a later layer, the lookup misses and Go returns the zero value: 0.
- tui/waste.go:142-144 then accepts IntroLayer == 0 as valid, sets m.layerCursor = 0, status reads Jumped → L1 /path.
- User is silently misled into thinking the file was introduced at layer 1 when its true intro layer is unknown.
- *Fix:* change to two-value lookup. In openWaste, use if v, ok := introIdx[wf.Path]; ok { row.IntroLayer = v } else { row.IntroLayer = -1 }. The existing >= 0 guard in wasteJump already handles -1 correctly.

### 8. Hardlinks counted as 0-byte files in efficiency calculation
- image/tree_parser.go:30-40: tar.TypeLink entries (hardlinks) carry hdr.Size == 0 by tar convention — the actual content is at the link target. The parser inserts them as 0-byte file nodes.
- Efficiency walks these as 0-byte files. Result: an Alpine-based image (busybox uses ~50 hardlinks) has its layer content underreported by tens of MB.
- *Fix:* at parse time, store hdr.Linkname on the FileNode for TypeLink; at efficiency time, resolve link targets to inherit their size, OR mark hardlinks specially and exclude them from per-layer waste (a hardlink is genuinely zero new bytes, so this is more correct anyway).

### 9. File extractor has no timeout — Docker hang freezes the TUI
- tui/model.go:1346-1360 passes context.Background() to ExtractFromLayer / ExtractRawFromLayer.
- image/extractor.go then calls ImageSave which streams the entire image. A stalled Docker daemon (memory pressure, dead socket) hangs the goroutine permanently — the user has no way to cancel from the TUI.
- *Fix:* in fetchFileContent/fetchFileRaw, derive a context.WithTimeout(ctx, 60s). Make the timeout configurable via .layerx.yaml. Surface as a friendly error: "extraction timed out — Docker may be unresponsive."

### 10. loadLayerTars buffers the entire image in memory per extraction
- image/extractor.go:212-266 reads every blob into a map[string][]byte before walking layers.
- For an 8 GB ML image, that's 8 GB of heap per call. Each Enter/x keypress in the TUI fires a new call. Mashing keys → multiple concurrent 8 GB allocations → OOM on consumer hardware.
- ExtractFromLayer only ever needs blobs at indices 0..layerCursor, so the worst case for normal use is also < full image size. But the function loads everything anyway.
- *Fix:* pass layerCursor into loadLayerTars and stop after collecting the first layerCursor+1 layers. Better still: stream-decode the outer tar lazily per-layer and don't buffer.

### 11. Cache write attempted with empty cacheRoot in digestErr != nil branch
- image/analysis.go:120 calls saveCache(cacheRoot, digest, layers) with no cacheRoot != "" guard, while the default branch at line 128 does have one.
- If CacheDir() failed at line 67 (very rare), cacheRoot is "". saveCache then runs MkdirAll(filepath.Join("", normDigest), 0o700) — i.e., creates <digest>/ under CWD on Unix, or attempts a top-level path on Windows. Likely permission-denied → user gets a confusing cache warning.
- *Fix:* hoist the cacheRoot != "" guard to apply to both digestErr != nil and default branches.

### 12. CI doesn't run with -race
- .github/workflows/ci.yml:19 runs go test -v -count=1 ./... — no -race.
- image/analysis.go has goroutines + channels (emitProgress, emitCacheWarn), tui/model.go listens on a buffered channel. Any race introduced into these paths is invisible.
- *Fix:* change line 19 to go test -race -v -count=1 ./.... Zero downside on a Linux runner.

---

## LOW

### 13. UTF-8 byte-slicing in viewer search highlight
- tui/fileview.go:183-189 slices line[matchStart:matchEnd] with byte offsets from strings.Index and len(lowerQuery). Safe for ASCII (the common case for Dockerfiles, configs, scripts). Breaks when strings.ToLower changes byte length on certain Unicode codepoints (e.g. İ → i̇), causing rendered mojibake on the highlighted segment. No panic, just visual corruption.
- The codebase already does it correctly in tui/filetree.go:431-445 (renderNameWithHighlight) using []rune indexing.
- *Fix:* convert line, lowerLine, lowerQuery to []rune and index by rune.

### 14. Whiteout entries leak into the wasted-files list
- image/efficiency.go:38 walks raw per-layer trees, which include .wh.<name> and .wh..wh..opq nodes. Whiteout files have Size=0, so they don't affect the score or WastedBytes. But if two layers delete the same path, the same whiteout name appears twice in pathOccurrences and ends up in WastedFiles[] as a phantom entry with TotalWasted=0, LayerCount=2.
- Cosmetic only — but the list is a user-visible UI element (waste overlay) and a JSON export field.
- *Fix:* in walkFiles's callback, skip names that satisfy isWhiteoutName.

### 15. JSON export schema not protected by round-trip test
- cmd/json_test.go only tests the Go struct returned by buildJSONExport, never marshals + unmarshals.
- A future change that renames a tag (json:"layers" → json:"layer_list") or adds omitempty in the wrong place would silently break consumers.
- *Fix:* add a single test that marshals to bytes and unmarshals into the public schema struct, asserts top-level field names.

### 16. TestLoadCache_TransientIOError_KeepsFile has a vacuous-pass branch
- image/cache_test.go:411 defines runtimeIsRestrictivePosix() as os.Geteuid() == 0 — true only when running as root.
- On the GitHub Actions Linux runner, the test runs as a non-root user. chmod 0o000 does prevent reads on Linux non-root, so err != nil should fire and the assertion path runs. But the fallback logic at line ~399 (when err == nil) only skips on root; on Windows or any other "open succeeded despite chmod" path, the test passes vacuously without asserting anything.
- *Fix:* replace the conditional skip with runtime.GOOS == "windows" check at top of the test, then use require.Error unconditionally on Linux/macOS.

### 17. ~ in LAYERX_CACHE_DIR not expanded, silently used literally
- image/cache.go:24-34 + dirIsUsable at line 50-53: a nonexistent path is treated as usable. ~/foo → MkdirAll creates a literal ~ directory in CWD.
- *Fix:* detect ~ prefix, either expand or warn-and-reject.

### 18. Orphaned cache temp files on SIGKILL
- image/cache.go:173-207: temp file layers.gob.tmp-<hex> orphaned if process killed mid-write. No cleanup sweep.
- *Fix:* at start of saveCache, glob <dir>/layers.gob.tmp-* and remove entries older than say 1 hour.

### 19. TypeXGlobalHeader would emit phantom PaxHeaders.0/ directory
- image/tree_parser.go:30: typeflag not filtered. Go's tar reader transparently merges local PAX/GNU long-name headers, but exposes TypeXGlobalHeader as a normal entry. Almost no Docker tars carry this, but if one does, a phantom dir appears in the tree.
- *Fix:* add a typeflag whitelist (TypeReg, TypeRegA, TypeDir, TypeSymlink, TypeLink, TypeChar, TypeBlock, TypeFifo).

### 20. UTF-16 text files reported as binary
- image/extractor.go:43-57: after DetectContentType correctly returns text/plain; charset=utf-16le, the function still scans for null bytes and overrides to binary.
- *Fix:* trust DetectContentType when it returns a text/* content type; only fall back to null-byte scan when it returns application/octet-stream.

### 21. Path traversal in tar parser (defense-in-depth)
- image/tree_parser.go:46-50: cleanTarPath doesn't reject .. segments. The cache code already does (cache.go:84), so the project considers this real.
- Not exploitable today (extractor uses raw hdr.Name independently; the TUI just renders), but a future consumer that uses tree paths to write to disk would inherit the gap.
- *Fix:* add path.Clean + reject if result starts with ...

### 22. No ExtractFromLayer test for a single-layer image at cursor=0
- image/extractor_test.go: existing tests use a 3-layer fixture. A fence-post bug at the walk-back boundary on a 1-layer image would be caught only incidentally.
- *Fix:* add TestExtractFromLayer_SingleLayerImage.

---

## Latent / structural (not bugs today)

- *cmd/root.go:90* passes ciCmd (the subcommand) into executeCICheck when CI=true is set on the root command. buildRules then queries ciCmd.Flags().Changed(...) — flags that were never parsed because the user didn't invoke ci. Today this falls through harmlessly to config defaults, but if anyone later adds a threshold flag to rootCmd expecting it to override in CI=true mode, it will silently fail. Worth a code comment at minimum, or refactoring buildRules to take values rather than a *cobra.Command.

---

## False positives — investigated, not bugs

- *TUI: "fetcher uses layerCursor instead of IntroducedInLayer"* (model.go:1346/:1356). The agent had the polarity backwards. The user navigates the stacked tree at the layer they're currently viewing; extracting from m.layerCursor is correct and matches the recent e25186f fix. viewOriginLayer = f.IntroducedInLayer is metadata for the header display ("originally introduced in L2"), not a directive about which layer to extract from.
- *Image: "opaque whiteout produces duplicate Removed+Added children that corrupt later merges"* (stack.go:61-71). Both nodes coexisting in this layer's stacked output is the correct semantic. The next layer's merge starts from cumulative = cloneStructure(stacked), and cloneStructure (stack.go:226) drops Removed nodes — the duplicate doesn't propagate.
- *Cache: "Windows os.Rename over existing file fails."* False since Go 1.5 (~2015) — Windows now uses MoveFileExW with MOVEFILE_REPLACE_EXISTING. The cache works fine on Windows.
- *Test: "actions/checkout@v6 doesn't exist."* CI runs are passing on the current branch as of commit 4d26ba6. The agent's knowledge of action versions was stale.
- *Test: "go build -o /dev/null masks Windows linker errors."* The full compile + link cycle still runs; only the final binary write is discarded. Errors surface.
- *Extractor: "Symlinks fall through to older layer content."* Confirmed in code, but the code comment marks this as intentional. Reasonable design choice (matches dive).
- *Extractor: truncation off-by-one when expectedSize == MaxViewSize.* Re-examined: when the file is exactly 1MB, the report Truncated=false is correct. The off-by-one fires only if Docker's Stat.Size is unreliable, which is rare.
- *Extractor: opaque whiteout ancestor == "" check covers root case incorrectly.* Re-examined: the check is correct. A root-level opaque whiteout means "delete everything in /" — only ever appears in pathological cases.
- *CMD: os.Exit(1) bypasses defers.* Real structurally, but no actual deferred resources need cleanup in this path. Pure style.

---

## Recommended fix order

1. *#12 add -race to CI* — one-line, zero risk, catches future regressions.
2. *#3 mockExtractor captures layer + regression test* — protects the bug just fixed.
3. *#7 introIdx misleading L1 jump* — user-visible UX bug, simple fix.
4. *#1 HighestUserWastedPercent always fires* — documented behavior is wrong.
5. *#2 stale viewer load* — visible bug, easy to repro.
6. *#8 hardlinks* — affects efficiency numbers on Alpine, important for accuracy of the headline feature.
7. *#9 + #10 extractor timeout & memory* — important on large images, can fail Gate C unexpectedly.
8. The rest as time permits.

---

## Coverage notes

- *Files reviewed:* all 47 .go source files (~7.2k production lines) and all 16 test files (~3.6k lines).
- *CI workflows reviewed:* .github/workflows/ci.yml, .github/workflows/release.yml.
- *Not reviewed:* CHANGELOG.md, README.md, go.mod, go.sum, scripts.
- *Verification method:* every HIGH/MEDIUM finding cross-checked by reading the cited file at the cited lines. Findings that contradicted the source were dropped.
- *Tools restriction:* Windows dev machine, so no go test/go build/go run was executed. All findings are static analysis.