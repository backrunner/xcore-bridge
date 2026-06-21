#!/usr/bin/env sh
set -eu

case "$(uname -s)" in
  Darwin) ;;
  *)
    echo "install-macos.sh: this installer is for macOS; use scripts/install-linux.sh on Linux" >&2
    exit 1
    ;;
esac

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
exec "$script_dir/install.sh" "$@"
