# Questions and troubleshooting

## What is LayerX Image Inspector?

LayerX Image Inspector is an open-source command-line tool that opens a container image (Docker, Podman, or OCI archive) in an interactive terminal UI. You can browse each layer, see the files it added or removed, view file contents inline, extract any file to disk, and measure image efficiency (wasted bytes across layers). It also runs headless in CI to fail a build when an image drifts past a size or efficiency threshold.

## Which container engines does LayerX support?

Docker and Podman today, with more engines planned. Any daemon exposing the Docker Engine REST API works out of the box (LayerX negotiates the API version, so it doesn't break on engine upgrades). You can also skip the daemon entirely and point LayerX at a `docker save` or OCI-layout archive on disk.

## Do I need Docker installed to use LayerX?

No. If you have a `docker save` output or an OCI-layout tarball, LayerX reads it directly — no daemon, no network, no root. This makes LayerX useful on airgapped machines, in CI runners that don't allow docker-in-docker, and on developer machines that use Podman or rootless containers instead of Docker.

## How does LayerX relate to `dive`?

LayerX is inspired by [wagoodman/dive](https://github.com/wagoodman/dive) and keeps the same mental model. If you're a Dive user, see [Switching from Dive](migrating-from-dive.md).

## How is LayerX different from `docker history` and `docker inspect`?

`docker history` shows *what commands ran* to build the image; `docker inspect` shows *the image's metadata*. Neither shows *what files each layer actually put into the image*. LayerX opens the layer tarballs, walks the file trees, applies whiteouts, and gives you both: the commands and their concrete filesystem impact.

## Can LayerX gate my CI pipeline on image size?

Yes. `layerx ci IMAGE_OR_ARCHIVE` has three configurable thresholds (lowest efficiency, highest wasted bytes, highest user-wasted percent) and clean exit codes (`0` pass, `1` rule failure, `2` operational error) so you can gate a merge with a single line in GitHub Actions, GitLab CI, or any pipeline runner. See [CI integration](ci-integration.md).

## Does LayerX work on Windows natively?

Yes — Windows binaries for amd64 and arm64 are published with every release, and Scoop is a supported install path. LayerX runs against Docker Desktop, Podman Desktop, or archive files directly. WSL is not required.

## Should I use the container image or the native binary?

Native binaries (Homebrew, Scoop, `.deb`, `.rpm`, direct download) are the smoother path for interactive daily use — they're smaller, launch instantly, and don't need any socket plumbing. The container image at `ghcr.io/deveshctl/layerx` is the right pick when you don't want to install anything on the host: CI runners that already have Docker, one-off inspections from a colleague's machine, or when you want to pin the exact LayerX version alongside your other build tooling. The container image still needs the engine's socket bind-mounted in (or a bind-mounted archive), so it's not usable in a fully-sandboxed environment where the daemon is off-limits.

## Is LayerX safe to run against untrusted images?

LayerX reads image tarballs, walks their file trees, and caps in-memory reads at 2 GiB per file to prevent a crafted tar entry from exhausting memory. It never executes anything from the image. That said, treat any container image as untrusted input: pair LayerX (which tells you *what's inside*) with a vulnerability scanner like Trivy or Grype (which tells you *whether what's inside has known CVEs*).

## Is this project actively maintained?

Every release ships signed artefacts, an SBOM, and SLSA provenance. See the [Releases page](https://github.com/deveshctl/layerx/releases) for the current version and [CHANGELOG.md](../CHANGELOG.md) for what's new.

---

## Troubleshooting

- **"Cannot connect to the Docker daemon"** — Docker isn't running. Start Docker Desktop, or `sudo systemctl start docker` on Linux. If your image is already saved as a tarball, pass the file path instead — no daemon required.
- **"Archive not found"** — the path you passed doesn't exist or isn't a regular file. Check spelling; make sure you're not pointing at a directory.
- **"Not a valid image archive"** — the file exists but isn't a `docker save` or OCI-layout tarball. Re-export with `docker save -o image.tar IMAGE` or build with `--output type=oci,dest=image.tar`.
- **"image not found"** — LayerX pulls images on demand. Confirm the reference and that you can `docker pull` (or `podman pull`) it manually.
- **Cache permission errors** — point `LAYERX_CACHE_DIR` at a writable path, e.g. `LAYERX_CACHE_DIR=$HOME/.cache/layerx`.

[Back to the README](../README.md) · [Engine setup](engines.md)
