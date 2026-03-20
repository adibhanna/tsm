APP      := tsm
MODULE   := github.com/adibhanna/tsm
GHOSTTY_REVISION := c9e1006213eb9234209924c91285d6863e59ce4c
REQUIRED_ZIG_VERSION := $(shell grep '^ZIG_VERSION=' scripts/install_zig.sh | cut -d'"' -f2)
GO_VERSION := 1.25
PREFIX   ?= $(HOME)/.local
BINDIR   ?= $(if $(filter %/bin,$(PREFIX)),$(PREFIX),$(PREFIX)/bin)
LIBDIR   ?= $(if $(filter %/bin,$(PREFIX)),$(patsubst %/bin,%/lib/tsm,$(PREFIX)),$(PREFIX)/lib/tsm)
LOCAL_GHOSTTY_PREFIX := $(CURDIR)/.ghostty-prefix
ifeq ($(wildcard $(LOCAL_GHOSTTY_PREFIX)),)
GHOSTTY_PREFIX ?= $(HOME)/.local
else
GHOSTTY_PREFIX ?= $(LOCAL_GHOSTTY_PREFIX)
endif
PKG_CONFIG_PATH_BUILD := $(GHOSTTY_PREFIX)/share/pkgconfig:$(GHOSTTY_PREFIX)/lib/pkgconfig$(if $(PKG_CONFIG_PATH),:$(PKG_CONFIG_PATH))
DYLD_LIBRARY_PATH_BUILD := $(GHOSTTY_PREFIX)/lib$(if $(DYLD_LIBRARY_PATH),:$(DYLD_LIBRARY_PATH))
LD_LIBRARY_PATH_BUILD := $(GHOSTTY_PREFIX)/lib$(if $(LD_LIBRARY_PATH),:$(LD_LIBRARY_PATH))
BUILD_ENV := PKG_CONFIG_PATH="$(PKG_CONFIG_PATH_BUILD)" DYLD_LIBRARY_PATH="$(DYLD_LIBRARY_PATH_BUILD)" LD_LIBRARY_PATH="$(LD_LIBRARY_PATH_BUILD)"
EXTLDFLAGS := -Wl,-rpath,$(GHOSTTY_PREFIX)/lib
INSTALL_EXTLDFLAGS := -Wl,-rpath,$(LIBDIR)

VERSION  := $(shell git describe --tags --exact-match 2>/dev/null || echo dev)
COMMIT   := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE     := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
DIRTY    := $(shell if ! git diff --quiet --ignore-submodules -- 2>/dev/null || ! git diff --cached --quiet --ignore-submodules -- 2>/dev/null; then echo true; else echo false; fi)

LDFLAGS  := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE) \
	-X main.dirty=$(DIRTY)

.PHONY: build run install uninstall test lint fmt clean help check-bootstrap-deps check-ghostty-vt setup-ghostty-vt setup-ghostty-src setup release

check-bootstrap-deps: ## Verify bootstrap tools (go, zig, pkg-config) are available
	@command -v go >/dev/null 2>&1 || \
		( echo "go not found." >&2; \
		  echo "Install Go $(GO_VERSION)+: https://go.dev/dl/ or use mise/asdf." >&2; \
		  exit 1 )
	@command -v zig >/dev/null 2>&1 || \
		( echo "zig not found." >&2; \
		  echo "Install Zig $(REQUIRED_ZIG_VERSION): https://ziglang.org/download/" >&2; \
		  echo "Or run: bash scripts/install_zig.sh && export PATH=\"\$$PWD/zig-local:\$$PATH\"" >&2; \
		  exit 1 )
	@actual_zig=$$(zig version 2>/dev/null); \
	if [ "$$actual_zig" != "$(REQUIRED_ZIG_VERSION)" ]; then \
		echo "zig version mismatch: found $$actual_zig, need $(REQUIRED_ZIG_VERSION)." >&2; \
		echo "Run: bash scripts/install_zig.sh && export PATH=\"\$$PWD/zig-local:\$$PATH\"" >&2; \
		exit 1; \
	fi
	@command -v pkg-config >/dev/null 2>&1 || \
		( echo "pkg-config not found." >&2; \
		  echo "Install it via your package manager (brew install pkg-config, apt install pkg-config)." >&2; \
		  exit 1 )

setup-ghostty-src: ## Clone ghostty at pinned revision into ./ghostty
	@if [ ! -d ghostty ]; then \
		git init ghostty && \
		git -C ghostty fetch --depth=1 https://github.com/ghostty-org/ghostty.git $(GHOSTTY_REVISION) && \
		git -C ghostty checkout FETCH_HEAD; \
	elif [ "$$(git -C ghostty rev-parse HEAD)" != "$(GHOSTTY_REVISION)" ]; then \
		echo "ghostty/ exists but is at $$(git -C ghostty rev-parse --short HEAD), expected $(GHOSTTY_REVISION)." >&2; \
		echo "Updating to pinned revision..." >&2; \
		git -C ghostty fetch --depth=1 https://github.com/ghostty-org/ghostty.git $(GHOSTTY_REVISION) && \
		git -C ghostty checkout FETCH_HEAD; \
	fi

check-ghostty-vt:
	@env $(BUILD_ENV) pkg-config --exists libghostty-vt || \
		( echo "libghostty-vt not found." >&2; \
		  echo "Looked in: $(GHOSTTY_PREFIX)/share/pkgconfig and $(GHOSTTY_PREFIX)/lib/pkgconfig" >&2; \
		  echo "Run 'make setup' to bootstrap the build environment, or override GHOSTTY_PREFIX=/path/to/prefix." >&2; \
		  exit 1 )

setup-ghostty-vt: setup-ghostty-src ## Build libghostty-vt into ./.ghostty-prefix from ./ghostty
	cd ghostty && zig build lib-vt --prefix "$(LOCAL_GHOSTTY_PREFIX)"

setup: check-bootstrap-deps setup-ghostty-vt ## First-time setup: verify deps and build libghostty-vt
	@echo ""
	@echo "Setup complete. You can now run:"
	@echo "  make build    # build tsm"
	@echo "  make test     # run tests"
	@echo "  make lint     # run linter"

release: check-ghostty-vt ## Build a self-contained current-platform release archive in dist/
	bash scripts/release_current_platform.sh

build: check-ghostty-vt ## Build the binary
	$(BUILD_ENV) go build -ldflags '$(LDFLAGS) -linkmode external -extldflags "$(EXTLDFLAGS)"' -o $(APP) .

run: build ## Build and run (passes extra args: make run ARGS="list")
	$(BUILD_ENV) ./$(APP) $(ARGS)

install: check-ghostty-vt ## Install binary and bundled libghostty-vt under PREFIX
	mkdir -p "$(BINDIR)" "$(LIBDIR)"
	$(BUILD_ENV) go build -ldflags '$(LDFLAGS) -linkmode external -extldflags "$(INSTALL_EXTLDFLAGS)"' -o $(APP) .
	install -m 755 $(APP) "$(BINDIR)/$(APP)"
	@for lib in "$(GHOSTTY_PREFIX)"/lib/libghostty-vt*; do \
		test -e "$$lib" || continue; \
		if [ -L "$$lib" ]; then \
			ln -sf "$$(readlink "$$lib")" "$(LIBDIR)/$$(basename "$$lib")"; \
		else \
			install -m 755 "$$lib" "$(LIBDIR)/$$(basename "$$lib")"; \
		fi; \
	done
	@echo "$(APP) installed to $(BINDIR)/$(APP)"
	@echo "libghostty-vt installed to $(LIBDIR)"

uninstall: ## Remove installed binary and bundled libghostty-vt under PREFIX
	rm -f "$(BINDIR)/$(APP)"
	rm -f "$(LIBDIR)"/libghostty-vt*
	-rmdir "$(LIBDIR)" 2>/dev/null || true
	@echo "$(APP) removed from $(BINDIR)/$(APP)"
	@echo "libghostty-vt removed from $(LIBDIR)"

test: check-ghostty-vt ## Run all tests
	$(BUILD_ENV) go test ./...

lint: check-ghostty-vt ## Run go vet
	$(BUILD_ENV) go vet ./...

fmt: ## Format code
	gofmt -w .

clean: ## Remove build artifacts
	rm -f $(APP)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'
