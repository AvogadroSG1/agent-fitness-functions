#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: install-hooks.sh <repo>" >&2
  exit 2
fi

repo=$1
repo_root=$(git -C "$repo" rev-parse --show-toplevel)
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
source_hook=$(cd -- "$script_dir/.." && pwd)/hooks/pre-commit.sh
target_hook=$(git -C "$repo_root" rev-parse --git-path hooks/pre-commit)
case "$target_hook" in
  /*) ;;
  *) target_hook="$repo_root/$target_hook" ;;
esac
target_dir=$(dirname -- "$target_hook")

mkdir -p "$target_dir"
if [[ -e "$target_hook" ]] && ! grep -q "CALM pre-commit hook" "$target_hook"; then
  if [[ "${CALM_HOOK_OVERWRITE:-}" != "1" ]]; then
    echo "refusing to overwrite existing non-CALM pre-commit hook: $target_hook" >&2
    echo "set CALM_HOOK_OVERWRITE=1 to replace it explicitly" >&2
    exit 1
  fi
fi
cp -f "$source_hook" "$target_hook"
chmod +x "$target_hook"

echo "installed $target_hook"
