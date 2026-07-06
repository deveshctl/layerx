# syntax=docker/dockerfile:1.9
#
# Container image for layerx.
#
# layerx is a client to a running container engine's daemon socket, so a
# containerised layerx is only useful when that socket is bind-mounted in.
# See the "Install via container image" section of README.md for the exact
# `docker run` / `podman run` incantations, socket paths, and the group-access
# note for Docker on Linux.
#
# This Dockerfile does NOT build the Go binary. During a release, the binary
# produced by GoReleaser (bit-identical to the archives published on the
# GitHub release page) is copied in per-platform. This keeps the image and
# the native install paths on exactly the same commit / version / ldflags.
#
# Build context: the repository root, with a populated `dist/` directory from
# a prior `goreleaser release` (or `goreleaser build --snapshot`). Buildx sets
# TARGETOS and TARGETARCH per platform in the manifest list.

ARG BASE_IMAGE=gcr.io/distroless/static-debian12:nonroot
FROM --platform=$BUILDPLATFORM alpine:3.20 AS stage
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

# Copy the GoReleaser output for the target platform. The path pattern matches
# GoReleaser's default layout for `builds:` with no id override:
#   dist/layerx_<goos>_<goarch>[_<goarm>|_<goamd64>]/layerx
# The `v1` (amd64 microarch) and `v8.0` (arm64 microarch) suffixes are the
# GoReleaser defaults; the wildcard tolerates future changes.
COPY dist/ /src/dist/
RUN set -eux; \
    mkdir -p /out; \
    src="$(find /src/dist -type d -name "layerx_${TARGETOS}_${TARGETARCH}*" -print -quit)"; \
    if [ -z "$src" ] || [ ! -f "$src/layerx" ]; then \
        echo "no layerx binary for ${TARGETOS}/${TARGETARCH} under /src/dist" >&2; \
        find /src/dist -maxdepth 2 -type f >&2; \
        exit 1; \
    fi; \
    install -m 0755 "$src/layerx" /out/layerx

FROM ${BASE_IMAGE}

# Default OCI image labels for a plain `docker build .` (local, no CI). During
# a release the workflow's docker/metadata-action supplies the full label set
# — including the concrete version, revision, and creation timestamp — and
# those take precedence over the defaults below. Left in the Dockerfile so
# `docker inspect` on a locally-built image still reports something useful.
ARG VERSION=dev
ARG REVISION=unknown
ARG CREATED=unknown
LABEL org.opencontainers.image.title="layerx" \
      org.opencontainers.image.description="Terminal container image layer explorer — inspect Docker, Podman, and OCI images." \
      org.opencontainers.image.url="https://github.com/deveshctl/layerx" \
      org.opencontainers.image.source="https://github.com/deveshctl/layerx" \
      org.opencontainers.image.documentation="https://github.com/deveshctl/layerx#readme" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.vendor="deveshctl" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.created="${CREATED}"

COPY --from=stage /out/layerx /usr/local/bin/layerx

# distroless-static:nonroot ships a `nonroot` user (uid 65532) whose home is
# `/home/nonroot` and is writable by that uid. Set WORKDIR there so anything
# layerx writes with a relative path lands somewhere the process can actually
# write to — `/` (the base image default) is uid 0-only.
#
# No shell is present, so the exec-form ENTRYPOINT executes layerx directly
# — SIGINT / SIGTERM reach the TUI cleanly without a shell wrapper swallowing
# them.
WORKDIR /home/nonroot
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/layerx"]
