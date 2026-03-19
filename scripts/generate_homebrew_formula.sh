#!/usr/bin/env bash
set -euo pipefail

VERSION=${VERSION:?VERSION is required}
REPOSITORY=${REPOSITORY:?REPOSITORY is required}
DARWIN_AMD64_URL=${DARWIN_AMD64_URL:?DARWIN_AMD64_URL is required}
DARWIN_AMD64_SHA256=${DARWIN_AMD64_SHA256:?DARWIN_AMD64_SHA256 is required}
DARWIN_ARM64_URL=${DARWIN_ARM64_URL:?DARWIN_ARM64_URL is required}
DARWIN_ARM64_SHA256=${DARWIN_ARM64_SHA256:?DARWIN_ARM64_SHA256 is required}
LINUX_AMD64_URL=${LINUX_AMD64_URL:?LINUX_AMD64_URL is required}
LINUX_AMD64_SHA256=${LINUX_AMD64_SHA256:?LINUX_AMD64_SHA256 is required}
LINUX_ARM64_URL=${LINUX_ARM64_URL:?LINUX_ARM64_URL is required}
LINUX_ARM64_SHA256=${LINUX_ARM64_SHA256:?LINUX_ARM64_SHA256 is required}

cat <<EOF
class Tsm < Formula
  desc "Terminal session manager with persistent sessions"
  homepage "https://github.com/${REPOSITORY}"
  license "MIT"
  version "${VERSION}"

  on_macos do
    if Hardware::CPU.arm?
      url "${DARWIN_ARM64_URL}"
      sha256 "${DARWIN_ARM64_SHA256}"
    else
      url "${DARWIN_AMD64_URL}"
      sha256 "${DARWIN_AMD64_SHA256}"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "${LINUX_ARM64_URL}"
      sha256 "${LINUX_ARM64_SHA256}"
    else
      url "${LINUX_AMD64_URL}"
      sha256 "${LINUX_AMD64_SHA256}"
    end
  end

  def install
    entries = Dir[buildpath/"*"]
    if entries.length == 1 && File.directory?(entries.first)
      entries = Dir[entries.first/"*"]
    end
    raise "unexpected archive layout" if entries.empty?

    libexec.install entries
    (bin/"tsm").write_env_script(libexec/"tsm", {})
  end

  test do
    assert_match "tsm", shell_output("#{bin}/tsm help")
    assert_match "backend=libghostty-vt", shell_output("#{bin}/tsm version")
  end
end
EOF
