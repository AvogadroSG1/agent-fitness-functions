#!/usr/bin/env bash
# stack-fitness-functions git-guard PreToolUse hook
# Blocks git commands that bypass fitness-function enforcement before they execute.
set -euo pipefail

payload=$(cat)

command=$(python3 - "$payload" <<'PY'
import json, sys
try:
    d = json.loads(sys.argv[1])
    tool_input = d.get("tool_input", d)
    print(tool_input.get("command", ""))
except Exception:
    print("")
PY
)

[[ -z "$command" ]] && exit 0

deny() {
  echo "stack-fitness-functions git-guard: $1" >&2
  echo "  Fix fitness-function violations in the code rather than bypassing enforcement." >&2
  exit 2
}

# Block --no-verify (short: -n) on git commit
if echo "$command" | grep -qE 'git\s+commit\s+.*--no-verify'; then
  deny "'git commit --no-verify' is blocked. stack-fitness-functions hooks must run."
fi
short_n_blocked=$(python3 - "$command" <<'PY'
import re, sys
cmd = sys.argv[1]
# Match git commit with -n flag (standalone or combined, e.g. -amn)
if re.search(r'git\s+commit\b', cmd) and re.search(r'(^|\s)-[a-zA-Z]*n[a-zA-Z]*(\s|$)', cmd):
    print("1")
else:
    print("0")
PY
)
if [[ "$short_n_blocked" == "1" ]]; then
  deny "'git commit -n' (--no-verify shorthand) is blocked. stack-fitness-functions hooks must run."
fi

# Block --no-gpg-sign
if echo "$command" | grep -qE 'git\s+commit\s+.*--no-gpg-sign'; then
  deny "'git commit --no-gpg-sign' is blocked."
fi

# Block --ff-only merges (the specific bypass tactic: fast-forward to a pre-existing commit)
if echo "$command" | grep -qE 'git\s+(merge|pull)\s+.*--ff-only'; then
  deny "'git merge/pull --ff-only' is blocked when stack-fitness-functions enforcement is active. Use a regular merge or rebase so the stack-fitness-functions pre-commit hook fires."
fi

# Block force push (not --force-with-lease, which is safe)
# Use python3 for the negative lookahead since BSD grep doesn't support -P
force_push_blocked=$(python3 - "$command" <<'PY'
import re, sys
cmd = sys.argv[1]
# Match git push ... --force but not --force-with-lease
if re.search(r'git\s+push\b', cmd) and re.search(r'--force\b', cmd) and not re.search(r'--force-with-lease\b', cmd):
    print("1")
    sys.exit(0)
# Match git push ... -f (short form)
if re.search(r'git\s+push\b', cmd) and re.search(r'\s-[a-zA-Z]*f[a-zA-Z]*(\s|$)', cmd):
    print("1")
    sys.exit(0)
print("0")
PY
)
if [[ "$force_push_blocked" == "1" ]]; then
  deny "'git push --force' / 'git push -f' is blocked. Use --force-with-lease if you must force push."
fi

exit 0
