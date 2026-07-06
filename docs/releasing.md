# Releasing LayerX

The release pipeline is driven from a single git tag. Everything downstream
— archives, deb/rpm packages, Homebrew tap, Scoop bucket, cosign signatures,
SBOMs, SLSA provenance, and the GHCR container image — is produced by
`.github/workflows/release.yml` when the tag lands.

## Cutting a release

1. Move the `[Unreleased]` section in `CHANGELOG.md` to a dated
   `[vX.Y.Z] - YYYY-MM-DD` heading. Update the README version badge if the
   feature set changed.
2. Open a `chore/release-vX.Y.Z` branch, PR it, review, squash-merge to
   `main`.
3. Tag the squash-merge commit on `main`:
   ```bash
   git tag -a vX.Y.Z -m "vX.Y.Z"
   git push origin vX.Y.Z
   ```
   The tag must exactly match the most recent `[vX.Y.Z]` heading in
   `CHANGELOG.md`.

Pushing the tag triggers the release workflow. `goreleaser` runs first;
once it completes, `provenance` and `ghcr` fan out in parallel:

- **`goreleaser`** — cross-compiles Linux / macOS / Windows binaries
  (amd64 + arm64), builds `.tar.gz` / `.zip` archives + `.deb` / `.rpm`
  packages, writes the Homebrew formula and Scoop manifest to their
  respective repos, signs `checksums.txt` with cosign (keyless, GitHub
  OIDC), attaches an SPDX SBOM per archive, and cuts the GitHub release.
- **`provenance`** — the SLSA reusable workflow issues a Build Level 3
  provenance attestation over the `checksums.txt` subjects and attaches
  it to the release.
- **`ghcr`** — packages the same Linux binaries (downloaded as a workflow
  artifact from `goreleaser`) into a multi-arch container image and
  pushes it to `ghcr.io/deveshctl/layerx`. Tags: `latest`, `vX.Y.Z`,
  `vX.Y`, `vX`. Pre-release tags (`vX.Y.Z-rc.N`) get only the concrete
  version tag — never `latest`.

## First-time GHCR setup

GHCR packages default to **private**. On the very first push to
`ghcr.io/deveshctl/layerx`, flip it to public so `docker pull` needs no
authentication:

1. Open the package page at
   `https://github.com/deveshctl?tab=packages` (or click the **Packages**
   widget on the repository's landing page). GHCR packages are surfaced
   on the user / organization profile, not under the repository's
   **Settings** tab.
2. Click the `layerx` package, then **Package settings** in the right
   sidebar of the package page.
3. Scroll to **Danger Zone** → **Change package visibility** → **Public**.

This is a one-time step. Subsequent releases inherit the visibility.

## Dry-running the container image build

Push a branch and trigger the release workflow manually via
`workflow_dispatch` in the Actions UI. GoReleaser runs in `--snapshot
--skip=publish` mode (no release cut, no signatures), and the `ghcr` job
builds the multi-platform image with `push: false`. Any Dockerfile or
buildx configuration issue surfaces without publishing anything.

## Verifying releases

See [SECURITY.md](../SECURITY.md) for the cosign / slsa-verifier commands
users run to verify a downloaded archive or checksum file.
