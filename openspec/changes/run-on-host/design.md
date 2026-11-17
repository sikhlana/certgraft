# Design

## Context

See proposal.md (Why). Prior work, all on 2026-11-17, before certgraft left
github.com/sikhlana/ai-context:

- **BuildKit source-policy spike** (in ai-context's `.scratch/2026-11-17-buildkit-source-policy-spike/`):
  a client-side policy set through `EXPERIMENTAL_BUILDKIT_SOURCE_POLICY` makes the default builder
  resolve `FROM` images from a local registry, with or without `--pull`. Every reference is
  rewritten, including the `# syntax=` frontend. Digest pins only survive as a `:sha256-<hex>` tag.
  The registry ran as a container published on `localhost:5555`.
- **Host reachability check:** a listener on the Mac's `127.0.0.1` answered from a Docker Desktop
  container at `host.docker.internal`, and didn't connect at `localhost`. Inside a runtime's VM,
  `localhost` is the VM.

Existing state checked: Docker Desktop's `~/.docker/daemon.json` has no `insecure-registries` or
`registry-mirrors`. OrbStack keeps its daemon config in `~/.orbstack/config/docker.json`. Both
runtimes are installed here, with Docker contexts `desktop-linux` and `orbstack`.

## Goals / Non-Goals

**Goals:**
- Install with Homebrew, then about four commands to a working setup, each reversible.
- Builds on Docker Desktop and OrbStack resolve every allowlisted image through certgraft, without
  editing a Dockerfile.
- A stopped registry never breaks a new shell's builds.

**Non-Goals:**
- Other runtimes (Colima, Podman, Rancher Desktop), Linux service units, Windows. Parked.
- The interactive `setup` wizard. Its own change, built on these commands.
- Signing or notarising the macOS binary.

## Decisions

### D1. A host binary on loopback; no container, no health server
`serve` listens on `127.0.0.1` at the registry port (`registry.server.host`,
`registry.server.port`, default 5555). The container image goes: a host binary needs no mounts, no
volume and no port publishing, and loopback-only becomes a bind address the code itself enforces.

**The port is configurable, and nothing else hardcodes it.**
- `configure` loads the config and generates the policy target and the daemon config entries from
  the configured port (D3, D4).
- `init` reads the port back from the policy file (D5).
- `status` reads it from the config (D7).

5555 is only the default. Changing the port means running `configure` again and restarting the
runtime; `status` shows a policy or daemon entry that disagrees with the config.

The health server goes too. Nothing on a workstation probes it, 9090 often clashes with other dev
tools, and `status` checks `/v2/` directly.

### D2. Configuration in the operator's home
- **Config file:** `~/.config/certgraft/config.yaml`. The configloader in `sikhlana/pkg` already
  searches there; `CERTGRAFT_*` env vars and `--config` override it.
- **Search order:** configloader checks `./.certgraft.<ext>` in the working directory before
  `~/.config/certgraft/` (configloader@v0.1.4 file.go, `candidates`). A command run inside a
  directory holding a `.certgraft.yaml` would read different config from the Homebrew service. That's
  unlikely, so it's a docs note (task 5.2), not code.
- **`roots.files`:** a list of PEM paths, replacing `roots.dir`. On a host, operators point at
  certificates they already have, rather than copying them into a directory.
  - The env form `CERTGRAFT_ROOTS_FILES` is comma-separated with spaces trimmed, and an empty string
    is an empty list (configloader@v0.1.4 decode.go, `splitList`).
  - The empty list is rejected by the roots loader, which names the problem, not by a struct tag. So
    commands that don't load roots never fail on it.
- **`store.dir`:** defaults to `~/.cache/certgraft`. The default is resolved from the home
  directory when the key is empty (a `DefaultStoreDirName` constant plus a resolver), because
  configloader defaults are static values.

### D3. One source policy, targeting `host.docker.internal:<registry port>`
The buildx client reads the policy at build time, and the operator may switch Docker contexts
between builds. So the policy has to name a host that both runtimes' BuildKit resolve to the Mac's
loopback. `host.docker.internal` does on Docker Desktop (for containers; BuildKit is spiked) and is
documented for OrbStack.

- The rules are the two from spike finding 04, digest form first.
- They're generated from the same allowlist the registry enforces, so the two can't drift, and from
  the configured registry port.
- The file is `~/.config/certgraft/buildkit-policy.json`. It's also the record `init` reads the
  target from.

Fallback, if OrbStack's BuildKit doesn't resolve it: one policy file per runtime, chosen by `init`
from the active context. That gives up switching contexts mid-shell, so only if the spike forces it.

### D4. Runtime daemon config: insecure registry, optional Hub mirror
BuildKit and the Docker daemon allow plain HTTP only to `localhost`, so each runtime needs
`host.docker.internal:<registry port>` in `insecure-registries`.

`--mirror` also adds `http://host.docker.internal:<registry port>` to `registry-mirrors`, so
`docker pull`, `docker run` and compose services get patched Hub images too. It's kept behind a flag
until the spike shows the daemon falls back to upstream when certgraft is down.

The port comes from the loaded config. `--remove` removes the entries for the configured port.

| Runtime | Detected by | Config file | Restart, told to the operator |
|---|---|---|---|
| Docker Desktop | ~/.docker/run/docker.sock exists | ~/.docker/daemon.json | Docker Desktop, Restart |
| OrbStack | ~/.orbstack exists | ~/.orbstack/config/docker.json | orb restart docker |

- Each edit keeps the file's other keys, and copies the file to
  `<file>.certgraft-backup-<UTC timestamp>` first, only when something changes. A second run reports
  nothing to do.
- `--remove` deletes only the entries certgraft added.
- certgraft never restarts a runtime. Restarting is the operator's step, and `configure` ends by
  naming it.

### D5. `init` is a plain command: the policy export, or nothing
`init` runs in every new shell through `eval "$(certgraft init zsh)"`, so it must be quiet, fast and
unable to fail.

It's a plain Cobra command that never goes through the kernel. `kernel.App.Run` loads and validates
the whole config through configloader and logs fx events and "loaded config file" / "no config file
found" (kernel@v0.3.2 kernel.go `Run`; configloader@v0.1.4 file.go:34,48). That would put a log line
on stderr in every shell, or a validation error before `configure` has ever run.

What `init` does:
1. Read `~/.config/certgraft/buildkit-policy.json` and take the target `host:port` from its rules.
   The policy is the record of what `configure` set up, so `init` needs no config file.
2. Probe `http://127.0.0.1:<port>/v2/` with a 300 ms timeout. A refused connection on loopback
   returns at once.
3. When the registry answers, print `export EXPERIMENTAL_BUILDKIT_SOURCE_POLICY=<policy file>` for
   zsh and bash, or `set -gx …` for fish.
4. Otherwise (no policy file, an unreadable or foreign policy, no answer) print nothing.

In every case it writes nothing to stderr and exits 0.

The gap that remains: a shell opened while certgraft was up keeps the policy if certgraft later
stops. `status` names the fix.

### D6. `brew services` keeps it running
certgraft has no service command of its own. Its Homebrew formula declares the service:

```ruby
service do
  run [opt_bin/"certgraft", "serve"]
  keep_alive true
  log_path var/"log/certgraft.log"
  error_log_path var/"log/certgraft.log"
end
```

`brew services start certgraft` writes and loads the launchd agent (systemd with Homebrew on Linux);
`stop` unloads it.

Why this over a `certgraft service install` that writes its own plist (decided 2026-11-17):
- **It's Homebrew's standard**, so operators already know `brew services list|start|stop|restart`.
- **`opt_bin` is Homebrew's stable path.** It survives `brew upgrade`, which removes the
  absolute-path problem a hand-written plist has. launchd resolves a relative `ProgramArguments[0]`
  only against `_PATH_STDPATH` (`man launchd.plist`), so a bare `certgraft` would never be found.
- **No launchctl code or tests in certgraft**, and Linux gets systemd for free.

It costs installs that don't come from Homebrew (`go install`, a downloaded binary): they get no
service. The docs show running `certgraft serve` and a sample launchd plist for them.

Only a formula can do this. Homebrew casks have no `service do` block: the Cask Cookbook's `service`
stanza installs Services-menu extensions into `~/Library/Services`, and launchd appears only under
`uninstall`/`zap`. That's why D8 publishes a formula, not a cask.

### D7. `status` names the cause and the fix, quickly
By default `status` makes only cheap checks, fast enough to run from a script:
- the registry: does `/v2/` answer on the configured port;
- the roots and their fingerprint;
- the policy env var, its file, and whether the file's target port matches the config;
- per runtime: the insecure registry and mirror entries for the configured port.

It exits 1, naming both fixes (`brew services start certgraft` or `certgraft serve`, or `unset` the
env var), when the policy is set and the registry is down.

`status --build` adds the rewrite check: a one-line `docker build` on the active context, reporting
the reference the base resolved through. It's opt-in because it's slow, pulls an image and writes
build cache.

### D8. Releases: GoReleaser for binaries, a source-building formula for Homebrew
A tag `v*` runs the release workflow, in two parts.

**GoReleaser** publishes the GitHub release: darwin and linux, arm64 and amd64, `CGO_ENABLED=0`,
version stamped through `sikhlana/pkg/cli`'s ldflags, and checksums. It has no Homebrew section.
- Go's crypto/x509 on macOS uses the system verifier without cgo, so the static darwin build loses
  nothing.
- Locally, `goreleaser check` and a `--snapshot` build run in GoReleaser's pinned image.

**The formula** is rendered from `packaging/homebrew/certgraft.rb.tmpl` and pushed to
`sikhlana/homebrew-tap` as `Formula/certgraft.rb`, using a token held in a repository secret. Only the
version and the tag tarball's sha256 are filled in. The template builds from source:
`depends_on "go" => :build`, `go build` with the same ldflags, and D6's service block.
- **A formula, not a cask,** because only formulae can declare a service (D6).
- **Not GoReleaser's `brews`,** which is deprecated (soft since v2.10, formally since v2.16, removed
  at a future major), with casks as GoReleaser's replacement.
- **From source,** because that's Homebrew's native shape for a formula. It also means no Gatekeeper
  quarantine on an unsigned download. It costs about a minute of `go build` on install; bottles are
  parked.

The same render-and-push step creates the formula on the first release and updates it after that,
so there's no separate bump action. Before any tag, the rendered template is checked against a local
`git archive` tarball with `brew audit --strict --formula` and a real `brew install
--build-from-source`, then `brew services start` and `stop`.

### D9. Spike first: one restart window per runtime
Before any runtime code, on both Docker Desktop and OrbStack:
1. With `insecure-registries` set, a build using the policy pulls through `host.docker.internal:5555`.
2. Without it, the same build fails, which confirms D4 is needed.
3. OrbStack's BuildKit resolves `host.docker.internal` (D3, or its fallback).
4. With `registry-mirrors` set, `docker pull` hits certgraft, and falls back to upstream when
   certgraft is stopped (D4's `--mirror`).

Each daemon config is restored from its backup afterwards. Results go to `.scratch/` findings. Any
failed check updates D3/D4 and the specs before code is written.

## Risks / Trade-offs

- **The policy env var is labelled experimental.** A buildx release could rename it. Mitigation:
  `status --build`'s rewrite check catches it.
- **Any container on the machine can reach the registry** through `host.docker.internal`, since the
  runtimes forward to loopback. Accepted: it's read-only, serves only allowlisted public images, and
  holds nothing secret.
- **Daemon config key order.** Rewriting the JSON through a map sorts its keys. The backup keeps the
  original, and the content is unchanged. Accepted rather than adding a JSON-editing dependency.
- **Installing builds from source** (D8): about a minute, with Go as a build-only dependency.
  Accepted for now; bottles would remove it later.
- **The tap formula is written by CI with a token.** A leaked token could rewrite the formula.
  Mitigation: a fine-grained token scoped to `sikhlana/homebrew-tap` contents only, used only on tag
  pushes.
- **A running shell keeps a stale policy** after certgraft stops (D5). Mitigation: `status`, and the
  service's `keep_alive`.

## Migration Plan

Nothing is released, so there's nothing to migrate. Before the first tag:
1. Create `sikhlana/homebrew-tap`.
2. Add the token secret (the operator's step).

After merge: tag `v0.1.0`, check that the release and `Formula/certgraft.rb` appear in the tap, then
install on this workstation with Homebrew and run through README's setup.

Rollback per operator: remove the `init` line, `certgraft configure --remove`, restart the runtime,
`brew services stop certgraft`, `brew uninstall certgraft`. Deleting `~/.cache/certgraft` is separate.

## Open Questions

None. How `CERTGRAFT_ROOTS_FILES` becomes a list is answered under D2 (comma-separated, spaces trimmed).
