# The split view (`A`)

The split view is one of the more powerful things the TUI can do, and also the easiest to misread. This page explains it in plain language first, then goes deep for anyone who wants to know exactly what they're looking at.

For the rest of the interactive controls, see the [TUI keybindings](../README.md#tui-keybindings) in the README.

---

## The short version

Press **`A`** (that's **Shift + a** — a capital A) while browsing an image. The file tree splits into two stacked panes:

```
┌─ Layer Δ ──────────────────────────┐
│  what THIS layer changed           │   ← top: just the diff
│  (added / modified / removed)      │
├─ ▾ Cumulative ─────────────────────┤
│  the whole filesystem at this      │   ← bottom: the result
│  point, all earlier layers merged  │
└────────────────────────────────────┘
```

Both panes always show the **same layer** — the one selected in the layers pane on the left. Press `A` again to go back to the single tree.

That's the whole feature. If you only remember one thing: **top is the change, bottom is the result.**

---

## Walking through it

Say you're inspecting `nginx:latest` and you've selected the layer that runs `apt-get install nginx`.

- The **top pane (Layer Δ)** shows only what that one command touched — the `nginx` binary, the config files under `/etc/nginx`, the handful of libraries it pulled in. Nothing else. If a file wasn't added, modified, or removed by this layer, it isn't here.
- The **bottom pane (Cumulative)** shows the *entire* filesystem as it exists right after that layer runs — the base image's `/bin`, `/lib`, `/usr`, plus everything the earlier layers added, plus this layer's changes folded in. It's what you'd see if you `docker run` this image and stopped it at this layer.

Select the next layer down and both panes move together: the top shows *that* layer's changes, the bottom shows the filesystem grown by one more layer.

### Driving it

You move between the layers pane and the two tree panes with `Tab`:

```
Tab  →  layers pane      (pick which layer both halves show)
Tab  →  top tree (Δ)     (scroll / open files in the change set)
Tab  →  bottom tree      (scroll / open files in the full tree)
Tab  →  back to layers
```

When focus is on the layers pane, `j`/`k` (or `↑`/`↓`) change the selected layer and **both halves update at once**. When focus is on either tree, the same keys scroll that tree. The focused pane is highlighted so you always know which one your keys are driving.

Everything else works as normal inside whichever tree has focus — `Enter` opens a file, `/` filters, `x` extracts, `d` hides unchanged files.

---

## What each half actually is

This is the part that trips people up, so it's worth being precise.

| | Top pane — **Layer Δ** | Bottom pane — **Cumulative** |
|---|---|---|
| Shows | Only the files this layer added, modified, or removed | The full merged filesystem up to and including this layer |
| Answers | "What did this build step do?" | "What does the image look like at this point?" |
| Grows as you go down | No — always just one layer's diff | Yes — each layer merges into the running total |
| Diff colouring | Green added, yellow modified, red removed | Same colours, relative to the layer that introduced each file |

The bottom pane is built by stacking every layer from the base up to the selected one, honouring whiteouts (deletions) exactly the way the container runtime would. A file deleted by a later layer disappears from the cumulative tree at that layer, even though it's still present in the layers below it.

---

## What it is *not*

The most common expectation — and the reason this page exists — is that `A` gives you a **diff between two arbitrary layers**. It does not.

The split is always one layer *against its own history*: the change on top, the result below. There is no "compare layer 3 to layer 7" mode inside the split view.

If you want to compare two whole *images* (for example, this build against the last release), that's a different tool — the `layerx compare OLD NEW` command, documented in the [README](../README.md#compare-two-images).

---

## Details worth knowing

- **`A` is a toggle.** Press it once to split, again to return to the single tree. Turning it off while you were focused on the bottom pane snaps focus back to the single tree so your keys keep working.
- **Your filters carry over.** Toggling the split keeps your active filter (`/`), diff-only mode (`d`), and sort order (`s`) — they apply to both halves the same way, so you don't have to set them up twice.
- **Cursor position resets.** The two trees have different shapes (a small change set vs. a full filesystem), so a scroll position saved from one wouldn't mean anything in the other. Each pane starts at the top when you toggle.
- **The divider is live.** The `▾ Cumulative` label in the middle divider doubles as a position readout — when the bottom pane has focus it shows `3/240`-style counts so you can see where you are without a separate scrollbar.

---

## See also

- [TUI keybindings](../README.md#tui-keybindings) — the full key reference.
- [Compare two images](../README.md#compare-two-images) — for image-to-image diffs.
- [Switching from Dive to LayerX](migrating-from-dive.md) — if you're used to Dive's aggregated view.
