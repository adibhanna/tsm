#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
APP=${APP:-tsm}
GHOSTTY_PREFIX=${GHOSTTY_PREFIX:-"$ROOT/.ghostty-prefix"}
DIST_DIR=${DIST_DIR:-"$ROOT/dist"}
VERSION=${VERSION:-$(git -C "$ROOT" describe --tags --exact-match 2>/dev/null || echo dev)}
COMMIT=${COMMIT:-$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo none)}
DATE=${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}
DIRTY=${DIRTY:-false}
if [[ "$COMMIT" != "none" ]]; then
  COMMIT=$(printf '%s' "$COMMIT" | cut -c1-7)
fi

case "$(uname -s)" in
  Darwin)
    os=darwin
    rpath='@executable_path'
    ;;
  Linux)
    os=linux
    rpath='$ORIGIN'
    ;;
  *)
    echo "unsupported OS: $(uname -s)" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *)
    echo "unsupported architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

PKG_PATH="$GHOSTTY_PREFIX/share/pkgconfig:$GHOSTTY_PREFIX/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
LIB_DIR="$GHOSTTY_PREFIX/lib"

if ! env PKG_CONFIG_PATH="$PKG_PATH" pkg-config --exists libghostty-vt; then
  echo "libghostty-vt not found in $GHOSTTY_PREFIX" >&2
  echo "Run 'make setup-ghostty-vt' first or override GHOSTTY_PREFIX." >&2
  exit 1
fi

shopt -s nullglob
libs=("$LIB_DIR"/libghostty-vt*.dylib "$LIB_DIR"/libghostty-vt*.so*)
shopt -u nullglob
if [[ ${#libs[@]} -eq 0 ]]; then
  echo "no libghostty-vt runtime files found in $LIB_DIR" >&2
  exit 1
fi

STAGE="$DIST_DIR/${APP}_${VERSION}_${os}_${arch}"
ARCHIVE="${STAGE}.tar.gz"
rm -rf "$STAGE"
mkdir -p "$STAGE"

(
  cd "$ROOT"
  env \
    PKG_CONFIG_PATH="$PKG_PATH" \
    go build \
      -ldflags "-s -w -X main.version=$VERSION -X main.commit=$COMMIT -X main.date=$DATE -X main.dirty=$DIRTY -linkmode external -extldflags -Wl,-rpath,${rpath}" \
      -o "$STAGE/$APP" .
)

cp -a "${libs[@]}" "$STAGE/"
cp "$ROOT/README.md" "$STAGE/"
mkdir -p "$STAGE/config/tsm"
cp "$ROOT/config/tsm/config.toml" "$STAGE/config/tsm/"
tar -C "$DIST_DIR" -czf "$ARCHIVE" "$(basename "$STAGE")"

echo "$ARCHIVE"
