BIN     := later
PREFIX  ?= $(HOME)/.local
BINDIR  := $(PREFIX)/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install uninstall check release clean

# Pure Go, no syscalls beyond flock and the filesystem, so every desktop
# platform the agents run on is fair game.
PLATFORMS ?= darwin/arm64 darwin/amd64 linux/amd64 linux/arm64

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) .

install: build
	@mkdir -p $(BINDIR)
	install -m 0755 $(BIN) $(BINDIR)/$(BIN)
	@echo "installed $(BINDIR)/$(BIN) ($(VERSION))"

uninstall:
	rm -f $(BINDIR)/$(BIN)

check: build
	go vet ./...
	go test ./...
	./$(BIN) --version > /dev/null
	@echo ok

release: ## Cross-compile, package, tag, and publish (make release VERSION=v0.1.0)
	@BINARY="$(BIN)" VERSION="$(VERSION)" PLATFORMS="$(PLATFORMS)" ./scripts/release.sh

clean:
	rm -rf $(BIN) dist
