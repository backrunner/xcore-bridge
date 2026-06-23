#!/usr/bin/env sh
set -eu

version="${1:-latest}"

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ] && [ -z "${XCORE_BRIDGE_NO_COLOR:-}" ]; then
  green="$(printf '\033[32m')"
  cyan="$(printf '\033[36m')"
  reset="$(printf '\033[0m')"
else
  green=""
  cyan=""
  reset=""
fi

printf '%s•%s Updating xray-core to %s\n' "$cyan" "$reset" "$version"
go get "github.com/xtls/xray-core@${version}"
go mod tidy
go test ./...

printf '%s✓%s xray-core version\n' "$green" "$reset"
go list -m github.com/xtls/xray-core
