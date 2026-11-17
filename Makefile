# certgraft: every toolchain runs in a pinned container, so Docker is all the host needs.
# Only `install`, `tidy` and `vulncheck` get network; everything else runs offline against the module cache.
.DEFAULT_GOAL := help
.PHONY: help cache-init install tidy fix lint test test-short build generate vulncheck image check

GO_VERSION := $(shell cat .go-version)
# Go plus golangci-lint, public and pinned. Keep its version equal to GOLANGCI_LINT_VERSION in
# .github/workflows/ci.yaml. Any image with Go and golangci-lint works as an override.
GO_TOOLS_IMAGE   ?= golangci/golangci-lint:v2.15.0@sha256:fbfcfd12a6f8d7f1b97b3362ca80fc2a244d46863148c73cb88e7793af96c481
ACTIONLINT_IMAGE ?= rhysd/actionlint:1.7.12@sha256:b1934ee5f1c509618f2508e6eb47ee0d3520686341fec936f3b79331f9315667
HADOLINT_IMAGE   ?= hadolint/hadolint:v2.15.1@sha256:32dac94127fd60b7b7e3fbfc65e1383b9b5e25c9bfd7b8536de7a539fe68a12d
GO_CACHE_VOL     := certgraft-go-cache

# The checkout is mounted at /work, with .git hidden behind an empty tmpfs; files are written as the host user.
RUN_BASE = docker run --rm -i -u $$(id -u):$$(id -g) -e HOME=/tmp -v $(CURDIR):/work -w /work --tmpfs /work/.git
GO_ENV   = -e GOMODCACHE=/cache/mod -e GOCACHE=/cache/build -e GOLANGCI_LINT_CACHE=/cache/lint -e GOTOOLCHAIN=local \
  -v $(GO_CACHE_VOL):/cache
GO_FETCH = $(RUN_BASE) $(GO_ENV) -e GOFLAGS=-buildvcs=false $(GO_TOOLS_IMAGE)
GO_RUN   = $(RUN_BASE) --network none $(GO_ENV) -e GOPROXY=off -e "GOFLAGS=-mod=mod -buildvcs=false" $(GO_TOOLS_IMAGE)

## help: list the targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'

## cache-init: hand the Go cache volume to your user (once)
cache-init:
	docker run --rm -u 0 -v $(GO_CACHE_VOL):/cache $(GO_TOOLS_IMAGE) chown $$(id -u):$$(id -g) /cache < /dev/null

## install: fetch the modules into the cache volume
install:
	$(GO_FETCH) go mod download < /dev/null

## tidy: go mod tidy
tidy:
	$(GO_FETCH) go mod tidy < /dev/null

## fix: golangci-lint --fix (formatters, and modernize through its linter)
fix:
	$(GO_RUN) golangci-lint run --fix ./... < /dev/null

## lint: golangci-lint, actionlint on the workflows, hadolint on the Dockerfile
lint:
	$(GO_RUN) golangci-lint run ./... < /dev/null
	$(RUN_BASE) --network none $(ACTIONLINT_IMAGE) -config-file .github/actionlint.yaml < /dev/null
	docker run --rm -i --network none $(HADOLINT_IMAGE) < Dockerfile

## test: every test
test:
	$(GO_RUN) go test -count=1 ./... < /dev/null

## test-short: tests without the slow ones
test-short:
	$(GO_RUN) go test -count=1 -short ./... < /dev/null

## build: compile everything
build:
	$(GO_RUN) go build ./... < /dev/null

## generate: regenerate mocks (mockgen, pinned by the tool directive in go.mod)
generate:
	$(GO_RUN) go generate ./... < /dev/null

## vulncheck: govulncheck (needs the vulnerability database, so it has network)
vulncheck:
	$(RUN_BASE) $(GO_ENV) $(GO_TOOLS_IMAGE) go tool govulncheck ./... < /dev/null

## image: build the container image as certgraft
image:
	docker build --build-arg GO_VERSION="$(GO_VERSION)" \
		--build-arg VERSION="$$(git describe --tags --always --dirty 2>/dev/null)" \
		--build-arg REVISION="$$(git rev-parse HEAD 2>/dev/null)" \
		-t certgraft .

## check: lint and test, as CI runs them
check: lint test
