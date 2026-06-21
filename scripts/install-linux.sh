#!/usr/bin/env sh
set -eu

case "$(uname -s)" in
  Linux) ;;
  *)
    echo "install-linux.sh: this installer is for Linux; use scripts/install-macos.sh on macOS" >&2
    exit 1
    ;;
esac

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
exec "$script_dir/install.sh" "$@"
