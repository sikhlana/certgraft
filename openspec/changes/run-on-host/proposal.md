# Proposal

## Why

certgraft only runs as a container today, and nothing connects it to a runtime: an operator has to
build the image, run it with the right mounts, write a BuildKit source policy by hand and export it.
To be a tool other developers can install and forget, it has to run as a plain binary on the host,
keep itself running, and wire itself into Docker Desktop and OrbStack in a few commands.

## What Changes

- **BREAKING** certgraft runs on the host. `serve` listens on `127.0.0.1` at the configured registry
  port (default 5555), and the container image, its Dockerfile and `make image` are removed. Every
  piece that names the port (the policy, the daemon config entries, `init`'s probe, `status`) takes it
  from the config or from the policy generated from it, never a hardcoded 5555.
- **BREAKING** The health server (port 9090) is removed: nothing on a workstation probes it, and
  `certgraft status` checks the registry itself.
- **BREAKING** Configuration lives in `~/.config/certgraft/config.yaml`. The roots become a list of
  PEM files (`roots.files`) instead of a directory, and the cache defaults to `~/.cache/certgraft`.
- New commands, so each piece is one step and reversible:
  - `certgraft init <shell>` prints the shell line that sets `EXPERIMENTAL_BUILDKIT_SOURCE_POLICY`,
    for `eval "$(certgraft init zsh)"`, and prints nothing when the registry isn't answering, so
    builds go straight upstream instead of failing. It runs in every new shell, so it's a plain
    command: no config load, no logging, nothing on stderr, always exit 0. It reads the port back
    from the policy file.
  - `certgraft configure [--runtime ...] [--remove]` writes the source policy and adds (or removes)
    certgraft as an insecure registry, and optionally as the Docker Hub mirror, in each runtime's
    daemon config, backing the file up first, all from the configured port. It never restarts
    anything; it says what to restart.
  - No service command of its own: the Homebrew formula declares a service, so
    `brew services start certgraft` keeps `serve` running under launchd (systemd with Homebrew on
    Linux).
  - `certgraft status` reports the registry, roots fingerprint, policy and each runtime's config,
    fast enough to script, and exits non-zero when builds would fail. `status --build` adds a rewrite
    check with a real build.
- The source policy targets `host.docker.internal:<registry port>`, the address both runtimes' VMs
  use to reach the host's loopback (verified for containers on Docker Desktop; BuildKit and OrbStack are spiked
  first).
- Releases: on a tag, GoReleaser publishes darwin and linux binaries for arm64 and amd64 with
  checksums to the GitHub release, and the release workflow writes a source-building Homebrew formula
  with that service block to `sikhlana/homebrew-tap`. It's a formula, not a cask, because only
  formulae can declare a `brew services` service; GoReleaser's own formula output is deprecated, so
  the formula is rendered from a template in this repo.
- Out of scope, parked: runtimes beyond Docker Desktop and OrbStack; a service for installs that
  don't come from Homebrew (the docs show running `certgraft serve` instead); bottles; the
  interactive `setup` wizard (its own change, built on these commands).

## Capabilities

### New Capabilities
- `runtime-integration`: wiring certgraft into a container runtime and the operator's shell: the
  source policy, the `init` shell line, daemon config edits with backups, running as a Homebrew
  service, and `status`.

### Modified Capabilities
- `extra-roots`: roots come from a configured list of PEM files rather than a directory.
- `patched-image-serving`: the registry listens on loopback on the host at the configured port
  (default 5555); the health server is removed.
- `patch-cache`: the cache defaults to `~/.cache/certgraft`; `prune` runs as a plain command.

## Impact

Capability specs: `runtime-integration` (new); `extra-roots`, `patched-image-serving`, `patch-cache`
(modified).

Packages and files:
- New: `internal/runtime` (Docker Desktop and OrbStack: daemon config paths, edits, backups),
  `internal/policy` (the source policy), `internal/shell` (`init` output), `cmd/` entries for
  `init`, `configure`, `status`; `.goreleaser.yaml`; `packaging/homebrew/certgraft.rb.tmpl` (the
  formula template); a release workflow; `docs/`.
- Changed: `internal/roots`, `internal/registry` (listener), `internal/store` (default dir),
  `internal/config`, `internal/app`, `Makefile`, README, AGENTS.md (drops `make image`),
  `openspec/config.yaml` (the hadolint convention).
- Removed: `internal/health`, `Dockerfile`, `.dockerignore`, the hadolint step and `make image`.

Outside the repo, all opt-in and reversible: `~/.config/certgraft/`, `~/.cache/certgraft/`, Docker
Desktop's `~/.docker/daemon.json` and OrbStack's `~/.orbstack/config/docker.json` (backed up before
any edit), the launchd agent `brew services` manages, and the formula in `sikhlana/homebrew-tap`
(written by the release workflow with a token held as a repository secret).

Rollout: merge, tag `v0.1.0`; the release workflow publishes the GitHub release and writes the tap
formula. Operators then run `brew install sikhlana/tap/certgraft`, `brew services start certgraft`,
`certgraft configure`, restart the runtime, and add the `init` line. Nothing is released yet, so the
breaking changes affect no one.

Divergences from the constitution:
- The convention "Workflows pass actionlint and the Dockerfile passes hadolint (make lint)" changes
  to actionlint only, because the Dockerfile goes. The constitution is updated in this change.
- "Listen on loopback only" still holds, but the runtimes' VMs reach loopback through
  `host.docker.internal`, so any container on the machine can reach the registry. It is read-only
  and serves only allowlisted public images; design.md records this.
