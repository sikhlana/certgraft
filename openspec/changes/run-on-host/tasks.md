# Tasks

## 1. Spike the runtime paths (go/no-go for D3, D4)

- [ ] 1.1 In `.scratch/<date>-runtime-spike/`, with a throwaway registry on the host's `127.0.0.1:5555`, run design D9's four checks on Docker Desktop, then on OrbStack, in one restart window each. Back up each daemon config first and restore it afterwards. Verify with one finding per check, linking the build and registry logs.
- [ ] 1.2 If any check fails, update proposal.md, design.md (D3/D4) and the delta specs before continuing. Verify with `openspec validate run-on-host --strict` passing.

## 2. Host serving and configuration

- [ ] 2.1 Replace `roots.dir` with `roots.files`, rejecting an empty list, a missing path and a file without PEM, each naming the path, in the roots loader rather than a struct tag (D2). Document that `CERTGRAFT_ROOTS_FILES` is comma-separated with spaces trimmed. Verify with tests for each case and the env form (`"a.pem, b.pem"` loads both).
- [ ] 2.2 Default the registry to `127.0.0.1` on port 5555 and `store.dir` to `~/.cache/certgraft` (a resolver over the zero value). Verify with config tests, and a test that a running server's address is 127.0.0.1.
- [ ] 2.3 Remove `internal/health`, the Dockerfile, `.dockerignore`, `make image` and the hadolint step from the Makefile, and drop `make image` from AGENTS.md's Build, test, run. Verify `make check` passes, the serve graph test lists only the registry runner, and `grep -rn "make image\|hadolint\|Dockerfile" AGENTS.md Makefile` finds nothing.
- [ ] 2.4 Update the constitution's convention in `openspec/config.yaml` from "Workflows pass actionlint and the Dockerfile passes hadolint (make lint)" to actionlint only. Verify `openspec validate --specs --strict` passes and the line names no Dockerfile.

## 3. Source policy and runtimes

- [ ] 3.1 Add `internal/policy`: generate the two rules from the registry's allowlist (one list, used by both), targeting `host.docker.internal` at a given port, and read the target `host:port` back out of a policy file. Verify with tests comparing generated rules to the spec's at port 5555 and at 6000, one that every allowlisted host appears, and a round-trip of the port.
- [ ] 3.2 Add `internal/runtime` for Docker Desktop and OrbStack: detection, the daemon config path, adding and removing `insecure-registries` (and `registry-mirrors` with `--mirror`) for a given port, a timestamped backup only when something changes, and the restart instruction. Verify with tests on a throwaway HOME: configure twice makes one backup, other keys are kept, remove takes out only certgraft's entries, and a non-default port gives `host.docker.internal:6000`.
- [ ] 3.3 Add `certgraft configure [--runtime ...] [--mirror] [--remove]`, loading the config and writing the policy file and runtime entries from the configured registry port. Verify with a command test on a throwaway HOME that checks the output names each restart, starts no process, and uses the configured port.

## 4. Shell and status

- [ ] 4.1 Add `certgraft init zsh|bash|fish` as a plain Cobra command outside the kernel (D5): read the policy file, take the port from its target, probe `/v2/` on `127.0.0.1:<port>` within 300 ms, and print the export line or nothing; never read the config, never write to stderr, always exit 0. Verify with tests for a listening port, a closed port, no policy file, and no config file with `roots.files` unset, each asserting stdout, empty stderr and exit 0.
- [ ] 4.2 Add `certgraft status`: the cheap checks in design D7 against the configured port, exiting non-zero with both fixes named (`brew services start certgraft` or `certgraft serve`, and unsetting the env var) when the policy is set and the registry is down, and `status --build` for the rewrite check. Verify with tests of the down case, the healthy case against a test server, a policy targeting a different port than the config, and that the default runs no build.

## 5. Releases and docs

- [ ] 5.1 Add `.goreleaser.yaml` (darwin/linux × arm64/amd64, CGO off, version ldflags, checksums; no Homebrew section). Verify with `goreleaser check` and a `--snapshot` build in the pinned GoReleaser image.
- [ ] 5.2 Add `packaging/homebrew/certgraft.rb.tmpl`: a formula building from the tag tarball with `depends_on "go" => :build`, the version ldflags, a `test do` that runs `certgraft version`, and design D6's service block; only the version and sha256 are filled in. Verify by rendering it against a local `git archive` tarball, then `brew audit --strict --formula` on the result, `brew install --build-from-source` of it, the three scenarios of "Homebrew keeps certgraft running as a service" (start, kill and restart, stop), and `brew uninstall`.
- [ ] 5.3 Add the tag-triggered release workflow: GoReleaser, then render the formula with the tag's tarball sha256 and push it to `sikhlana/homebrew-tap` as `Formula/certgraft.rb` with a token from a repository secret (fine-grained, tap contents only). Verify with actionlint, and a dry run of the render step against a local tag that prints the formula instead of pushing it.
- [ ] 5.4 Write `docs/`: install with Homebrew and `brew services`, setup on Docker Desktop and OrbStack, running without Homebrew (`certgraft serve`, with a sample launchd plist), turning it off, the push caveat, failure modes, configuration keys (including `CERTGRAFT_ROOTS_FILES`' comma form, changing the port means re-running `configure`, and that a `.certgraft.yaml` in the working directory overrides `~/.config/certgraft/`), and how it works. Update README's How section. Verify each documented command runs as written on this Mac.

## 6. End to end and gates

- [ ] 6.1 With certgraft installed from the locally rendered formula, running under `brew services`, and both runtimes configured: build `FROM node:22-alpine` with a `RUN` step reaching a host the proxy intercepts, on each runtime; then the digest pin, the frontend pass-through, `init` with the registry down, and `status` down. Record each in `.scratch/` findings. Verify every scenario passes, or list the failures with their output.
- [ ] 6.2 Run `make fix`, `make lint`, `make test`, `make tidy` (no diff) and `make vulncheck`. Verify all pass.
