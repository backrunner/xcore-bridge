class XcoreBridgeBeta < Formula
  desc "Wrap xray-core VLESS nodes as Surge External Proxy programs"
  homepage "https://github.com/backrunner/xcore-bridge"

  if OS.mac? && (Hardware::CPU.arm? || Hardware::CPU.in_rosetta2?)
    url "https://github.com/backrunner/xcore-bridge/releases/download/v0.1.0-beta.15/xcore-bridge_v0.1.0-beta.15_darwin_arm64.tar.gz"
    sha256 "bd51a38bb708f1e61c42bdcd7535fa4366ad5e05de38ba96cde4ff1efb58254c"
  else
    url "https://github.com/backrunner/xcore-bridge/releases/download/v0.1.0-beta.15/xcore-bridge_v0.1.0-beta.15_darwin_amd64.tar.gz"
    sha256 "8bce2828ce7600eab03f43cdefb6384b3900a2a5a554a1b75b6a4cb820b2d99e"
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
