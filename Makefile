CONTAINER_SUBSYS ?= podman
REGISTRY ?= quay.io
IMAGE_NAME ?= clcollins/mnemo
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_SHA ?= $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
CI_IMAGE ?= mnemo-ci:latest

BINARY := mnemo
BUILD_DIR := $(or $(GOBIN),/tmp)

GO := CGO_ENABLED=0 go
GOFLAGS := -trimpath
LDFLAGS := -s -w

-include .env

.PHONY: build
build:
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/mnemo

.PHONY: test
test: ci-all

.PHONY: unit-test
unit-test:
	$(GO) test ./... -v -count=1

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: fmt-check
fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "Files need formatting:"; gofmt -l .; exit 1)

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: tidy-check
tidy-check:
	$(GO) mod tidy
	@git diff --exit-code go.mod go.sum || (echo "go.mod/go.sum not tidy"; exit 1)

.PHONY: yaml-lint
yaml-lint:
	yamllint -c .yamllint.yaml .

.PHONY: markdown-lint
markdown-lint:
	markdownlint-cli2 '**/*.md' '#vendor'

.PHONY: docs-check
docs-check:
	@test -d docs/plans && test "$$(find docs/plans -name '*.md' | wc -l)" -gt 0 || \
		(echo "No plan documents found in docs/plans/"; exit 1)

.PHONY: ci-build
ci-build:
	$(CONTAINER_SUBSYS) build -f test/Containerfile.ci -t $(CI_IMAGE) .

.PHONY: ci-checks
ci-checks: unit-test vet fmt-check tidy-check

.PHONY: ci-all
ci-all: ci-build
	$(CONTAINER_SUBSYS) run --rm \
		-v $(CURDIR):/src:Z \
		-w /src \
		$(CI_IMAGE) \
		make ci-checks

.PHONY: clean
clean:
	rm -f $(BUILD_DIR)/$(BINARY)
	$(GO) clean -testcache
