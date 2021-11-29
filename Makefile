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

CTIMEVAR=-X $(PKG)/version.GitCommit=$(GITCOMMIT) \
	-X $(PKG)/version.BuildMeta=$(BUILDMETA) \
	-X $(PKG)/version.ProjectName=$(NAME) \
	-X $(PKG)/version.ProjectURL=$(PKG)

GO ?= "go"
GO_LDFLAGS=-ldflags "-s -w $(CTIMEVAR)"
GOOSARCHES = linux/amd64 darwin/amd64 windows/amd64


.PHONY: test 
test: ## test everything
	go test ./...

.PHONY: lint 
lint: ## run go mod tidy
	go run github.com/golangci/golangci-lint/cmd/golangci-lint run ./...

.PHONY: tidy 
tidy: ## run go mod tidy
	go mod tidy -compat=1.17

.PHONY: tools 
tools: ## generate protobuff files
	go install google.golang.org/protobuf/cmd/protoc-gen-go
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc
	buf generate

.PHONY: clean
clean: ## Cleanup any build binaries or packages.
	@echo "==> $@"
	$(RM) -r $(BINDIR)
	$(RM) -r $(BUILDDIR)

.PHONY: build
build: ## Build everything.
	@echo "==> $@"
	@CGO_ENABLED=0 GO111MODULE=on go build -tags "$(BUILDTAGS)" ${GO_LDFLAGS} -o $(BINDIR)/$(NAME) ./cmd/"$(NAME)"

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
