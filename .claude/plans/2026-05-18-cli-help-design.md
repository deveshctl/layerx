# CLI Help Improvements — Design

**Date:** 2026-05-18
**Status:** Draft, awaiting user review

## Goal

Make `layerx --help` and every subcommand's help useful on its own, in the Linux convention: a clear one-paragraph description plus realistic examples. Also surface version info via a `version` subcommand (in addition to the existing `--version` flag), since users expect it.

This is a docs/UX-only change. No flags added, no behavior changed, no public Go API touched.

## Scope

In scope:
- Tighten and expand `Long` descriptions on `root`, `ci`, `completion`.
- Add cobra `Example` blocks on `root`, `ci`, `completion`, `version`.
- Add a `version` subcommand that mirrors `--version` output (`layerx version 1.0.0`).
- Polish flag descriptions where they are vague (units, sentinel values).

Out of scope:
- New flags or features.
- `man` page generation.
- The TUI in-app `?` help screen (separate concern).
- Reworking how `--json` is exposed (currently a flag on root, not a subcommand — leave as is).

## Per-command design

### Root: `layerx`

Current `Long` is one bland sentence. Replace with three sentences covering: what it does, what it requires (Docker daemon), how default vs flagged invocations differ.

**`Long`:**
```
A terminal-based Docker image layer inspector.

Launches an interactive TUI by default to browse layers, explore filesystem
changes, view file contents, and surface wasted bytes. Requires a running
Docker daemon. Use --json to skip the TUI and export analysis to a file,
or run "layerx ci" for non-interactive CI checks.
```

**`Example`:**
```
  # Inspect an image interactively
  layerx nginx:latest

  # Export analysis as JSON (skips the TUI)
  layerx --json out.json nginx:latest

  # Run efficiency checks (also triggered by CI=true)
  layerx ci nginx:latest
```

**Flag polish (`--json`):**
Current: `export analysis to JSON file (skip TUI)`
Proposed: `write analysis to PATH as JSON and exit (skips TUI)` — names the argument and clarifies side effect.

### Subcommand: `layerx ci`

Existing `Long` is good (already shows the YAML config snippet). Just append an `Example` block.

**`Example`:**
```
  # Run with default thresholds (lowest-efficiency: 0.9)
  layerx ci nginx:latest

  # Override a single threshold
  layerx ci --lowest-efficiency 0.95 nginx:latest

  # Combine multiple rules
  layerx ci \
    --lowest-efficiency 0.9 \
    --highest-wasted-bytes 10485760 \
    nginx:latest

  # Use a .layerx.yaml file in the current directory (no flags needed)
  layerx ci nginx:latest
```

**Flag polish:**
- `--lowest-efficiency`: `minimum efficiency score (0.0-1.0)` → `minimum acceptable efficiency score, 0.0–1.0 (default 0.9)`
- `--highest-wasted-bytes`: `maximum allowed wasted bytes` → `maximum allowed wasted bytes (0 disables the rule)`
- `--highest-user-wasted-percent`: same shape — note `0.0–1.0` and the disabled-when-zero behavior.

### Subcommand: `layerx completion`

Current `Long` is example-heavy. Move the install snippets into `Example` (where cobra renders them under "Examples:") and tighten `Long` to one sentence.

**`Long`:**
```
Generate an autocompletion script for the specified shell.

The script enables tab completion for subcommands, flags, and image
references (read from "docker images") in your current shell session.
```

**`Example`:**
```
  # Bash (current session)
  source <(layerx completion bash)

  # Bash (system-wide install)
  layerx completion bash | sudo tee /etc/bash_completion.d/layerx

  # Zsh
  layerx completion zsh > "${fpath[1]}/_layerx"

  # Fish
  layerx completion fish | source
  layerx completion fish > ~/.config/fish/completions/layerx.fish

  # PowerShell
  layerx completion powershell | Out-String | Invoke-Expression
```

### New subcommand: `layerx version`

Minimal addition. Mirrors cobra's default `--version` output exactly so there is no behavioral surprise.

**Definition (new file `cmd/version.go`):**
```go
var versionCmd = &cobra.Command{
    Use:     "version",
    Short:   "Print the layerx version",
    Long:    "Print the layerx version. Identical output to --version.",
    Example: "  layerx version",
    Args:    cobra.NoArgs,
    Run: func(cmd *cobra.Command, _ []string) {
        fmt.Fprintf(cmd.OutOrStdout(), "%s version %s\n", rootCmd.Name(), rootCmd.Version)
    },
}

func init() { rootCmd.AddCommand(versionCmd) }
```

Output (matches `layerx --version`):
```
layerx version 1.0.0
```

In `dev` builds where `version = "dev"`:
```
layerx version dev
```

Note: `commit` and `date` from `main.go` remain wired through `SetVersionInfo` but unused for now. Leaving the signature alone preserves the option to surface them later without an API break in `main.go`.

## File-level changes

| File | Change |
|---|---|
| `cmd/root.go` | Expand `Long`; add `Example`; tighten `--json` description. |
| `cmd/ci.go` | Add `Example`; tighten three flag descriptions. |
| `cmd/completion.go` | Tighten `Long`; move install snippets into `Example`. |
| `cmd/version.go` | **New.** Defines `versionCmd` and registers it in `init()`. |

No other files touched. No tests added (cobra string outputs aren't worth pinning; CI gate is `go build` + `go vet`).

## Verification plan

Per CLAUDE.md, only `go build ./...` and `go vet ./...` run on the dev machine. Manual help inspection happens on the second machine via Gate C:

```
[ ] go build ./...   passes
[ ] go vet ./...     passes
[ ] layerx --help              shows new Long + Examples
[ ] layerx ci --help           shows YAML config + Examples
[ ] layerx completion --help   shows tightened Long + Examples
[ ] layerx version             prints "layerx version 1.0.0"
[ ] layerx --version           prints same output as `layerx version`
[ ] layerx help version        works (cobra auto-routes)
[ ] No regressions: layerx nginx:latest still launches TUI
```

## Risks

- **Cobra `Example` indentation.** Cobra renders `Example` verbatim under "Examples:". Each example line should start with two spaces for consistency with cobra conventions and the existing `completion.go` style.
- **`Long` width.** Cobra wraps at terminal width. Hard-wrapping at ~78 chars in source keeps the help readable on narrow terminals and in `man`-style pagers.
- **None for behavior.** No code paths change; no flag semantics change; no new dependencies.

## Out-of-scope follow-ups

- If we ever want `version` to surface commit and build date (the plumbed-but-unused `commit`/`date` vars), that becomes a separate small task — likely a `--verbose` flag on `version` rather than changing the default output.
- Generating shell-specific `man` pages from cobra (`cobra.GenManTree`) could be a later distribution polish.
