#!/usr/bin/env bash
set -euo pipefail

repo=$(git rev-parse --show-toplevel)
agent_fitness_functions_bin=${AGENT_FITNESS_FUNCTIONS_BIN:-agent-fitness-functions}
# The container/production server serves HTTPS with mandatory mTLS, so default to
# an https loopback addr. In managed mode (no explicit client TLS material), the
# AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR selector — if set — passes through untouched
# to `client validate`, which resolves the machine governance root itself (ADR-0007:
# hooks never resolve, pin, or pass client TLS material in managed mode).
# Explicit AGENT_FITNESS_FUNCTIONS_CLIENT_* env vars win (12-factor precedence).
addr=${AGENT_FITNESS_FUNCTIONS_ADDR:-https://127.0.0.1:7890}
managed_selector=${AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR:-}
explicit_client_tls=0
[[ -n "${AGENT_FITNESS_FUNCTIONS_CLIENT_CERT:-}${AGENT_FITNESS_FUNCTIONS_CLIENT_KEY:-}${AGENT_FITNESS_FUNCTIONS_CLIENT_CA:-}" ]] && explicit_client_tls=1
if [[ -n "$managed_selector" && "$explicit_client_tls" -eq 1 ]]; then
  echo "AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR cannot be combined with explicit client TLS inputs" >&2
  exit 2
fi
if [[ "$explicit_client_tls" -eq 1 ]]; then
  client_cert=${AGENT_FITNESS_FUNCTIONS_CLIENT_CERT:-}
  client_key=${AGENT_FITNESS_FUNCTIONS_CLIENT_KEY:-}
  client_ca=${AGENT_FITNESS_FUNCTIONS_CLIENT_CA:-}
else
  client_cert=""
  client_key=""
  client_ca=""
fi
repo_name=${AGENT_FITNESS_FUNCTIONS_REPO_NAME:-}
# Default the governance repo name to the working-tree basename (consistent with
# pre-commit.sh); the absolute worktree path is not a valid ^[a-z][a-z0-9_-]{0,63}$
# repo name, so a freshly onboarded repo would otherwise send an invalid --repo.
if [[ -n "$repo_name" ]]; then
  repo_arg=$repo_name
else
  repo_arg=$(basename "$repo")
fi

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
  if [[ "${AGENT_FITNESS_FUNCTIONS_ALLOW_REMOTE:-}" != "1" ]]; then
    echo "AGENT_FITNESS_FUNCTIONS_ADDR must be loopback unless AGENT_FITNESS_FUNCTIONS_ALLOW_REMOTE=1 is set" >&2
    exit 2
  fi
  if ! bridge_addr_is_https "$addr"; then
    echo "remote AGENT_FITNESS_FUNCTIONS_ADDR must use https" >&2
    exit 2
  fi
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

# On-error policy for infrastructure/setup failures (server down, cert/TLS problem,
# repo not configured, auth rejected): fail-closed (block) by default, or non-blocking
# when AGENT_FITNESS_FUNCTIONS_ON_ERROR=advisory — mirroring the server-side
# enforcement-on-error setting. Real architecture violations are unaffected.
on_error_mode=${AGENT_FITNESS_FUNCTIONS_ON_ERROR:-block}

# is_infra_error reports whether a client failure is an infrastructure/setup problem
# (client exit code 3, or a {"status":"error"} object) rather than a real violation.
is_infra_error() {
  local rc=$1 payload=$2
  [[ "$rc" -eq 3 ]] && return 0
  printf '%s' "$payload" | grep -q '"status":"error"'
}

# report_infra_error prints the client's machine-readable setup-failure object to
# stderr as a clearly labeled SETUP problem with its remediation, so the coding agent
# is told exactly what to run next instead of mistaking it for an architecture block.
report_infra_error() {
  local file=$1 payload=$2
  local kind message remediation
  kind=$(printf '%s' "$payload" | json_field error_kind 2>/dev/null || true)
  message=$(printf '%s' "$payload" | json_field message 2>/dev/null || true)
  remediation=$(printf '%s' "$payload" | json_field remediation 2>/dev/null || true)
  {
    echo "agent-fitness-functions SETUP problem for $file (infrastructure/configuration, NOT an architecture violation)"
    if [[ -n "$kind" ]]; then echo "  kind: $kind"; fi
    if [[ -n "$message" ]]; then echo "  detail: $message"; fi
    if [[ -n "$remediation" ]]; then echo "  fix: $remediation"; fi
  } >&2
}


payload=$(cat)
payload_file=$(mktemp)
content_file=$(mktemp)
cleanup() {
  rm -f "$payload_file" "$content_file"
}
trap cleanup EXIT
printf '%s' "$payload" > "$payload_file"

parsed=$(python3 - "$payload_file" <<'PY'
import json
import sys

try:
    with open(sys.argv[1], encoding="utf-8") as handle:
        payload = json.load(handle)
    if not isinstance(payload, dict):
        tool_input = {}
    else:
        tool_input = payload.get("tool_input") or payload.get("args") or payload
        if not isinstance(tool_input, dict):
            tool_input = {}
    file_path = tool_input.get("file_path") or tool_input.get("filePath") or tool_input.get("path", "")
    if not file_path:
        print(json.dumps({"error": "missing file_path"}))
    else:
        print(json.dumps({"file_path": file_path}))
except Exception as exc:
    print(json.dumps({"error": f"invalid JSON payload: {exc}"}))
PY
)

parse_error=$(printf '%s' "$parsed" | json_field error)
if [[ -n "$parse_error" ]]; then
  echo "Invalid PreToolUse payload: $parse_error" >&2
  exit 2
fi

file_path=$(printf '%s' "$parsed" | json_field file_path)

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
    echo "Skipping agent-fitness-functions check for file outside repository: $file_path" >&2
    exit 0
    ;;
esac

if ! language=$(language_for_file "$file"); then
  echo "Skipping agent-fitness-functions check for unsupported file type: $file" >&2
  exit 0
fi

content_result=$(python3 - "$payload_file" "$content_file" "$repo_prefix" "$file" <<'PY'
import json
import os
import sys

try:
    with open(sys.argv[1], encoding="utf-8") as handle:
        payload = json.load(handle)
    if not isinstance(payload, dict):
        tool_input = {}
    else:
        tool_input = payload.get("tool_input") or payload.get("args") or payload
        if not isinstance(tool_input, dict):
            tool_input = {}
    if "content" in tool_input:
        content = tool_input["content"]
    elif "new_string" in tool_input or "newString" in tool_input:
        old_string = tool_input.get("old_string") if "old_string" in tool_input else tool_input.get("oldString")
        new_string = tool_input.get("new_string") if "new_string" in tool_input else tool_input.get("newString")
        if old_string is None:
            print(json.dumps({"error": "missing old_string for Edit"}))
            sys.exit(0)
        absolute_file = os.path.join(sys.argv[3], sys.argv[4])
        with open(absolute_file, encoding="utf-8", newline="") as source_file:
            current_content = source_file.read()
        occurrences = current_content.count(old_string)
        replace_all = bool(tool_input.get("replace_all", tool_input.get("replaceAll", False)))
        if occurrences == 0:
            print(json.dumps({"error": "old_string not found in current file"}))
            sys.exit(0)
        if not replace_all and occurrences > 1:
            print(json.dumps({"error": "old_string matched multiple locations"}))
            sys.exit(0)
        count = -1 if replace_all else 1
        content = current_content.replace(old_string, new_string, count)
    else:
        print(json.dumps({"error": "missing proposed content"}))
        sys.exit(0)
    binary = "\x00" in content
    if not binary:
        with open(sys.argv[2], "w", encoding="utf-8", newline="") as content_file:
            content_file.write(content)
    print(json.dumps({"binary": binary}))
except Exception as exc:
    print(json.dumps({"error": f"invalid proposed content: {exc}"}))
PY
)

content_error=$(printf '%s' "$content_result" | json_field error)
if [[ -n "$content_error" ]]; then
  echo "Invalid PreToolUse payload: $content_error" >&2
  exit 2
fi
binary=$(printf '%s' "$content_result" | json_field binary)

if [[ "$binary" == "True" || "$binary" == "true" ]]; then
  echo "agent-fitness-functions check blocked binary content for supported source file: $file" >&2
  exit 2
fi

args=(client validate --file "$file" --repo "$repo_arg" --content-file "$content_file" --language "$language")
args+=(--addr "$addr")
# Pass mTLS client cert+key only as a pair (the client requires both together);
# omit when the files are absent so a plain-HTTP local server still works.
if [[ -f "$client_cert" && -f "$client_key" ]]; then
  args+=(--client-cert "$client_cert" --client-key "$client_key")
fi
if [[ -f "$client_ca" ]]; then
  args+=(--client-ca "$client_ca")
fi

if result=$("$agent_fitness_functions_bin" "${args[@]}"); then
  rc=0
else
  rc=$?
fi
if [[ "$rc" -ne 0 ]]; then
  if is_infra_error "$rc" "$result"; then
    report_infra_error "$file" "$result"
    if [[ "$on_error_mode" == "advisory" ]]; then
      echo "  AGENT_FITNESS_FUNCTIONS_ON_ERROR=advisory: allowing this edit despite the setup failure" >&2
      exit 0
    fi
    exit 2
  fi
  echo "agent-fitness-functions check failed for $file" >&2
  printf '%s\n' "$result" >&2
  exit 2
fi

if ! status=$(printf '%s' "$result" | json_field status 2>/dev/null); then
  echo "agent-fitness-functions check returned invalid JSON for $file" >&2
  exit 2
fi
case "$status" in
  block)
    printf '%s' "$result" | python3 "$(dirname "${BASH_SOURCE[0]}")/format-violations.py" \
      --mode "$status" --file "$file" >&2 || true
    exit 2
    ;;
  advisory)
    printf '%s' "$result" | python3 "$(dirname "${BASH_SOURCE[0]}")/format-violations.py" \
      --mode "$status" --file "$file" >&2 || true
    ;;
  pass)
    ;;
  *)
    echo "agent-fitness-functions check returned unknown status for $file: ${status:-<empty>}" >&2
    exit 2
    ;;
esac
