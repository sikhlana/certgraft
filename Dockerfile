# syntax=docker/dockerfile:1
# certgraft as a container. Build with `make image`, or:
#   docker build --build-arg GO_VERSION="$(cat .go-version)" -t certgraft .
#
# Run it published on loopback only, with the cache in a volume and each root mounted read-only:
#   docker run -d --name certgraft --restart unless-stopped -p 127.0.0.1:5555:8080 \
#     -v certgraft-data:/var/lib/certgraft -v <root.pem>:/etc/certgraft/roots/1-root.pem:ro certgraft

# No default: a forgotten build arg fails the build instead of silently using a stale version.
ARG GO_VERSION
# The build image: official by default; make passes GO_IMAGE only when it's set (e.g. local/go:<version>).
ARG GO_IMAGE=golang:${GO_VERSION}

FROM ${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod go.sum ./
# The only step with network. Every module is public, so no credentials.
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION
ARG REVISION
ARG TIME
RUN --network=none \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOPROXY=off GOFLAGS=-mod=readonly \
    go build -trimpath -ldflags "-s -w \
      -X 'github.com/sikhlana/pkg/cli.Version=${VERSION}' \
      -X 'github.com/sikhlana/pkg/cli.Revision=${REVISION}' \
      -X 'github.com/sikhlana/pkg/cli.Time=${TIME}'" \
    -o /out/app . \
 && mkdir -p /out/data

# Pin by digest alongside the tag. Multi-arch, so it runs on arm64 laptops and amd64 nodes.
FROM cgr.dev/chainguard/static:latest@sha256:54b5f14edad6f2554774d499539e837588a501bd5bf22ac6cd97b1068c35d44d
COPY --from=build /out/app /app
# The cache volume mounts here; a new named volume copies this directory's ownership, so nonroot can write it.
COPY --from=build --chown=65532:65532 /out/data /var/lib/certgraft
# Chainguard's nonroot user, by number so the host can resolve it.
USER 65532:65532
# Registry, health probes. Only 8080 is published, and only on 127.0.0.1.
EXPOSE 8080 9090
ENTRYPOINT ["/app"]
CMD ["serve"]
