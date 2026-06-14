#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: install-hooks.sh <repo>" >&2
  exit 2
fi

repo=$1
repo_root=$(git -C "$repo" rev-parse --show-toplevel)
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
calm_root=$(cd -- "$script_dir/.." && pwd)

source_pre_commit="$calm_root/hooks/pre-commit.sh"
source_pre_push="$calm_root/hooks/pre-push.sh"
source_git_guard="$calm_root/hooks/git-guard.sh"
source_formatter="$calm_root/hooks/format-violations.py"

[[ -f "$source_formatter" ]] || {
  echo "error: format-violations.py not found at $source_formatter" >&2
  echo "  ensure the calm-poc repo is complete (STACK_FITNESS_FUNCTIONS_SRC=$calm_root)" >&2
  exit 2
}

# ---------------------------------------------------------------------------
# Helper: install a single hook (pre-commit or pre-push)
# ---------------------------------------------------------------------------
install_git_hook() {
  local hook_name=$1          # e.g. pre-commit
  local source_hook=$2        # e.g. /path/to/pre-commit.sh
  local sidecar_marker=$3     # text to grep for in existing hook
  local hooks_dir

  local target_hook
  target_hook=$(git -C "$repo_root" rev-parse --git-path "hooks/$hook_name")
  case "$target_hook" in
    /*) ;;
    *) target_hook="$repo_root/$target_hook" ;;
  esac
  hooks_dir=$(dirname -- "$target_hook")
  mkdir -p "$hooks_dir"

  if [[ -e "$target_hook" ]]; then
    if grep -q "$sidecar_marker" "$target_hook"; then
      # Already using sidecar pattern — just update the sidecar file
      local calm_sidecar="$hooks_dir/calm-${hook_name}"
      cp -f "$source_hook" "$calm_sidecar"
      chmod +x "$calm_sidecar"
      cp -f "$source_formatter" "$hooks_dir/format-violations.py"
      echo "updated $calm_sidecar"
      return 0
    elif ! grep -q "CALM ${hook_name} hook" "$target_hook"; then
      if [[ "${STACK_FITNESS_FUNCTIONS_HOOK_APPEND:-}" == "1" ]]; then
        local calm_sidecar="$hooks_dir/calm-${hook_name}"
        cp -f "$source_hook" "$calm_sidecar"
        chmod +x "$calm_sidecar"
        cp -f "$source_formatter" "$hooks_dir/format-violations.py"
        printf '\n# CALM %s hook (sidecar)\n"%s"\n' "$hook_name" "$calm_sidecar" >> "$target_hook"
        echo "appended CALM call to $target_hook (sidecar: $calm_sidecar)"
        return 0
      elif [[ "${STACK_FITNESS_FUNCTIONS_HOOK_OVERWRITE:-}" != "1" ]]; then
        echo "refusing to overwrite existing non-CALM $hook_name hook: $target_hook" >&2
        echo "set STACK_FITNESS_FUNCTIONS_HOOK_OVERWRITE=1 to replace it, or STACK_FITNESS_FUNCTIONS_HOOK_APPEND=1 to append" >&2
        return 1
      fi
    fi
  fi

  cp -f "$source_hook" "$target_hook"
  chmod +x "$target_hook"
  cp -f "$source_formatter" "$hooks_dir/format-violations.py"
  echo "installed $target_hook"
}

# ---------------------------------------------------------------------------
# Install pre-commit
# ---------------------------------------------------------------------------
install_git_hook "pre-commit" "$source_pre_commit" "# CALM pre-commit hook (sidecar)"

# ---------------------------------------------------------------------------
# Install pre-push
# ---------------------------------------------------------------------------
install_git_hook "pre-push" "$source_pre_push" "# CALM pre-push hook (sidecar)"

# ---------------------------------------------------------------------------
# Install git-guard into .claude/settings.json (Claude Code PreToolUse hook)
# ---------------------------------------------------------------------------
settings_json="$repo_root/.claude/settings.json"
if [[ ! -f "$settings_json" ]]; then
  echo "warning: $settings_json not found — skipping Claude Code git-guard installation" >&2
  echo "  Run 'claude' once in the repo to create settings.json, then re-run calm-install-hooks." >&2
  exit 0
fi

hooks_dir_for_guard=$(git -C "$repo_root" rev-parse --git-path hooks/pre-commit)
case "$hooks_dir_for_guard" in
  /*) hooks_dir_for_guard=$(dirname "$hooks_dir_for_guard") ;;
  *) hooks_dir_for_guard="$repo_root/$(dirname "$hooks_dir_for_guard")" ;;
esac

git_guard_target="$hooks_dir_for_guard/calm-git-guard"
cp -f "$source_git_guard" "$git_guard_target"
chmod +x "$git_guard_target"
echo "installed $git_guard_target"

python3 - "$settings_json" "$git_guard_target" <<'PY'
import json
import sys

settings_path, guard_path = sys.argv[1], sys.argv[2]

with open(settings_path, encoding="utf-8") as fh:
    settings = json.load(fh)

hooks = settings.setdefault("hooks", {})
pre_tool_use = hooks.setdefault("PreToolUse", [])

# Check if a git-guard entry already exists (by command path or presence of calm-git-guard)
existing = next(
    (i for i, entry in enumerate(pre_tool_use)
     if isinstance(entry, dict)
     and any("calm-git-guard" in h.get("command", "")
             for h in entry.get("hooks", []))),
    None
)

new_entry = {
    "hooks": [{"command": guard_path, "type": "command"}],
    "matcher": "Bash"
}

if existing is not None:
    old_cmd = pre_tool_use[existing]["hooks"][0]["command"]
    pre_tool_use[existing] = new_entry
    if old_cmd != guard_path:
        print(f"updated calm-git-guard path in {settings_path}")
    else:
        print(f"calm-git-guard already configured in {settings_path} (no change)")
else:
    pre_tool_use.append(new_entry)
    print(f"added calm-git-guard to PreToolUse hooks in {settings_path}")

with open(settings_path, "w", encoding="utf-8") as fh:
    json.dump(settings, fh, indent=2)
    fh.write("\n")
PY
