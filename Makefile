APP      := tsm
MODULE   := github.com/adibhanna/tsm
PREFIX   := /usr/local/bin
LOCAL_GHOSTTY_PREFIX := $(CURDIR)/.ghostty-prefix
ifeq ($(wildcard $(LOCAL_GHOSTTY_PREFIX)),)
GHOSTTY_PREFIX ?= $(HOME)/.local
else
GHOSTTY_PREFIX ?= $(LOCAL_GHOSTTY_PREFIX)
endif
PKG_CONFIG_PATH_BUILD := $(GHOSTTY_PREFIX)/share/pkgconfig:$(GHOSTTY_PREFIX)/lib/pkgconfig$(if $(PKG_CONFIG_PATH),:$(PKG_CONFIG_PATH))
DYLD_LIBRARY_PATH_BUILD := $(GHOSTTY_PREFIX)/lib$(if $(DYLD_LIBRARY_PATH),:$(DYLD_LIBRARY_PATH))
BUILD_ENV := PKG_CONFIG_PATH="$(PKG_CONFIG_PATH_BUILD)" DYLD_LIBRARY_PATH="$(DYLD_LIBRARY_PATH_BUILD)"
EXTLDFLAGS := -Wl,-rpath,$(GHOSTTY_PREFIX)/lib

VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE     := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS  := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

.PHONY: build build-fallback run install uninstall test test-fallback lint fmt clean help check-ghostty-vt setup-ghostty-vt setup-ghostty-src release

setup-ghostty-src: ## Clone ghostty into ./ghostty if missing
	@test -d ghostty || git clone --depth=1 https://github.com/ghostty-org/ghostty.git ghostty

check-ghostty-vt:
	@env $(BUILD_ENV) pkg-config --exists libghostty-vt || \
		( echo "libghostty-vt not found." >&2; \
		  echo "Looked in: $(GHOSTTY_PREFIX)/share/pkgconfig and $(GHOSTTY_PREFIX)/lib/pkgconfig" >&2; \
		  echo "Build/install it first, or override GHOSTTY_PREFIX=/path/to/prefix." >&2; \
		  echo "Example:" >&2; \
		  echo "  git clone https://github.com/ghostty-org/ghostty.git ghostty" >&2; \
		  echo "  cd ghostty && zig build lib-vt --prefix $(GHOSTTY_PREFIX)" >&2; \
		  exit 1 )

setup-ghostty-vt: setup-ghostty-src ## Build libghostty-vt into ./.ghostty-prefix from ./ghostty
	cd ghostty && zig build lib-vt --prefix "$(LOCAL_GHOSTTY_PREFIX)"

release: check-ghostty-vt ## Build a self-contained current-platform release archive in dist/
	bash scripts/release_current_platform.sh

build: check-ghostty-vt ## Build the binary
	$(BUILD_ENV) go build -ldflags '$(LDFLAGS) -linkmode external -extldflags "$(EXTLDFLAGS)"' -o $(APP) .

build-fallback: ## Build the binary without libghostty-vt
	go build -tags noghosttyvt -ldflags '$(LDFLAGS)' -o $(APP) .

run: build ## Build and run (passes extra args: make run ARGS="list")
	$(BUILD_ENV) ./$(APP) $(ARGS)

install: build ## Install to /usr/local/bin
	install -m 755 $(APP) $(PREFIX)/$(APP)
	@echo "$(APP) installed to $(PREFIX)/$(APP)"

uninstall: ## Remove from /usr/local/bin
	rm -f $(PREFIX)/$(APP)
	@echo "$(APP) removed from $(PREFIX)/$(APP)"

test: check-ghostty-vt ## Run all tests
	$(BUILD_ENV) go test ./...

test-fallback: ## Run all tests without libghostty-vt
	go test -tags noghosttyvt ./...

lint: ## Run go vet
	go vet ./...

fmt: ## Format code
	gofmt -w .

clean: ## Remove build artifacts
	rm -f $(APP)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'
