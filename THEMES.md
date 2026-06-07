# Bundled themes

layerx ships several themes adapted from upstream palettes. The hex
values are reproduced directly under the licenses below; full credit
goes to the upstream authors.

## Catppuccin (default, latte, frappe, macchiato)

The `default` theme is Catppuccin Mocha; `latte`, `frappe`, and
`macchiato` are the other three flavors.

- Project: <https://github.com/catppuccin/catppuccin>
- License: MIT

```
MIT License

Copyright (c) 2021 Catppuccin

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, ...
```

(See the upstream LICENSE for the full text.)

## Nord (nord)

- Project: <https://www.nordtheme.com>
- License: MIT
- Author: Sven Greb / Arctic Ice Studio

## minimal

The `minimal` theme uses only the 8 base ANSI colors (and their bright
variants), which means it inherits whatever palette the user has
configured in their terminal. No hex values are reproduced.

`minimal` is the only bundled theme that does not paint its own panel
background — its `Base` token is `lipgloss.NoColor{}`, so panel bodies
show through to the terminal default. Every other theme paints a solid
`Base` surface under panel bodies so the theme's foreground colors
read against the background they were designed for, regardless of the
user's terminal default.
