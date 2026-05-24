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
source_formatter=$(cd -- "$script_dir/.." && pwd)/hooks/format-violations.py
target_hook=$(git -C "$repo_root" rev-parse --git-path hooks/pre-commit)
case "$target_hook" in
  /*) ;;
  *) target_hook="$repo_root/$target_hook" ;;
esac
target_dir=$(dirname -- "$target_hook")

mkdir -p "$target_dir"
if [[ -e "$target_hook" ]]; then
  if grep -q "# CALM pre-commit hook (sidecar)" "$target_hook"; then
    calm_sidecar="$(dirname "$target_hook")/calm-pre-commit"
    cp -f "$source_hook" "$calm_sidecar"
    chmod +x "$calm_sidecar"
    echo "updated $calm_sidecar"
    exit 0
  elif ! grep -q "CALM pre-commit hook" "$target_hook"; then
    if [[ "${CALM_HOOK_APPEND:-}" == "1" ]]; then
      calm_sidecar="$(dirname "$target_hook")/calm-pre-commit"
      cp -f "$source_hook" "$calm_sidecar"
      chmod +x "$calm_sidecar"
      printf '\n# CALM pre-commit hook (sidecar)\n"%s"\n' "$calm_sidecar" >> "$target_hook"
      echo "appended CALM call to $target_hook (sidecar: $calm_sidecar)"
      exit 0
    elif [[ "${CALM_HOOK_OVERWRITE:-}" != "1" ]]; then
      echo "refusing to overwrite existing non-CALM pre-commit hook: $target_hook" >&2
      echo "set CALM_HOOK_OVERWRITE=1 to replace it, or CALM_HOOK_APPEND=1 to append" >&2
      exit 1
    fi
  fi
fi
cp -f "$source_hook" "$target_hook"
chmod +x "$target_hook"

echo "installed $target_hook"
