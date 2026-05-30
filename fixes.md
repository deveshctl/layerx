# layerx — Bugs & Improvements

> **Canonical tracker** for bugs and targeted improvements.  
> **Do not duplicate** these items in `cursor.md`, `PROMPT_PLAYBOOK.md`, or release prompts — link here instead.  
> **Product features** (compare, CI path rules, etc.) stay in [`cursor.md`](cursor.md).

> **Total findings: 13** (7 bugs, 6 improvements; **#1 fixed**)
> Ordered by priority: critical bugs first, then improvements.

---

## Table of Contents

| # | Type | Summary | Files |
|---|------|---------|-------|
| 1 | ✅ Fixed | `q` key quits app inside filter/search input | `tui/model.go` |
| 2 | 🐛 Bug | Directory metadata lost during layer stacking | `image/stack.go` |
| 3 | ✅ Fixed | File viewer shows wrong file version (always final layer) | `image/extractor.go`, `tui/model.go` |
| 4 | 🐛 Bug | Waste overlay has no scrolling — cursor goes off-screen | `tui/waste.go`, `tui/model.go` |
| 5 | 🐛 Bug | CI command calls `os.Exit(1)` directly, breaking tests | `cmd/ci.go` |
| 6 | 🐛 Bug | JSON export missing `schemaVersion` (contract violation) | `cmd/json.go` |
| 7 | 🐛 Bug | `padRight`/`padLeft` use byte length, not rune length | `tui/filetree.go` |
| 8 | 🔧 Improvement | Memory-efficient streaming layer parsing | `image/docker.go` |
| 9 | 🔧 Improvement | Invisible search matches on truncated long lines | `tui/fileview.go`, `tui/model.go` |
| 10 | 🔧 Improvement | `FindChild` linear scan → O(1) map lookup | `image/filetree.go` |
| 11 | 🔧 Improvement | Cache `displayTree()` to avoid redundant recomputation | `tui/model.go` |
| 12 | 🔧 Improvement | Symlink detection and display in file tree | `image/tree_parser.go`, `image/filetree.go`, `tui/filetree.go` |
| 13 | 🔧 Improvement | Graceful cancellation during image loading | `tui/model.go` |

---

## 🐛 Bug #1 — `q` Key Quits App Inside Filter/Search Input

> **Status: FIXED** (2026-05-23). Do not re-track in `cursor.md` / playbook — use this section only for regression context.

### Description

Typing the letter `q` while the file tree filter or file viewer search is active instantly quits the application instead of adding `q` to the query text.

### Root Cause

In `tui/model.go`, the global quit keybinding (`m.keys.Quit` which matches `q` and `ctrl+c`) is evaluated **before** the `m.filterActive` and `m.viewSearchActive` input handlers:

```go
// Line ~341 in tui/model.go
if key.Matches(msg, m.keys.Quit) {   // ← catches 'q' first
    m.quitting = true
    return m, tea.Quit
}
// Line ~347 — never reached when 'q' is pressed
if m.filterActive {
    return m.handleFilterInput(msg)
}
```

### Reproduction

1. `layerx nginx:latest`
2. Press `Tab` to focus file tree → press `/` to open filter
3. Type `q` → app exits immediately

### Fix Location

`tui/model.go` — the `case tea.KeyPressMsg` handler in `Update()`, around line 341.

---

### Claude Code Prompt

```
Fix a bug in the layerx TUI where typing the letter "q" while the file tree filter input or the file viewer search input is active quits the application instead of typing the character.

Root cause: In tui/model.go, inside the Update() method's `case tea.KeyPressMsg` handler, the global quit check `key.Matches(msg, m.keys.Quit)` is evaluated BEFORE checking `m.filterActive` and `m.viewSearchActive`. Since m.keys.Quit is bound to both "q" and "ctrl+c" (defined in tui/keymap.go), pressing "q" during text input matches the quit handler first.

Fix: Move the quit check so that when `m.filterActive` or `m.viewSearchActive` is true, the "q" key falls through to the input handlers. Ctrl+C should still quit immediately in all states. The Escape handler (which is above the quit check and already has correct precedence) should NOT be changed.

Specifically, change the quit check block around line 341 from:

```go
if key.Matches(msg, m.keys.Quit) {
    m.quitting = true
    return m, tea.Quit
}
```

To something like:

```go
if key.Matches(msg, m.keys.Quit) {
    if !(m.filterActive || m.viewSearchActive) || msg.Code == tea.KeyCtrlC {
        m.quitting = true
        return m, tea.Quit
    }
}
```

Do NOT change the Escape key handling above this block. Do NOT change keymap.go. Only modify the quit check logic in the Update method.

After the fix, verify:
- Typing "q" in filter input adds "q" to the filter query
- Typing "q" in viewer search input adds "q" to the search query
- Pressing Ctrl+C still quits from any state
- Pressing "q" outside input fields still quits normally
- Run `go test ./tui/...` to verify nothing breaks

Add a unit test in tui/model_test.go that verifies: when filterActive is true and a "q" KeyPressMsg is sent, the model does NOT set quitting=true and the filterQuery contains "q".
```

---

## 🐛 Bug #2 — Directory Metadata Lost During Layer Stacking

### Description

When a subsequent layer modifies directory metadata (permissions, UID, GID) without modifying its children (e.g., `RUN chmod 0777 /app`), the stacked tree silently retains the old metadata from the earlier layer.

### Root Cause

In `image/stack.go`, the `mergeLayer` function initializes the merged node entirely from `cumulative`:

```go
// Line 38-47 in image/stack.go
merged := &FileNode{
    Name:              cumulative.Name,
    Mode:              cumulative.Mode,  // ← Bug: ignores layerRoot.Mode
    UID:               cumulative.UID,   // ← Bug: ignores layerRoot.UID
    GID:               cumulative.GID,   // ← Bug: ignores layerRoot.GID
    IntroducedInLayer: cumulative.IntroducedInLayer,
}
```

When both `cChild.IsDir && lChild.IsDir`, the recursive `mergeLayer` call copies metadata from the old (cumulative) node, completely discarding any new metadata from the current layer.

### Reproduction

Build an image:
```dockerfile
FROM alpine
RUN mkdir /app && chmod 0755 /app
RUN chmod 0777 /app
```
Inspect with layerx → Layer 2's `/app` shows `drwxr-xr-x` (0755) instead of `drwxrwxrwx` (0777).

### Fix Location

`image/stack.go` — `mergeLayer()` function, lines 37-47 and lines 83-84.

---

### Claude Code Prompt

```
Fix a bug in layerx where directory metadata (permissions, UID, GID) changes are silently lost during layer stacking.

Root cause: In image/stack.go, the mergeLayer() function (line ~37) always initializes the merged FileNode with metadata from the `cumulative` node. When both the cumulative and layer nodes are directories (line ~83: `cChild.IsDir && lChild.IsDir`), the recursive call to mergeLayer passes the old cumulative metadata forward, ignoring any metadata changes from the new layer.

Fix: In the mergeLayer function, after the merged node is created from cumulative's metadata, check whether the layerRoot has different metadata (Mode, UID, GID). The key insight is: the ROOT of the merged node (the directory being merged) should check if layerRoot itself has updated metadata.

Specifically, in the `mergeLayer` function, after creating the merged node from cumulative at line ~38, add a check: if the layerRoot's metadata (Mode, UID, GID) differs from cumulative's, update the merged node to use layerRoot's values, set DiffType to Modified, and update IntroducedInLayer to layerIdx.

Additionally, when recursing for matching directories (line ~83-84: `if cChild.IsDir && lChild.IsDir`), the mergeLayer call already handles this recursively because the lChild becomes the new layerRoot in the recursive call. But we need to make sure the TOP-LEVEL merged node in mergeLayer also picks up metadata changes. So after creating `merged` at line 38, compare `cumulative.Mode != layerRoot.Mode || cumulative.UID != layerRoot.UID || cumulative.GID != layerRoot.GID` — if true, update merged's Mode/UID/GID from layerRoot and set IntroducedInLayer to layerIdx.

Files to modify:
- image/stack.go — mergeLayer() function

Add unit tests in image/stack_test.go that verify:
1. Create two layers: Layer 0 adds /app dir with mode 0755, UID 0, GID 0. Layer 1 has /app dir with mode 0777, UID 1000, GID 1000 (and no child changes).
2. Stack them and verify the stacked tree at layer 1 shows /app with mode 0777, UID 1000, GID 1000, and DiffType=Modified.

Run `go test ./image/...` after the fix.
```

---

## 🐛 Bug #3 — File Viewer Shows Wrong File Version (Always Final Layer)

### Description

When viewing or extracting a file from an intermediate layer, the extractor creates a temporary container from the final built image and copies the file from that container. This means you always see the file as it exists in the final layer, not the selected layer.

### Root Cause

In `image/extractor.go`, both `Extract()` and `ExtractRaw()` use `ContainerCreate` with the full `imageRef` (the final image). In `tui/model.go`, `fetchFileContent` and `fetchFileRaw` pass `m.imageRef` (the original image reference) — there is no concept of "which layer" to extract from.

```go
// Line 89-91 in image/extractor.go
func (e *DockerExtractor) Extract(ctx context.Context, imageRef string, filePath string) (*FileContent, error) {
    createResult, err := e.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
        Image: imageRef,  // ← Always uses the final image
    })
```

### Reproduction

1. Build an image where Layer 1 creates `/tmp/config.txt` with content "v1", Layer 2 overwrites it with "v2"
2. Open layerx, select Layer 1, navigate to `/tmp/config.txt`, press Enter
3. The viewer shows "v2" (the final version) instead of "v1"

### Fix Location

This is an architectural improvement. The extractor should extract from the layer tar directly instead of spawning containers.

---

### Claude Code Prompt

```
Fix a fundamental correctness bug in layerx's file viewer: when browsing an intermediate layer, pressing Enter to view a file always shows the file's content from the FINAL image, not the selected layer.

Root cause: In image/extractor.go, the Extract() and ExtractRaw() methods create a temporary Docker container from the full imageRef (the final image) and use `CopyFromContainer` to read the file. There is no way to specify which layer to read from. In tui/model.go, fetchFileContent() and fetchFileRaw() always pass m.imageRef.

The fix requires a different extraction strategy. Instead of spawning containers, extract files directly from the layer tar data. Since the image tar is already parsed during analysis, we need to either:

Option A (simpler, recommended): Re-export the image tar and scan for the file in the correct layer blob. This is slower but doesn't require caching the raw tar data in memory.

Option B: During the initial parse in image/docker.go parseLayers(), save the raw layer tar data (or the paths to access them) so the extractor can read from a specific layer later.

For this fix, implement Option A with these changes:

1. In image/extractor.go, add a new method to the Extractor interface:
   `ExtractFromLayer(ctx context.Context, imageRef string, filePath string, layerIndex int) (*FileContent, error)`

2. In DockerExtractor, implement ExtractFromLayer:
   - Call cli.ImageSave to get the image tar
   - Parse the manifest to find the layer paths
   - Open the tar entry for the specified layer (manifest.Layers[layerIndex])
   - Decompress if gzip (reuse the existing decompressIfGzip helper)
   - Walk the inner layer tar to find the target file path
   - Read and return its contents using processContent()

3. In tui/model.go, update fetchFileContent() to pass m.layerCursor (the currently selected layer index) and call ExtractFromLayer instead of Extract. The viewer should show the file from the SELECTED layer, but if the file was introduced in an earlier layer (f.IntroducedInLayer), use that layer index instead.

4. Similarly update fetchFileRaw() for the extract-to-disk feature.

5. Keep the existing Extract() and ExtractRaw() methods working (don't break them) for backwards compatibility.

Files to modify:
- image/extractor.go — add ExtractFromLayer method
- tui/model.go — update fetchFileContent and fetchFileRaw to pass layer index

Add a test in image/extractor_test.go that verifies ExtractFromLayer returns the correct file content for a specific layer index.

Run `go test ./...` after the fix.
```

---

## 🐛 Bug #4 — Waste Overlay Has No Scrolling

### Description

The waste overlay (`w` key) supports cursor movement with `j/k` keys, but has no scroll offset tracking. When the cursor moves past the visible area, it goes off-screen and the user navigates blind.

### Root Cause

In `tui/waste.go`, `handleWasteOverlay` increments `m.wasteCursor` but there is no `wasteOffset` field in the model. The `renderWasteOverlay` function renders all rows starting from index 0 every time — it never shifts the view window based on cursor position.

### Reproduction

1. Open layerx on an image with 50+ wasted files
2. Press `w` to open waste overlay → press `a` to expand
3. Press `j` repeatedly 30+ times
4. Cursor disappears below the visible panel

### Fix Location

`tui/model.go` (add `wasteOffset` field), `tui/waste.go` (add scroll logic to render and handle functions).

---

### Claude Code Prompt

```
Fix a bug in the layerx waste overlay where the cursor can move off-screen because there is no scroll offset tracking.

Root cause: In tui/waste.go, handleWasteOverlay() increments m.wasteCursor when j/k is pressed, but there is no wasteOffset field in the model struct (tui/model.go). The renderWasteOverlay() function always renders rows starting from index 0.

Fix with these specific changes:

1. In tui/model.go, add a `wasteOffset int` field to the model struct (near wasteCursor on line ~149).

2. In tui/model.go, update closeWaste() to also reset wasteOffset to 0:
   ```go
   func (m *model) closeWaste() {
       m.showWaste = false
       m.wasteRows = nil
       m.wasteCursor = 0
       m.wasteOffset = 0
       m.wasteExpanded = false
   }
   ```

3. In tui/model.go, update openWaste() to also reset wasteOffset to 0.

4. In tui/waste.go, add an adjustWasteScroll method:
   ```go
   func (m *model) adjustWasteScroll(visibleHeight int) {
       if visibleHeight <= 0 {
           return
       }
       if m.wasteCursor < m.wasteOffset {
           m.wasteOffset = m.wasteCursor
       }
       if m.wasteCursor >= m.wasteOffset+visibleHeight {
           m.wasteOffset = m.wasteCursor - visibleHeight + 1
       }
   }
   ```

5. In tui/waste.go, update handleWasteOverlay() — after every cursor change (Down, Up, Top, Bottom), call m.adjustWasteScroll(wasteVisibleHeight) where wasteVisibleHeight is calculated from m.height minus the chrome rows used by the overlay (title, header, footer, padding — approximately m.height - 10, but compute it properly based on the actual overlay layout).

6. In tui/waste.go, update renderWasteOverlay() — when rendering the row list (the `for i, r := range rows` loop around line 231), slice the rows using wasteOffset:
   ```go
   visibleHeight := ... // compute based on available space
   end := m.wasteOffset + visibleHeight
   if end > len(rows) {
       end = len(rows)
   }
   visibleRows := rows[m.wasteOffset:end]
   for i, r := range visibleRows {
       lines = append(lines, formatWasteRow(r, m.wasteOffset+i == m.wasteCursor, innerWidth))
   }
   ```

7. Also update the "a" toggle (expand/collapse) to reset wasteOffset to 0 alongside wasteCursor.

Files to modify:
- tui/model.go — add wasteOffset field, reset in openWaste/closeWaste
- tui/waste.go — add adjustWasteScroll, update handleWasteOverlay and renderWasteOverlay

Run `go test ./tui/...` after the fix.
```

---

## 🐛 Bug #5 — CI Command Calls `os.Exit(1)` Directly

### Description

In `cmd/ci.go`, when CI checks fail, the code calls `os.Exit(1)` directly inside a Cobra `RunE` handler, bypassing Go's deferred cleanup and making the CI command impossible to unit test.

### Root Cause

```go
// Lines 85-87 in cmd/ci.go
if report.ExitCode() != 0 {
    os.Exit(1)
}
```

### Reproduction

Not a TUI bug — this is a code quality / testability issue. Writing a test that calls `executeCICheck` with a failing image will kill the entire test process.

### Fix Location

`cmd/ci.go`, `main.go`

---

### Claude Code Prompt

```
Fix a code quality bug in layerx where the CI command calls os.Exit(1) directly inside a Cobra RunE handler, bypassing deferred cleanup and making the command untestable.

Root cause: In cmd/ci.go, the executeCICheck function (line ~85) calls os.Exit(1) when CI checks fail, instead of returning an error. This kills the process immediately, skipping all defer statements and preventing unit tests from testing failure paths.

Fix:

1. In cmd/ci.go, define a sentinel error type:
   ```go
   // ErrCIFailed is returned when CI checks do not pass.
   type ErrCIFailed struct{}

   func (e *ErrCIFailed) Error() string {
       return "CI check failed"
   }
   ```

2. In cmd/ci.go, change executeCICheck to return the error instead of calling os.Exit:
   Replace:
   ```go
   if report.ExitCode() != 0 {
       os.Exit(1)
   }
   return nil
   ```
   With:
   ```go
   if report.ExitCode() != 0 {
       return &ErrCIFailed{}
   }
   return nil
   ```

3. In main.go, the existing code already handles this correctly:
   ```go
   if err := cmd.Execute(); err != nil {
       os.Exit(1)
   }
   ```
   Cobra's RunE will propagate the error up, and main.go will call os.Exit(1). No changes needed in main.go.

4. Verify that the root command's CI=true path (cmd/root.go line ~61) also works correctly since it calls the same executeCICheck function.

Files to modify:
- cmd/ci.go — remove os.Exit(1), add ErrCIFailed type, return error instead

Do NOT modify main.go. The existing error handling there already covers this.

Run `go test ./...` after the fix.
```

---

## 🐛 Bug #6 — JSON Export Missing `schemaVersion`

### Description

The project's own rules in CLAUDE.md state: *"JSON export has `schemaVersion`"*. But the actual `jsonExport` struct in `cmd/json.go` has no `SchemaVersion` field. This is a contract violation.

### Root Cause

The `jsonExport` struct (line 12-18 in `cmd/json.go`) never had the field added:

```go
type jsonExport struct {
    ImageRef   string         `json:"imageRef"`
    // ... no schemaVersion field
}
```

### Fix Location

`cmd/json.go`

---

### Claude Code Prompt

```
Fix a contract violation in layerx: the JSON export is missing the schemaVersion field that CLAUDE.md requires.

CLAUDE.md states: "JSON export has schemaVersion; do not break flags/JSON without a MAJOR version plan."

But cmd/json.go's jsonExport struct (line ~12) has no SchemaVersion field.

Fix:

1. In cmd/json.go, add SchemaVersion as the FIRST field in the jsonExport struct:
   ```go
   type jsonExport struct {
       SchemaVersion int            `json:"schemaVersion"`
       ImageRef      string         `json:"imageRef"`
       TotalSize     int64          `json:"totalSize"`
       LayerCount    int            `json:"layerCount"`
       Efficiency    jsonEfficiency `json:"efficiency"`
       Layers        []jsonLayer    `json:"layers"`
   }
   ```

2. In cmd/json.go, in the buildJSONExport function, set SchemaVersion to 1:
   ```go
   export := &jsonExport{
       SchemaVersion: 1,
       ImageRef:      analysis.ImageRef,
       ...
   }
   ```

3. Update the existing test in cmd/json_test.go to verify the schemaVersion field is present and equals 1 in the JSON output.

Files to modify:
- cmd/json.go — add field and set value
- cmd/json_test.go — update test assertions

Run `go test ./cmd/...` after the fix.
```

---

## 🐛 Bug #7 — `padRight`/`padLeft` Use Byte Length Instead of Rune Length

### Description

The `padRight` and `padLeft` helper functions in `tui/filetree.go` use `len(s)` (byte count) instead of `len([]rune(s))` (character count). This causes column misalignment and potential mid-character truncation with multi-byte Unicode filenames.

### Root Cause

```go
// Lines 333-338 in tui/filetree.go
func padRight(s string, width int) string {
    if len(s) >= width {     // ← byte count, not rune count
        return s[:width]     // ← can slice mid-character
    }
    return s + strings.Repeat(" ", width-len(s))
}
```

The codebase already has a correct implementation in `tui/layers.go`:
```go
func padLeftRunes(s string, width int) string {
    n := len([]rune(s))  // ← correct: rune count
    ...
}
```

### Fix Location

`tui/filetree.go` — `padRight()` and `padLeft()` functions.

---

### Claude Code Prompt

```
Fix a bug in layerx where the padRight and padLeft helper functions in tui/filetree.go use byte length instead of rune length, causing column misalignment and potential mid-character truncation with multi-byte Unicode filenames (e.g., CJK characters in container filesystems).

Root cause: In tui/filetree.go, both padRight (line ~333) and padLeft (line ~340) use len(s) which counts bytes, not characters. For multi-byte UTF-8 strings, this gives wrong measurements. The codebase already has a correct implementation — padLeftRunes in tui/layers.go (line ~156) uses len([]rune(s)).

Fix both functions in tui/filetree.go:

Change padRight from:
```go
func padRight(s string, width int) string {
    if len(s) >= width {
        return s[:width]
    }
    return s + strings.Repeat(" ", width-len(s))
}
```

To:
```go
func padRight(s string, width int) string {
    runes := []rune(s)
    if len(runes) >= width {
        return string(runes[:width])
    }
    return s + strings.Repeat(" ", width-len(runes))
}
```

Change padLeft from:
```go
func padLeft(s string, width int) string {
    if len(s) >= width {
        return s[:width]
    }
    return strings.Repeat(" ", width-len(s)) + s
}
```

To:
```go
func padLeft(s string, width int) string {
    runes := []rune(s)
    if len(runes) >= width {
        return string(runes[:width])
    }
    return strings.Repeat(" ", width-len(runes)) + s
}
```

Files to modify:
- tui/filetree.go — padRight and padLeft functions only

Run `go test ./tui/...` after the fix.
```

---

## 🔧 Improvement #8 — Memory-Efficient Streaming Layer Parsing

### Description

`image/docker.go`'s `parseLayers()` function calls `io.ReadAll` on every single layer blob, storing all raw bytes in an in-memory map before building file trees. Inspecting a 3GB image loads 3GB+ into RAM.

### Root Cause

```go
// Lines 223-229 in image/docker.go
case strings.HasSuffix(hdr.Name, "/layer.tar"):
    data, err := io.ReadAll(tr)     // ← Entire layer loaded into RAM
    ...
    blobs[hdr.Name] = data          // ← Stored in memory map
```

### Fix Location

`image/docker.go` — `parseLayers()` function.

---

### Claude Code Prompt

```
Improve memory efficiency in layerx by processing layer tar data in a streaming fashion instead of loading all layer blobs into memory.

Current behavior: In image/docker.go, parseLayers() (line ~184) reads every layer blob into memory using io.ReadAll(tr) and stores them in a blobs map. For a 3GB image, this means 3GB+ of RAM usage.

The fix should process layer tars inline as they are encountered during the outer tar scan, since we only need file metadata (names, sizes, modes) to build the FileTree — not the actual file contents.

Changes to image/docker.go parseLayers() function:

1. Instead of storing layer tar data in `blobs[hdr.Name] = data`, process it immediately when encountered:

For entries matching `strings.HasSuffix(hdr.Name, "/layer.tar")`:
- Call ParseLayerTar directly using the tar reader: `tree, _ := ParseLayerTar(tr)`
- Store only the tree: `layerTrees[hdr.Name] = tree`
- Do NOT call io.ReadAll — let ParseLayerTar read from tr directly

For entries matching `strings.HasPrefix(hdr.Name, "blobs/sha256/")`:
- These are OCI format layers which may be gzip-compressed
- Read the first 2 bytes to check for gzip magic (0x1f, 0x8b)
- If gzip: create a gzip.Reader wrapping the tar reader, then call ParseLayerTar on it
- If not gzip: call ParseLayerTar directly on the tar reader
- Store only the resulting tree, not the raw bytes
- Note: you need to be careful here because you can't seek back after reading 2 bytes. Use io.MultiReader to prepend the peeked bytes back.

2. The metadata files (manifest.json, config .json files) are small and should still be read with io.ReadAll into the `contents` map — don't change that part.

3. In the layer assembly loop at the bottom (line ~275), instead of:
   ```go
   if tarData, ok := blobs[layerPath]; ok && len(tarData) > 0 {
       r, err := decompressIfGzip(tarData)
       if err == nil {
           tree, _ := ParseLayerTar(r)
           layers[i].Tree = tree
       }
   }
   ```
   Change to:
   ```go
   if tree, ok := layerTrees[layerPath]; ok {
       layers[i].Tree = tree
   }
   ```

4. Remove the `blobs` map for layer data entirely. Keep it only if needed for config resolution (OCI format stores config in blobs/sha256/).

Important: ParseLayerTar in image/tree_parser.go already accepts an io.Reader and reads entries sequentially with tr.Next(). It does NOT need the entire blob in memory. It only reads tar headers and discards file content (it never calls io.ReadAll on file entries within the layer tar — it just reads metadata like name, size, mode).

Wait — actually, check tree_parser.go carefully. ParseLayerTar creates a new tar.NewReader(r) and only calls tr.Next() in a loop reading headers. It does NOT read file content bodies. So passing the tar reader directly should work because tar.Reader.Next() automatically skips unread content from the previous entry.

Files to modify:
- image/docker.go — parseLayers() function

Do NOT modify:
- image/tree_parser.go
- image/extractor.go

Run `go test ./image/...` after the fix. The existing tests in image/docker_test.go should still pass since the output ([]Layer with Trees) is identical.
```

---

## 🔧 Improvement #9 — Invisible Search Matches on Truncated Long Lines

### Description

In the file viewer, long lines are truncated at the terminal width. If a search match is beyond the visible portion, the status bar says "Match found" but the highlighted text is invisible because it was cut off.

### Root Cause

In `tui/fileview.go` (line ~104), lines are truncated:
```go
if len([]rune(line)) > maxLineWidth {
    line = string([]rune(line)[:maxLineWidth-1]) + "…"
}
```
There is no horizontal scroll offset to bring off-screen matches into view.

### Fix Location

`tui/fileview.go`, `tui/model.go`

---

### Claude Code Prompt

```
Improve the layerx file viewer so that search matches on long lines that are truncated are always visible by auto-scrolling horizontally.

Current behavior: In tui/fileview.go (line ~104), lines longer than the terminal width are truncated with "…". If a search match is at column 150 but the terminal is 80 columns wide, the match is invisible even though the status bar says it was found.

Fix by adding a horizontal scroll offset to the viewer:

1. In tui/model.go, add a `viewHorizontalOffset int` field to the model struct (near viewOffset on line ~137).

2. In tui/model.go, reset viewHorizontalOffset to 0 when:
   - Opening a new file (in the fileContentMsg handler, line ~271)
   - Closing the viewer (Esc handler, line ~310)

3. In tui/model.go, update scrollToViewerMatch() to also compute the horizontal offset. After setting the vertical offset, check the column position of the current match:
   ```go
   matchCol := m.viewSearchMatches[m.viewSearchCursor][1]
   maxLineWidth := m.width - gutterWidth - 3  // approximate
   if matchCol < m.viewHorizontalOffset {
       m.viewHorizontalOffset = matchCol
   } else if matchCol+len(m.viewSearchQuery) > m.viewHorizontalOffset+maxLineWidth {
       m.viewHorizontalOffset = matchCol - maxLineWidth/2
   }
   if m.viewHorizontalOffset < 0 {
       m.viewHorizontalOffset = 0
   }
   ```

4. In tui/fileview.go, in the renderFileView function, before truncating lines (line ~104), apply the horizontal offset:
   ```go
   lineRunes := []rune(line)
   hOffset := p.horizontalOffset  // add this to viewerParams
   if hOffset > 0 && hOffset < len(lineRunes) {
       lineRunes = lineRunes[hOffset:]
       line = string(lineRunes)
   } else if hOffset >= len(lineRunes) {
       line = ""
   }
   // Then apply truncation as before
   if len([]rune(line)) > maxLineWidth {
       line = string([]rune(line)[:maxLineWidth-1]) + "…"
   }
   ```

5. Add `horizontalOffset int` to the viewerParams struct and pass m.viewHorizontalOffset from viewReady().

6. When no search is active and the user navigates vertically, reset viewHorizontalOffset to 0 so the default view stays left-aligned.

Files to modify:
- tui/model.go — add viewHorizontalOffset field, update scrollToViewerMatch, pass to viewerParams
- tui/fileview.go — add horizontalOffset to viewerParams, apply offset before truncation

Run `go test ./tui/...` after the fix.
```

---

## 🔧 Improvement #10 — `FindChild` Linear Scan → O(1) Map Lookup

### Description

`FileNode.FindChild()` does a linear scan through all children for every lookup. This is called heavily during layer stacking and tar parsing, causing O(n²) behavior on directories with many entries (e.g., `node_modules` with 1000+ files).

### Root Cause

```go
// Lines 40-47 in image/filetree.go
func (n *FileNode) FindChild(name string) *FileNode {
    for _, c := range n.Children {
        if c.Name == name {
            return c
        }
    }
    return nil
}
```

### Fix Location

`image/filetree.go`

---

### Claude Code Prompt

```
Improve layerx performance by replacing the linear-scan FindChild with an O(1) map lookup on FileNode.

Current behavior: In image/filetree.go, FindChild() (line ~40) loops through all children sequentially. This is called heavily during layer stacking (image/stack.go mergeLayer) and tar parsing (image/tree_parser.go insertNode). For directories with 1000+ entries, each lookup is O(n), causing O(n²) total.

Fix:

1. In image/filetree.go, add a private map field to FileNode:
   ```go
   type FileNode struct {
       Name              string
       Path              string
       Size              int64
       Mode              fs.FileMode
       UID               int
       GID               int
       DiffType          DiffType
       IntroducedInLayer int
       Children          []*FileNode
       IsDir             bool
       childIndex        map[string]*FileNode  // private, lazy-init
   }
   ```

2. Update AddChild to maintain the index:
   ```go
   func (n *FileNode) AddChild(child *FileNode) {
       n.Children = append(n.Children, child)
       if n.childIndex == nil {
           n.childIndex = make(map[string]*FileNode)
       }
       n.childIndex[child.Name] = child
   }
   ```

3. Update FindChild to use the index:
   ```go
   func (n *FileNode) FindChild(name string) *FileNode {
       if n.childIndex != nil {
           return n.childIndex[name]
       }
       for _, c := range n.Children {
           if c.Name == name {
               return c
           }
       }
       return nil
   }
   ```

4. Update RemoveChild to maintain the index:
   ```go
   func (n *FileNode) RemoveChild(name string) bool {
       for i, c := range n.Children {
           if c.Name == name {
               n.Children = append(n.Children[:i], n.Children[i+1:]...)
               delete(n.childIndex, name)
               return true
           }
       }
       return false
   }
   ```

5. IMPORTANT: The childIndex field is private (lowercase) so it won't affect JSON serialization. Make sure the clone functions in image/stack.go (cloneAsUnchanged, cloneAsRemoved, cloneAsAdded, cloneStructure) do NOT copy the childIndex — let it be rebuilt naturally through AddChild calls, which they already use.

Files to modify:
- image/filetree.go — FileNode struct, FindChild, AddChild, RemoveChild

Do NOT modify: image/stack.go (the clone functions already use AddChild which will build the index automatically).

Run `go test ./image/...` after the fix. All existing tests should pass unchanged.
```

---

## 🔧 Improvement #11 — Cache `displayTree()` to Avoid Redundant Recomputation

### Description

`displayTree()` is called multiple times per render frame — it flattens, filters, and sorts the entire tree from scratch each time. On images with 10,000+ files, this causes noticeable input lag.

### Root Cause

In `tui/model.go`, `displayTree()` (line ~778) is called from `viewReady()` (line ~1071), from `moveDown()`, `moveUp()`, `clampCursors()`, and other methods. Each call does a full tree walk, filter, and sort.

### Fix Location

`tui/model.go`

---

### Claude Code Prompt

```
Improve layerx TUI performance by caching the displayTree() result to avoid redundant recomputation on every render frame and keypress.

Current behavior: In tui/model.go, displayTree() (line ~778) is called multiple times per render cycle. Each call walks the entire tree, applies filters, and sorts. On large images with 10,000+ files this causes input lag.

Fix:

1. In tui/model.go, add cache fields to the model struct:
   ```go
   cachedDisplayFiles  []*image.FileNode
   displayCacheValid   bool
   ```

2. Create a helper that invalidates the cache:
   ```go
   func (m *model) invalidateDisplayCache() {
       m.displayCacheValid = false
       m.cachedDisplayFiles = nil
   }
   ```

3. Update displayTree() to use the cache:
   ```go
   func (m model) displayTree() []*image.FileNode {
       if m.displayCacheValid && m.cachedDisplayFiles != nil {
           return m.cachedDisplayFiles
       }
       // ... existing logic unchanged ...
       return files
   }
   ```
   
   But since model is a value receiver, we need a different approach. Instead, compute and cache in a pointer-receiver method:
   ```go
   func (m *model) ensureDisplayCache() {
       if m.displayCacheValid {
           return
       }
       // ... existing displayTree logic ...
       m.cachedDisplayFiles = files
       m.displayCacheValid = true
   }
   
   func (m *model) getDisplayTree() []*image.FileNode {
       m.ensureDisplayCache()
       return m.cachedDisplayFiles
   }
   ```

   Keep the existing `displayTree()` method with value receiver for places that can't use a pointer. But update the hot paths (viewReady, renderStatusBar) to use the pointer version.

4. Call invalidateDisplayCache() in every place that changes tree-affecting state:
   - resetTreeForLayerChange() — already a pointer receiver, add invalidateDisplayCache()
   - When diffOnly changes (DiffOnly key handler, line ~535)
   - When sortMode changes (Sort key handler, line ~541)
   - When filterQuery changes (handleFilterInput, line ~596-608)
   - When treeCollapsed changes (toggleCollapsed call, line ~686)
   - When analysis is first loaded (analysisMsg handler, line ~244)

5. In viewReady() (line ~1047), compute the display tree ONCE at the top and pass it to both renderFileTree and renderStatusBar:
   ```go
   treeFiles := m.displayTree()  // this already exists at line 1071
   // ... use treeFiles everywhere, don't call displayTree() again
   ```
   This part is actually already done — treeFiles is computed once and passed to renderStatusBar. But verify no other calls to displayTree() happen during the render.

Files to modify:
- tui/model.go — add cache fields, invalidation, update state-changing methods

Run `go test ./tui/...` after the fix.
```

---

## 🔧 Improvement #12 — Symlink Detection and Display in File Tree

### Description

Docker images commonly contain hundreds of symlinks. Currently, symlinks are silently treated as regular zero-byte files with no indication of their type or target.

### Root Cause

In `image/tree_parser.go` (line ~30), only `tar.TypeDir` is checked. `tar.TypeSymlink` and `tar.TypeLink` are not handled — they fall through and become regular files with no link target information.

### Fix Location

`image/filetree.go`, `image/tree_parser.go`, `tui/filetree.go`

---

### Claude Code Prompt

```
Add symlink support to layerx so symlinks are detected during tar parsing and displayed distinctly in the TUI file tree.

Current behavior: In image/tree_parser.go (line ~30), only tar.TypeDir is checked. Symlinks (tar.TypeSymlink) are treated as regular 0-byte files. Their link targets are lost.

Fix:

1. In image/filetree.go, add a LinkTarget field to FileNode:
   ```go
   type FileNode struct {
       Name              string
       Path              string
       Size              int64
       Mode              fs.FileMode
       UID               int
       GID               int
       DiffType          DiffType
       IntroducedInLayer int
       Children          []*FileNode
       IsDir             bool
       LinkTarget        string  // non-empty for symlinks
   }
   ```

2. In image/tree_parser.go, in ParseLayerTar(), detect symlinks and capture their target:
   After the existing `isDir` check (line ~30), add:
   ```go
   isDir := hdr.Typeflag == tar.TypeDir
   isSymlink := hdr.Typeflag == tar.TypeSymlink
   linkTarget := ""
   if isSymlink {
       linkTarget = hdr.Linkname
   }
   ```

3. In image/tree_parser.go, update the insertNode function signature to accept linkTarget:
   ```go
   func insertNode(root *FileNode, fullPath string, size int64, isDir bool, mode fs.FileMode, uid, gid int, linkTarget string) {
   ```
   And set it on the created node:
   ```go
   node := &FileNode{
       Name:       part,
       Path:       absPath,
       Size:       size,
       IsDir:      isDir,
       Mode:       mode,
       UID:        uid,
       GID:        gid,
       LinkTarget: linkTarget,
   }
   ```
   Also update the existing node path (line ~66-76) to set LinkTarget if provided.

4. In tui/filetree.go, in formatFileNodeLine(), if the node has a non-empty LinkTarget, append " → target" after the display name:
   ```go
   if f.LinkTarget != "" {
       displayName += " → " + f.LinkTarget
   }
   ```
   Place this right after the displayName is set (around line ~178-183 for flat mode, or after line ~196 for tree mode).

5. The permissions string already handles symlinks correctly — FormatMode in image/format.go checks for fs.ModeSymlink and shows 'l' prefix. So `lrwxrwxrwx` will display automatically if the mode has the symlink bit set. Verify that tar headers for symlinks set this bit via hdr.FileInfo().Mode().

6. In image/stack.go, update ALL clone functions (cloneAsUnchanged, cloneAsRemoved, cloneAsAdded, cloneStructure) to copy the LinkTarget field:
   ```go
   clone := &FileNode{
       ...
       LinkTarget:        node.LinkTarget,
   }
   ```

7. In cmd/json.go, add LinkTarget to jsonFile struct (only include in JSON when non-empty):
   ```go
   type jsonFile struct {
       Path       string `json:"path"`
       Size       int64  `json:"size"`
       DiffType   string `json:"diffType"`
       LinkTarget string `json:"linkTarget,omitempty"`
   }
   ```
   And populate it in collectFiles().

Files to modify:
- image/filetree.go — add LinkTarget field
- image/tree_parser.go — detect symlinks, pass linkTarget to insertNode
- image/stack.go — copy LinkTarget in all clone functions
- tui/filetree.go — display " → target" for symlinks
- cmd/json.go — add LinkTarget to JSON output

Add a test in image/tree_parser_test.go that creates a tar with a symlink entry and verifies ParseLayerTar sets LinkTarget correctly.

Run `go test ./...` after the fix.
```

---

## 🔧 Improvement #13 — Graceful Cancellation During Image Loading

### Description

When the TUI is loading an image, pressing `q` tears down the TUI but leaves the background Docker export/pull goroutine running indefinitely because it uses `context.Background()`.

### Root Cause

In `tui/model.go`, `fetchAnalysisWithProgress` (line ~187) and `fetchInspect` (line ~172) use `context.Background()`, which cannot be cancelled. When `tea.Quit` is called during loading, the goroutine continues running.

### Fix Location

`tui/model.go`

---

### Claude Code Prompt

```
Add graceful cancellation to layerx's image loading so that quitting during loading actually stops the background Docker operations instead of leaving them running.

Current behavior: In tui/model.go, fetchAnalysisWithProgress() (line ~187) and fetchInspect() (line ~172) use context.Background(). When the user quits during loading, tea.Quit tears down the TUI but the goroutine continues pulling/exporting the image in the background, wasting CPU, bandwidth, and disk I/O.

Fix:

1. In tui/model.go, add cancellation fields to the model struct:
   ```go
   cancelLoading context.CancelFunc
   loadCtx       context.Context
   ```

2. In NewModel(), create a cancellable context:
   ```go
   func NewModel(cfg Config) model {
       ch := make(chan image.ProgressEvent, 16)
       ctx, cancel := context.WithCancel(context.Background())
       return model{
           state:         stateLoading,
           imageRef:      cfg.ImageRef,
           resolver:      cfg.Resolver,
           progressCh:    ch,
           writeFile:     os.WriteFile,
           keys:          defaultKeys(),
           loadCtx:       ctx,
           cancelLoading: cancel,
       }
   }
   ```

3. Update fetchInspect() to use m.loadCtx instead of context.Background():
   ```go
   func (m model) fetchInspect() tea.Cmd {
       resolver := m.resolver
       imageRef := m.imageRef
       ctx := m.loadCtx
       return func() tea.Msg {
           meta, err := resolver.Inspect(ctx, imageRef)
           return inspectMsg{meta: meta, err: err}
       }
   }
   ```

4. Update fetchAnalysisWithProgress() similarly:
   ```go
   func (m model) fetchAnalysisWithProgress(progressCh chan<- image.ProgressEvent) tea.Cmd {
       resolver := m.resolver
       imageRef := m.imageRef
       ctx := m.loadCtx
       return func() tea.Msg {
           defer close(progressCh)
           result, err := image.AnalyzeWithProgress(ctx, resolver, imageRef, progressCh)
           return analysisMsg{analysis: result, err: err}
       }
   }
   ```

5. In the Update() method, when quitting during the loading state, call the cancel function:
   In the Escape handler (line ~336) and Quit handler (line ~342), before setting m.quitting:
   ```go
   if m.cancelLoading != nil {
       m.cancelLoading()
   }
   m.quitting = true
   return m, tea.Quit
   ```

6. Also handle the case where loading returns a context.Canceled error — in the analysisMsg handler (line ~239), don't show an error screen if the context was cancelled and we're already quitting:
   ```go
   case analysisMsg:
       if msg.err != nil {
           if m.quitting {
               return m, nil  // already quitting, ignore cancelled error
           }
           m.state = stateError
           m.errMsg = friendlyError(msg.err)
           return m, nil
       }
   ```

Files to modify:
- tui/model.go — add context fields, update NewModel, fetchInspect, fetchAnalysisWithProgress, quit handlers, analysisMsg handler

Run `go test ./tui/...` after the fix.
```

---

> [!TIP]
> **Recommended fix order:** ~~#1~~ → #5 → #6 → #7 → #2 → #4 → #13 → #10 → #11 → #3 → #12 → #9 → #8
>
> Start with the quick wins (#5, #6, #7 — all under 10 lines each), then the medium domain fixes (2, 4, 13), then the larger architectural improvements (10, 11, 3, 12, 9, 8).
