class Tsm < Formula
  desc "Terminal session manager with persistent sessions"
  homepage "https://github.com/adibhanna/tsm"
  license "MIT"
  url "https://github.com/adibhanna/tsm/archive/4df37fd61b5f16e9e9f78db31f6c9cda0fa2b0b9.tar.gz"
  version "4df37fd"
  sha256 "75109fb8fdc7ac54877473131c6991644d3b83aaef05b68bdb51f0e2c0efa9c7"
  head "https://github.com/adibhanna/tsm.git", branch: "main"

  depends_on "go" => :build
  depends_on "pkgconf" => :build
  depends_on "zig" => :build

  resource "ghostty" do
    url "https://github.com/ghostty-org/ghostty/archive/c9e1006213eb9234209924c91285d6863e59ce4c.tar.gz"
    sha256 "a46adceb08eb84d0dc460a7a079492b0e3efe1062ece3030ea35b2b583ce42a9"
  end

  def install
    ghostty_prefix = buildpath/"ghostty-prefix"

    resource("ghostty").stage do
      system "zig", "build", "lib-vt", "--prefix", ghostty_prefix.to_s
    end

    ENV["PKG_CONFIG_PATH"] = [
      "#{ghostty_prefix}/share/pkgconfig",
      "#{ghostty_prefix}/lib/pkgconfig",
    ].join(":")

    output = buildpath/"tsm"
    system "go", "build",
      "-ldflags", "-s -w -X main.dirty=false -linkmode external -extldflags -Wl,-rpath,#{libexec}",
      "-o", output,
      "."

    libexec.install output
    Dir["#{ghostty_prefix}/lib/libghostty-vt*"].each do |lib|
      libexec.install lib
    end
    (bin/"tsm").write_env_script(libexec/"tsm", {})
  end

  test do
    assert_match "tsm", shell_output("#{bin}/tsm help")
    assert_match "backend=libghostty-vt", shell_output("#{bin}/tsm version")
  end
end
