#!/usr/bin/env bash
# agent-fitness-functions pre-push hook
set -euo pipefail

repo=$(git rev-parse --show-toplevel)
AGENT_FITNESS_FUNCTIONS_bin=${AGENT_FITNESS_FUNCTIONS_BIN:-agent-fitness-functions}
# The container/production server serves HTTPS with mandatory mTLS, so default to
# an https loopback addr and auto-discover dev client credentials in <repo>/certs.
# Explicit AGENT_FITNESS_FUNCTIONS_CLIENT_* env vars win (12-factor precedence).
addr=${AGENT_FITNESS_FUNCTIONS_ADDR:-https://127.0.0.1:7890}
cert_dir=${AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR:-$repo/certs}
client_cert=${AGENT_FITNESS_FUNCTIONS_CLIENT_CERT:-$cert_dir/client.crt}
client_key=${AGENT_FITNESS_FUNCTIONS_CLIENT_KEY:-$cert_dir/client.key}
client_ca=${AGENT_FITNESS_FUNCTIONS_CLIENT_CA:-$cert_dir/ca.crt}
repo_name=${AGENT_FITNESS_FUNCTIONS_REPO_NAME:-}
remote_mode=0
repo_arg=$repo
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
    exit 1
  fi
  if ! bridge_addr_is_https "$addr"; then
    echo "remote AGENT_FITNESS_FUNCTIONS_ADDR must use https" >&2
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

tmpdir=$(mktemp -d)
cleanup() { rm -rf "$tmpdir"; }
trap cleanup EXIT

while read -r _local_ref local_sha _remote_ref remote_sha; do
  # Skip deletions
  null_sha="0000000000000000000000000000000000000000"
  [[ "$local_sha" == "$null_sha" ]] && continue

  if [[ "$remote_sha" == "$null_sha" ]]; then
    base=$(git merge-base "$local_sha" "origin/HEAD" 2>/dev/null \
           || git merge-base "$local_sha" "origin/main" 2>/dev/null \
           || echo "${local_sha}^")
    range="${base}..${local_sha}"
  else
    range="${remote_sha}..${local_sha}"
  fi

  while IFS= read -r file; do
    [[ -z "$file" ]] && continue
    if ! language=$(language_for_file "$file"); then
      continue
    fi

    content_file="$tmpdir/${local_sha:0:8}_$(echo "$file" | tr '/' '_')"
    if ! git show "${local_sha}:${file}" > "$content_file" 2>/dev/null; then
      continue
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

    if result=$("$AGENT_FITNESS_FUNCTIONS_bin" "${args[@]}"); then
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
        echo "agent-fitness-functions check returned unknown status for $file: ${status:-<empty>}" >&2
        blocked=1
        ;;
    esac
  done < <(git diff --name-only --diff-filter=ACM "$range" -- 2>/dev/null || true)
done

if [[ "$blocked" -ne 0 ]]; then
  exit 1
fi
