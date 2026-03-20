# Contributing to tsm

## Prerequisites

Install these before building:

| Tool         | Version | Install                                                            |
| ------------ | ------- | ------------------------------------------------------------------ |
| Go           | 1.25+   | https://go.dev/dl/ or `mise install go`                            |
| Zig          | 0.15.2  | https://ziglang.org/download/ or `bash scripts/install_zig.sh`     |
| pkg-config   | any     | `brew install pkg-config` (macOS) / `apt install pkg-config` (Linux) |

`scripts/install_zig.sh` downloads Zig 0.15.2 into `./zig-local`. Add it to your PATH:

```bash
bash scripts/install_zig.sh
export PATH="$PWD/zig-local:$PATH"
```

## First-Time Setup

```bash
git clone https://github.com/adibhanna/tsm.git
cd tsm
make setup
```

`make setup` will:

1. verify Go, Zig, and pkg-config are available (`check-bootstrap-deps`)
2. clone Ghostty and build `libghostty-vt` into `./.ghostty-prefix` (`setup-ghostty-vt`)
3. print next steps

After setup completes, the build environment is ready.

## Build, Test, Lint

```bash
make build      # build the tsm binary
make test       # run all tests
make lint       # run go vet
make fmt        # format code with gofmt
```

All `make` targets handle the `PKG_CONFIG_PATH` and library paths automatically. Use `make` commands rather than bare `go` commands.

## Install Locally

```bash
make install                        # installs to ~/.local/bin + ~/.local/lib/tsm
make install PREFIX=/opt/homebrew   # custom prefix
make uninstall                      # clean removal
```

## Build a Release Archive

```bash
make release
```

This creates a self-contained archive for the current platform under `dist/`.

## What CI Runs

Every push and PR runs the same `make` targets:

1. `gofmt` check
2. `make setup` (installs Zig, builds libghostty-vt)
3. `make lint`
4. `make test`
5. `make build`

If it passes locally with `make lint && make test && make build`, it will pass in CI.

## Code Style

- Format with `gofmt` (run `make fmt`)
- No additional linters beyond `go vet`

## Pinned Versions

| Dependency | Where it's pinned | Enforced by |
| ---------- | ----------------- | ----------- |
| Go         | `mise.toml`, CI workflows | `actions/setup-go` in CI, mise locally |
| Zig        | `scripts/install_zig.sh` | `make check-bootstrap-deps` (exact version match) |
| Ghostty    | `GHOSTTY_REVISION` in `Makefile` | `make setup-ghostty-src` (fetches pinned commit) |

To bump Ghostty, update `GHOSTTY_REVISION` in the Makefile to the new commit hash and re-run `make setup`.
