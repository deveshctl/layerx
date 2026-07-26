<!-- docs/theming.md — reference for LayerX TUI colour themes -->

# Theming

LayerX ships 8 built-in colour themes. The theme controls every colour in the
TUI: panel borders, file tree diff colours, the status bar, the header, search
highlights, and syntax highlighting in the file viewer.

---

## Selecting a theme

Three ways to set the theme, in precedence order (highest to lowest):

| Method | How | Scope |
|--------|-----|-------|
| `--theme` flag | `layerx --theme dracula nginx:latest` | One run |
| `theme:` in `.layerx.yaml` | `theme: dracula` | All runs in that directory |
| Built-in default | No action needed | Fallback when neither of the above is set |

The built-in default is `tokyo-night`.

### `--theme` flag

```bash
layerx --theme dracula nginx:latest
layerx --theme gruvbox-dark ./build/app.tar
```

Passing an unrecognised name exits immediately with a clear error listing the
valid options. The flag takes effect even if `theme:` is set in `.layerx.yaml`.

### `theme:` in `.layerx.yaml`

```yaml
version: 1
theme: rose-pine
```

Validated at config load time. An unknown value fails with:

```
.layerx.yaml (theme): unknown theme "my-theme"; valid themes: catppuccin-mocha,
tokyo-night, kanagawa, gruvbox-dark, rose-pine, dracula, oxocarbon, cyberdream
```

Run `layerx init` to get a starter `.layerx.yaml` that includes a commented
`theme:` block with all available values.

---

## Available themes

| Name | Base | Character |
|------|------|-----------|
| `tokyo-night` | Deep blue-grey | Cool blues and lavender — the default |
| `catppuccin-mocha` | Dark mauve | Pastel palette with soft blues and pinks |
| `kanagawa` | Deep indigo | Warm gold and jade inspired by Hokusai |
| `gruvbox-dark` | Dark brown | Retro earthy amber and orange |
| `rose-pine` | Midnight | Dusty rose, mauve, and pine green |
| `dracula` | Near-black | High-contrast purple and cyan |
| `oxocarbon` | Charcoal | IBM Carbon-inspired cyan and teal |
| `cyberdream` | Near-black | Neon synthwave cyan and magenta |

### tokyo-night

```bash
layerx --theme tokyo-night nginx:latest
```

Cool blue-grey base (`#1A1B26`). Blue accent, lavender selection, muted diffs.
Chroma syntax style: `tokyonight-dark`.

### catppuccin-mocha

```bash
layerx --theme catppuccin-mocha nginx:latest
```

The original built-in theme. Dark mauve base (`#1E1E2E`). Pastel blues, greens,
and pinks. Chroma syntax style: `monokai`.

### kanagawa

```bash
layerx --theme kanagawa nginx:latest
```

Deep indigo base (`#1F1F28`). Wave-blue accent, carp-yellow modified, spring-green
added, samurai-red removed. Chroma syntax style: `monokai`.

### gruvbox-dark

```bash
layerx --theme gruvbox-dark nginx:latest
```

Dark brown base (`#282828`). Amber and teal accents, warm earthy palette.
Chroma syntax style: `gruvbox`.

### rose-pine

```bash
layerx --theme rose-pine nginx:latest
```

Midnight base (`#191724`). Iris accent, gold modified, pine-green added,
love-pink removed. Chroma syntax style: `monokai`.

### dracula

```bash
layerx --theme dracula nginx:latest
```

Near-black base (`#282A36`). Purple accent, cyan command colour, high-contrast
green/orange/red diffs. Chroma syntax style: `dracula`.

### oxocarbon

```bash
layerx --theme oxocarbon nginx:latest
```

Charcoal base (`#1E1E1E`). Blue accent, teal command colour, IBM Carbon palette.
Chroma syntax style: `monokai`.

### cyberdream

```bash
layerx --theme cyberdream nginx:latest
```

Near-black base (`#16161F`). Electric cyan accent, neon purple command colour,
neon green/gold/red diffs. Chroma syntax style: `monokai`.

---

## Syntax highlighting

The file viewer (`Enter` on any file) syntax-highlights source code using
[Chroma](https://github.com/alecthomas/chroma). Each theme maps to a matching
Chroma style:

| Theme | Chroma style |
|-------|-------------|
| `tokyo-night` | `tokyonight-dark` |
| `catppuccin-mocha` | `monokai` |
| `kanagawa` | `monokai` |
| `gruvbox-dark` | `gruvbox` |
| `rose-pine` | `monokai` |
| `dracula` | `dracula` |
| `oxocarbon` | `monokai` |
| `cyberdream` | `monokai` |

The Chroma style is initialised once per session from the active theme. It
cannot be changed mid-session without restarting.

---

## Terminal background

Each theme sets the terminal background colour via `tea.WithBackgroundColor`.
This means the header bar, footer bar, and any empty space in the panels all
share the same surface colour as the rest of the UI — no mismatch with the
terminal's own background setting. If your terminal overrides background colours
and the result looks wrong, check whether it has a "force background" option
and disable it for layerx.
