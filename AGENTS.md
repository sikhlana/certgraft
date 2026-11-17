# certgraft

A local OCI registry that serves upstream container images with the operator's extra CA roots
grafted in, for developers behind TLS-intercepting proxies. One Go module.

## Read these first

| File | Owns |
|------|------|
| openspec/config.yaml | the constitution: position, glossary, principles, conventions, change rules |
| openspec/specs/ | what each capability verifiably does today, and which packages carry it |
| openspec/changes/ | changes in flight: proposal, design, delta specs, tasks |
| internal/app/app.go | the one fx graph every command shares |
| .golangci.yml | lint rules and bans (use X, not Y) |
| docs/ | documentation for people |

## Build, test, run

- Docker is the only host requirement: every toolchain runs in a pinned container through make.
- `make cache-init` once, then `make install` to fetch modules.
- `make check` (lint then test) before every commit; `make fix` first. `make tidy` must leave no diff.
- `make image` builds the container image.
