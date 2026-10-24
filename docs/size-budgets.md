# Image-size budgets and comparison

An image can be 100% layer-efficient and still be too large. Size budgets
independently limit how much layer data your project allows.

## Start with an absolute limit

After building your image, run:

```sh
layerx ci --max-image-size 500MB app:local
```

This adds a size check to the existing CI efficiency and path checks. Any
failed check exits 1; an analysis or configuration error exits 2. Inspect
your production image before choosing a limit; 500 MB is only an example.

For a shared project policy, add this to `.layerx.yaml`:

```yaml
size-budget:
  max: 500MB
  max-growth: 5%
```

Omitted size limits are disabled. Standard units are supported: `MB` means
1,000,000 bytes; `MiB` means 1,048,576 bytes. You can use whole byte counts
without a suffix. Equality passes. `0B` or `0%` allows zero, rather than
disabling a limit. Negative sizes, fractional bytes and overflow are errors.
Flags override configuration. An explicit empty flag value, such as
`--max-growth=`, disables that limit for this invocation.

## Check growth against a baseline

```sh
layerx ci --baseline app:previous app:local
layerx compare app:previous app:local

# Limit additional bytes instead of a percentage:
layerx compare --max-growth 20MB previous.tar current.tar
```

`compare` uses OLD as the baseline. `ci` requires `--baseline OLD` whenever
a growth budget is enabled. A requested baseline that cannot be analysed is
an error; growth is never silently skipped. Both absolute and growth limits
must pass when configured together. A reduction passes a nonnegative growth
limit. Percentage allowances are computed from exact bytes, not display
rounding. Positive growth from a zero-byte baseline requires a byte limit
because its percentage is undefined.

Compare the same OS, architecture and variant. Growth checks reject unknown
or differing platforms. Use `--platform linux/amd64`, for example, and export
single-image archives for archive comparisons. Run a separate CI matrix entry
for each deployment platform.

Prefer immutable release references or digests for repeatable baselines.
Image references follow normal LayerX resolution: an available local image
may be reused, and missing references are pulled through the selected engine.
LayerX verifies a locally cached variant before reusing it, including with `--platform`. It does not automatically discover a main-branch image or refresh a tag.
JSON reports include the resolved identities and measured platforms.

## What size means

**Uncompressed layers** is the sum of the complete uncompressed layer tar
streams for the selected image, including headers and padding. It includes
payload retained in earlier layers after a file is overwritten or deleted.
Repeated layer occurrences count at each manifest position.

It excludes the outer save archive, manifests, config, signatures and
attestations. Equivalent layer streams have the same measurement whether
stored plain or gzip-compressed. Unsupported layer encodings produce an
error rather than a substituted measurement.

This metric is not registry download traffic, incremental disk usage,
runtime memory, or a prediction of startup time. Registry compression,
shared layers, existing caches and filesystem allocation affect those
separately. File additions/removals describe the final filesystem; their
totals do not necessarily explain every byte of layer-tar growth.

The viewer header shows uncompressed size and the configured absolute budget
when space permits. Growth budgets need a baseline and are evaluated by CI
or comparison, not by the single-image viewer.

## Read the comparison

The report starts with its verdict, selected inputs and platform, followed
by before/after totals and deltas. Size limits show PASS/FAIL, the allowance,
and the amount over budget. A failed limit also shows exact bytes.

Shared layers are matched using full hashes of uncompressed streams, so an
inserted build step does not shift every later match. Remaining layers may
be paired by a command that occurs uniquely on both sides; these matches are
explicitly labelled as command matches. Other layers are shown as added or
removed, rather than paired by index.

File differences compare size and metadata, **not file contents**. Same-size
content changes can be absent. Layer hashes still reveal changed layer
streams. File and directory byte changes are investigation leads, not proof
that a particular Dockerfile instruction caused all growth.

The default compact report ranks changes by absolute byte impact and omits
shared layer rows. It includes directory summaries when there are many file
changes. Use `--top 20` to increase the compact detail, `--mode full` for all
changes, and `--mode summary` for totals and checks only. The text layout
adapts to the terminal width; long paths keep their filename suffix. Color
uses the configured theme in a terminal and is disabled for redirected
output, `NO_COLOR`, and `TERM=dumb`.

`compare` reads `size-budget` and `theme` from `.layerx.yaml` (`--config`
selects another file). Existing CI efficiency thresholds and path rules are
still exclusive to `ci`. Comparison also retains its existing checks for
increased waste or decreased efficiency. A size improvement can therefore
coexist with a failed waste check; the report names each failed check.

## Export check reports

```sh
layerx compare --format json app:previous app:local > comparison.json
layerx compare --format markdown app:previous app:local > comparison.md
layerx ci --format json --max-image-size 500MB app:local > checks.json
```

Reports are emitted after successful analysis even when a policy fails.
Analysis errors return an error without pretending that incomplete checks
passed. Text reports retain the final `verdict:` line for existing scripts;
JSON and Markdown are separate output formats.

Check-report JSON has schema `1.0.0`, metric
`uncompressed-layer-tar-bytes`, exact integer byte measurements, effective
size budgets, check results, image identities, and platforms. Comparison
JSON includes shared/command/added/removed layer matches and final-file
changes. Layer indexes in JSON are zero-based; a missing side is `-1`.
File `diffType` values are 0 unchanged, 1 added, 2 modified, 3 removed.
CI reports include their existing rule results as well as size checks.

`--json FILE` retains its existing role as an **analysis** export. Its schema
`1.0.2` adds `uncompressedLayerBytes` and optional `measuredPlatform`;
`totalSize` and per-layer `size` retain their stored-blob meanings. Use the
new explicit field for comparable sizes. Analysis caches from older versions
are rebuilt automatically for the new measurement and layer hashes.

## Add a GitHub Actions summary

Once LayerX is installed and both images are available in the job:

```yaml
- name: Compare image sizes
  shell: bash
  run: |
    status=0
    layerx compare --format markdown app:previous app:local > comparison.md || status=$?
    cat comparison.md >> "$GITHUB_STEP_SUMMARY"
    exit "$status"
```

This preserves the comparison exit code while publishing its report. For
Buildx builds, use `--load` for a local single-platform image or export a
supported image archive; a result left only in the builder cache is not a
local image. The baseline must be supplied by your pipeline. Automatic
history, baseline discovery and PR comments are not part of this feature.
