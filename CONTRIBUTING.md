# Contributing to layerx

Thanks for your interest. Bug reports, fixes, and small features are welcome.

For anything beyond a small fix, **open an issue first** to discuss the approach — it's faster than guessing whether a PR will land.

## Prerequisites

- Go 1.26+ (`go version`).
- Docker Engine running locally if you want to test against live images. Tests don't require it.

## Build & test

```bash
go build ./...
go vet ./...
go test ./...
```

All three must pass before a PR is mergeable. CI runs the same on `ubuntu-latest`.

## Branch naming

```
feat/<name>     new features
fix/<name>      bug fixes
chore/<name>    tooling, docs, refactors
```

Don't push to `main`; PRs are merged on GitHub.

## Commits

- One logical change per commit.
- The commit message explains *why*, not just *what*. Minimum 20 characters.
- If your `.go` change fixes a bug or adds user-visible behaviour, update `CHANGELOG.md` (`[Unreleased]` section) in the same commit. Pure internal refactors can skip it.

## What goes where

```
cmd/      Cobra CLI — wires packages together
image/    Domain layer — Docker SDK, tar parsing, file tree, cache
tui/      Bubbletea TUI
ci/       CI evaluator
config/   .layerx.yaml loader
```

Hard rules:

- `image/` does not import `tui/`, `ci/`, `config/`, or `mcp/`.
- `tui/` and `ci/` import `image/` interfaces, never concrete Docker SDK types.
- `cmd/` is the only package allowed to wire the rest together.

If a change forces you to break one of these, the change is in the wrong layer.

## Tests

- New code in `image/` and `ci/` requires tests. Mock via interfaces — never the Docker SDK directly.
- `tui/` is tested pragmatically: cover non-trivial logic (state transitions, key handling). Skip pure rendering.
- All tests must run without Docker. Use a test double if you'd otherwise need it.

## Dependencies

Don't add a dependency without asking first. Open an issue and explain why.

## Releasing

Maintainer-only. See `docs/releasing.md` for the full sequence (CHANGELOG + README + tag + GoReleaser + Homebrew tap + Scoop bucket).

## Reporting security issues

See [SECURITY.md](SECURITY.md). Don't open public issues for vulnerabilities.
