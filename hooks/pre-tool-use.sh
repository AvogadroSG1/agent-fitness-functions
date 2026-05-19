#!/usr/bin/env bash
set -euo pipefail

repo=$(git rev-parse --show-toplevel)
calm_bridge=${CALM_BRIDGE_BIN:-calm-bridge}
addr=${CALM_BRIDGE_ADDR:-}

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

json_messages() {
  python3 -c 'import json,sys
payload = json.load(sys.stdin)
for violation in payload.get("violations", []):
    message = violation.get("message", "")
    if message:
        print(message)
'
}

payload=$(cat)
parsed=$(CALM_HOOK_PAYLOAD="$payload" python3 - <<'PY'
import json
import os
import sys

payload = json.loads(os.environ["CALM_HOOK_PAYLOAD"])
tool_input = payload.get("tool_input", payload)
file_path = tool_input.get("file_path", "")
content = tool_input.get("new_string", tool_input.get("content", None))
if not file_path:
    print(json.dumps({"error": "missing file_path"}))
elif content is None:
    print(json.dumps({"error": "missing proposed content"}))
elif "\x00" in content:
    print(json.dumps({"skip": "binary", "file_path": file_path}))
else:
    print(json.dumps({"file_path": file_path, "content": content}))
PY
)

parse_error=$(printf '%s' "$parsed" | json_field error)
if [[ -n "$parse_error" ]]; then
  echo "Invalid PreToolUse payload: $parse_error" >&2
  exit 1
fi

skip=$(printf '%s' "$parsed" | json_field skip)
file_path=$(printf '%s' "$parsed" | json_field file_path)
if [[ "$skip" == "binary" ]]; then
  echo "Skipping CALM check for binary content in $file_path" >&2
  exit 0
fi

repo_prefix=$(cd "$repo" && pwd -P)
absolute_file=$(python3 - "$repo_prefix" "$file_path" <<'PY'
import os
import sys

repo, file_path = sys.argv[1], sys.argv[2]
if not os.path.isabs(file_path):
    file_path = os.path.join(repo, file_path)
print(os.path.realpath(file_path))
PY
)
case "$absolute_file" in
  "$repo_prefix"/*)
    file=$(python3 - "$repo_prefix" "$absolute_file" <<'PY'
import os
import sys

print(os.path.relpath(sys.argv[2], sys.argv[1]))
PY
)
    ;;
  *)
    echo "Skipping CALM check for file outside repository: $file_path" >&2
    exit 0
    ;;
esac

if ! language=$(language_for_file "$file"); then
  echo "Skipping CALM check for unsupported file type: $file" >&2
  exit 0
fi

content=$(printf '%s' "$parsed" | json_field content)
args=(check --file "$file" --repo "$repo" --content "$content" --language "$language")
if [[ -n "$addr" ]]; then
  args+=(--addr "$addr")
fi

if ! result=$("$calm_bridge" "${args[@]}"); then
  echo "CALM check failed for $file" >&2
  printf '%s\n' "$result" >&2
  exit 1
fi

status=$(printf '%s' "$result" | json_field status)
case "$status" in
  block)
    echo "CALM violation in $file:" >&2
    printf '%s' "$result" | json_messages >&2
    exit 1
    ;;
  advisory)
    echo "CALM advisory for $file:" >&2
    printf '%s' "$result" | json_messages >&2
    ;;
  pass)
    ;;
  *)
    echo "CALM check returned unknown status for $file: ${status:-<empty>}" >&2
    exit 1
    ;;
esac
