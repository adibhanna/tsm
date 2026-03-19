class Tsm < Formula
  desc "Terminal session manager with persistent sessions"
  homepage "https://github.com/adibhanna/tsm"
  license "MIT"
  head "https://github.com/adibhanna/tsm.git", branch: "main"

  depends_on "go" => :build
  depends_on "pkgconf" => :build
  depends_on "zig" => :build

  resource "ghostty" do
    url "https://github.com/ghostty-org/ghostty/archive/refs/heads/main.tar.gz"
    sha256 :no_check
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
