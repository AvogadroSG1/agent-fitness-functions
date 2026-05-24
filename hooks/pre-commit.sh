#!/usr/bin/env bash
# CALM pre-commit hook
set -euo pipefail

repo=$(git rev-parse --show-toplevel)
calm_bridge=${CALM_BRIDGE_BIN:-calm-bridge}
addr=${CALM_BRIDGE_ADDR:-}
blocked=0

bridge_addr_is_loopback() {
  python3 - "$1" <<'PY'
import ipaddress
import sys
from urllib.parse import urlparse

parsed = urlparse(sys.argv[1])
if parsed.scheme not in {"http", "https"} or not parsed.hostname:
    sys.exit(1)
if parsed.hostname == "localhost":
    sys.exit(0)
try:
    sys.exit(0 if ipaddress.ip_address(parsed.hostname).is_loopback else 1)
except ValueError:
    sys.exit(1)
PY
}

if [[ -n "$addr" && "${CALM_ALLOW_REMOTE_BRIDGE:-}" != "1" ]] && ! bridge_addr_is_loopback "$addr"; then
  echo "CALM_BRIDGE_ADDR must be loopback unless CALM_ALLOW_REMOTE_BRIDGE=1 is set" >&2
  exit 1
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
      printf '%s' "$result" | python3 "$(dirname "${BASH_SOURCE[0]}")/format-violations.py" \
        --mode "$status" --file "$file" >&2 || true
      blocked=1
      ;;
    advisory)
      printf '%s' "$result" | python3 "$(dirname "${BASH_SOURCE[0]}")/format-violations.py" \
        --mode "$status" --file "$file" >&2 || true
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
