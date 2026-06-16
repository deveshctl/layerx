<!-- docs/json-export.md — reference for the --json export feature -->

# JSON Export

`layerx --json PATH IMAGE` writes a structured, versioned snapshot of the
full layer analysis to disk. Schema version is `1.0.1` and is pinned by
tests — a breaking change to any field will fail CI before a release.

---

## Overview

The JSON export contains everything the TUI uses to render its panels:

- Image identity (`imageRef`, `totalSize`, `layerCount`)
- Per-layer metadata (`index`, `id`, `size`, `netDelta`, `command`)
- The full file listing per layer (post-stack, post-whiteout view), each
  file tagged with its diff status against the prior layer
- Aggregate efficiency data (`score`, `wastedBytes`, top wasted files)

Use it for:

- **CI artifacts.** Store one JSON per build for trend dashboards or
  regression alerts.
- **Programmatic gates.** Feed it into a custom check that the built-in
  rules don't cover.
- **Cross-tool integration.** A stable, documented contract with a
  pinned schema version means downstream tooling won't break unannounced.

---

## Basic Usage

```bash
# Standalone JSON export. Skips the TUI; writes the file atomically.
layerx --json analysis.json nginx:latest

# Combined with a CI run — same analysis powers both the gate and the JSON.
layerx ci --json out/analysis.json myapp:latest

# Implicit CI mode (CI=true) plus JSON.
CI=true layerx --json out/analysis.json myapp:latest

# From a saved archive — no Docker daemon required.
layerx --json analysis.json ./build/app.tar
```

A few practical notes:

- The JSON is written atomically (temp file in the target directory →
  `fsync` → rename), so partial writes never reach the destination path.
- The success message `Written to <path>` goes to **stderr**, not stdout.
  This keeps stdout reserved for any pipeline that combines `--json` with
  another stdout-producing flow (it doesn't in v1, but the convention
  matters).
- There is no `--json -` (stdout) option. Write to a regular file, then
  pipe its contents (`cat out.json | jq …`). The export uses an atomic
  temp-file-and-rename, which is incompatible with character devices like
  `/dev/stdout`.
- When combined with `layerx ci`, the **CI exit code wins**. JSON write
  errors on a passing CI run propagate as the returned error; JSON write
  errors on a failing CI run produce a stderr warning but the exit code
  stays at `1`.

---

## Full JSON Schema

Schema version `1.0.1`. Pretty-printed with two-space indent.

```json
{
  "schemaVersion": "1.0.1",
  "imageRef": "nginx:latest",
  "platform": "linux/arm64",
  "totalSize": 187654321,
  "layerCount": 7,
  "efficiency": {
    "score": 0.94,
    "wastedBytes": 11200000,
    "wastedFiles": [
      {
        "path": "/var/lib/apt/lists/example.gz",
        "totalWasted": 4500000,
        "layerCount": 2
      }
    ]
  },
  "layers": [
    {
      "index": 0,
      "id": "sha256abcdef1",
      "size": 75000000,
      "netDelta": 75000000,
      "command": "ADD file:abc... in /",
      "files": [
        {
          "path": "/etc/os-release",
          "size": 412,
          "diffType": "added"
        }
      ]
    }
  ]
}
```

### Top-level fields

| Field | Type | Description |
|---|---|---|
| `schemaVersion` | string | `"1.0.1"`. Always emitted first. Bump = breaking change. |
| `imageRef` | string | The argument passed on the command line (`nginx:latest`, `./build/app.tar`, etc.). Not the digest. |
| `platform` | string | Optional. Canonical `"os/arch[/variant]"` form of `--platform`. Omitted (not emitted at all) when `--platform` was not set on the run. Available in schema **1.0.1+**. |
| `totalSize` | int64 | Sum of every layer's raw tar size, pre-stack. Bytes. |
| `layerCount` | int | `len(layers)`. |
| `efficiency` | object | Aggregate efficiency data. Always present. |
| `layers` | array \| null | Per-layer entries. Can be `null` when the image has zero layers (rare; some intermediate scratch images). |

### `efficiency`

| Field | Type | Description |
|---|---|---|
| `score` | float64 | `1.0 − wastedBytes / (liveBytes + wastedBytes)`, in `[0.0, 1.0]`. Higher is better. |
| `wastedBytes` | int64 | Bytes duplicated across layers (file-path appears in more than one layer; only first occurrence counts as "live"). |
| `wastedFiles` | array | Per-file waste breakdown. Always `[]` when empty (never `null`). Sorted by `totalWasted` descending, then `path` ascending. |

#### `efficiency.wastedFiles[]`

| Field | Type | Description |
|---|---|---|
| `path` | string | Absolute path inside the image. |
| `totalWasted` | int64 | Bytes wasted by this file across all duplicated layers. |
| `layerCount` | int | Number of layers in which this file contributes bytes (`size > 0`). |

### `layers[]`

| Field | Type | Description |
|---|---|---|
| `index` | int | 0-based layer position. Layer `0` is the base. |
| `id` | string | Short digest — first 12 hex chars of the layer's `sha256:`. |
| `size` | int64 | Raw tar size of the layer, bytes. |
| `netDelta` | int64 | Live-bytes delta vs the previous stacked snapshot. Negative when a layer cleans up more than it adds. Layer `0`'s `netDelta` equals its full live-bytes total. |
| `command` | string | Dockerfile instruction that produced the layer (verbatim, including any leading `/bin/sh -c`). |
| `files` | array | Files visible at this stacked position. Always `[]` when empty (never `null`). Leaves only — directory nodes are recursed into but never emitted as their own entries. |

### `layers[].files[]`

| Field | Type | Description |
|---|---|---|
| `path` | string | Absolute path inside the image. |
| `size` | int64 | File size in bytes. |
| `diffType` | string | One of `"added"`, `"modified"`, `"removed"`, `"unchanged"`. |

#### `diffType` values

| String | Meaning |
|---|---|
| `"added"` | File appears in this layer for the first time. |
| `"modified"` | File existed in a prior layer; this layer changed it. |
| `"removed"` | This layer deletes the file (whiteout in the underlying overlay). |
| `"unchanged"` | File present at this stacked position but unchanged from a prior layer. |

### Fields deliberately omitted

The internal `FileNode` type carries `Mode`, `UID`, `GID`, `Linkname`,
`IsDir`, `IsHardlink`, and `IntroducedInLayer`. These are **not** part of
the v1 JSON schema. They may be added later under a `1.x` minor bump
(additive, non-breaking).

### Initialization rules to rely on

- `efficiency.wastedFiles` is always an array (`[]` when empty), never
  `null`.
- Each layer's `files` is always an array (`[]` when empty), never
  `null`.
- Top-level `layers` is `null` when the image has zero layers; for any
  real image it is always an array.
- Field declaration order is fixed: `schemaVersion` first, then
  `imageRef`, `platform` (when present), `totalSize`, `layerCount`,
  `efficiency`, `layers`. Tests pin this so streaming consumers can
  rely on it.
- `platform` is the only `omitempty` field: it is omitted entirely
  when `--platform` was not set on the run. Every other field is
  always emitted.

---

## jq Examples

Practical one-liners. Each assumes you have `analysis.json` from a recent
`layerx --json analysis.json IMAGE` run.

```bash
# 1. Confirm the schema version before parsing further.
jq -r '.schemaVersion' analysis.json

# 2. List every layer with its index, short id, and human size.
jq -r '.layers[] | "\(.index)\t\(.id)\t\(.size)\t\(.command | .[0:60])"' analysis.json

# 3. Find every file added in layer 3.
jq '.layers[3].files[] | select(.diffType == "added")' analysis.json

# 4. Show all modified or removed files across the whole image, with the
#    layer that changed them.
jq -r '
  .layers[] as $L |
  $L.files[] |
  select(.diffType == "modified" or .diffType == "removed") |
  "\($L.index)\t\(.diffType)\t\(.path)"
' analysis.json

# 5. Pull just the efficiency score (useful in shell scripts).
jq -r '.efficiency.score' analysis.json

# 6. Top 10 largest files anywhere in the image, with the layer they
#    landed in.
jq -r '
  .layers[] as $L |
  $L.files[] |
  [$L.index, .size, .path] |
  @tsv
' analysis.json | sort -k2 -n -r | head -10

# 7. Total wasted bytes — quick gate without the full ci report.
jq '.efficiency.wastedBytes' analysis.json

# 8. The 5 worst wasted files, formatted for a slack message.
jq -r '
  .efficiency.wastedFiles[0:5][] |
  "• \(.path) — \(.totalWasted) bytes across \(.layerCount) layers"
' analysis.json

# 9. Sum of all "removed" files per layer (helps spot wasteful cleanup
#    patterns where files are added in one layer and removed in another).
jq -r '
  .layers[] |
  {index, removed: ([.files[] | select(.diffType == "removed") | .size] | add // 0)} |
  "\(.index)\t\(.removed)"
' analysis.json
```

---

## Scripting Use Cases

### Custom gate beyond the built-in rules

```bash
#!/usr/bin/env bash
# Fail if any single layer is over 50 MiB, regardless of total efficiency.
set -euo pipefail

layerx --json out/analysis.json myapp:latest

biggest=$(jq '[.layers[].size] | max' out/analysis.json)
limit=$((50 * 1024 * 1024))

if (( biggest > limit )); then
  echo "Largest layer is ${biggest} bytes (limit ${limit})." >&2
  exit 1
fi
```

### Trend dashboard, one record per build

```bash
# Emit a single-line JSONL record from each pipeline run.
layerx --json /tmp/analysis.json myapp:latest

jq -c '{
  build:    env.GITHUB_SHA,
  ts:       (now | floor),
  image:    .imageRef,
  layers:   .layerCount,
  size:     .totalSize,
  score:    .efficiency.score,
  wasted:   .efficiency.wastedBytes
}' /tmp/analysis.json >> dashboard.jsonl
```

### Detecting cache leakage

```bash
# Flag any layer that contains a path under /root/.cache.
jq -r '
  .layers[] as $L |
  $L.files[] |
  select(.path | test("^/root/\\.cache/")) |
  "\($L.index)\t\(.path)"
' analysis.json
```

### Generating a layer-by-layer markdown diff

```bash
jq -r '
  .layers[] |
  "## Layer \(.index) — `\(.id)`\n",
  "Command: `\(.command)`\n",
  "Net delta: \(.netDelta) bytes\n",
  "",
  "| Path | Size | Change |",
  "|---|---|---|",
  (.files[] | "| \(.path) | \(.size) | \(.diffType) |"),
  ""
' analysis.json > layer-report.md
```

---

## Tips

- **Pretty-print is the default.** `layerx` writes two-space-indented
  JSON. If you want compact JSON for storage, pipe through `jq -c .`.
- **`jq` for queries, `gron` for diffing.** `gron analysis.json |
  grep diffType | sort | uniq -c` is a one-liner for spotting unusual
  diff-type distributions across layers.
- **`fx` for exploration.** `fx analysis.json` gives you a navigable TUI
  view of the whole document — handy when you don't yet know what shape
  you want to query.
- **Diff two builds.**
  `diff <(jq -S . a.json) <(jq -S . b.json) | less` works well as a
  manual regression check between successive builds.
- **Combine with `layerx ci` once.** `layerx ci --json out.json IMAGE`
  reuses the same analysis for both the rule evaluation and the JSON
  dump — no double pull, no double parse.
- **Stable schema version, additive-only minor bumps.** Treat
  `schemaVersion` as the contract. New fields may appear under `1.x`;
  existing fields and their types will not change without a major bump.
  Check `schemaVersion` at the start of any consumer.
- **Archive paths in `imageRef`.** When you pass an archive
  (`./build/app.tar`), the `imageRef` field contains the path you typed,
  not the image's content digest. If you need a stable identity, hash the
  archive yourself before scanning.
- **No `null` for empty arrays.** `efficiency.wastedFiles` and per-layer
  `files` are always arrays. The single exception is top-level `layers`,
  which can be `null` for zero-layer images.
