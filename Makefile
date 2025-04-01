PREFIX?=$(shell pwd)
NAME := pomerium-cli
PKG := github.com/pomerium/cli


BUILDDIR := ${PREFIX}/dist
BINDIR := ${PREFIX}/bin

GITCOMMIT := $(shell git rev-parse --short HEAD)
BUILDMETA:=
GITUNTRACKEDCHANGES := $(shell git status --porcelain --untracked-files=no)
ifneq ($(GITUNTRACKEDCHANGES),)
	BUILDMETA := dirty
endif

# Keep cgo disabled on Linux, to avoid the cgo version of the Go standard
# library 'net' package. On macOS and Windows we need cgo for the system cert
# store integrations, but as of Go 1.20 the 'net' package does not use cgo on
# macOS, and the 'net' package never used cgo on Windows (see
# https://go.dev/doc/go1.20#cgo).
ifeq ($(shell uname),Linux)
	export CGO_ENABLED=0
endif

CTIMEVAR=-X $(PKG)/version.GitCommit=$(GITCOMMIT) \
	-X $(PKG)/version.BuildMeta=$(BUILDMETA) \
	-X $(PKG)/version.ProjectName=$(NAME) \
	-X $(PKG)/version.ProjectURL=$(PKG)

GO ?= "go"
GO_LDFLAGS=-ldflags "-s -w $(CTIMEVAR)"
GOOSARCHES = linux/amd64 darwin/amd64 windows/amd64

.PHONY: all
all: clean lint test build

.PHONY: test
test: ## test everything
	go test ./...


.PHONY: lint
lint:
	@echo "@==> $@"
	@VERSION=$$(go run github.com/mikefarah/yq/v4@v4.34.1 '.jobs.lint.steps[] | select(.uses == "golangci/golangci-lint-action*") | .with.version' .github/workflows/lint.yml) && \
	go run github.com/golangci/golangci-lint/cmd/golangci-lint@$$VERSION run ./...

.PHONY: tidy
tidy: ## run go mod tidy
	go mod tidy -compat=1.19

.PHONY: tools
tools: generate

.PHONY: generate
generate:
	@echo "==> $@"
	go run github.com/bufbuild/buf/cmd/buf@v1.51.0 generate

.PHONY: clean
clean: ## Cleanup any build binaries or packages.
	@echo "==> $@"
	$(RM) -r $(BINDIR)
	$(RM) -r $(BUILDDIR)

.PHONY: build
build: ## Build everything.
	@echo "==> $@"
	@go build -tags "$(BUILDTAGS)" $(GO_LDFLAGS) -o $(BINDIR)/$(NAME) ./cmd/"$(NAME)"

.PHONY: snapshot
snapshot: ## Create release snapshot
	APPARITOR_GITHUB_TOKEN=foo VERSION_FLAGS="$(CTIMEVAR)" goreleaser release --snapshot --rm-dist


.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
