# Using the TUI

Open a live image or a saved image archive:

```bash
layerx nginx:stable
layerx ./build/app.tar
```

## Read the screen

- **Layers** lists Dockerfile commands. `#1` is the first layer; the visible
  layer numbers and `L1` origin labels are one-based. JSON layer indices stay
  zero-based.
- **Layer files** shows the filesystem at the selected layer, with changes
  relative to the preceding layer highlighted. Unchanged files remain visible
  until you press `d`.
- **Cumulative** appears when you press `A`. It retains diff markers from
  earlier layers. See [the split-view guide](split-view.md) for exact semantics.
- **Focus** is marked by `▸` in the active panel title and a highlighted
  border. Inactive panes retain a quieter selection so you can keep your place.
- **Size columns** cycle with `S` while Layers has focus: `change` is the
  live-filesystem byte delta, `stored` is the layer's stored size, and
  `stored+change` shows both when the pane is wide enough. A negative change
  does not reclaim bytes already stored in earlier layers.

## Find and open a file

1. Select a layer with `j`/`k`, then press `Tab` to focus Layer files.
2. Press `/`, type part of its path, and press `Enter` to confirm the filter.
3. Select a matching file and press `Enter` again to open the viewer.
4. Press `/` in the viewer to search its contents; use `n`/`N` for matches.
5. Press `Esc` to clear a search, then `Esc` again to close the viewer.

`Enter` expands or collapses a directory in the normal tree. `h` collapses an
open folder or moves to its parent; `l` expands a closed folder or moves to its
first child. Filtering and diff-only mode show flat paths. Sorting lists files
only, largest or smallest first, without directory totals mixed in; press `s`
again to return to the tree. `Backspace` or `Esc` clears a confirmed filter.

Press `m` to hide metadata columns and give file names more room; press it
again to restore the columns. The diff markers remain visible in both modes.

Long paths shorten in the middle so the filename and extension remain visible
where they fit. Deep indentation yields space first. This affects display only:
`y` copies the complete path, and opening or saving uses the original path.
The viewer title prioritizes the path and an earlier origin layer such as
`← L1:`; its command summary uses the remaining space.

## Investigate waste

The waste screen always shows two separate totals: **Layer waste** is stored
data from earlier file copies, and **Potential cleanup** is live cache content
worth reviewing. The footer names the next action, such as **Review cleanup**
or **View layer waste**.

Press `w` to see wasted files sorted by wasted bytes. Select a row and press
`Enter` to jump to its introducing layer and path. Press `a` to expand the
top 20 entries to up to 500; `Esc` closes the overlay.

Press `i` to explain waste for the selected file. A short explanation appears
before its layer history. Use the arrow keys or `j`/`k` to select a history
event and read its Dockerfile command, stored payload and wasted-byte
contribution. `Enter` jumps to that event's layer; `Esc` returns to the list.

`Tab` switches between layer waste and potential cleanup candidates. The
candidate list groups recognized package and build caches that remain in the
final filesystem. `i` explains the candidate and `Enter` jumps to its directory
at the final layer. Candidates require review and do not change CI thresholds
or the efficiency score. [Understanding waste](waste-analysis.md) explains why
a large image can still have 100% layer efficiency.

A file written in one layer and deleted later still occupies image space.
Move temporary downloads and cleanup into the same build step, or keep build
dependencies in a separate build stage.

## Save or copy

- `x` saves the selected file into the current working directory using its
  basename. Check the footer for the resulting path or an error.
- `y` copies the complete selected path; `Y` copies file contents in the viewer.
- `c` copies the selected layer's Dockerfile command.

Removed-file markers cannot be opened or saved. Clipboard operations use OSC 52;
terminal and tmux settings determine whether the clipboard accepts them.

## Terminal size and themes

The main view needs at least **50 columns × 11 rows**. Short terminals show
only the active tree in split mode, marked `compact`; `Tab` still reaches both
trees, and enlarging the terminal restores the split. Selections are retained
when resizing. The footer stays at the bottom as command lengths change.

Use `?` for scrollable help and `q` or `Ctrl+C` to quit. While typing a filter
or search, `q` is ordinary text; `Ctrl+C` still quits.

Choose one of eight [themes](theming.md), or enable `transparent_background`
in `.layerx.yaml` to use your terminal background.

## Keybindings

| Key         | Action                                                       |
|-------------|--------------------------------------------------------------|
| `Tab`       | Cycle panels (Layers → Layer files → Cumulative in split view) |
| `j` / `k`   | Move up / down                                               |
| `g` / `G`   | Jump to top / bottom                                         |
| `Ctrl-d` / `Ctrl-u` | Half page down / up (file tree and layers)           |
| `Ctrl-f` / `Ctrl-b` | Page down / up — also `PgDn` / `PgUp`                |
| `h` / `l`   | Navigate folders (tree) or scroll left / right (file viewer) |
| `Enter`     | Confirm a filter, open a file, or expand/collapse a folder    |
| `Esc`       | Close help, clear search/filter, or close the viewer. Quits only on loading and error screens. |
| `/`         | Filter file tree (tree) / search in viewer (viewer)          |
| `Backspace` | Clear a confirmed tree filter |
| `n` / `N`   | Next / previous search match (viewer)                        |
| `y`         | Copy file path to clipboard                                  |
| `Y`         | Copy file content to clipboard (viewer only)                 |
| `c`         | Copy Dockerfile command to clipboard (layers panel)          |
| `d`         | Toggle diff-only mode (hide unchanged files)                 |
| `s`         | Cycle file order: tree → largest → smallest                  |
| `m`         | Toggle file metadata columns                                 |
| `S`         | Cycle layer size column: change → stored → stored+change     |
| `w`         | Toggle wasted-files overlay (Enter jumps to introducing layer) |
| `x`         | Extract focused file to disk                                 |
| `A`         | Toggle Layer files + Cumulative split view  |
| `?`         | Toggle scrollable help (↑/↓ or PgUp/PgDn)                     |
| `q`         | Quit                                                         |

[Back to the README](../README.md) · [Split view](split-view.md)
