#!/usr/bin/env bash
# CALM pre-commit hook
set -euo pipefail

repo=$(git rev-parse --show-toplevel)
calm_bridge=${CALM_BRIDGE_BIN:-calm-bridge}
addr=${CALM_BRIDGE_ADDR:-}
blocked=0

if [[ -n "$addr" && "${CALM_ALLOW_REMOTE_BRIDGE:-}" != "1" ]]; then
  case "$addr" in
    http://127.0.0.1:*|http://localhost:*|http://[::1]:*)
      ;;
    *)
      echo "CALM_BRIDGE_ADDR must be loopback unless CALM_ALLOW_REMOTE_BRIDGE=1 is set" >&2
      exit 1
      ;;
  esac
fi

language_for_file() {
  case "$1" in
    *.go) printf 'go' ;;
    *.py) printf 'python' ;;
    *.cs) printf 'csharp' ;;
    *) return 1 ;;
  esac
}

json_field() {
  python3 -c 'import json,sys; print(json.load(sys.stdin).get(sys.argv[1], ""))' "$1"
}

json_messages() {
  python3 -c 'import json,sys
payload = json.load(sys.stdin)
for violation in payload.get("violations", []):
    message = violation.get("message", "")
    if message:
        print(message)
'
}

while IFS= read -r -d '' file; do
  if ! language=$(language_for_file "$file"); then
    continue
  fi

  args=(check --file "$file" --repo "$repo" --staged --language "$language")
  if [[ -n "$addr" ]]; then
    args+=(--addr "$addr")
  fi

  if ! result=$("$calm_bridge" "${args[@]}"); then
    echo "CALM check failed for $file" >&2
    blocked=1
    continue
  fi

  status=$(printf '%s' "$result" | json_field status)
  case "$status" in
    block)
      echo "CALM violation in $file:" >&2
      printf '%s' "$result" | json_messages >&2
      blocked=1
      ;;
    advisory)
      echo "CALM advisory for $file:" >&2
      printf '%s' "$result" | json_messages >&2
      ;;
    pass)
      ;;
    *)
      echo "CALM check returned unknown status for $file: ${status:-<empty>}" >&2
      blocked=1
      ;;
  esac
done < <(git diff --cached --name-only --diff-filter=ACM -z)

if [[ "$blocked" -ne 0 ]]; then
  exit 1
fi
