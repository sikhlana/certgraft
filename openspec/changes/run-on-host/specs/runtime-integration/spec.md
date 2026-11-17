# Spec Delta

## Purpose

Wires certgraft into Docker Desktop and OrbStack and the operator's shell, so builds resolve every
allowlisted image through it with a handful of reversible commands, and a stopped registry never
breaks a new shell's builds. Carried by: internal/policy, internal/runtime, internal/shell, cmd, and
packaging/homebrew/certgraft.rb.tmpl (the formula, with its service).

## ADDED Requirements

### Requirement: The source policy rewrites allowlisted images to certgraft
`certgraft configure` SHALL write `~/.config/certgraft/buildkit-policy.json` with two ordered rules
for exactly the registries the registry allowlists, targeting `host.docker.internal` at the configured
registry port (default 5555): first `<registry>/<repo>[:tag]@sha256:<hex>` to
`host.docker.internal:<port>/<registry>/<repo>:sha256-<hex>`, then `<registry>/<repo>[:tag]` to
`host.docker.internal:<port>/<registry>/<repo>[:tag]`.

#### Scenario: Policy content
- **WHEN** `certgraft configure` runs with the default config
- **THEN** the policy file holds two CONVERT rules targeting `host.docker.internal:5555`, the digest rule first, both covering docker.io, gcr.io, ghcr.io, quay.io and mcr.microsoft.com

#### Scenario: Non-default port
- **WHEN** `certgraft configure` runs with `registry.server.port` set to 6000
- **THEN** both rules target `host.docker.internal:6000` and no rule mentions 5555

#### Scenario: Build through the policy
- **WHEN** a Dockerfile `FROM node:22-alpine` is built with the policy exported, certgraft running and the runtime configured, on Docker Desktop and on OrbStack
- **THEN** the build log shows the base resolved from `host.docker.internal:<port>/docker.io/library/node:22-alpine`

### Requirement: Runtime daemon config gains certgraft's entries
`certgraft configure` SHALL add `host.docker.internal:<port>` (the configured registry port) to
`insecure-registries` in the daemon config of each detected runtime (or those named with `--runtime`),
and with `--mirror` add `http://host.docker.internal:<port>` to `registry-mirrors`. It SHALL keep every
other key, back the file up before changing it, and be idempotent.

#### Scenario: Configure twice
- **WHEN** `certgraft configure --runtime docker-desktop` runs twice
- **THEN** the second run reports nothing to change and creates no second backup

#### Scenario: Entry follows the port
- **WHEN** `configure` runs with `registry.server.port` set to 6000
- **THEN** `insecure-registries` lists `host.docker.internal:6000`

### Requirement: Daemon config edits are reversible and leave restarts to the operator
`certgraft configure --remove` SHALL remove only the entries certgraft added, keeping every other key,
and be idempotent. `configure` SHALL end by naming the restart the operator must do, and MUST NOT
restart a runtime itself.

#### Scenario: Other keys kept
- **WHEN** `daemon.json` holds `builder` and `experimental` keys and `configure` then `configure --remove` run
- **THEN** both keys keep their values and `insecure-registries` no longer lists certgraft

#### Scenario: Restart named, not done
- **WHEN** `configure` changes OrbStack's config
- **THEN** it prints `orb restart docker` as the operator's next step and starts no process that restarts it

### Requirement: The shell line exports the policy only when certgraft answers
`certgraft init zsh` and `init bash` SHALL print `export EXPERIMENTAL_BUILDKIT_SOURCE_POLICY=<policy
file>`, and `init fish` the fish equivalent, when the policy file exists and the registry answers on
`127.0.0.1` at the port the policy targets. Otherwise they SHALL print nothing. `init` MUST NOT read
the config file, MUST NOT write to stderr, and SHALL exit 0 in every case.

#### Scenario: Registry up
- **WHEN** `certgraft init zsh` runs with the policy file present and the registry answering on its port
- **THEN** stdout is exactly the export line, stderr is empty, and it exits 0

#### Scenario: Registry down
- **WHEN** `certgraft init zsh` runs with the policy file present and nothing listening on its port
- **THEN** stdout and stderr are empty, and it exits 0

#### Scenario: No policy file
- **WHEN** `certgraft init zsh` runs before `configure` ever has
- **THEN** stdout and stderr are empty, and it exits 0

#### Scenario: No config file
- **WHEN** `certgraft init zsh` runs with no config file anywhere and `roots.files` unset
- **THEN** stderr is empty and it exits 0, with no validation error

### Requirement: Homebrew keeps certgraft running as a service
certgraft's Homebrew formula SHALL declare a service that runs `certgraft serve` from Homebrew's
stable `opt_bin` path, kept alive, logging to `$(brew --prefix)/var/log/certgraft.log`, so
`brew services start certgraft` starts it at login and restarts it if it exits, and
`brew services stop certgraft` stops it. certgraft itself SHALL have no command that writes launchd
or systemd units.

#### Scenario: Start
- **WHEN** `brew services start certgraft` runs after `brew install sikhlana/tap/certgraft`
- **THEN** `brew services info certgraft` reports it running, and the registry answers on the configured port

#### Scenario: Survives an exit
- **WHEN** the running `certgraft serve` process is killed
- **THEN** launchd starts it again and the registry answers once more

#### Scenario: Stop
- **WHEN** `brew services stop certgraft` runs
- **THEN** nothing listens on the configured port and `brew services info certgraft` reports it stopped

### Requirement: Status names the cause and the fix
`certgraft status` SHALL report, without running a build: whether the registry answers on the
configured port, the roots fingerprint, whether the policy env var is set, whether its file exists
and targets the configured port, and each runtime's entries for that port. With the policy set and
the registry down, it SHALL exit non-zero and name both fixes: `brew services start certgraft` (or
`certgraft serve`), and unsetting the env var.

#### Scenario: Policy set, registry down
- **WHEN** `certgraft status` runs with the env var set and nothing on the configured port
- **THEN** it exits non-zero, says builds will fail with connection refused, and names both fixes

#### Scenario: Default runs no build
- **WHEN** `certgraft status` runs without `--build`
- **THEN** it starts no `docker build`

### Requirement: Status can check a real build
`certgraft status --build` SHALL also run a one-line build on the active Docker context and report
the reference its base resolved through.

#### Scenario: Build check
- **WHEN** `certgraft status --build` runs with everything configured
- **THEN** it reports the base resolved through `host.docker.internal:<port>`
