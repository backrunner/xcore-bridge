#!/usr/bin/env sh
set -eu

repo="${XCORE_BRIDGE_REPO:-github.com/orchiliao/xcore-bridge}"
prefix="${PREFIX:-/usr/local}"

if command -v brew >/dev/null 2>&1; then
  prefix="$(brew --prefix)"
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

GOBIN="$tmpdir/bin" go install "$repo/cmd/xcore-bridge@latest"
install -d "$prefix/bin"
install "$tmpdir/bin/xcore-bridge" "$prefix/bin/xcore-bridge"

echo "installed $prefix/bin/xcore-bridge"
