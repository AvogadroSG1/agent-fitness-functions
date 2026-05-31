#!/usr/bin/env bash
# CALM pre-commit hook
set -euo pipefail

repo=$(git rev-parse --show-toplevel)
calm_bridge=${CALM_BRIDGE_BIN:-calm-bridge}
addr=${CALM_BRIDGE_ADDR:-}
client_cert=${CALM_CLIENT_CERT:-}
client_key=${CALM_CLIENT_KEY:-}
client_ca=${CALM_CLIENT_CA:-}
repo_name=${CALM_REPO_NAME:-}
remote_mode=0
repo_arg=$repo
blocked=0
tmp_files=()
cleanup() {
  if [[ "${#tmp_files[@]}" -gt 0 ]]; then
    rm -f "${tmp_files[@]}"
  fi
}
trap cleanup EXIT

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

bridge_addr_is_https() {
  python3 - "$1" <<'PYCHECK'
import sys
from urllib.parse import urlparse

sys.exit(0 if urlparse(sys.argv[1]).scheme == "https" else 1)
PYCHECK
}

if [[ -n "$addr" ]] && ! bridge_addr_is_loopback "$addr"; then
  if [[ "${CALM_ALLOW_REMOTE_BRIDGE:-}" != "1" ]]; then
    echo "CALM_BRIDGE_ADDR must be loopback unless CALM_ALLOW_REMOTE_BRIDGE=1 is set" >&2
    exit 1
  fi
  if ! bridge_addr_is_https "$addr"; then
    echo "remote CALM_BRIDGE_ADDR must use https" >&2
    exit 1
  fi
  remote_mode=1
fi

if [[ -n "$repo_name" ]]; then
  repo_arg=$repo_name
elif [[ "$remote_mode" -eq 1 ]]; then
  repo_arg=$(basename "$repo")
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

  args=(check --file "$file" --repo "$repo_arg" --language "$language")
  content_file=""
  if [[ "$remote_mode" -eq 1 ]]; then
    content_file=$(mktemp)
    tmp_files+=("$content_file")
    if ! git -C "$repo" show ":$file" >"$content_file"; then
      echo "CALM check failed for $file" >&2
      blocked=1
      continue
    fi
    args+=(--content-file "$content_file")
  else
    args+=(--staged)
  fi
  if [[ -n "$addr" ]]; then
    args+=(--addr "$addr")
  fi
  if [[ -n "$client_cert" ]]; then
    args+=(--client-cert "$client_cert")
  fi
  if [[ -n "$client_key" ]]; then
    args+=(--client-key "$client_key")
  fi
  if [[ -n "$client_ca" ]]; then
    args+=(--client-ca "$client_ca")
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
