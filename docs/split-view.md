# The split view (`A`)

Split view shows two ways to read the filesystem at the selected layer: changes
relative to the preceding layer, and diff history accumulated from the base.

For file viewing, searching, saving and all keys, see [Using the TUI](tui.md).

---

## The short version

Press **`A`** (that's **Shift + a** — a capital A) while browsing an image. The file tree splits into two stacked panes:

```
┌─ ▸ Layer files ────────────────────┐
│  changes from the selected layer   │
│  plus unchanged files for context  │
├─   Cumulative ─────────────────────┤
│  diff history from the base up to  │
│  this layer, including removals    │
└────────────────────────────────────┘
```

Both panes always show the **same layer** — the one selected in the layers pane on the left. The focused half gets more rows; switching focus moves that extra space with it. Press `A` again to go back to the single tree.

**Layer files highlights this step; Cumulative retains earlier diff markers.**
Press `d` to hide entries marked unchanged.

---

## Walking through it

Say you're inspecting `nginx:latest` and you've selected the layer that runs `apt-get install nginx`.

- **Layer files** highlights what the command added, modified or removed, such
  as the `nginx` binary and files under `/etc/nginx`. Files carried forward from
  earlier layers appear as unchanged. Press `d` to narrow the list to changes.
- **Cumulative** keeps the added, modified and removed labels built up from the
  base through this layer. A base-image file can still be green here even when
  the selected command did not touch it. Red entries are historical removal
  markers; they do not mean the file exists in the live filesystem.

Select the next layer and both panes update together. Layer files resets prior
diff labels; Cumulative carries them forward.

### Driving it

You move between the layers pane and the two tree panes with `Tab`:

```
Tab  →  layers pane      (pick which layer both halves show)
Tab  →  Layer files      (browse the selected layer's changes and context)
Tab  →  Cumulative       (browse accumulated file history)
Tab  →  back to layers
```

When Layers has focus, `j`/`k` (or `↑`/`↓`) change the selected layer and both
trees update. When either tree has focus, the same keys move its cursor.
The active panel title has a `▸` marker and a highlighted border. When Cumulative
has focus, its name leads the outer title and its divider also shows `▸`.
Inactive selections remain visible with quieter styling.

Everything else works as normal inside whichever tree has focus — `Enter` opens a file, `/` filters, `x` extracts, `d` hides unchanged files.

---

## What each half actually is

This is the part that trips people up, so it's worth being precise.

| | Top pane — **Layer files** | Bottom pane — **Cumulative** |
|---|---|---|
| Shows | Files at this layer, current-step changes and removal markers | Files and diff markers accumulated through this layer |
| Answers | "What did this build step do?" | "What does the image look like at this point?" |
| Unchanged entries | Shown by default; `d` hides them | Earlier diff labels persist, so `d` can leave many earlier files visible |
| Diff colouring | Green added, yellow modified, red removed by the selected step | Labels retained from earlier steps and updated by later changes |

Both views interpret whiteouts as deletions. Cumulative retains removal markers
for inspection, so it is not a literal directory listing from a running
container. Removed entries cannot be opened or saved. Bytes stored by earlier
layers remain in the image even after a later step deletes their files.

---

## Compare two images

The split follows one selected layer. It has no selector for comparing two
arbitrary layers. To compare separate builds or releases, use
`layerx compare OLD NEW`; see [Compare two images](../README.md#compare-two-images).

---

## Details worth knowing

- **`A` is a toggle.** Press it once to split, again to return to the single tree. Turning it off while you were focused on the bottom pane snaps focus back to the single tree so your keys keep working.
- **Your filters carry over.** Toggling the split keeps your active filter (`/`), diff-only mode (`d`), and sort order (`s`) — they apply to both halves the same way, so you don't have to set them up twice.
- **Toggling resets tree cursors.** Each pane starts at the top when you turn
  split mode on or off. Changing the selected layer also resets tree cursors,
  directory collapse state and size sorting. Resizing retains each selection.
- **The divider is live.** The Cumulative divider shows its item count while
  inactive, and `▸ Cumulative 3/240`-style position information when focused.
- **Short terminals use compact mode.** When both trees cannot fit, only the
  active tree is drawn. `Tab` still reaches each pane. Enlarging the terminal
  restores the split and retains each tree's selection. The main view needs
  at least 50 columns and 11 rows.

---

## See also

- [TUI keybindings](tui.md#keybindings) — the full key reference.
- [Compare two images](../README.md#compare-two-images) — for image-to-image diffs.
- [Switching from Dive to LayerX](migrating-from-dive.md) — if you're used to Dive's aggregated view.
