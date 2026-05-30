# layerx — Prompt Playbook (one task per session)

**Purpose:** Give Claude Code / Cursor **exactly one job per chat**. Do not paste `cursor.md` + `Maintainer_Guide.md` + this file together unless the task is “read and summarize only.”

**Always-on (automatic):** [`layerx/CLAUDE.md`](layerx/CLAUDE.md) — short invariant rules only.  
**Deep reference (read on demand):** [`Maintainer_Guide.md`](Maintainer_Guide.md), [`cursor.md`](cursor.md).  
**Bugs / perf fixes:** [`fixes.md`](fixes.md) — use its prompts for items listed there; do not duplicate as P0/P2 entries below.

---

## How to use this framework

| Step | What you do |
|------|-------------|
| 1 | Open a **new** chat for each row below (or each feature from `cursor.md`). |
| 2 | Copy **only** that row’s prompt block (between the `---` lines). |
| 3 | Wait until done; review diff; merge PR. |
| 4 | Next chat → next prompt. |

**Rule:** If Claude starts implementing a *different* item than the prompt names, reply: `Stop. Only do prompt <ID>. Re-read the prompt scope.`

---

## Phase 0 — Process only (no product code)

### P0-A — SemVer changelog migration

```
You are working on github.com/deveshpharswan/layerx (folder: layerx/).

TASK (only this): Migrate CHANGELOG.md to SemVer headings for future releases.
- Add a short "Versioning" section to layerx/README.md (PATCH/MINOR/MAJOR table).
- Keep existing [M##] sections as historical entries; add a note that new entries use [X.Y.Z].
- Add an empty template section [Unreleased] at the top.
- Do NOT change Go code. Do NOT implement features from cursor.md.

Read: Maintainer_Guide.md §6.1 only.
When done: list files changed and suggest the git commit message.
```

---

### P0-B — Pre-release checklist doc

```
TASK (only this): Add layerx/docs/RELEASE_CHECKLIST.md with the pre-release checklist from Maintainer_Guide.md §6.4.
Link it from README.md under a small "For maintainers" subsection.
Do NOT change application code.
```

---

### ~~P0-C — JSON schemaVersion~~ — **moved to [`fixes.md`](fixes.md) #6**

Use the copy-paste prompt in fixes.md (or REL-01 in `docs/RELEASE_STRATEGY_AND_CLAUDE_PROMPTS.md`, which points to the same fix).

---

## Phase 1 — v1.1 product (one vertical slice per prompt)

### P1-A — image/compare domain

```
TASK (only this): Implement image/compare.go + compare_test.go.
- Function CompareAnalysis(old, new *Analysis) with layer delta, filesystem delta, efficiency delta.
- Pure domain logic in image/ only. No new CLI. No TUI.
Read: Maintainer_Guide.md §8; cursor.md v1.1 compare concept.
Run: go test ./image/...
```

---

### P1-B — layerx compare CLI (text only)

```
TASK (only this): Add cmd/compare.go and register `layerx compare <old> <new>`.
- Human-readable text report to stdout.
- Wire image/compare.go. Tests for cmd where practical.
- No JSON flag yet. No TUI. No --fail-on-regression yet.
Run: go test ./...
```

---

### P1-C — compare JSON + fail-on-regression

```
TASK (only this): Extend compare command only.
- --format json (stdout)
- --fail-on-regression (exit 1 if new image is worse)
- Tests + update CHANGELOG [Unreleased].
Do NOT touch TUI or ci subcommand.
```

---

### P1-D — ci --format json

```
TASK (only this): Add --format json to `layerx ci` (stdout).
- Human format remains default.
- Tests in ci/ or cmd/.
- Document in README and ci --help.
Do NOT implement compare or --no-pull in this session.
```

---

### P1-E — --no-pull flag

```
TASK (only this): Add global or per-command --no-pull.
- Plumb through DockerResolver (skip pull if image missing; return ErrImageNotFound).
- Apply to root, ci, compare. Tests with mock client.
Do NOT add other features.
```

---

### P1-F — v1.1 release prep

```
TASK (only this): Prepare v1.1.0 release docs.
- Fill CHANGELOG [1.1.0] from [Unreleased].
- Update README curl/install examples from v1.0.0 to v1.1.0.
- Run go test ./...
- Output RELEASE_CHECKLIST completed items; do NOT git tag unless I ask.
```

---

## Phase 2 — Refinements (pick one per session from cursor.md)

Copy this template and fill in the bracketed parts:

```
TASK (only this): Implement [FEATURE NAME] from cursor.md [section number].

Scope:
- [1-2 sentences max]
Out of scope:
- No other cursor.md items
- No model.go split unless this task explicitly includes it

Rules: Read layerx/CLAUDE.md + Maintainer_Guide.md §8 and §13.
Run: go test ./... from layerx/
```

**Examples to paste:**

| ID | Fill in |
|----|---------|
| ~~P2-A~~ | ~~Waste navigator~~ — **shipped** (`w` overlay); optional: `layerx wasted` CLI |
| P2-B | Layer panel — show size delta per layer |
| P2-C | CI failure — print layer index + command for top wasted files |
| P2-D | Keybinding audit — README, help overlay, keymap.go match |
| ~~P2-E~~ | Streaming `parseLayers` — **moved to [`fixes.md`](fixes.md) #8** (full prompt there) |

---

## Phase 3 — Structure (when model.go grows)

### P3-A — Split tui/model.go

```
TASK (only this): Split tui/model.go into model.go, model_load.go, model_filter.go, model_viewer.go, model_render.go.
- Zero behavior change. All existing tests must pass unchanged.
- No new features.
Run: go test ./tui/...
```

---

## Anti-patterns (do not paste)

- “Implement everything in cursor.md”
- “Read Maintainer_Guide and cursor.md and refactor the whole project”
- “Add compare, cache, waste navigator, and CI path rules”

---

## Session starter (optional, prepend to any prompt)

```
Before coding: confirm you understand the ONE task ID and list files you will touch.
If the task is unclear, ask one question, then wait.
```

---

*Add new prompts to this file as one block per PR. Link from Maintainer_Guide.md §17.*
