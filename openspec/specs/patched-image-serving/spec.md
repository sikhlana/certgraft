# patched-image-serving Specification

## Purpose
Answers the OCI distribution read API so that a build or pull pointed at certgraft gets any image from
an allowlisted registry, at any tag or digest pin, with the extra roots added, and images with nothing
to patch exactly as upstream. Carried today by: internal/registry, internal/upstream, internal/health.

## Requirements
### Requirement: Read-only distribution API
certgraft SHALL serve `GET` and `HEAD` for `/v2/`, `/v2/<name>/manifests/<reference>` and
`/v2/<name>/blobs/<digest>`, with registry errors in the distribution spec's JSON format. Any other
path under `/v2/` SHALL get a 404 with code `UNSUPPORTED`.

#### Scenario: Version check
- **WHEN** `GET /v2/` is requested
- **THEN** it answers 200

#### Scenario: Unsupported path
- **WHEN** `GET /v2/docker.io/library/x/tags/list` is requested
- **THEN** it answers 404 with code `UNSUPPORTED`

### Requirement: Upstreams come only from the allowlist
certgraft SHALL fetch only from Docker Hub, gcr.io, ghcr.io, quay.io and mcr.microsoft.com. A name
whose first segment has a dot or port names its registry, which MUST be on the allowlist; any other
name is a Docker Hub repository. A request naming another registry SHALL get a 404 with code
`NAME_UNKNOWN`, and certgraft makes no outbound connection for it.

#### Scenario: Unlisted upstream
- **WHEN** `GET /v2/example.com/foo/manifests/latest` is requested
- **THEN** it answers 404 with `NAME_UNKNOWN` and no connection is opened

#### Scenario: Policy and mirror shapes
- **WHEN** `docker.io/library/alpine:3.21` and `library/alpine:3.21` are both requested
- **THEN** both resolve to the same manifest digest

### Requirement: Images with nothing to patch pass through
An image with no trust point to patch SHALL be served with the upstream manifest's exact bytes and
digest, and its blobs streamed from upstream without being stored.

#### Scenario: Dockerfile frontend
- **WHEN** an image holding only a single binary is requested
- **THEN** its digest equals upstream's, its blobs are readable through certgraft, and nothing is stored

### Requirement: Digest pins keep their meaning
A tag of the form `sha256-<hex>` SHALL resolve to the patch of upstream digest `sha256:<hex>`, fetched
by that digest and verified against it. When upstream has no such digest, the request SHALL fail with
`MANIFEST_UNKNOWN` naming the digest.

#### Scenario: Known digest
- **WHEN** `sha256-<hex>` of an existing upstream image is requested
- **THEN** the patched manifest's layers are the upstream image's layers plus one

#### Scenario: Unknown digest
- **WHEN** a `sha256-<hex>` tag with no upstream manifest is requested
- **THEN** it answers 404 with `MANIFEST_UNKNOWN` and the message names `sha256:<hex>`

### Requirement: Tags track upstream with a cached fallback
A tag request SHALL check upstream for the tag's current digest first and serve the patch of that
digest. When upstream can't answer (no connection, a 5xx or a 429), certgraft SHALL serve the patch
of the digest the tag last resolved to and log one degraded-mode line; with none cached, the client
gets an error. Any other upstream error passes through.

#### Scenario: Moved tag
- **WHEN** an upstream tag moves to a new image and is requested again
- **THEN** the patch of the new image is served

#### Scenario: Upstream unreachable with a cached patch
- **WHEN** upstream drops connections and a previously served tag is requested
- **THEN** the same patched digest is served and one degraded-mode line is logged

#### Scenario: Upstream unreachable, nothing cached
- **WHEN** upstream drops connections and a tag never served before is requested
- **THEN** it answers 502

### Requirement: Every platform of an index is served patched
For an upstream index, certgraft SHALL serve an index listing the same platforms, each pointing at its
patched manifest, with attestation manifests dropped.

#### Scenario: Two-platform index with an attestation
- **WHEN** an index with linux/amd64, linux/arm64 and an attestation is requested
- **THEN** the served index lists the two platforms only, each with the upstream layers plus one

### Requirement: Listeners
certgraft SHALL serve the registry on port 8080 with a 30-minute write timeout, and liveness and
readiness probes (`/healthz`, `/readyz`, answering 204) on port 9090, both configurable.

#### Scenario: Defaults
- **WHEN** certgraft loads its config with nothing set
- **THEN** the registry port is 8080 with a 30-minute write timeout and the health port is 9090

#### Scenario: Probes
- **WHEN** `GET /healthz` or `GET /readyz` is requested on the health port
- **THEN** it answers 204
