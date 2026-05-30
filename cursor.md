# layerx — Feature & Refinement Planning Guide

> **For maintainers and AI agents (Claude Code Opus, Cursor, etc.)**  
> - **This file (`cursor.md`):** *what* to build — features, refinements, prioritization.  
> - **[`Maintainer_Guide.md`](Maintainer_Guide.md):** *how* to build and ship — architecture, releases, tests, PR rules.  
>  
> **Read both before starting work.** Implement features per this doc; follow process and package rules in the Maintainer Guide.  
> **Bugs and targeted fixes:** [`fixes.md`](fixes.md) — canonical list with copy-paste Claude prompts. Do not duplicate those items here.

---

## AI agent — quick context

| Field | Value |
|-------|--------|
| Project | **layerx** — Docker image layer inspector (Go CLI + TUI) |
| Code | [`layerx/`](layerx/) — module `github.com/deveshpharswan/layerx` |
| Kernel package | `image/` — must **not** import `tui/`, `ci/`, or `config/` |
| Entry | `main.go` → `cmd/` → `image` / `tui` / `ci` / `config` |
| Tests | `go test ./...` from `layerx/` (no Docker in CI; use mocks) |
| Planned v1.1 | `compare`, `ci --format json`, `--no-pull` (see bottom of this file) |

**When implementing any item below:** add domain logic in `image/`, wire CLI in `cmd/`, UI in `tui/`, rules/reports in `ci/`. One user-visible outcome per PR. See Maintainer Guide §7–§8.

---

Plain-language breakdown for planning. Each item uses: **Current → Issue → Improvement → Impact**.

---

# Part A — Additional Features (High Impact)

These are new capabilities that fit layerx’s core job: help people understand and shrink Docker images.

---

## ~~1. Waste Navigator~~ — **SHIPPED** (TUI `w` overlay)

Waste overlay with ranked paths, jump-to-tree, and copy is in the TUI. Remaining waste UX work (e.g. overlay scroll when list is long) is tracked in [`fixes.md`](fixes.md) (#4), not here.

Optional follow-ups still in backlog: CLI `layerx wasted`, richer hints in the overlay (Part A #5).

---

## 2. Directory Size Rollup

**Current**  
The file tree shows size per file. You can sort files by size. Folders can be expanded and collapsed.

**Issue**  
Large waste often sits under a folder (`/usr`, `/var/cache`, `node_modules`, `.git`). A single big file might be rare; many medium files under one directory are common. Per-file size does not answer: “How big is this whole folder in this image?”

**Improvement**  
Show **total size per directory** — the sum of everything inside it (files and subfolders). Optionally sort directories by total size. Lets users spot “this folder is 400 MB” without opening every child.

**Impact**  
Matches how people think when optimizing images (“apt cache is huge”, “we copied node_modules”). Cuts investigation time on deep trees, especially for language runtimes and package managers.

---

## 3. CI Path Rules

**Current**  
CI checks three global rules: minimum efficiency score, maximum wasted bytes, and maximum waste as a percent of total image size. All are image-wide numbers.

**Issue**  
A fat base image (Node, Python, Ubuntu) can fail efficiency even when the Dockerfile steps are fine. Teams often care about specific mistakes: “apt cache must not live in its own layer”, “no duplicate `node_modules`”, “no secrets path”. Global scores cannot express those rules.

**Improvement**  
Allow rules on **paths or patterns**, for example:
- This path must not appear in more than one layer
- No file matching `*.pyc` or `**/node_modules/**` as wasted
- Block list: `/var/lib/apt/lists`, `/root/.cache`

Configurable in `.layerx.yaml` like existing rules.

**Impact**  
CI becomes actionable and stable in real pipelines. Teams enforce *their* standards, not a single efficiency number that fights the base image. Fewer false failures, clearer pass/fail reasons.

---

## 4. Base-Relative Efficiency

**Current**  
Efficiency is calculated over the **entire** image — every layer, including the base (`FROM node:20`, `FROM alpine`, etc.).

**Issue**  
If the base is 900 MB and your app adds 50 MB with small waste, the overall score still looks bad because base layers dominate the math. Developers feel punished for choices they did not make. Hard to set one threshold for all image types.

**Improvement**  
Add an option like “measure efficiency only **after** this base image” (e.g. `--base node:20-alpine`). Waste and score apply only to layers you added on top of the base.

**Impact**  
CI thresholds become fair for real app images. Compares apples to apples: “Did *our* Dockerfile get better?” not “Is the whole image including Node perfect?” Essential for teams on large official bases.

---

## 5. Actionable Hints (Simple Suggestions)

**Current**  
When CI fails, users see numbers and a list of wasted paths. The TUI shows efficiency in the status bar. There is no guidance on *what to change in the Dockerfile*.

**Issue**  
Beginners and even experienced devs know something is wrong but not the fix. “`/var/lib/apt/lists` wasted 40 MB across 4 layers” does not automatically mean “merge apt install and cleanup into one RUN”.

**Improvement**  
After finding top waste, show **short, fixed suggestions** (templates, not AI), for example:
- “This looks like package manager cache left in a separate layer — combine install and cleanup in one RUN.”
- “This path appears in many layers — consider multi-stage build or `.dockerignore`.”

Show in CI output and optionally in the TUI waste view.

**Impact**  
Shortens the learning curve. layerx becomes a coach, not only a microscope. Especially valuable in teams where not everyone is a Docker expert.

---

## 6. `layerx layers` — Quick Layer Table (No TUI)

**Current**  
To see layers interactively, you run `layerx <image>` and use the TUI. For scripts, there is full JSON export (`--json`) which is heavy, or you use `docker history` which does not show the same layer sizes or efficiency layerx computes.

**Issue**  
Sometimes you only want a quick table in the terminal or in a script: layer index, size, command, maybe efficiency summary. JSON is overkill; TUI is too heavy for CI logs or copy-paste into docs.

**Improvement**  
A lightweight command that prints a simple table: layer number, size, command snippet, optional wasted bytes for that layer. No interactive UI. Fast to run in scripts and README examples.

**Impact**  
Better for automation, docs, and “quick peek” workflows. Complements the TUI instead of replacing it. Low friction for people who live in terminals and CI logs.

---

## 7. Analysis Cache

**Current**  
Every run resolves the image through Docker: save image as tar, parse every layer. Large images can take tens of seconds or more each time you open layerx.

**Issue**  
During Dockerfile iteration, people run layerx many times on the same tag or nearly the same image. Re-parsing from scratch feels slow and discourages frequent checks.

**Improvement**  
Cache parsed results locally (e.g. under a user cache folder), keyed by image digest. If the image has not changed, reload from cache instead of re-exporting and re-parsing. Optional flag to force refresh.

**Impact**  
Makes repeated inspection feel instant. Encourages “check after every build” behavior. Big quality-of-life improvement for power users with large images — without changing what layerx reports, only how fast it gets there.

---

# Part B — Refinements to Existing Features

These improve what you already ship. Smaller scope than new commands, but still meaningful for daily use.

---

## ~~R1. Connect Efficiency Score to Navigation~~ — **SHIPPED** (see Part A #1 / `w` overlay)

---

## R2. Layer Panel — Show “Size Added This Step”

**Current**  
Each layer shows its own size (from the layer archive) and the Dockerfile command that created it.

**Issue**  
Hard to see which **step** bloated the image. Layer 12 might be 200 MB — is that normal for that command or a mistake? Cumulative “image grew by X MB here” is clearer than raw layer blob size alone.

**Improvement**  
Next to each layer, show **delta**: how much the image grew at that step compared to the previous layer (or show both raw layer size and delta).

**Impact**  
Directly ties Dockerfile lines to size outcomes. The main question when optimizing is “which RUN hurt?” — this answers it at a glance.

---

## R3. Filter UX — Clearer and More Powerful

**Current**  
Press `/` to filter the file tree by substring. Enter confirms filter; a second Enter may open a file. Esc clears through several steps. Diff-only mode (`d`) hides unchanged files.

**Issue**  
- Two-step Enter is easy to miss; users think Enter should open the file immediately.
- Substring-only filter is weak for patterns like “all `.so` files” or “everything under `node_modules`”.
- No quick filter for “only added” or “only removed” files.

**Improvement**  
- Visible hint in the status bar when filter is active (you already use badges like `[diff]` — extend that).
- Optional regex or glob filter.
- Shortcuts or toggles: show only added / only removed / only modified.

**Impact**  
Less confusion, faster targeting during layer-by-layer review. Especially on images with thousands of files.

---

## R4. File Viewer — Flexible Limits and Binary Peek

**Current**  
Text files open inline with line numbers and search. Files larger than 1 MB are truncated with a notice. Binary files show a message that content cannot be displayed. Full file can still be saved to disk with `x`.

**Issue**  
- 1 MB limit is arbitrary; some config or log files are slightly larger.
- For binary, users only know “it’s binary” — not whether it’s the wrong JAR, binary, or empty artifact.
- Search matches on truncated long lines can be invisible (horizontal scroll fix: [`fixes.md`](fixes.md) #9).
- No general line wrap for very long lines (some configs, minified JSON).

**Improvement**  
- Let max view size be configured (flag, env, or config file).
- For binary: show first chunk as hex or a short summary (“ELF executable, 2.4 MB”) so users can confirm without extracting.
- Optional word wrap in viewer (separate from fixes.md #9 search scroll).

**Impact**  
Fewer “I have to extract and open elsewhere” moments. Better debugging when the wrong file was copied into the image.

---

## R5. File Extraction — Choose Where Files Go

**Current**  
Press `x` to save the focused file to the **current working directory**, using the file’s basename only.

**Issue**  
- Overwrites or clashes if two files share a name from different paths.
- No choice of output folder without moving files afterward.
- Directories cannot be extracted (only single files).

**Improvement**  
- Prompt or flag for output path (or save as `path-with-slashes-flattened` to avoid clashes).
- Later: extract a whole directory subtree for deeper debugging.

**Impact**  
Safer and more convenient when pulling configs, certs, or binaries out of images for comparison or incident response.

---

## R6. Diff-Only Mode and Folder Collapse

**Current**  
Diff-only mode shows only changed files in a flat list. Tree collapse/expand is disabled in that mode (documented in help).

**Issue**  
Flat lists get long. Users lose folder context (“is this under `/app` or `/tmp`?”). Collapse would help group changes if it worked with diff-only.

**Improvement**  
Either enable collapse for directories that still contain diff entries, or group the flat list by parent path / show path prefixes more clearly.

**Impact**  
Diff-only stays useful on large changes without feeling like a endless scroll of paths.

---

## R7. Keybindings and Help — One Source of Truth

**Current**  
README and help overlay document keys like `y` (path), `Y` (content/command), `/` (filter or search). Internal key maps may not match every doc (e.g. alternate bindings for copy).

**Issue**  
Users trust `?` help and README; mismatches cause frustration (“doc says Y, nothing happens”). Small inconsistencies erode trust in a keyboard-driven app.

**Improvement**  
Audit all bindings: README, help overlay, and actual keys must match. Remove or document unused bindings. One clear rule per action.

**Impact**  
Low development cost, high trust. Essential for a vim-style TUI where users rarely click menus.

---

## R8. Layer Origin `(LN)` — Make It Clickable

**Current**  
Files introduced in an earlier layer show `(LN)` in the tree (N = layer number). The file viewer title can show which layer and command introduced the file.

**Issue**  
Information is visible but passive. To inspect that layer, users must remember the number, switch to the layer panel, and scroll to layer N manually.

**Improvement**  
From a file with `(L3)` (or similar), press a key (e.g. `o` for origin) to **jump the layer panel to layer 3** and highlight the command that introduced the file.

**Impact**  
Connects file tree and layer history — core value of a *layer* inspector. Makes “who put this file here?” a one-key answer.

---

## R9. CI Failure Messages — Context, Not Only Numbers

**Current**  
CI prints pass/fail per rule, actual vs threshold, and up to 10 top wasted files with sizes and layer counts.

**Issue**  
Still missing **which Dockerfile step** caused the waste. “`/var/lib/apt/lists` — 4 layers” requires the user to map layers to commands themselves.

**Improvement**  
For each top wasted path, also print: layer index, short layer id, and the **command string** for the layer where the waste matters most.

**Impact**  
CI logs become self-contained for PR review. Reviewers see “layer 7, RUN apt-get install …” without opening the TUI.

---

## R10. CI — Short Remediation Under Each Failed Rule

**Current**  
Failed rules show actual value vs threshold. No suggested next step.

**Issue**  
Same as Actionable Hints (Part A #5) but scoped to CI output only. Pipelines fail; developers stare at numbers.

**Improvement**  
One line under each failed rule, e.g. “Try merging RUN steps that touch the same paths” or “See top wasted files below”.

**Impact**  
Faster fixes in CI without reading full docs. Pairs well with path rules and base-relative efficiency.

---

## R11. CI — Extra Simple Rules (Layer Count / Max Layer Size)

**Current**  
Only efficiency-related rules.

**Issue**  
Some teams have simple policies: “no layer over 500 MB”, “no more than 40 layers”. An efficient image can still violate ops policies.

**Improvement**  
Optional rules: max number of layers, max size of a single layer, max total image size (from inspect).

**Impact**  
Covers policy checks that efficiency does not. Easy to explain to management and security (“we enforce layer limits”).

---

## R12. JSON Export — Stdout and Lighter Modes

**Current**  
`layerx --json <file> <image>` writes a full JSON file and skips the TUI. CI has human text only.

**Issue**  
Scripts must write a temp file to pipe into `jq`. Huge images produce huge JSON if every file is listed — slow and heavy.

**Improvement**  
- Allow JSON to stdout (`--json -` or similar).
- “Slim” export: layers + efficiency + wasted files, **without** every file in every layer.
- `schemaVersion` in export: tracked in [`fixes.md`](fixes.md) #6 (contract fix), not a feature item here.

**Impact**  
Easier integration with jq, GitHub Actions, and internal tools. Slim mode avoids timeouts on massive images.

---

## R13. Efficiency Calculation — Smarter Categories (Longer Term)

**Current**  
Same path in multiple layers counts prior copies as “waste”. Score = 1 − (wasted bytes / total file bytes across layers).

**Issue**  
- Replacing a file (new content, same path) is treated like duplicate copies — slightly different problem.
- Base image paths inflate waste counts.
- Symlink **display** in the tree: [`fixes.md`](fixes.md) #12.
- Symlink-aware **efficiency counting** (longer term, not in fixes.md).

**Improvement**  
- Treat “same path, changed content” separately from “identical path duplicated unchanged”.
- Optional ignore lists for known base paths.
- Later: symlink-aware waste scoring (after #12 display work).

**Impact**  
Scores match intuition better. Fewer arguments about “false” waste. Supports base-relative efficiency and path rules.

---

## R14. Loading Progress — Time and Progress Detail

**Current**  
Pull and export show phases (pulling, exporting, parsing) with spinners and some byte/layer progress during pull.

**Issue**  
On large images, export/parsing dominates and feels like a black box. Users do not know if it hung or is still working.

**Improvement**  
During export/parse: show percent or “layer 12 of 45”, elapsed time, rough ETA if possible.

**Impact**  
Reduces abandonment on first use with big images. Professional feel for enterprise-sized images.

---

## R15. Parallel Layer Parsing (Performance)

**Current**  
After Docker returns the image tar, layers are parsed sequentially.

**Issue**  
50+ layer images spend noticeable time in CPU-bound parsing. Same user-visible wait as slow network.

**Improvement**  
Parse independent layer archives in parallel (with safe limits on memory).

**Impact**  
Faster time-to-TUI on fat images. Pairs well with analysis cache (first run slow, later instant).

---

## R16. Documentation — Dive Comparison and CI Recipes

**Current**  
README is strong on features and install. CHANGELOG once mentioned a “dive comparison” section that is not in the current README. No copy-paste CI examples in-repo.

**Issue**  
New users choosing between tools lack a honest comparison. Teams adopting CI mode must invent workflow from `--help` alone.

**Improvement**  
- Short table: layerx vs dive (viewer, CI, JSON, whiteouts, etc.).
- “Common workflows”: optimize Dockerfile, PR check, debug “why is my image 2GB”.
- Example GitHub Actions / GitLab job snippet (even before official Action).

**Impact**  
Better adoption and fewer support questions. Not a code feature but high leverage for planning releases.

---

# Suggested Planning Order (Simple View)

If you want a practical sequence:

| Phase | Focus | Why |
|-------|--------|-----|
| **Already planned (v1.1)** | Compare two images, CI JSON, no-pull | Closes the biggest workflow gap (before/after builds) |
| **Next refinement batch** | Layer delta sizes, CI context lines, keybinding audit; bugs via [`fixes.md`](fixes.md) | Mostly UI/words on existing data — fast wins |
| **Next feature batch** | Directory rollup, CI path rules, base-relative efficiency | Makes CI and tree actually fair for real apps |
| **Polish batch** | Analysis cache, `layerx layers`, hints, filter/viewer/extract refinements | Speed and daily comfort |
| **Later** | Smarter efficiency math, parallel parse, Podman, MCP | Deeper or broader scope |

---

# One-Line Summary Table

| Item | In one sentence |
|------|------------------|
| ~~Waste navigator~~ | **Shipped** (`w` overlay); CLI `layerx wasted` still optional. |
| Directory rollup | Show total size per folder, not only per file. |
| CI path rules | Fail CI on specific paths/patterns, not only global score. |
| Base-relative efficiency | Score only *your* layers, not the whole base image. |
| Actionable hints | One-line Dockerfile-style fixes after failures. |
| `layerx layers` | Quick layer table without TUI or huge JSON. |
| Analysis cache | Remember parsed images so reopening is fast. |
| Layer delta sizes | See which RUN step added how much size. |
| Filter/viewer/extract polish | Easier search, view, and save files. |
| CI richer failures | Tie wasted paths to layer number and command. |
| JSON stdout + slim | Pipe to tools; avoid giant files. |

---

# Previously Recommended for v1.1 (from earlier analysis)

| Priority | Item | Effort |
|----------|------|--------|
| P0 | `layerx compare <old> <new>` + JSON + `--fail-on-regression` | Medium |
| P1 | `layerx ci --format json` | Small |
| P1 | `--no-pull` on all commands | Small |
| P2 | README + CI example snippet | Small |

---

# Related documentation

| File | Purpose |
|------|---------|
| [`Maintainer_Guide.md`](Maintainer_Guide.md) | Releases, SemVer, package boundaries, testing, PR workflow, AI agent rules |
| [`fixes.md`](fixes.md) | **Bugs & improvements** — one issue per prompt; do not duplicate in cursor.md |
| [`PROMPT_PLAYBOOK.md`](PROMPT_PLAYBOOK.md) | **One copy-paste prompt per session** — use this to avoid scope creep |
| [`layerx/README.md`](layerx/README.md) | End-user install and usage |
| [`layerx/CHANGELOG.md`](layerx/CHANGELOG.md) | Shipped history (migrate new entries to SemVer) |

---

# Notes for Claude Code Opus

1. **Two-file system:** `cursor.md` = product backlog; `Maintainer_Guide.md` = engineering playbook. Do not mix release process into feature specs.  
2. **Do not implement everything in this file at once** — follow the planning order table above.  
3. **v1.1 scope is fixed** unless the maintainer says otherwise: compare, CI JSON, no-pull. Other items are v1.1.x / v1.2+.  
4. **Efficiency data already exists** — CI hints and optional CLI waste are mostly UX on `image.Efficiency()`; TUI waste overlay is shipped.  
5. **JSON is a public contract** — implement missing `schemaVersion` via [`fixes.md`](fixes.md) #6; bump version when changing export shape.  
6. **`tui/model.go` is ~1200 lines** — split before large TUI features (see Maintainer Guide §8.2).  
7. **No commits/tags/push** unless the maintainer explicitly requests git operations.  
8. **Manual verification:** anything touching `image/docker.go` or tar parsing should be smoke-tested with a real image when Docker is available.
