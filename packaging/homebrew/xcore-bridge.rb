class XcoreBridge < Formula
  desc "Wrap xray-core VLESS nodes as Surge External Proxy programs"
  homepage "https://github.com/backrunner/xcore-bridge"
  url "https://github.com/backrunner/xcore-bridge/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "REPLACE_WITH_RELEASE_TARBALL_SHA256"
  license "MIT"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.version=#{version}"), "./cmd/xcore-bridge"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/xcore-bridge version")
  end
end
