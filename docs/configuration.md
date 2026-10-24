<!-- docs/configuration.md — full reference for the .layerx.yaml configuration file -->

# Configuration Reference

`.layerx.yaml` configures CI thresholds, path-scoped rules for `layerx ci`,
and the TUI theme and background. LayerX searches the current directory and
parents for `.layerx.yaml`, then the user configuration directory for
`layerx/config.yaml`. Use `--config FILE` to choose a file explicitly.

---

## Overview

- **What it is.** A small YAML file describing efficiency thresholds and
  path rules used by `layerx ci` (and the `CI=true` shortcut).
- **When it is created.** Run `layerx init` once per repository. That writes
  a starter `.layerx.yaml` matched to your stack (`node`, `python`, `java`,
  `go`, or `generic`).
- **Where it should live.** Usually the repository root. LayerX uses the
  nearest `.layerx.yaml` in the current directory or its parents, then falls
  back to `layerx/config.yaml` in the operating system's user configuration
  directory. `--config FILE` overrides this search.
  The first file found is used; project, parent, and user files are not merged.
  Missing values in that file use built-in defaults. A missing file chosen with
  `--config` also uses built-in defaults rather than searching for another file.
- **YAML library.** Decoded with `goccy/go-yaml` in **strict mode**: any
  unknown top-level key fails with a parse error. Empty files (or files
  containing only comments and whitespace) silently fall back to built-in
  defaults.
- **Schema version.** Only schema version `1` is accepted. `version: 0` is
  treated as `1`; `version: 2` or higher is rejected.

---

## Which commands use configuration?

| Command | Configuration behavior |
|---|---|
| `layerx IMAGE` (CI disabled) | Loads and validates configuration; applies theme and background settings to the viewer. |
| `layerx --json FILE IMAGE` (CI disabled) | Loads and validates configuration; exports analysis without evaluating CI rules. |
| `layerx ci IMAGE` or `CI=true layerx IMAGE` | Loads and evaluates global and path rules. |
| `layerx build ...` | Loads and validates configuration after a successful build; applies appearance settings when opening the viewer. It does not run CI rules. |
| `layerx compare OLD NEW` | Loads `size-budget` and `theme`; `--config` selects a file. Retains built-in efficiency/waste regression checks. CI thresholds and path rules do not apply. |

`keybindings` is reserved: a mapping is accepted for compatibility but does not
change shortcuts. Press `?` in the viewer for the supported shortcuts.

---

## Full Example

A complete `.layerx.yaml` exercising every implemented field:

```yaml
# .layerx.yaml — schema version 1
version: 1

# TUI colour theme. Precedence: --theme flag > this field > built-in default (tokyo-night).
# Valid values: tokyo-night, catppuccin-mocha, kanagawa, gruvbox-dark,
#               rose-pine, dracula, oxocarbon, cyberdream
theme: tokyo-night
transparent_background: false       # true removes viewer background fills

# Global efficiency thresholds. Set any value to 0 to disable that rule.
rules:
  lowest-efficiency: 0.9              # fail if score < 90%
  highest-wasted-bytes: 10485760      # fail if wasted bytes > 10 MiB
  highest-user-wasted-percent: 0.1    # fail if waste > 10% of live + wasted payload

# Path-scoped rules — flat form (the form `layerx init` writes).
path-rules:
  block:
    - "**/.git/**"
    - "/tmp/**"
    - "/var/tmp/**"
    - "**/.DS_Store"
  deny-waste:
    - "**/*.log"
    - "**/*.tmp"
  max-layer-count: 5
```

The same configuration in **list form** (use this when you want multiple
independent rule instances of the same type, each with its own ID):

```yaml
version: 1

rules:
  lowest-efficiency: 0.9
  highest-user-wasted-percent: 0.1

path-rules:
  - id: block-vcs
    type: block
    paths:
      - "**/.git/**"
      - "**/.svn/**"

  - id: block-build-scratch
    type: block
    paths:
      - "/tmp/**"
      - "/var/tmp/**"

  - id: deny-stale-logs
    type: deny-waste
    paths:
      - "**/*.log"

  - id: cap-layer-fanout
    type: max-layer-count
    threshold: 5
```

Flat form and list form **cannot be mixed** in the same file — `path-rules`
is parsed as either a YAML mapping or a YAML sequence, never both.

---

## Field Reference

### Top-level keys

| Field | Type | Default | Required | Description |
|---|---|---|---|---|
| `version` | int | `1` | optional | Schema version. Accepts `0` (treated as `1`) or `1`. Anything else fails validation. |
| `theme` | string | `""` (uses `tokyo-night`) | optional | TUI colour theme. See [docs/theming.md](theming.md) for all values and precedence. |
| `transparent_background` | bool | `false` | optional | Remove viewer background fills when `true`. |
| `rules` | mapping | populated from defaults | optional | Global CI thresholds. See below. |
| `size-budget` | mapping | empty | optional | Uncompressed image size and baseline growth limits. |
| `path-rules` | mapping or sequence | empty | optional | Path-scoped rules. Two equivalent forms documented below. |
| `keybindings` | mapping | empty | optional | **Reserved.** Accepted for compatibility; does not change shortcuts. |

Any other top-level key (for example `display:`) fails to load — the
decoder is strict.

### `size-budget` — optional image-size limits

```yaml
size-budget:
  max: 500MB
  max-growth: 5%
```

Both limits are disabled when omitted. Growth accepts a byte quantity such as `20MB` or a percentage. Standard decimal (`MB`) and binary (`MiB`) units are distinct. Equality passes; `0B` and `0%` are real zero limits. Null sections and unknown keys are rejected. Flags `--max-image-size` and `--max-growth` override these settings. An empty flag value disables that limit.

CI requires `--baseline OLD` when growth is enabled; comparison uses OLD automatically. Growth requires matching known image platforms. The single-image viewer displays size and the absolute budget without enforcing exit codes. See [Image-size budgets](size-budgets.md) for metric definitions and examples.

### `rules` — global thresholds

| Field | Type | Default | Required | Description |
|---|---|---|---|---|
| `rules.lowest-efficiency` | float64, `[0, 1]` | `0.9` | optional | Fail when `score < threshold`. `0` disables the rule. |
| `rules.highest-wasted-bytes` | int64, raw bytes | `0` (disabled) | optional | Fail when wasted bytes exceed the threshold. `0` disables the rule. |
| `rules.highest-user-wasted-percent` | float64, `[0, 1]` | `0.1` | optional | Fail when wasted payload / (live + wasted payload) exceeds the threshold. Uses uncompressed file bytes, not compressed image size. `0` disables the rule. |

Omitting a threshold keeps its built-in default; it does not disable that rule.
An absent `rules` section or `rules: {}` keeps all defaults. Set a threshold to
`0` explicitly to disable it. `rules: null` (including a bare `rules:` key or
`rules: ~`) is invalid; it is not a way to disable checks.

Range validation rejects `NaN`, `±Inf`, negative byte counts, and floats
outside `[0, 1]`.

Wasted bytes include files added in one layer and deleted in a later one: the
delete records a whiteout but never reclaims the earlier layer's bytes, so they
still ship in the image. A score may be lower than a tool that counts only
files duplicated across layers.

### `path-rules` — flat form (mapping)

| Field | Type | Default | Required | Description |
|---|---|---|---|---|
| `path-rules.block` | `[]string` of doublestar globs | empty | optional | Files matching any pattern must not appear in any layer. |
| `path-rules.deny-waste` | `[]string` of doublestar globs | empty | optional | Fail when a matching file has wasted payload, including a file written once and deleted later. |
| `path-rules.max-layer-count` | int, `0` or `≥ 2` | `0` (disabled) | optional | Cap the contributing layer count reported for files with wasted payload. `1` is not supported. |

The count is the waste analysis's `LayerCount`, not the number of layers in
which a file remains visible. Zero-byte writes and unchanged carryover do not
contribute; a separate live reinstall with no wasted payload does not inflate
the count. See [Understanding waste](waste-analysis.md).

Each glob is validated at load time. An invalid pattern fails fast with a
clear error pointing at the rule ID.

### `path-rules` — list form (sequence)

Each list entry:

| Field | Type | Required? | Description |
|---|---|---|---|
| `id` | string | **required** | Unique identifier across the list. Used in CI report lines and error messages. |
| `type` | string | **required** | One of `block`, `deny-waste`, `max-layer-count`. |
| `paths` | `[]string` | required for `block` and `deny-waste`; ignored for `max-layer-count` | Doublestar globs. |
| `threshold` | int | optional for `max-layer-count`; omitted or `0` disables the entry | Must be `≥ 2` when enabled. |

---

## Minimal Config

Smallest valid `.layerx.yaml` — relies entirely on built-in defaults
(`lowest-efficiency: 0.9`, `highest-user-wasted-percent: 0.1`,
`highest-wasted-bytes: 0` disabled, no path rules):

```yaml
version: 1
```

You can omit `version:` entirely (an empty file works) and `layerx`
treats it as schema 1.

---

## Common Use Cases

### Strict production images

Tight efficiency budget, common cache and VCS leakage blocked, no file may
sprawl across many layers:

```yaml
version: 1

rules:
  lowest-efficiency: 0.97
  highest-wasted-bytes: 5242880     # 5 MiB
  highest-user-wasted-percent: 0.03

path-rules:
  block:
    - "**/.git/**"
    - "/root/.cache/**"
    - "/tmp/**"
    - "/var/tmp/**"
    - "**/*.swp"
  deny-waste:
    - "**/*.log"
    - "**/*.bak"
  max-layer-count: 3
```

### Relaxed dev / testing setup

Useful for local builds and feature branches where you only want to catch
egregious regressions:

```yaml
version: 1

rules:
  lowest-efficiency: 0.7
  highest-user-wasted-percent: 0.3

path-rules:
  block:
    - "**/.git/**"
```

### CI-specific, opinionated by stack

Run `layerx init --flavour python` for a Python service. The generated file:

```yaml
version: 1

rules:
  lowest-efficiency: 0.9
  highest-user-wasted-percent: 0.1

path-rules:
  block:
    - "**/.git/**"
    - "/root/.cache/pip/**"
    - "**/__pycache__/**"
    - "/tmp/**"
  deny-waste:
    - "**/*.pyc"
    - "**/*.pyo"
```

Other flavours ship with the same shape — `node`, `java`, `go`, `generic`.
The `go` flavour is tighter (`lowest-efficiency: 0.95`,
`highest-user-wasted-percent: 0.05`) because Go images are typically
distroless or scratch-based.

---

## CLI Flag Overrides

Only the three global thresholds can be overridden on the command line.
Path rules are config-only.

| Config field | CLI flag | Where |
|---|---|---|
| `rules.lowest-efficiency` | `--lowest-efficiency` | `layerx ci` |
| `rules.highest-wasted-bytes` | `--highest-wasted-bytes` | `layerx ci` |
| `rules.highest-user-wasted-percent` | `--highest-user-wasted-percent` | `layerx ci` |

Precedence: CLI flag (when present) overrides the config-file value, which
overrides the built-in default. Passing `0` to a flag disables that rule
for the run, regardless of what the config says.

The `CI=true` env-var shortcut (`CI=true layerx <image>`) cannot accept
threshold flags — it always uses config-file values or the built-in
defaults. Use explicit `layerx ci --lowest-efficiency 0.95 <image>` if you
need an inline override.

---

## Notes & Tips

- **Strict decoding.** Typos in top-level keys are errors, not warnings.
  This is intentional — silent ignores hide configuration drift. Watch for
  `unknown field` parse errors.
- **Disabling rules cleanly.** Set the threshold to `0`; omission keeps the default.
  `layerx ci` errors out if every rule is disabled and no path rules are
  configured — by design, an empty rule set is almost always a mistake.
  To run only path rules, set all three global thresholds to `0` and configure
  at least one path rule.
- **Glob syntax.** Patterns use [doublestar](https://github.com/bmatcuk/doublestar)
  semantics: `**` matches any number of path segments, `*` matches a single
  segment, brace expansion (`{a,b}`) works. Patterns are validated at load
  time, so an invalid glob fails fast rather than at evaluation.
- **`max-layer-count: 1` is rejected.** The supported range is `2` or higher,
  or `0` to disable. It applies to files with wasted payload, not every live file.
- **Quote your globs.** YAML treats `*` as a node anchor in some positions.
  All starter configs quote globs uniformly (`"**/*.pyc"`); follow suit for
  any glob that begins with `*`.
- **`layerx init --force` overwrites.** Without `--force`, `layerx init`
  refuses to overwrite an existing `.layerx.yaml`.
- **Empty file is valid.** A file containing only comments or whitespace
  loads as the built-in defaults. Useful for opting in to defaults
  explicitly.
- **No migration logic.** If a future schema bump arrives, this file will
  need to be edited or regenerated. There is no automatic upgrade.
