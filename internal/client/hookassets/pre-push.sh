#!/usr/bin/env bash
# stack-fitness-functions pre-push hook
set -euo pipefail

repo=$(git rev-parse --show-toplevel)
stack_fitness_functions_bin=${STACK_FITNESS_FUNCTIONS_BIN:-stack-fitness-functions}
# The container/production server serves HTTPS with mandatory mTLS, so default to
# an https loopback addr and auto-discover dev client credentials in <repo>/certs.
# Explicit STACK_FITNESS_FUNCTIONS_CLIENT_* env vars win (12-factor precedence).
addr=${STACK_FITNESS_FUNCTIONS_ADDR:-https://127.0.0.1:7890}
cert_dir=${STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR:-$repo/certs}
client_cert=${STACK_FITNESS_FUNCTIONS_CLIENT_CERT:-$cert_dir/client.crt}
client_key=${STACK_FITNESS_FUNCTIONS_CLIENT_KEY:-$cert_dir/client.key}
client_ca=${STACK_FITNESS_FUNCTIONS_CLIENT_CA:-$cert_dir/ca.crt}
repo_name=${STACK_FITNESS_FUNCTIONS_REPO_NAME:-}
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

# Derive the LOGICAL repository name the server governs by, decoupled from the
# local working directory. For a git worktree the working dir basename is the
# worktree name (e.g. feature+relocate-stats), not the repo name, so resolve the
# common git dir's parent (.../relocate/.git -> relocate). Falls back to the
# working dir basename if git cannot answer. Non-standard layouts such as
# --separate-git-dir need STACK_FITNESS_FUNCTIONS_REPO_NAME to override this.
logical_repo_name() {
  local common_dir
  common_dir=$(cd "$repo" && git rev-parse --git-common-dir 2>/dev/null) || { basename "$repo"; return; }
  case "$common_dir" in
    /*) ;;
    *) common_dir=$repo/$common_dir ;;
  esac
  basename "$(dirname "$common_dir")"
}

if [[ -n "$addr" ]] && ! bridge_addr_is_loopback "$addr"; then
  if [[ "${STACK_FITNESS_FUNCTIONS_ALLOW_REMOTE:-}" != "1" ]]; then
    echo "STACK_FITNESS_FUNCTIONS_ADDR must be loopback unless STACK_FITNESS_FUNCTIONS_ALLOW_REMOTE=1 is set" >&2
    exit 1
  fi
  if ! bridge_addr_is_https "$addr"; then
    echo "remote STACK_FITNESS_FUNCTIONS_ADDR must use https" >&2
    exit 1
  fi
  remote_mode=1
fi

# --repo is always the logical name; the working tree is passed separately as
# --git-dir. STACK_FITNESS_FUNCTIONS_REPO_NAME overrides the derived name.
if [[ -n "$repo_name" ]]; then
  repo_arg=$repo_name
else
  repo_arg=$(logical_repo_name)
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
    if ! git -C "$repo" show "${local_sha}:${file}" > "$content_file" 2>/dev/null; then
      continue
    fi

    args=(client validate --file "$file" --repo "$repo_arg" --git-dir "$repo" --content-file "$content_file" --language "$language")
    args+=(--addr "$addr")
    # Pass mTLS client cert+key only as a pair (the client requires both together);
    # omit when the files are absent so a plain-HTTP local server still works.
    if [[ -f "$client_cert" && -f "$client_key" ]]; then
      args+=(--client-cert "$client_cert" --client-key "$client_key")
    fi
    if [[ -f "$client_ca" ]]; then
      args+=(--client-ca "$client_ca")
    fi

    if ! result=$("$stack_fitness_functions_bin" "${args[@]}"); then
      echo "stack-fitness-functions check failed for $file" >&2
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
        echo "stack-fitness-functions check returned unknown status for $file: ${status:-<empty>}" >&2
        blocked=1
        ;;
    esac
  done < <(git diff --name-only --diff-filter=ACM "$range" -- 2>/dev/null || true)
done

if [[ "$blocked" -ne 0 ]]; then
  exit 1
fi
