# layerx — Maintainer Guide

**Audience:** Human maintainers and AI coding agents (e.g. Claude Code Opus, Cursor).  
**Codebase location:** [`layerx/`](layerx/) (Go module `github.com/deveshpharswan/layerx`).  
**Companion docs:** [`cursor.md`](cursor.md) — product roadmap (features & refinements); [`fixes.md`](fixes.md) — bugs and improvements (canonical prompts).

Read this file before making structural, release, or cross-package changes.

---

## 1. Project snapshot

| Item | Value |
|------|--------|
| **What it is** | Terminal Docker image layer inspector (TUI + CI + JSON) |
| **Runtime deps** | Docker daemon only |
| **Language** | Go 1.26.2+ |
| **Current release** | v1.0.0 (tag exists)
| **License** | MIT |

**Commands today:**

```bash
layerx <image>                    # Interactive TUI
layerx ci <image>                 # CI efficiency checks (exit 0/1)
layerx --json <path> <image>      # Full analysis JSON export
layerx completion [shell]         # Shell completions
CI=true layerx <image>            # CI mode via env
```

---

## 2. Repository layout

```
layerx/
  main.go              # Entry; version ldflags
  cmd/                 # Cobra CLI — wires packages, flags, exit codes
  image/               # DOMAIN KERNEL — Docker, tar, stack, efficiency (no imports from tui/ci/config)
  tui/                 # Bubbletea v2 UI — consumes image/ only
  ci/                  # CI rules & report — consumes image/ only
  config/              # .layerx.yaml loader
  README.md            # User-facing docs
  CHANGELOG.md         # History (migrating to SemVer headings)
  .github/workflows/   # ci.yml, release.yml
  .goreleaser.yaml     # Multi-platform release
```

**Dependency rule (non-negotiable):**

```
main → cmd → { image, tui, ci, config }
              tui  → image
              ci   → image
              config → (yaml only)
              image → (stdlib + moby client only)
```

Never import `tui/`, `ci/`, or `config/` from `image/`.

---

## 3. Key files map (for agents)

| Task | Start here |
|------|------------|
| Add CLI command | `cmd/*.go`, register in `cmd/root.go` |
| Docker / layers / tar | `image/docker.go`, `image/tree_parser.go` |
| Filesystem stacking / whiteouts | `image/stack.go` |
| Efficiency / waste | `image/efficiency.go` |
| Full analysis pipeline | `image/analysis.go` |
| File read from image | `image/extractor.go` |
| TUI state / keys | `tui/model.go` (~1200 lines — split before adding more) |
| TUI file tree display | `tui/filetree.go` |
| TUI file viewer | `tui/fileview.go` |
| CI rules | `ci/rules.go`, `ci/evaluator.go` |
| User config | `config/config.go`, `.layerx.yaml` |
| JSON export schema | `cmd/json.go` |
| Known bugs / fix prompts | `fixes.md` (repo root) |
| Errors | `image/errors.go` — extend typed errors here |

**Largest files (split carefully):** `tui/model.go`, `tui/filetree.go`, `image/docker.go`.

---

## 4. How to build and test

```bash
cd layerx
go build ./...
go vet ./...
go test -v -count=1 ./...
```

**Manual smoke (requires Docker running):**

```bash
layerx nginx:latest
layerx ci nginx:latest
layerx --json /tmp/out.json nginx:latest
```

**CI today:** `go test ./...` only — no Docker in GitHub Actions. Unit tests use mocks in `image/*_test.go` and fake analysis in `tui/model_test.go`.

---

## 5. What we do well (preserve this)

1. **Clean kernel** — `image/` is pure domain logic, well unit-tested.
2. **Milestone discipline** — M01–M16 delivered a real v1.0.
3. **Interfaces where it matters** — `Resolver`, `Extractor`; mock in tests.
4. **Release automation** — GoReleaser: linux/darwin/windows × amd64/arm64, deb/rpm, Homebrew, Scoop.
5. **Structured errors** — `ErrDaemonNotRunning`, `ErrPullFailed`, `ErrImageNotFound`.

---

## 6. Release strategy

### 6.1 SemVer policy

| Change | Bump | Example |
|--------|------|---------|
| Bug fix, same CLI/JSON | **PATCH** | 1.1.0 → 1.1.1 |
| New command or flag, backward-compatible JSON | **MINOR** | 1.1.0 → 1.2.0 |
| Breaking JSON fields, removed flags, renamed config keys | **MAJOR** | 1.x → 2.0 |

Write user-facing notes in `CHANGELOG.md` as `[1.1.0] - YYYY-MM-DD`, not only `[M17]`.

### 6.2 Release cadence

- Prefer **small, frequent** releases over waiting for a large batch.
- **v1.1** target: compare + `ci --format json` + `--no-pull` (see `cursor.md`).
- **PATCH** anytime: crash fix, wrong layer size, broken CI on common image.

### 6.3 Tag and release flow

1. Merge PRs to `main` (CI green on `feat/**` PRs too).
2. Update `CHANGELOG.md` under SemVer heading.
3. Update README install URLs if version strings are hardcoded (currently `v1.0.0` in curl examples).
4. Tag: `git tag v1.1.0 && git push origin v1.1.0`
5. GitHub Actions `release.yml` runs GoReleaser on `v*` tags.
6. Requires `TAP_GITHUB_TOKEN` for Homebrew/Scoop buckets (`homebrew-tap`, `scoop-bucket`).

### 6.4 Pre-release checklist

- [ ] `go test ./...` passes
- [ ] `CHANGELOG.md` updated
- [ ] README version strings updated if applicable
- [ ] Smoke on one real image (`nginx:latest`)
- [ ] JSON export: if schema changed, bump `schemaVersion` and note in changelog

---

## 7. How to land changes incrementally

### 7.1 One PR = one user-visible outcome

PR title should imply: *“After this, users can ___.”*

**Good:** “Add `layerx compare` text output and tests”  
**Avoid:** compare + cache + keybinding audit in one PR

### 7.2 Vertical slices over horizontal layers

Ship end-to-end thin slices:

1. `layerx compare a b` text report + `image/compare.go` tests  
2. JSON output + `--fail-on-regression`  
3. TUI integration (if ever)

Do not merge half-finished commands without happy-path tests.

### 7.3 Branching

- Short-lived `feat/<name>` → PR → `main`
- CI runs on `push` to `main` and `feat/**`, and on PRs to `main`
- No long-lived integration branches unless multiple contributors conflict

---

## 8. Code structure and scalability

### 8.1 Where new logic goes

| Feature type | Package |
|--------------|---------|
| Parse, stack, efficiency, compare, cache keys | `image/` |
| Flags, exit codes, stdout/stderr | `cmd/` |
| Rules, report text/JSON formatting | `ci/` |
| Thresholds, yaml keys | `config/` |
| Rendering, keys, panels | `tui/` |

### 8.2 When to split `tui/model.go`

Split before adding 2+ major TUI features. Suggested files:

- `model.go` — state, `Update`, lifecycle  
- `model_load.go` — fetch, progress, errors  
- `model_filter.go` — filter / diff / sort  
- `model_viewer.go` — viewer + search  
- `model_render.go` — `View()`  

### 8.3 Interfaces

Add a new interface only when there is a **second implementation** (e.g. Podman) or a hard testing need. Prefer pure functions in `image/` (e.g. `CompareAnalysis(a, b *Analysis)`).

### 8.4 Stable contracts (treat as public API)

- JSON export shape (`cmd/json.go`) — `schemaVersion` implementation tracked in [`fixes.md`](fixes.md) #6; bump when changing export  
- `layerx ci` exit codes: 0 pass, 1 fail  
- `.layerx.yaml` rule names  
- Config precedence: **defaults → `.layerx.yaml` → CLI flags** (never change order without MAJOR bump)

### 8.5 Errors

Define in `image/errors.go`. Map to messages in `cmd/` and friendly screens in `tui/`. Do not create ad-hoc Docker errors in TUI.

---

## 9. Testing strategy

| Layer | Location | Notes |
|-------|----------|-------|
| Unit (primary) | `image/*_test.go`, `ci/*_test.go`, `config/*_test.go` | Use tar fixtures, mocks |
| TUI behavior | `tui/model_test.go` | Inject `testAnalysis*` — no Docker |
| cmd | `cmd/json_test.go` only today | Add tests for new commands |
| Integration | Not in CI yet | Optional `integration.yml` + docker:dind on `main` |

**Practices:**

- Golden JSON files in `testdata/` for compare/CI JSON when added  
- Run `go test -count=1 ./...` to avoid cache hiding flakes  
- Consider coverage profile before large releases  

**CI gap to fix eventually:** cross-compile matrix is amd64-only; GoReleaser also builds arm64 — add arm64 to `.github/workflows/ci.yml`.

---

## 10. CI/CD reference

| Workflow | Trigger | Does |
|----------|---------|------|
| `ci.yml` | push `main`, `feat/**`; PRs to `main` | build, vet, test, cross-compile (linux/darwin/windows amd64) |
| `release.yml` | push tag `v*` | GoReleaser binaries, deb/rpm, brew, scoop |

**Dependabot:** recommended for Go modules and GitHub Actions.

---

## 11. Documentation layout (target)

```
layerx/
  README.md           # install, usage, keybindings
  CHANGELOG.md        # SemVer user-facing
  docs/               # optional future
    CI.md             # GitHub Actions example
    ARCHITECTURE.md   # package diagram
```

Planning docs at repo root (`cursor.md`, `Maintainer_Guide.md`, `fixes.md`) are for maintainer/agent use; consider syncing summaries into `docs/` for GitHub visibility.

---

## 12. Dependencies

- **Go:** match `go.mod` exactly in CI (`1.26.2`).  
- **CGO_ENABLED=0** in releases — keep portable static binaries.  
- **Bubbletea/Lipgloss:** upgrade on a schedule; always run full tests + manual TUI smoke.  
- **moby client:** use `client.WithAPIVersionNegotiation()` — already required.

---

## 13. Rules for AI agents (Claude Code, Cursor, etc.)

When implementing work from `cursor.md`:

1. **Read** `Maintainer_Guide.md` (this file) and the relevant package before editing.  
2. **Respect** `image/` import boundaries — no exceptions.  
3. **Prefer** minimal diffs; match existing naming and patterns.  
4. **Add tests** in `image/` or `ci/` for domain logic; `tui/model_test.go` for UI behavior.  
5. **Do not** commit secrets, `.env`, or local `.layerx.yaml`.  
6. **Do not** change JSON/config semantics without updating `schemaVersion` or SemVer plan.  
7. **Do not** grow `model.go` without a plan to split if adding >200 lines.  
8. **One feature per PR** unless maintainer explicitly asks for a bundle.  
9. **Do not** force-push `main` or tag releases unless asked.  
10. **After implementation:** run `go test ./...`; mention manual Docker smoke if behavior touches resolver/parser.

**Priority order for product work:** see `cursor.md` → v1.1 block first, then refinement batch, then feature batch.

---

## 14. Suggested roadmap (process, not features)

| When | Action |
|------|--------|
| **v1.1** | SemVer changelog; compare + ci json + no-pull; README version bumps |
| **v1.1.x** | Patches only; small refinement PRs |
| **v1.2** | Split `model.go` if needed; `docs/CI.md`; optional Docker integration CI job |
| **Before v2.0** | Document breaking JSON/YAML changes; MAJOR bump only |

Full feature list: [`cursor.md`](cursor.md).

---

## 15. Maintainer secrets and infra

Before first release (or if taps are empty):

1. Public repo: `github.com/deveshpharswan/homebrew-tap`  
2. Public repo: `github.com/deveshpharswan/scoop-bucket`  
3. GitHub secret: `TAP_GITHUB_TOKEN` (PAT with `repo` scope)

---

## 16. Milestone history (context for agents)

Development used internal milestones M01–M16 (complete). M15 (MCP server) deferred.  
CHANGELOG still contains `[M##]` headings — new entries should use SemVer.

| Milestone | Topic |
|-----------|--------|
| M01–M05 | Docker plumbing, TUI, file tree, stacking |
| M06–M07 | Filter, diff-only, sort |
| M08 | File viewer |
| M09–M10 | Efficiency, extract to disk |
| M11–M12 | CI mode, `.layerx.yaml` |
| M13–M14 | Completion, JSON export |
| M16 | Clipboard, viewer search, layer origin `(LN)` |

---

---

## 17. Working with Claude Code (one task at a time)

**Problem:** Pasting `cursor.md` + this guide + release notes in one prompt causes scope creep.

**Solution:** Use three layers:

| Layer | File | When it loads |
|-------|------|----------------|
| Always-on (short) | `layerx/CLAUDE.md` | Every Claude Code session in `layerx/` |
| One task per chat | `PROMPT_PLAYBOOK.md` | You paste **one** prompt block per session |
| Reference | `Maintainer_Guide.md`, `cursor.md`, `fixes.md` | Only when the playbook prompt says “Read §…” or fix ID |

**Workflow:**

1. New chat → copy one block from `PROMPT_PLAYBOOK.md` (e.g. `P1-A`).
2. Let Claude finish → you review → merge PR.
3. Next chat → next block (`P1-B`). Never combine P1-A + P1-B in one prompt.

If Claude drifts: reply `Stop. Only do prompt P1-A. Re-read the scope.`

### Cursor vs Claude Code hooks

| Tool | Config | Purpose here |
|------|--------|----------------|
| **Cursor** | `.cursor/hooks.json` + `.cursor/hooks/*.sh` | Optional: run `go test` after Go edits; ask before `git commit/push/tag` |
| **Claude Code** | `~/.claude/settings.json` (permissions) + project `CLAUDE.md` | No project hooks required; use playbook prompts |

Claude Code does **not** use Cursor’s `settings.json`. For Claude:

- Put invariant rules in `layerx/CLAUDE.md` (already done).
- Do **not** put the full Maintainer Guide in `CLAUDE.md`.
- Use `PROMPT_PLAYBOOK.md` for each session’s task.
- In `~/.claude/settings.json`, only set permissions (e.g. allow `go test`, ask on `git push`) if you want — optional.

Restart Cursor after adding hooks; confirm in **Settings → Hooks**.

---

*Last updated: 2026-05-23. Update this file when release process or architecture rules change.*
