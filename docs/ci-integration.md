<!-- docs/ci-integration.md — guide for using layerx in CI/CD pipelines -->

# CI Integration

`layerx ci` runs efficiency checks against a Docker image (or a saved
archive) and exits non-zero when any configured rule fails. It is built to
slot into a pipeline: machine-readable exit codes, the report on stdout,
progress and errors on stderr, archive support so you don't need a Docker
daemon in CI.

---

## Overview

CI mode evaluates two families of rules against the image:

- **Global rules** — `lowest-efficiency`, `highest-wasted-bytes`,
  `highest-user-wasted-percent`. Each compares one number from the
  efficiency analysis against a threshold.
- **Path rules** — `block`, `deny-waste`, `max-layer-count`. Each fires per
  matching path and reports which layer introduced the violation.

Rules come from `.layerx.yaml` (in the working directory) and can be
overridden inline with CLI flags for the three global thresholds. If
`.layerx.yaml` is missing, built-in defaults apply.

Two ways to invoke CI mode:

```bash
# Explicit subcommand — full control over flags.
layerx ci nginx:latest

# Env-var shortcut — calling `layerx <image>` with CI=true forwards to ci.
CI=true layerx nginx:latest
```

The shortcut is convenient when CI runners already export `CI=true`. It
uses config-file or default thresholds — you cannot pass threshold flags
through it.

`layerx ci` accepts both image references and saved archives, so a
pipeline that runs `docker save -o image.tar app:latest` can hand the tar
straight to `layerx ci ./image.tar` without a Docker daemon at evaluation
time.

---

## GitHub Actions Example

A minimal workflow that builds the image and gates the merge on
efficiency:

```yaml
# .github/workflows/image-efficiency.yml
name: Image Efficiency

on:
  pull_request:
  push:
    branches: [main]

jobs:
  layerx:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Build image
        run: docker build -t myapp:${{ github.sha }} .

      - name: Install layerx
        run: |
          curl -LO https://github.com/deveshctl/layerx/releases/latest/download/layerx_linux_amd64.deb
          sudo dpkg -i layerx_linux_amd64.deb

      - name: Run efficiency check
        run: layerx ci myapp:${{ github.sha }}
        # Reads ./.layerx.yaml automatically.
```

Tighten or split it as needed:

```yaml
      - name: Run efficiency check (with overrides)
        run: |
          layerx ci \
            --lowest-efficiency 0.95 \
            --highest-user-wasted-percent 0.05 \
            myapp:${{ github.sha }}
```

The job fails the moment `layerx ci` exits 1. The full report is already
on the workflow log — no extra plumbing required.

### Caching the binary

The `.deb` install is fast on a warm runner cache; if your runner doesn't
cache APT downloads, pin a version and cache the unpacked binary directly:

```yaml
      - name: Cache layerx
        id: cache-layerx
        uses: actions/cache@v4
        with:
          path: /usr/local/bin/layerx
          key: layerx-${{ runner.os }}-v1.5.0

      - name: Install layerx
        if: steps.cache-layerx.outputs.cache-hit != 'true'
        run: |
          curl -LO https://github.com/deveshctl/layerx/releases/download/v1.5.0/layerx_linux_amd64.deb
          sudo dpkg -i layerx_linux_amd64.deb
```

Pin to a specific version (`v1.5.0` here) so a tagged release upgrade is a
deliberate change, not an accidental green-to-red regression.

---

## GitLab CI Example

```yaml
# .gitlab-ci.yml
stages:
  - build
  - quality

variables:
  IMAGE: "$CI_REGISTRY_IMAGE:$CI_COMMIT_SHORT_SHA"
  LAYERX_VERSION: "v1.5.0"

build:
  stage: build
  image: docker:25
  services:
    - docker:25-dind
  script:
    - docker build -t "$IMAGE" .
    - docker save -o image.tar "$IMAGE"
  artifacts:
    paths:
      - image.tar
    expire_in: 1 hour

layerx:
  stage: quality
  image: debian:stable-slim
  needs: [build]
  before_script:
    - apt-get update -qq && apt-get install -y -qq curl
    - curl -LO "https://github.com/deveshctl/layerx/releases/download/$LAYERX_VERSION/layerx_linux_amd64.deb"
    - dpkg -i layerx_linux_amd64.deb
  script:
    - layerx ci ./image.tar
```

Two notes specific to GitLab:

- This job runs in `debian:stable-slim` — no Docker daemon. `layerx ci`
  reads the saved tarball directly, which is the fast path in CI. (If you
  prefer Alpine, use a static binary from the Releases page instead of the
  `.deb` package.)
- Pinning `LAYERX_VERSION` keeps the gate deterministic. Bump it
  intentionally.

---

## Podman runners

`layerx ci` talks to any daemon that implements the Docker Engine REST
API. On Podman-native runners (common on RHEL/Fedora GitLab runners and
some self-hosted GitHub Actions hosts), there are two equivalent paths:

```bash
# 1. Talk to the Podman socket directly. --engine auto picks Podman when no
#    Docker socket is present, so the explicit flag is optional but makes
#    the intent obvious in a CI script.
systemctl --user enable --now podman.socket
layerx ci --engine podman myapp:${CI_COMMIT_SHORT_SHA}

# 2. Or skip the socket and hand layerx a saved archive. Works identically
#    to the `docker save` flow shown above.
podman save -o image.tar "$IMAGE"
layerx ci ./image.tar
```

Path 2 is the simplest gate for a containerised CI job — no socket
mounts, no daemon at evaluation time. Path 1 is useful when the runner
already has a long-lived Podman service and you want to skip the
`podman save` step.

---

## CLI Flag Usage

You don't strictly need `.layerx.yaml`. Inline flags cover every global
rule:

```bash
# Strict gate, no config file.
layerx ci \
  --lowest-efficiency 0.95 \
  --highest-wasted-bytes 5242880 \
  --highest-user-wasted-percent 0.05 \
  myapp:latest

# Disable a single rule for a release with known waste, then re-enable.
layerx ci --highest-wasted-bytes 0 myapp:latest

# Skip the analysis cache (force a fresh re-resolve).
layerx ci --no-cache myapp:latest

# Both an efficiency check and a JSON dump in one pass — the analysis is
# computed once and reused.
layerx ci --json out/analysis.json myapp:latest
```

Flags inherited from the root command:

| Flag | Effect |
|---|---|
| `--json PATH` | Also write the full analysis to `PATH` as JSON. CI exit code wins; JSON write failures only warn. |
| `--no-cache` | Bypass the analysis cache for this run. |

Path rules (`block`, `deny-waste`, `max-layer-count`) have no CLI flag —
they are config-only. If you need them, write a `.layerx.yaml`.

---

## Threshold Recommendations

Sensible starting points. Tune up or down based on your image's baseline.

| Image profile | `lowest-efficiency` | `highest-user-wasted-percent` | `highest-wasted-bytes` | `max-layer-count` |
|---|---|---|---|---|
| Production (distroless / scratch / Go) | `0.95`–`0.97` | `0.03`–`0.05` | `5 MiB` (`5242880`) | `3` |
| Production (Node, Python, Java) | `0.90` | `0.10` | `20 MiB` (`20971520`) | `5` |
| Base / builder image | `0.85` | `0.15` | `0` (disabled) | `0` (disabled) |
| Dev / test / feature branch | `0.70`–`0.80` | `0.20`–`0.30` | `0` (disabled) | `0` (disabled) |

A few ground rules:

- The default `lowest-efficiency: 0.9` catches most accidental waste
  (forgotten `apt-get clean`, stray build caches, COPY-then-rm anti-patterns).
- The `go` starter flavour (`layerx init --flavour go`) sets `0.95` /
  `0.05` because Go images are typically distroless or scratch and almost
  never have legitimate waste.
- Set `highest-wasted-bytes` for a hard floor on absolute waste,
  independent of image size. Useful when an image grows over time.

---

## Exit Codes

| Code | Meaning |
|---|---|
| `0` | All rules passed. Report ends with `PASS: Image efficiency check passed (score: NN%)`. |
| `1` | One or more rules failed. Report ends with `FAIL: Image efficiency check failed` and a per-rule breakdown. |
| `2` | Internal / operational error: Docker daemon unreachable, image not found, archive missing or malformed, invalid CLI flags, malformed `.layerx.yaml`, every rule disabled. |

The exit code is the contract — your pipeline only needs to look at it.
The rest of the output is human-readable detail.

A `2` is **not** a CI failure to gate on — it means the check itself
could not run. Surface it loudly: a missing image or a broken config will
silently let bad images through if you only watch for `1`.

---

## Tips

- **Pin the version.** `releases/latest/download` is convenient for
  exploration; pin a tag (`v1.5.0` or whatever your latest release is)
  before you put it on a protected branch. Tooling that gates merges
  should never auto-upgrade.
- **Cache the binary.** A few hundred KB to download, but every PR pays
  the cost. Cache on the binary path with the version in the cache key
  and the install step becomes idempotent.
- **Save once, scan once.** If your pipeline already produces a
  `docker save` tarball for security scanning or signing, hand the same
  artifact to `layerx ci` — no second pull, no daemon.
- **Stream stdout to the log.** `layerx ci` writes the report to stdout
  and progress to stderr; both end up in the GitHub Actions / GitLab CI
  log naturally. If you redirect stdout for further processing, the report
  is grep-clean (no stray progress noise).
- **Combine `--json` with the gate.** `layerx ci --json analysis.json
  myapp:latest` runs the gate **and** writes a structured analysis.
  Upload the JSON as an artifact for trend dashboards. CI exit code is
  the gate; the JSON is the audit trail.
- **Fail fast.** Run the layerx job before slow integration tests. A 30-
  second efficiency gate that catches a 200 MiB layer regression beats
  finding it after the smoke suite.
- **Keep `.layerx.yaml` in the repo.** Same review surface as code; the
  reasons for tightening or relaxing a threshold belong in a commit
  message, not in a CI variable.
- **Don't gate on the `CI=true` shortcut.** It works, but the explicit
  `layerx ci` form makes the intent obvious in the workflow file and
  lets you pass overrides. Reserve `CI=true` for ad-hoc local runs that
  mimic CI.
- **Cache the analysis on long-lived runners.** Self-hosted runners that
  re-scan the same image digest benefit from the analysis cache — point
  `LAYERX_CACHE_DIR` at a path the runner persists across jobs to skip
  the tar parse on a repeat run. On runners that build up cache over
  time, prune it occasionally with `layerx cache prune --older-than 7d`
  (the cache also self-prunes by `LAYERX_CACHE_TTL_DAYS` and
  `LAYERX_CACHE_MAX_BYTES`). Ephemeral runners can ignore this.
