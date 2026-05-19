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
target_dir="$repo_root/.git/hooks"
target_hook="$target_dir/pre-commit"

mkdir -p "$target_dir"
cp -f "$source_hook" "$target_hook"
chmod +x "$target_hook"

echo "installed $target_hook"
