#!/usr/bin/env bash
set -euo pipefail

# Install Zig to a local directory and optionally add it to PATH.
# Used by CI workflows and available for local contributor setup.
#
# Usage:
#   scripts/install_zig.sh              # install to ./zig-local
#   scripts/install_zig.sh /tmp/zig     # install to custom prefix
#
# After running, add the printed directory to your PATH.

ZIG_VERSION="0.15.2"

dest="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/zig-local}"

case "$(uname -s)" in
  Linux)  os=linux ;;
  Darwin) os=macos ;;
  *)
    echo "unsupported OS: $(uname -s)" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  x86_64)       arch=x86_64 ;;
  arm64|aarch64) arch=aarch64 ;;
  *)
    echo "unsupported arch: $(uname -m)" >&2
    exit 1
    ;;
esac

# Skip download if the right version is already there.
if [[ -x "$dest/zig" ]] && "$dest/zig" version 2>/dev/null | grep -q "^${ZIG_VERSION}$"; then
  echo "$dest"
  exit 0
fi

archive="zig-${arch}-${os}-${ZIG_VERSION}.tar.xz"
url="https://ziglang.org/download/${ZIG_VERSION}/${archive}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

curl -fSL "$url" -o "$tmp/$archive"
tar -xf "$tmp/$archive" -C "$tmp"

rm -rf "$dest"
mv "$tmp/zig-${arch}-${os}-${ZIG_VERSION}" "$dest"

echo "$dest"
