class XcoreBridgeBeta < Formula
  desc "Wrap xray-core VLESS nodes as Surge External Proxy programs"
  homepage "https://github.com/backrunner/xcore-bridge"

  if OS.mac? && (Hardware::CPU.arm? || Hardware::CPU.in_rosetta2?)
    url "https://github.com/backrunner/xcore-bridge/releases/download/v0.1.0-beta.14/xcore-bridge_v0.1.0-beta.14_darwin_arm64.tar.gz"
    sha256 "ce3f3198af435c2bf460a1097154cd41c610e5a408dff97bedc50e3fa826b1d9"
  else
    url "https://github.com/backrunner/xcore-bridge/releases/download/v0.1.0-beta.14/xcore-bridge_v0.1.0-beta.14_darwin_amd64.tar.gz"
    sha256 "e79e5ad02de07ae2d69955b0b771217dbf66d1b6a39126e53f4adcb8ea1a6e55"
  end

  license "MIT"

  depends_on :macos

  def install
    binary = Dir["xcore-bridge_*/xcore-bridge"].first
    bin.install binary => "xcore-bridge"
  end

  def caveats
    <<~EOS
      Homebrew manages upgrades for this installation:
        brew upgrade backrunner/xcore-bridge/xcore-bridge-beta
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/xcore-bridge version")
  end
end
