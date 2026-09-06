#!/usr/bin/env bash
# agent-fitness-functions pre-commit hook
set -euo pipefail

repo=$(git rev-parse --show-toplevel)
# Environment metadata is ignored safely by binaries that predate history.
export AGENT_FITNESS_FUNCTIONS_HISTORY_WORKTREE="$repo"
export AGENT_FITNESS_FUNCTIONS_HISTORY_SOURCE=git
export AGENT_FITNESS_FUNCTIONS_HISTORY_TOOL=git
export AGENT_FITNESS_FUNCTIONS_HISTORY_ACTION=pre-commit
export AGENT_FITNESS_FUNCTIONS_HISTORY_SESSION_ID="${AGENT_FITNESS_FUNCTIONS_HISTORY_SESSION_ID:-}"
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
  if [[ "${AGENT_FITNESS_FUNCTIONS_ALLOW_REMOTE:-}" != "1" ]]; then
    echo "AGENT_FITNESS_FUNCTIONS_ADDR must be loopback unless AGENT_FITNESS_FUNCTIONS_ALLOW_REMOTE=1 is set" >&2
    exit 2
  fi
  if ! bridge_addr_is_https "$addr"; then
    echo "remote AGENT_FITNESS_FUNCTIONS_ADDR must use https" >&2
    exit 2
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

# report_infra_error prints the client's machine-readable setup-failure object as a
# clearly labeled SETUP problem with its remediation, so a developer sees "fix your
# setup", never a spurious "fix your architecture".
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

# handle_infra_error reports the setup failure and applies the on-error policy.
handle_infra_error() {
  local file=$1 payload=$2
  report_infra_error "$file" "$payload"
  if [[ "$on_error_mode" == "advisory" ]]; then
    echo "  AGENT_FITNESS_FUNCTIONS_ON_ERROR=advisory: not blocking this setup failure" >&2
  else
    blocked=1
  fi
}

while IFS= read -r -d '' file; do
  if ! language=$(language_for_file "$file"); then
    continue
  fi

  args=(client validate --file "$file" --repo "$repo_arg" --language "$language")
  content_file=""
  if [[ "$remote_mode" -eq 1 ]]; then
    content_file=$(mktemp)
    tmp_files+=("$content_file")
    if ! git -C "$repo" show ":$file" >"$content_file"; then
      echo "agent-fitness-functions check failed for $file" >&2
      blocked=1
      continue
    fi
    args+=(--content-file "$content_file")
  else
    args+=(--staged)
  fi
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
      handle_infra_error "$file" "$result"
      continue
    fi
    echo "agent-fitness-functions check failed for $file" >&2
    blocked=1
    continue
  fi

  if ! status=$(printf '%s' "$result" | json_field status 2>/dev/null); then
    echo "agent-fitness-functions check returned invalid JSON for $file" >&2
    blocked=1
    continue
  fi
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
      echo "agent-fitness-functions check returned unknown status for $file: ${status:-<empty>}" >&2
      blocked=1
      ;;
  esac
done < <(git diff --cached --name-only --diff-filter=ACM -z)

if [[ "$blocked" -ne 0 ]]; then
  exit 1
fi
