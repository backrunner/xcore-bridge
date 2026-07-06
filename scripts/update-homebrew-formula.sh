#!/usr/bin/env sh
set -eu

repo="${XCORE_BRIDGE_REPO:-backrunner/xcore-bridge}"
download_base="${GITHUB_DOWNLOAD_URL:-https://github.com}"
tag="${GITHUB_REF_NAME:-}"
checksums_file=""
formula=""
output=""

usage() {
  cat <<'EOF'
Usage: update-homebrew-formula.sh --tag vX.Y.Z [--checksums FILE] [--formula NAME] [--output FILE]

Generate a Homebrew formula in this repository's tap layout.
Stable tags generate Formula/xcore-bridge.rb by default.
Prerelease tags generate Formula/xcore-bridge-beta.rb by default.

Environment:
  XCORE_BRIDGE_REPO   owner/repo, default backrunner/xcore-bridge
  GITHUB_DOWNLOAD_URL download base, default https://github.com
EOF
}

fail() {
  printf 'update-homebrew-formula.sh: %s\n' "$1" >&2
  exit 1
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tag)
      shift
      [ "$#" -gt 0 ] || fail "--tag requires a value"
      tag="$1"
      ;;
    --tag=*)
      tag="${1#--tag=}"
      ;;
    --checksums)
      shift
      [ "$#" -gt 0 ] || fail "--checksums requires a file"
      checksums_file="$1"
      ;;
    --checksums=*)
      checksums_file="${1#--checksums=}"
      ;;
    --formula)
      shift
      [ "$#" -gt 0 ] || fail "--formula requires a name"
      formula="$1"
      ;;
    --formula=*)
      formula="${1#--formula=}"
      ;;
    --output)
      shift
      [ "$#" -gt 0 ] || fail "--output requires a file"
      output="$1"
      ;;
    --output=*)
      output="${1#--output=}"
      ;;
    --repo)
      shift
      [ "$#" -gt 0 ] || fail "--repo requires owner/repo"
      repo="$1"
      ;;
    --repo=*)
      repo="${1#--repo=}"
      ;;
    --download-url)
      shift
      [ "$#" -gt 0 ] || fail "--download-url requires a URL"
      download_base="$1"
      ;;
    --download-url=*)
      download_base="${1#--download-url=}"
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument $1"
      ;;
  esac
  shift
done

[ -n "$tag" ] || fail "release tag is required"

case "$tag" in
  *-*) default_formula="xcore-bridge-beta" ;;
  *) default_formula="xcore-bridge" ;;
esac

if [ -z "$formula" ]; then
  formula="$default_formula"
fi

case "$formula" in
  xcore-bridge|xcore-bridge-beta) ;;
  *) fail "formula must be xcore-bridge or xcore-bridge-beta" ;;
esac

if [ -z "$output" ]; then
  output="Formula/$formula.rb"
fi

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "required command not found: $1"
  fi
}

need awk
need curl
need grep
need mkdir
need sed

class_name() {
  printf '%s\n' "$1" | awk -F- '{
    for (i = 1; i <= NF; i++) {
      printf "%s%s", toupper(substr($i, 1, 1)), substr($i, 2)
    }
    printf "\n"
  }'
}

checksum_for() {
  asset="$1"
  printf '%s\n' "$checksums" | awk -v asset="$asset" '
    {
      name = $NF
      sub(/^\*/, "", name)
      if (name == asset) {
        print $1
        found = 1
        exit
      }
    }
    END {
      if (!found) {
        exit 1
      }
    }
  '
}

validate_checksum() {
  asset="$1"
  value="$2"
  printf '%s\n' "$value" | grep -Eq '^[0-9a-fA-F]{64}$' ||
    fail "invalid checksum for $asset"
}

base_url="${download_base%/}/$repo/releases/download/$tag"

if [ -n "$checksums_file" ]; then
  [ -f "$checksums_file" ] || fail "checksums file not found: $checksums_file"
  checksums="$(sed -n '1,$p' "$checksums_file")"
elif [ -f dist/checksums.txt ]; then
  checksums="$(sed -n '1,$p' dist/checksums.txt)"
else
  checksums="$(curl -fsSL "$base_url/checksums.txt")"
fi

asset_amd64="xcore-bridge_${tag}_darwin_amd64.tar.gz"
asset_arm64="xcore-bridge_${tag}_darwin_arm64.tar.gz"
sha_amd64="$(checksum_for "$asset_amd64")" || fail "checksums.txt does not contain $asset_amd64"
sha_arm64="$(checksum_for "$asset_arm64")" || fail "checksums.txt does not contain $asset_arm64"
validate_checksum "$asset_amd64" "$sha_amd64"
validate_checksum "$asset_arm64" "$sha_arm64"

formula_class="$(class_name "$formula")"

mkdir -p "$(dirname "$output")"

cat > "$output" <<EOF
class $formula_class < Formula
  desc "Wrap xray-core VLESS nodes as Surge External Proxy programs"
  homepage "https://github.com/$repo"

  if OS.mac? && (Hardware::CPU.arm? || Hardware::CPU.in_rosetta2?)
    url "$base_url/$asset_arm64"
    sha256 "$sha_arm64"
  else
    url "$base_url/$asset_amd64"
    sha256 "$sha_amd64"
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
        brew upgrade $repo/$formula
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/xcore-bridge version")
  end
end
EOF

printf 'Wrote %s\n' "$output"
