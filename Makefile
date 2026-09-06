# Makefile for agent-fitness-functions

PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
DESTDIR ?=

GO ?= go
GOFLAGS ?=
CGO_ENABLED ?= 0

BIN_NAME ?= agent-fitness-functions
BIN_DIR ?= bin
CMD_DIR ?= ./cmd

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

HELPERS = agent-fitness-functions-serve agent-fitness-functions-test

# The hazard is a cp-style in-place overwrite of a running, signed binary: on Apple
# Silicon the stale code-signature cache SIGKILLs the next exec. macOS install(1)
# already replaces the target atomically (temporary file, then rename); -S additionally
# flushes the copy and states that atomic-replace intent explicitly. GNU coreutils
# install reads -S as --suffix=SUFFIX, so the flag stays on the Darwin branch.
ifeq ($(shell uname -s),Darwin)
INSTALL_BIN ?= install -S -m 755
else
INSTALL_BIN ?= install -m 755
endif

.PHONY: all build build-all install uninstall test test-short fmt vet clean dev-certs help

all: build

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build         Build $(BIN_NAME) into $(BIN_DIR)/ (default)"
	@echo "  build-all     Build all binaries ($(BIN_NAME) and rename-verify)"
	@echo "  install       Build and install binaries and helpers into $(DESTDIR)$(BINDIR)"
	@echo "  uninstall     Remove installed binaries and helpers from $(DESTDIR)$(BINDIR)"
	@echo "  test          Run all tests"
	@echo "  test-short    Run tests with -short flag"
	@echo "  fmt           Format Go source files"
	@echo "  vet           Run go vet"
	@echo "  dev-certs     Generate local development mTLS certificates"
	@echo "  clean         Remove built binaries"
	@echo ""
	@echo "Variables:"
	@echo "  PREFIX        Install prefix (default: $(HOME)/.local)"
	@echo "  BINDIR        Binary directory (default: $(PREFIX)/bin)"
	@echo "  DESTDIR       Staging directory prefix (default: empty)"
	@echo "  INSTALL_BIN   Install command for binaries (default: $(INSTALL_BIN))"

build:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) -o $(BIN_DIR)/$(BIN_NAME) $(CMD_DIR)/$(BIN_NAME)

build-all: build
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) -o $(BIN_DIR)/rename-verify $(CMD_DIR)/rename-verify

install: build
	@mkdir -p $(DESTDIR)$(BINDIR)
	$(INSTALL_BIN) $(BIN_DIR)/$(BIN_NAME) $(DESTDIR)$(BINDIR)/$(BIN_NAME)
	@for helper in $(HELPERS); do \
		if [ -f "$(BIN_DIR)/$$helper" ]; then \
			echo "Installing $$helper to $(DESTDIR)$(BINDIR)/$$helper"; \
			$(INSTALL_BIN) "$(BIN_DIR)/$$helper" "$(DESTDIR)$(BINDIR)/$$helper"; \
		fi \
	done
	@echo "Installed $(BIN_NAME) and helpers into $(DESTDIR)$(BINDIR)"

uninstall:
	@rm -f $(DESTDIR)$(BINDIR)/$(BIN_NAME)
	@for helper in $(HELPERS); do \
		rm -f "$(DESTDIR)$(BINDIR)/$$helper"; \
	done
	@echo "Uninstalled $(BIN_NAME) and helpers from $(DESTDIR)$(BINDIR)"

test:
	$(GO) test ./...

test-short:
	$(GO) test -short ./...

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

dev-certs:
	./scripts/generate-dev-certs.sh

clean:
	rm -f $(BIN_DIR)/$(BIN_NAME) $(BIN_DIR)/rename-verify $(BIN_NAME)
