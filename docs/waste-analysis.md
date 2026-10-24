# Understanding waste

LayerX separates proven redundant layer payload from live content that may be
unnecessary. Both matter when reducing image size, but they answer different
questions.

The waste screen shows both totals together. **Layer waste** means earlier
file copies that still take space; **Potential cleanup** means cache files
to review before removing. Press `Tab` to switch the list. The footer names
the destination, and `i` opens a plain-language explanation of the selected row.

## Why can a 2 GB image score 100%?

Layer efficiency measures how much stored file payload remains reachable in
the final filesystem:

```text
layer efficiency = live payload / (live payload + wasted payload)
```

The calculation uses uncompressed file bytes. It does not measure registry
download size or include tar headers and compression overhead. An overwritten
or deleted copy still occupies an earlier layer, so its payload is wasted
unless a surviving hardlink retains that specific copy. Multiple names for
the same hardlinked payload do not multiply the live-byte denominator.

An image with a 1 GB application and a 1 GB pip cache written once can have
100% layer efficiency: every byte is still live. That does not tell us whether
the application needs the cache. A 2 GB model file can also be necessary;
size alone is insufficient evidence to call it waste.

## Explain proven waste

Press `w`, select a file and press `i`. The detail view shows each write and
deletion, its layer and Dockerfile command, and its contribution to wasted
bytes. Payload is charged to the write that stored it, not the later deletion.
For example:

```text
L1  added     100 MB   waste 100 MB   overwritten in L2
L2  modified   80 MB   waste  80 MB   deleted in L3
L3  deleted     0 B    waste   0 B
```

The total is 180 MB. Cleanup in L3 cannot remove bytes already stored in L1
and L2. Downloading, using and deleting a temporary file within one build
step, or excluding it through a multi-stage build, avoids storing it in the
runtime image.

Use `j`/`k`, `g`/`G` or page keys to select a history event. `Enter` jumps to
its layer and path; `y` copies the path and `c` copies the full command, including
text too long for the panel. `Esc` returns to the waste list. The
list retains its existing `Enter` shortcut to jump directly to an earlier
stored copy.

## Review potential cleanup

Press `Tab` in the waste overlay to see recognized cache directories in the
final live filesystem. Candidate totals use uncompressed regular-file bytes;
earlier overwritten versions, removed files, symlinks and hardlink aliases
are excluded. Groups are sorted by byte count, then path.

Recognized locations include:

- `/var/cache/apt/archives`, `/var/lib/apt/lists` and `/var/cache/apk`.
- `.cache/pip`, `.npm/_cacache`, `.cache/yarn` and `.cache/go-build` beneath
  `/root` or `/home/USER`.

Press `i` for the reason and cleanup guidance, or `Enter` to browse the
directory at the final layer. These are review candidates: an offline build
or a development image may intentionally include caches. Candidate bytes are
not added to proven wasted bytes, and are not a prediction of compressed
download savings. Existing efficiency and waste CI thresholds retain their
meaning; corrected hardlink attribution can change scores for affected images.

The detector does not claim to identify every cache or unnecessary dependency.
No candidates means no recognized cache payload was found, not that the image
is fully optimized.

## Further ways to assess image size

Image-size budgets now cover overall growth; other possible improvements go beyond layer waste:

- **[Image-size budgets](size-budgets.md):** independently limit uncompressed layer size or growth against a baseline. A large image can violate a project budget at 100% efficiency.
- **Duplicate-content analysis:** hash file contents to identify identical
  payload under different paths. Matching sizes are not sufficient evidence;
  hashing needs an explicit performance and cache-format design.
- **Application-specific checks:** allow projects to identify unwanted paths
  or development artifacts. Existing `path-rules.block` rules can enforce known
  exclusions today. Runtime necessity cannot be inferred reliably from a
  filename or image size alone.

These measurements are separate from the layer-efficiency score. Duplicate-content analysis remains a possible future improvement.

[Using the TUI](tui.md) · [Configuration](configuration.md)
