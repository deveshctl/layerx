# layerx — Improvements (for Claude Code prompts)

This file is a **copy/paste-friendly backlog** distilled from:
- `fixes.md` (canonical bug/improvement prompts)
- `fixes_2.md` (deep sweep findings)
- `cursor.md` (product roadmap)

Format: **Current → Issue → Fix → Impact** (keep prompts small: one item per session/PR).

---

## Already fixed (keep as regression checks)

### F1. `q` quits inside filter/search input (FIXED)
- **Current**: Typing `q` in filter/search quit the app.
- **Issue**: Quit keybinding had higher precedence than text input.
- **Fix**: Make `q` behave as text input when input is active; keep `Ctrl+C` quitting.
- **Impact**: Makes filtering/search usable; avoids accidental quits.

### F3. File viewer always showed final layer version (FIXED)
- **Current**: Viewer/extract read file from the final image state.
- **Issue**: Extractor was container-based and couldn’t target a specific layer.
- **Fix**: Extract from the **selected layer** (layer-aware extraction).
- **Impact**: Correctness restored; the headline viewer feature becomes trustworthy.

### F4. CI `highest-user-wasted-percent` correctly disables at 0 (FIXED)
- **Current**: `0` is documented as “disable this rule”.
- **Issue**: Historically could be evaluated/constructed inconsistently vs sibling rules.
- **Fix**: Only add the rule when `> 0`, and also guard evaluation so disabled stays pass.
- **Impact**: CI behaves predictably; docs match behavior.

### F5. Stale viewer/save async results don’t overwrite current UI (FIXED)
- **Current**: Slow extraction/save could complete after user closed the viewer.
- **Issue**: Async messages weren’t correlated to the active request.
- **Fix**: Request IDs (`viewRequestID`, `saveRequestID`) and stale-message ignore.
- **Impact**: No surprise “viewer pops back”; TUI feels stable.

---

## P0 — correctness / “trust the numbers”

### B1. Directory metadata lost during stacking (Bug #2 in `fixes.md`)
- **Current**: If a layer only changes dir metadata (mode/uid/gid), stacked tree keeps old metadata.
- **Issue**: `mergeLayer` seeds merged node from cumulative metadata and drops layerRoot metadata changes.
- **Fix**: When merging directories, detect metadata changes and apply them to the merged node (mark Modified; update IntroducedInLayer).
- **Impact**: Correct permissions/ownership; avoids misleading diffs.

### B4. Layer-correct extraction regression test missing (HIGH #3 in `fixes_2.md`)
- **Current**: Tests don’t assert the selected layer index is passed to extractor.
- **Issue**: Mocks discard the layer parameter; regressions won’t be caught.
- **Fix**: Capture `lastLayer` in the mock and assert it equals `m.layerCursor` in a test.
- **Impact**: Protects the recently-fixed layer correctness long-term.

---

## P1 — stability/perf on real-world large images

### P1. Pull-progress channel can deadlock (MEDIUM #4 in `fixes_2.md`)
- **Current**: Progress sends can block if UI is busy and the channel fills.
- **Issue**: Some sends are blocking; the codebase already has a non-blocking `emitProgress` pattern elsewhere.
- **Fix**: Route all progress emissions through a non-blocking send (`select { default }`).
- **Impact**: Prevents rare but catastrophic hangs during pull/export/parse.

### P2. Stream layer parsing to avoid huge RAM spikes (Improvement #8 in `fixes.md`)
- **Current**: Layer parsing may read large blobs into memory.
- **Issue**: Big images can cause multi-GB allocations and slow startup.
- **Fix**: Stream-decode outer tar and build trees incrementally (avoid `io.ReadAll` of layer blobs).
- **Impact**: Handles fat images without OOM; faster time-to-TUI.

### P3. Extractor timeout + memory guardrails (MEDIUM #9 + #10 in `fixes_2.md`)
- **Current**: Extract uses background context; extraction can hang; extraction may buffer too much.
- **Issue**: No timeout/cancel; repeated extracts can allocate enormous memory on large images.
- **Fix**: Add timeout context (configurable) and avoid loading the entire image when only layers 0..cursor are needed.
- **Impact**: Prevents “Enter key freezes app” and OOM on real images.

### P4. Cache root guard in digest error branch (MEDIUM #11 in `fixes_2.md`)
- **Current**: Cache save can run with empty cacheRoot in a rare branch.
- **Issue**: Can create weird paths / permission errors.
- **Fix**: Apply `cacheRoot != ""` guard consistently across branches.
- **Impact**: Fewer confusing cache warnings; safer behavior on Windows.

---

## P2 — UX polish that reduces confusion

### U1. Waste overlay scrolling (Bug #4 in `fixes.md`)
- **Current**: Waste overlay cursor can go off-screen with long lists.
- **Issue**: No viewport/scroll handling for the overlay list.
- **Fix**: Implement scrolling/viewport and keep the selected row visible.
- **Impact**: Waste navigator remains usable on real images.

### U2. `introIdx` zero-value collision misleads jumps (MEDIUM #7 in `fixes_2.md`)
- **Current**: Some wasted files can show intro layer as 0 when actually unknown (e.g., whited out).
- **Issue**: Map lookup uses zero value on miss.
- **Fix**: Use two-value lookup; set “unknown” to `-1` and guard.
- **Impact**: Avoids misleading “Jumped → L1” behavior.

### U3. Invisible search matches on truncated long lines (Improvement #9 in `fixes.md`)
- **Current**: Viewer search can match text that isn’t visible due to truncation.
- **Issue**: Search runs on full line while rendering truncates.
- **Fix**: Search within rendered slice or indicate “match beyond truncation.”
- **Impact**: Search feels correct; avoids “it says found but I can’t see it.”

### U4. Unicode-safe padding + highlight (Bug #7 in `fixes.md` + LOW #13 in `fixes_2.md`)
- **Current**: Some padding/highlight operations use byte length / byte slicing.
- **Issue**: Unicode strings can misalign or render corrupted highlights.
- **Fix**: Use rune-aware measurement/slicing consistently.
- **Impact**: Correct rendering in non-ASCII paths/content.

---

## P2 — data accuracy (efficiency + diffs)

### A1. mergeLayer marks unchanged files as Modified (MEDIUM #5 in `fixes_2.md`)
- **Current**: Re-emitted identical files appear as Modified (yellow).
- **Issue**: No metadata equality check for non-dir nodes during merge.
- **Fix**: Compare size/mode/uid/gid; mark Unchanged when identical.
- **Impact**: Diff coloring becomes trustworthy; waste UX is less noisy.

### A2. Hardlinks distort size/efficiency math (MEDIUM #8 in `fixes_2.md`)
- **Current**: Hardlink entries are parsed as 0-byte nodes; totals undercount.
- **Issue**: Tar hardlinks encode size on target, not link entry.
- **Fix**: Represent hardlinks explicitly; either resolve to target size or exclude from “new bytes” accounting.
- **Impact**: Efficiency numbers make sense on Alpine/busybox images.

### A3. Skip whiteout artifacts in wasted-files list (LOW #14 in `fixes_2.md`)
- **Current**: Whiteout files can appear in wasted list with 0 size.
- **Issue**: Efficiency walk includes whiteout paths.
- **Fix**: Filter whiteout names from wasted file reporting.
- **Impact**: Cleaner UI/JSON; fewer confusing entries.

---

## CI / testing improvements

### C1. CI uses `os.Exit(1)` in command path (Bug #5 in `fixes.md`)
- **Current**: Direct exits can complicate unit tests and reuse.
- **Issue**: Hard exit bypasses test harness patterns.
- **Fix**: Refactor to return error / exit code and let `Execute()` decide.
- **Impact**: Better testability and future extensibility.

### C2. Add `-race` to CI workflow (MEDIUM #12 in `fixes_2.md`)
- **Current**: GitHub Actions runs tests without race detector.
- **Issue**: Goroutines/channels exist; races could slip in.
- **Fix**: Run `go test -race ./...` (at least on linux/amd64 job).
- **Impact**: Catches concurrency regressions early.

### C3. JSON schema round-trip test (LOW #15 in `fixes_2.md`)
- **Current**: Tests validate Go struct shape, not marshaled JSON contract.
- **Issue**: Tag/name changes could silently break consumers.
- **Fix**: Marshal + unmarshal and assert key fields/names.
- **Impact**: Prevents accidental breaking changes.

---

## Config safety

### CFG1. Malformed `.layerx.yaml` can silently disable rules (MEDIUM #6 in `fixes_2.md`)
- **Current**: Some malformed-but-parseable YAML can zero out rule thresholds.
- **Issue**: Defaults are overwritten in-place; “null” structures can erase defaults without error.
- **Fix**: Post-unmarshal validation; error on semantically invalid config when file is present.
- **Impact**: CI is safer; users get actionable config errors.

### CFG2. `~` in `LAYERX_CACHE_DIR` not expanded (LOW #17 in `fixes_2.md`)
- **Current**: `~/foo` can be treated literally.
- **Issue**: Creates unexpected directories / permission issues.
- **Fix**: Expand `~` (or reject + warn).
- **Impact**: Fewer “why is there a ~ folder” surprises.

---

## Product roadmap (vs dive + adoption)

These are from `cursor.md` (not “bugs”).

### R1. `layerx compare <old> <new>` (v1.1 planned)
- **Current**: Single-image inspection only.
- **Issue**: Teams need before/after reports for PRs.
- **Fix**: Add compare domain + CLI; include efficiency + filesystem delta + layer delta sizes.
- **Impact**: Makes layerx a PR gate and regression detector, not just a viewer.

### R2. `layerx ci --format json` (v1.1 planned)
- **Current**: CI output is primarily human text.
- **Issue**: Hard to integrate into dashboards and annotate PRs.
- **Fix**: JSON-formatted CI report output.
- **Impact**: Better automation and tooling integrations.

### R3. `--no-pull` (v1.1 planned)
- **Current**: Pull behavior can be implicit.
- **Issue**: CI reproducibility + offline workflows.
- **Fix**: Flag to prevent pulls; fail if image not present.
- **Impact**: Deterministic CI and faster pipelines.

### R4. Documentation: dive comparison + CI recipes (R16 in `cursor.md`)
- **Current**: README is strong on features/install; missing a direct “layerx vs dive” section.
- **Issue**: New users can’t quickly decide or copy CI snippets.
- **Fix**: Add short comparison + “common workflows” + copy/paste CI examples.
- **Impact**: Higher adoption; fewer setup questions.

