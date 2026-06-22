#!/usr/bin/env sh
set -eu

repo="${XCORE_BRIDGE_REPO:-backrunner/xcore-bridge}"
channel="${XCORE_BRIDGE_CHANNEL:-auto}"
version="${XCORE_BRIDGE_VERSION:-}"
bindir="${XCORE_BRIDGE_INSTALL_DIR:-${PREFIX:-/usr/local}/bin}"
api_base="${GITHUB_API_URL:-https://api.github.com}"
download_base="${GITHUB_DOWNLOAD_URL:-https://github.com}"

usage() {
  cat <<'EOF'
Usage: install.sh [--beta|--stable] [--version vX.Y.Z] [--bindir DIR]

Environment:
  XCORE_BRIDGE_CHANNEL     auto, stable, or beta; default auto
  XCORE_BRIDGE_VERSION     exact GitHub release tag
  XCORE_BRIDGE_INSTALL_DIR install directory, default /usr/local/bin
  XCORE_BRIDGE_REPO        owner/repo, default backrunner/xcore-bridge
EOF
}

ui_header() {
  cat <<EOF

xcore-bridge installer
----------------------
Repo:    $repo
Target:  $bindir/xcore-bridge

EOF
}

ui_step() {
  printf '[..] %s\n' "$1"
}

ui_done() {
  printf '[ok] %s\n' "$1"
}

ui_warn() {
  printf '[!] %s\n' "$1" >&2
}

ui_fail() {
  printf '[x] %s\n' "$1" >&2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --beta|--prerelease)
      channel="beta"
      ;;
    --stable)
      channel="stable"
      ;;
    --version)
      shift
      if [ "$#" -eq 0 ]; then
        echo "install.sh: --version requires a tag" >&2
        exit 2
      fi
      version="$1"
      ;;
    --version=*)
      version="${1#--version=}"
      ;;
    --bindir)
      shift
      if [ "$#" -eq 0 ]; then
        echo "install.sh: --bindir requires a directory" >&2
        exit 2
      fi
      bindir="$1"
      ;;
    --bindir=*)
      bindir="${1#--bindir=}"
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "install.sh: unknown argument $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

case "$channel" in
  auto|stable|beta) ;;
  *)
    echo "install.sh: channel must be auto, stable, or beta" >&2
    exit 2
    ;;
esac

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    ui_fail "required command not found: $1"
    exit 1
  fi
}

ui_header

ui_step "Checking required tools"
need curl
need tar
need uname
need sed
ui_done "Tools found"

ui_step "Checking platform"
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$os" in
  darwin) os="darwin" ;;
  *)
    ui_fail "unsupported OS: $os; xcore-bridge is only distributed for macOS because Surge for Mac is required"
    exit 1
    ;;
esac

case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *)
    ui_fail "unsupported architecture: $arch"
    exit 1
    ;;
esac
ui_done "Platform: $os/$arch"

resolve_stable_release() {
  curl -fsSL "$api_base/repos/$repo/releases/latest" 2>/dev/null |
    sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
    head -n 1
}

resolve_beta_release() {
  curl -fsSL "$api_base/repos/$repo/releases" 2>/dev/null |
    tr -d '\n' |
    sed 's/}[[:space:]]*,[[:space:]]*{/}\
{/g' |
    sed -n '/"prerelease"[[:space:]]*:[[:space:]]*true/ {
      s/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p
      q
    }'
}

resolved_channel="$channel"
ui_step "Resolving release"
if [ -z "$version" ]; then
  case "$channel" in
    auto)
      version="$(resolve_stable_release || true)"
      if [ -n "$version" ]; then
        resolved_channel="stable"
      else
        version="$(resolve_beta_release || true)"
        resolved_channel="beta"
      fi
      ;;
    stable)
      version="$(resolve_stable_release || true)"
      ;;
    beta)
      version="$(resolve_beta_release || true)"
      ;;
  esac
else
  resolved_channel="tag"
fi

if [ -z "$version" ]; then
  ui_fail "could not resolve a release for $repo (channel: $channel)"
  exit 1
fi
ui_done "Release: $version ($resolved_channel)"

asset="xcore-bridge_${version}_${os}_${arch}.tar.gz"
base_url="$download_base/$repo/releases/download/$version"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT INT TERM

ui_step "Downloading release assets"
curl -fsSL "$base_url/$asset" -o "$tmpdir/$asset"
curl -fsSL "$base_url/checksums.txt" -o "$tmpdir/checksums.txt"
ui_done "Downloaded $asset"

ui_step "Verifying checksum"
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$tmpdir" && grep "  $asset\$" checksums.txt | sha256sum -c - >/dev/null)
  ui_done "Checksum verified"
elif command -v shasum >/dev/null 2>&1; then
  (cd "$tmpdir" && grep "  $asset\$" checksums.txt | shasum -a 256 -c - >/dev/null)
  ui_done "Checksum verified"
else
  ui_warn "sha256sum/shasum not found; skipping checksum verification"
fi

ui_step "Unpacking archive"
tar -xzf "$tmpdir/$asset" -C "$tmpdir"

bin_src="$tmpdir/xcore-bridge"
if [ ! -f "$bin_src" ]; then
  for candidate in "$tmpdir"/xcore-bridge_*/xcore-bridge; do
    if [ -f "$candidate" ]; then
      bin_src="$candidate"
      break
    fi
  done
fi

if [ ! -f "$bin_src" ]; then
  ui_fail "release archive does not contain xcore-bridge"
  exit 1
fi
ui_done "Archive unpacked"

ui_step "Preparing install directory"
if [ ! -d "$bindir" ]; then
  mkdir -p "$bindir" 2>/dev/null || {
    if command -v sudo >/dev/null 2>&1; then
      ui_warn "Creating $bindir needs administrator permission because it is a system-level install directory."
      ui_warn "macOS may ask for your password so sudo can create the directory."
      sudo mkdir -p "$bindir"
    else
      ui_fail "cannot create $bindir; retry with --bindir"
      exit 1
    fi
  }
fi
ui_done "Install directory ready"

install_bin() {
  install -m 0755 "$bin_src" "$bindir/xcore-bridge" 2>/dev/null && return 0
  if command -v sudo >/dev/null 2>&1; then
    ui_warn "Installing to $bindir needs administrator permission because your user cannot write there."
    ui_warn "macOS may ask for your password so sudo can copy xcore-bridge into that directory."
    sudo install -m 0755 "$bin_src" "$bindir/xcore-bridge"
    return 0
  fi
  return 1
}

ui_step "Installing binary"
if ! install_bin; then
  ui_fail "cannot write $bindir/xcore-bridge; retry with --bindir"
  exit 1
fi
ui_done "Installed binary"

installed_version="$("$bindir/xcore-bridge" version)"

cat <<EOF

Installed xcore-bridge $installed_version
Path: $bindir/xcore-bridge

Next:
  xcore-bridge add 'vless://...'

EOF
