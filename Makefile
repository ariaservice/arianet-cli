BINARY     := arianet
MODULE     := github.com/arianet/arianet-cli
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS    := -s -w \
	-X $(MODULE)/pkg/version.Version=$(VERSION) \
	-X $(MODULE)/pkg/version.Commit=$(COMMIT) \
	-X $(MODULE)/pkg/version.BuildDate=$(BUILD_DATE)

BIN_DIR    := bin
MAIN       := ./cmd/arianet

.PHONY: all build install clean test lint completions release cross

all: build

## build: compile for the current platform
build:
	@mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(MAIN)
	@echo "Built $(BIN_DIR)/$(BINARY)"

## install: install to /usr/local/bin
install: build
	install -m 0755 $(BIN_DIR)/$(BINARY) /usr/local/bin/$(BINARY)
	@echo "Installed /usr/local/bin/$(BINARY)"

## run: build and run
run: build
	$(BIN_DIR)/$(BINARY) $(ARGS)

## test: run all tests
test:
	go test -v ./...

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## tidy: tidy go modules
tidy:
	go mod tidy

## completions: generate shell completion scripts
completions: build
	@mkdir -p completions
	$(BIN_DIR)/$(BINARY) completion bash  > completions/$(BINARY).bash
	$(BIN_DIR)/$(BINARY) completion zsh   > completions/_$(BINARY)
	$(BIN_DIR)/$(BINARY) completion fish  > completions/$(BINARY).fish
	$(BIN_DIR)/$(BINARY) completion powershell > completions/$(BINARY).ps1
	@echo "Shell completions written to completions/"

## cross: cross-compile for all supported platforms
cross:
	@mkdir -p $(BIN_DIR)
	GOOS=linux   GOARCH=amd64  go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY)_linux_amd64   $(MAIN)
	GOOS=linux   GOARCH=arm64  go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY)_linux_arm64   $(MAIN)
	GOOS=darwin  GOARCH=amd64  go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY)_darwin_amd64  $(MAIN)
	GOOS=darwin  GOARCH=arm64  go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY)_darwin_arm64  $(MAIN)
	GOOS=windows GOARCH=amd64  go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY)_windows_amd64.exe $(MAIN)
	@echo "Cross-compiled binaries in $(BIN_DIR)/"

## release: create a release with GoReleaser
release:
	goreleaser release --clean

## snapshot: create a snapshot release (no publish)
snapshot:
	goreleaser release --snapshot --clean

## clean: remove build artifacts
clean:
	rm -rf $(BIN_DIR) dist

help:
	@echo "Available targets:"
	@grep -E '^## ' Makefile | sed 's/## /  /'
