# Switching from Dive to LayerX

Dive taught most of us how to think about container image efficiency. If you've used it, you already know the workflow — layer panel on the left, file tree on the right, wasted-bytes score at the top. LayerX keeps all of that and fills in the gaps that come up in day-to-day use.

This guide covers the highlights that matter most to Dive users. For the complete feature set — JSON export, signed releases, waste navigator, clipboard integration, cache management, and more — see the [full README](../README.md).

---

## The one-minute version

```
# Everything you already type still works
dive nginx:latest       →    layerx nginx:latest

# Archives: no flag needed anymore
dive --source docker-archive app.tar    →    layerx ./app.tar

# Podman: works on Linux, macOS, and Windows now
dive --source podman IMAGE    →    layerx --engine podman IMAGE
```

That's it for the basics. The rest is things LayerX can do that Dive can't.

---

## What's actually different

### You can read files without leaving the terminal

This is the change most Dive users notice first.

In Dive, finding a suspicious file means navigating to it, then exiting the tool, running `docker cp`, opening the file in your editor, and coming back. In LayerX, you press `Enter`.

```
Navigate to any file  →  Enter  →  read it in place
                                    search with /
                                    extract to disk with x
```

Syntax highlighting, line numbers, in-file search. No round trip.

---

### Comparison: where each tool stops

| Workflow                              | Dive      | LayerX    |
|---------------------------------------|-----------|-----------|
| Browse layers and files interactively | ✅        | ✅        |
| Efficiency score and wasted bytes     | ✅        | ✅        |
| Build and inspect in one command      | ✅        | ✅        |
| CI gate on efficiency thresholds      | ✅        | ✅        |
| Read a file's contents in the TUI     | ❌        | ✅        |
| Search inside a file                  | ❌        | ✅        |
| Extract a file to disk                | ⚠️ limited | ✅       |
| Jump to worst wasted files instantly  | ❌        | ✅ (`w`)  |
| Compare two images for regressions    | ❌        | ✅        |
| Gate CI on specific file paths/globs  | ❌        | ✅        |
| Select a specific platform variant    | ❌        | ✅        |
| Analysis cache (fast repeat runs)     | ❌        | ✅        |
| Podman on Windows                     | ❌        | ✅        |
| OCI-layout archives as a source       | ❌        | ✅        |

---

### Catching regressions between builds

Dive's CI mode answers: *"is this image good enough?"*  
LayerX adds a second question: *"is this build worse than the last one?"*

```bash
layerx compare myapp:1.5.0 myapp:1.5.1
```

Every run ends with one line you can grep from any pipeline:

```
verdict: ok
verdict: regression  reason=efficiency,waste
verdict: noop        digest=sha256:...
```

Useful when absolute thresholds aren't catching the drift — a slowly degrading image that never crosses the line.

---

### Gate on what's *in* your image, not just how big it is

Dive checks three numbers. LayerX lets you write rules about what those layers actually contain.

```yaml
# .layerx.yaml
path-rules:
  block:
    - "**/.git/**"          # fail if .git ever lands in a layer
    - "/tmp/**"             # even if a later layer deletes it
  deny-waste:
    - "**/*.pyc"            # fail if .pyc appears in more than one layer
```

Run `layerx init --flavour python` (or `node`, `java`, `go`, `generic`) to get a starter config for your stack.

---

### Repeat runs are instant

Dive re-parses every image on every run. LayerX caches the analysis per image digest. The second run against the same image is nearly instant.

```bash
layerx --no-cache nginx:latest   # force a fresh parse
layerx cache list                # see what's cached
layerx cache prune --older-than 30d
```

---

### Inspect the exact platform variant your production runs

Most images on Docker Hub are multi-platform — `nginx:latest` contains separate builds for `linux/amd64`, `linux/arm64`, `linux/arm/v7`, and more. Dive always inspects whichever variant the local daemon defaults to. On an Apple Silicon Mac that's `arm64`; in your CI runner it's `amd64`. If you're optimising for production, those are different images.

LayerX lets you pin the variant explicitly:

```bash
# Inspect the amd64 build, even on an Apple Silicon Mac
layerx --platform linux/amd64 nginx:latest

# Check the arm64 variant your Raspberry Pi cluster runs
layerx --platform linux/arm64 myapp:latest

# Gate CI against the exact variant that ships
layerx ci --platform linux/amd64 --lowest-efficiency 0.9 myapp:${GIT_SHA}

# Compare the arm64 and amd64 variants of the same image using two separate inspect runs
layerx --platform linux/arm64 myapp:1.5.0
layerx --platform linux/amd64 myapp:1.5.0
```

`--platform` works on every subcommand — `layerx`, `layerx ci`, `layerx compare`, and `--json` — and on archives too. If the platform you asked for isn't in the image, LayerX tells you which variants are actually there instead of silently inspecting the wrong one.

---

> **That covers the highlights.** LayerX also ships JSON export with a versioned schema, clipboard integration (OSC 52 — works over SSH and inside tmux), per-layer net-delta columns, a synchronized split-pane view, shell completion, and signed releases with SBOM and SLSA provenance. The [README](../README.md) has the full picture.

---

## Shortcuts: what moved

Most keys are the same. A few changed.

| Action                    | Dive             | LayerX     |
|---------------------------|------------------|------------|
| Filter files              | `Ctrl+F`         | `/`        |
| Toggle diff view          | `Ctrl+A/R/M/U`   | `d`        |
| Aggregated view           | `Ctrl+A` (layer) | `A`        |
| Collapse directory        | `Space`          | `Enter`    |
| Jump to top / bottom      | —                | `g` / `G`  |
| Open file viewer          | —                | `Enter`    |
| Extract file to disk      | —                | `x`        |
| Copy path to clipboard    | —                | `y`        |
| Worst wasted files        | —                | `w`        |
| Help                      | —                | `?`        |

Press `?` at any time for the full keymap.

---

## Config file

Dive reads `.dive-ci`. LayerX reads `.layerx.yaml`.

The threshold keys are the same shape:

```yaml
# .layerx.yaml
rules:
  lowest-efficiency: 0.9
  highest-wasted-bytes: 52428800   # bytes, not "50MB"
  highest-user-wasted-percent: 0.1
```

The main difference: `highest-wasted-bytes` takes a plain integer (bytes), not a humanized string. Run `layerx init` to generate a commented starter.

---

## Install

```bash
# macOS / Linux
brew install deveshctl/tap/layerx

# Windows
scoop bucket add layerx https://github.com/deveshctl/scoop-bucket
scoop install layerx

# Debian / Ubuntu
curl -LO https://github.com/deveshctl/layerx/releases/latest/download/layerx_linux_amd64.deb
sudo dpkg -i layerx_linux_amd64.deb

# From source (Go 1.26+)
go install github.com/deveshctl/layerx@latest
```

---

## Common questions

**Can I keep using Dive?**  
Yes. They don't share config, cache, or state. Install both and switch when you're ready.

**Will my CI scripts break?**  
Not if you migrate the config file (see above). The `CI=true` env var works identically. Exit codes are the same for pass/fail — LayerX adds a third code (`2`) for operational errors like a daemon being down.

**Does it work with Podman?**  
Yes, on Linux, macOS, and Windows. If you've run `podman system connection default <name>`, LayerX picks that up automatically — no `DOCKER_HOST=...` workaround needed.

**Does it work without a daemon?**  
Yes. Pass a file path and LayerX reads the archive directly: `docker save`, `podman save`, and OCI-layout tarballs all work. No daemon, no network, no root.

**I'm on a team. Is this safe to roll out?**  
Every release ships a signed checksum file, a software bill of materials (SBOM), and provenance that proves it was built from this repo on GitHub's infrastructure. If your org requires signed tooling, it's already covered.

---

Dive shaped the way a generation of engineers thinks about container images. LayerX is built on the same model — just with the rough edges filed down and a few more tools in the kit.

[Releases](https://github.com/deveshctl/layerx/releases) · [CHANGELOG](../CHANGELOG.md) · [Open an issue](https://github.com/deveshctl/layerx/issues)
