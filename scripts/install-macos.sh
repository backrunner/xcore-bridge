#!/usr/bin/env sh
set -eu

case "$(uname -s)" in
  Darwin) ;;
  *)
    if [ -t 2 ] && [ -z "${NO_COLOR:-}" ] && [ -z "${XCORE_BRIDGE_NO_COLOR:-}" ]; then
      red="$(printf '\033[31m')"
      reset="$(printf '\033[0m')"
    else
      red=""
      reset=""
    fi
    printf '%sx%s install-macos.sh: this installer is for macOS; xcore-bridge requires Surge for Mac\n' "$red" "$reset" >&2
    exit 1
    ;;
esac

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
exec "$script_dir/install.sh" "$@"
