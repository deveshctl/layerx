# CLI Help Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every layerx CLI command (root, ci, completion) emit Linux-style help — concise `Long`, realistic `Example` block, polished flag descriptions — and add a `version` subcommand that mirrors `--version`.

**Architecture:** Edits to existing `cmd/*.go` files plus one new `cmd/version.go`. No behavior changes, no new flags, no new dependencies. Cobra renders `Long` and `Example` automatically; the `version` subcommand reuses `rootCmd.Version` so it can never drift from `--version`.

**Tech Stack:** Go 1.24+, `github.com/spf13/cobra` v1.10.x. Local verification: `go build ./...` and `go vet ./...` only (per CLAUDE.md — no `go test` on this dev machine; tests run on GitHub Actions CI).

**Spec:** `2026-05-18-cli-help-design.md` (repo root)

---

## Pre-flight

- [ ] **Step 0: Confirm clean working tree and current branch**

Run: `git status`
Expected: working tree clean, on `main`.

If not on a feature branch, create one:

Run: `git checkout -b feat/cli-help-polish`
Expected: `Switched to a new branch 'feat/cli-help-polish'`

---

### Task 1: Tighten root command Long description, add Example, polish --json flag

**Files:**
- Modify: `cmd/root.go`

- [ ] **Step 1: Open and review current state**

Read `cmd/root.go`. The current `rootCmd` block is at lines 15-21. The `--json` flag is registered at line 24.

- [ ] **Step 2: Replace the rootCmd struct literal**

Replace the existing `rootCmd` declaration (lines 15-21) with:

```go
var rootCmd = &cobra.Command{
	Use:   "layerx [image]",
	Short: "Inspect Docker image layers",
	Long: `A terminal-based Docker image layer inspector.

Launches an interactive TUI by default to browse layers, explore filesystem
changes, view file contents, and surface wasted bytes. Requires a running
Docker daemon. Use --json to skip the TUI and export analysis to a file,
or run "layerx ci" for non-interactive CI checks.`,
	Example: `  # Inspect an image interactively
  layerx nginx:latest

  # Export analysis as JSON (skips the TUI)
  layerx --json out.json nginx:latest

  # Run efficiency checks (also triggered by CI=true)
  layerx ci nginx:latest`,
	Args: cobra.ExactArgs(1),
	RunE: runInspect,
}
```

- [ ] **Step 3: Update the --json flag description**

Replace line 24:

```go
	rootCmd.Flags().StringVar(&flagJSON, "json", "", "export analysis to JSON file (skip TUI)")
```

with:

```go
	rootCmd.Flags().StringVar(&flagJSON, "json", "", "write analysis to PATH as JSON and exit (skips TUI)")
```

- [ ] **Step 4: Verify build and vet**

Run: `go build ./...`
Expected: no output (success).

Run: `go vet ./...`
Expected: no output (success).

- [ ] **Step 5: Commit**

```bash
git add cmd/root.go
git commit -m "docs(cli): expand root help with Long, Example, clearer --json description"
```

---

### Task 2: Add Example block and tighten flag descriptions on `ci` subcommand

**Files:**
- Modify: `cmd/ci.go`

- [ ] **Step 1: Open and review current state**

Read `cmd/ci.go`. The `ciCmd` struct literal is at lines 20-37. Flag registrations are at lines 40-42.

- [ ] **Step 2: Add the Example field to ciCmd**

In the `ciCmd` struct literal, add an `Example` field immediately after the `Long` field (between the closing backtick of `Long` on line 34 and the `Args:` line). The struct should look like:

```go
var ciCmd = &cobra.Command{
	Use:   "ci [image]",
	Short: "Run efficiency checks for CI pipelines",
	Long: `Evaluates image efficiency against configurable thresholds.
Exits 0 on pass, 1 on failure.

Thresholds can be set via flags or a .layerx.yaml file in the working directory:

  rules:
    lowest-efficiency: 0.9           # minimum efficiency score (0.0-1.0)
    highest-wasted-bytes: 0          # max wasted bytes (0 = disabled)
    highest-user-wasted-percent: 0.1 # max waste as fraction of total (0.0-1.0)

Flags take precedence over config file values.
Missing config file is silently ignored (defaults apply).`,
	Example: `  # Run with default thresholds (lowest-efficiency: 0.9)
  layerx ci nginx:latest

  # Override a single threshold
  layerx ci --lowest-efficiency 0.95 nginx:latest

  # Combine multiple rules
  layerx ci \
    --lowest-efficiency 0.9 \
    --highest-wasted-bytes 10485760 \
    nginx:latest

  # Use a .layerx.yaml file in the current directory (no flags needed)
  layerx ci nginx:latest`,
	Args: cobra.ExactArgs(1),
	RunE: runCICmd,
}
```

- [ ] **Step 3: Tighten the three flag descriptions**

Replace the three flag registration lines (currently lines 40-42) with:

```go
	ciCmd.Flags().Float64Var(&flagLowestEfficiency, "lowest-efficiency", -1, "minimum acceptable efficiency score, 0.0-1.0 (default 0.9)")
	ciCmd.Flags().Int64Var(&flagHighestWastedBytes, "highest-wasted-bytes", -1, "maximum allowed wasted bytes (0 disables the rule)")
	ciCmd.Flags().Float64Var(&flagHighestUserWastedPercent, "highest-user-wasted-percent", -1, "maximum wasted bytes as fraction of total size, 0.0-1.0 (0 disables the rule)")
```

- [ ] **Step 4: Verify build and vet**

Run: `go build ./...`
Expected: no output (success).

Run: `go vet ./...`
Expected: no output (success).

- [ ] **Step 5: Commit**

```bash
git add cmd/ci.go
git commit -m "docs(cli): add Example block and clarify flag descriptions on ci subcommand"
```

---

### Task 3: Tighten `completion` Long, move install snippets into Example

**Files:**
- Modify: `cmd/completion.go`

- [ ] **Step 1: Open and review current state**

Read `cmd/completion.go`. The `completionCmd` struct literal is at lines 15-54. The `Long` field currently contains all the install snippets.

- [ ] **Step 2: Replace the completionCmd struct literal**

Replace the existing `completionCmd` declaration (lines 15-54) with:

```go
var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion script",
	Long: `Generate an autocompletion script for the specified shell.

The script enables tab completion for subcommands, flags, and image
references (read from "docker images") in your current shell session.`,
	Example: `  # Bash (current session)
  source <(layerx completion bash)

  # Bash (system-wide install)
  layerx completion bash | sudo tee /etc/bash_completion.d/layerx

  # Zsh
  layerx completion zsh > "${fpath[1]}/_layerx"

  # Fish
  layerx completion fish | source
  layerx completion fish > ~/.config/fish/completions/layerx.fish

  # PowerShell
  layerx completion powershell | Out-String | Invoke-Expression`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(cmd.OutOrStdout())
		case "zsh":
			return rootCmd.GenZshCompletion(cmd.OutOrStdout())
		case "fish":
			return rootCmd.GenFishCompletion(cmd.OutOrStdout(), true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
		}
		return nil
	},
}
```

(Only `Long` and `Example` change — `DisableFlagsInUseLine`, `ValidArgs`, `Args`, and `RunE` are unchanged. Reproduced in full so the diff is unambiguous.)

- [ ] **Step 3: Verify build and vet**

Run: `go build ./...`
Expected: no output (success).

Run: `go vet ./...`
Expected: no output (success).

- [ ] **Step 4: Commit**

```bash
git add cmd/completion.go
git commit -m "docs(cli): tighten completion Long, move install snippets into Example block"
```

---

### Task 4: Add `version` subcommand

**Files:**
- Create: `cmd/version.go`

- [ ] **Step 1: Confirm rootCmd.Version is wired**

Read `cmd/root.go` lines 27-29. Confirm `SetVersionInfo` sets `rootCmd.Version = v`. (It does — that means `rootCmd.Version` is the source of truth and the new subcommand should read from it.)

- [ ] **Step 2: Create cmd/version.go**

Create the file `cmd/version.go` with this exact content:

```go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

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

func init() {
	rootCmd.AddCommand(versionCmd)
}
```

Notes for the engineer:
- This file lives in the same `cmd` package as `root.go`, so it sees `rootCmd` directly — no exports needed.
- `rootCmd.Name()` returns `"layerx"` (derived from `Use: "layerx [image]"`).
- The format string matches what cobra's default version template produces for `--version`, so `layerx version` and `layerx --version` print identical output.

- [ ] **Step 3: Verify build and vet**

Run: `go build ./...`
Expected: no output (success).

Run: `go vet ./...`
Expected: no output (success).

- [ ] **Step 4: Commit**

```bash
git add cmd/version.go
git commit -m "feat(cli): add version subcommand mirroring --version output"
```

---

### Task 5: Final local verification

- [ ] **Step 1: Full build and vet sweep**

Run: `go build ./...`
Expected: no output.

Run: `go vet ./...`
Expected: no output.

- [ ] **Step 2: Inspect git log**

Run: `git log --oneline main..HEAD`
Expected: four commits, one per task above, in order.

- [ ] **Step 3: Push the branch**

Run: `git push -u origin feat/cli-help-polish`
Expected: branch pushed, PR creation hint shown.

- [ ] **Step 4: Stop here for Gate B + Gate C**

Per CLAUDE.md, the remaining verification happens off this dev machine:

**Gate B (GitHub Actions CI) — automatic:**
- [ ] CI green: `go test ./...` passes on Linux runner
- [ ] Cross-compile builds: linux/amd64, darwin/amd64, windows/amd64
- [ ] `go vet ./...` passes

**Gate C (second machine, manual) — engineer to verify:**
- [ ] `layerx --help` shows the new `Long` paragraph and `Examples:` block
- [ ] `layerx ci --help` shows the YAML config block plus `Examples:` block
- [ ] `layerx completion --help` shows tightened `Long` plus `Examples:` block (four shells)
- [ ] `layerx version` prints `layerx version <X.Y.Z>` (or `layerx version dev` for dev builds)
- [ ] `layerx --version` prints identical output to `layerx version`
- [ ] `layerx help version` works (cobra auto-routes)
- [ ] Regression: `layerx nginx:latest` still launches the TUI normally
- [ ] Regression: `CI=true layerx nginx:latest` still runs the CI evaluator

Do **not** open the PR or tag a release until Gate B is green and Gate C is checked off.

---

## Notes for the implementing engineer

- **No `go test` on the Windows dev machine** — the Defender admin policy blocks it. Local verification is `go build ./...` and `go vet ./...` only. Real test execution happens on GitHub Actions.
- **No tests added in this plan.** Per the spec, cobra string outputs are not worth pinning in Go tests. CI's `go test ./...` will run the existing test suite (e.g. `cmd/json_test.go`) and confirm we have not broken anything else.
- **Cobra renders `Example` verbatim** under an `Examples:` heading. Each example line in the source already has two leading spaces — preserve that indentation exactly.
- **No CHANGELOG entry yet.** This change is documentation polish, not a milestone. If the repo's release workflow requires a CHANGELOG bump for the next tag, add a one-liner under the next milestone heading at PR time, not now.

---

## Spec coverage check

| Spec requirement | Task |
|---|---|
| Root `Long` expanded to ~3 sentences | Task 1, Step 2 |
| Root `Example` block (TUI / JSON / ci) | Task 1, Step 2 |
| `--json` flag description tightened | Task 1, Step 3 |
| `ci` `Example` block | Task 2, Step 2 |
| `ci` flag descriptions tightened (all three) | Task 2, Step 3 |
| `completion` `Long` tightened | Task 3, Step 2 |
| `completion` install snippets moved into `Example` | Task 3, Step 2 |
| New `version` subcommand mirroring `--version` | Task 4 |
| `commit` and `date` left unused (no API churn in `main.go`) | Task 4 (no changes outside `cmd/version.go`) |
| Local verification: `go build`, `go vet` | Each task, Step "Verify build and vet" |
| Gate C verification deferred to second machine | Task 5, Step 4 |
