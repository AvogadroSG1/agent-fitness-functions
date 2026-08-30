#!/usr/bin/env bash
#
# demo-agent-governed-edit.sh — the "fitness functions next to the coding agent" story,
# end to end, in one runnable script. Suitable for a live stakeholder demo AND doubles
# as an end-to-end smoke test (every act asserts its expected exit code and fails loudly
# on mismatch).
#
# The story, in three acts, all driven through the SAME hooks a real Claude Code agent
# would trigger:
#   Act 1  A coding agent tries to Write a Go function with cyclomatic complexity > 9.
#          The installed PreToolUse hook BLOCKS the edit (exit 2) and hands the agent an
#          actionable violation report BEFORE the code ever reaches the repository.
#   Act 2  The agent refactors to the same behavior with complexity <= 9. The hook now
#          PASSES the edit (exit 0).
#   Act 3  The agent tries to bypass enforcement with `git commit --no-verify`. The
#          git-guard PreToolUse hook BLOCKS that too (exit 2).
#
# Everything runs against a THROWAWAY git repo in a temp dir, onboarded with
# `agent-fitness-functions client onboard --enforcement block` on a private, non-default
# port so it never collides with a real governance daemon on :7890.
#
# Requirements: the built binary (built into .tmp/ if absent), python3, and git.
# The FINOS `calm` CLI must be on PATH for the server to run CALM validation.
#
# Daemon lifecycle: `onboard` auto-starts a detached local TLS daemon. We stop it
# cleanly on exit via the authenticated POST /shutdown admin endpoint (the demo
# pre-authorizes the dev client CN `dev-hook-pool` as an admin so the dev mTLS cert can
# call it). A best-effort port-scoped fallback covers the rare case where the graceful
# shutdown does not land — it only ever targets the PID listening on THIS demo's unique
# private port, never an unrelated process.

set -euo pipefail

repo_root=$(git -C "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)" rev-parse --show-toplevel)
bin=${AGENT_FITNESS_FUNCTIONS_BIN:-$repo_root/.tmp/agent-fitness-functions}

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT
# A valid governance repo name matches ^[a-z][a-z0-9_-]{0,63}$, so give the throwaway
# repo a controlled basename rather than mktemp's dotted directory name.
demo_repo="$tmp_dir/agent-demo"
repo_name="agent-demo"

addr=${AGENT_FITNESS_FUNCTIONS_ADDR:-}
if [[ -z "$addr" ]]; then
  host="127.0.0.1"
  port=$(python3 - <<'PY'
import socket

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
PY
  )
  addr="https://$host:$port"
else
  read -r host port < <(python3 - "$addr" <<'PY'
import sys
from urllib.parse import urlparse

parsed = urlparse(sys.argv[1])
if parsed.scheme != "https" or not parsed.hostname or parsed.port is None:
    raise SystemExit("AGENT_FITNESS_FUNCTIONS_ADDR must be an https URL with an explicit port")
print(parsed.hostname, parsed.port)
PY
  )
fi
# ADR-0007 machine-scoped governance root: the demo's own shutdown_daemon call needs
# raw cert file paths (it speaks straight to the daemon's admin endpoint, not through
# `client validate`), so it still resolves managed material itself — but the default
# managed root it resolves against is now the ONE machine root every governed repo
# shares, not this throwaway repo's certs/. Mirrors internal/installer.StateRoot and
# internal/client/stateroot.go's governanceCertsDir().
machine_governance_certs_dir="${XDG_STATE_HOME:-$HOME/.local/state}/agent-fitness-functions/governance/certs"
cert_dir="$machine_governance_certs_dir"
client_cert=""
client_key=""
client_ca=""
tls_mode=""

select_tls_mode() {
  local selector=${AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR:-}
  local explicit=0
  [[ -n "${AGENT_FITNESS_FUNCTIONS_CLIENT_CERT:-}${AGENT_FITNESS_FUNCTIONS_CLIENT_KEY:-}${AGENT_FITNESS_FUNCTIONS_CLIENT_CA:-}" ]] && explicit=1
  if [[ -n "$selector" && "$explicit" -eq 1 ]]; then
    echo "AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR cannot be combined with explicit client TLS inputs" >&2
    return 2
  fi
  if [[ "$explicit" -eq 1 ]]; then
    tls_mode=external
    client_cert=${AGENT_FITNESS_FUNCTIONS_CLIENT_CERT:-}
    client_key=${AGENT_FITNESS_FUNCTIONS_CLIENT_KEY:-}
    client_ca=${AGENT_FITNESS_FUNCTIONS_CLIENT_CA:-}
    unset AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR
    return 0
  fi
  tls_mode=managed
  cert_dir=${selector:-$machine_governance_certs_dir}
}

resolve_selected_tls() {
  [[ "$tls_mode" == managed ]] || return 0
  local resolver_rc
  set +e
  managed_version=$(AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR="$cert_dir" "$bin" client resolve-dev-cert-version)
  resolver_rc=$?
  set -e
  [[ "$resolver_rc" -eq 0 ]] || return "$resolver_rc"
  if [[ ! "$managed_version" =~ ^versions/v-[0-9a-f]{32}$ ]]; then
    echo "agent-fitness-functions returned an invalid managed certificate version" >&2
    return 1
  fi
  client_cert=$cert_dir/$managed_version/client.crt
  client_key=$cert_dir/$managed_version/client.key
  client_ca=$cert_dir/$managed_version/ca.crt
  unset AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR
}

select_tls_mode
if [[ "${AGENT_FITNESS_FUNCTIONS_DEMO_TLS_CONTRACT_ONLY:-}" == 1 ]]; then
  resolve_selected_tls
  printf 'mode=%s\nclient_cert=%s\nclient_key=%s\nclient_ca=%s\nselector=%s\n' \
    "$tls_mode" "$client_cert" "$client_key" "$client_ca" "${AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR-unset}"
  exit 0
fi

shutdown_daemon() {
  # Graceful stop via the admin /shutdown endpoint using the dev mTLS client cert.
  [[ -f "$client_cert" ]] || return 0
  python3 - "$host" "$port" "$client_cert" "$client_key" "$client_ca" <<'PY' || true
import http.client
import ssl
import sys

host, port, cert, key, ca = sys.argv[1], int(sys.argv[2]), sys.argv[3], sys.argv[4], sys.argv[5]
ctx = ssl.create_default_context(cafile=ca)
ctx.check_hostname = False  # loopback: we trust our own dev CA, hostname is not the control
ctx.load_cert_chain(cert, key)
try:
    conn = http.client.HTTPSConnection(host, port, context=ctx, timeout=3)
    conn.request("POST", "/shutdown")
    conn.getresponse().read()
    conn.close()
except Exception:
    pass
PY
}

cleanup() {
  if [[ "$tls_mode" == external ]]; then
    rm -rf "$tmp_dir"
    return
  fi
  shutdown_daemon
  # Best-effort safety net, strictly scoped to THIS demo's unique private port so it can
  # never touch an unrelated process. Only runs if the graceful shutdown left it bound.
  if command -v lsof >/dev/null 2>&1; then
    for _ in 1 2 3 4 5; do
      pid=$(lsof -ti "tcp:$port" 2>/dev/null || true)
      [[ -z "$pid" ]] && break
      kill "$pid" 2>/dev/null || true
      sleep 0.2
    done
  fi
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

banner() {
  echo
  echo "=================================================================="
  echo "$1"
  echo "=================================================================="
}

# run_hook <hook-path> <payload-file>: run an installed PreToolUse hook exactly the way
# Claude Code would (payload JSON on stdin, from inside the repo), capturing combined
# output in HOOK_OUT and the exit code in HOOK_RC without tripping `set -e`.
run_hook() {
  local hook=$1 payload=$2
  set +e
  HOOK_OUT=$(cd "$demo_repo" && \
    env AGENT_FITNESS_FUNCTIONS_BIN="$bin" AGENT_FITNESS_FUNCTIONS_ADDR="$addr" \
    "$hook" <"$payload" 2>&1)
  HOOK_RC=$?
  set -e
}

assert_exit() {
  local got=$1 want=$2 what=$3
  if [[ "$got" != "$want" ]]; then
    echo
    echo "FAIL: $what — expected exit $want, got $got" >&2
    echo "----- hook output -----" >&2
    echo "$HOOK_OUT" >&2
    exit 1
  fi
}

# write_payload <relative-file> <content-file> <out-payload>: build a Claude Code
# PreToolUse Write payload with JSON-safe escaping of the proposed file content.
write_payload() {
  python3 - "$1" "$2" "$3" <<'PY'
import json
import sys

rel, content_file, out = sys.argv[1], sys.argv[2], sys.argv[3]
with open(content_file, encoding="utf-8") as handle:
    content = handle.read()
payload = {"tool_name": "Write", "tool_input": {"file_path": rel, "content": content}}
with open(out, "w", encoding="utf-8") as handle:
    handle.write(json.dumps(payload))
PY
}

# -------------------------------------------------------------------------------------
# Setup: build the binary if needed, create + onboard the throwaway governed repo.
# -------------------------------------------------------------------------------------
if [[ ! -x "$bin" ]]; then
  echo "Building agent-fitness-functions into $bin ..."
  GOCACHE="$repo_root/.tmp/go-build" GOMODCACHE="$repo_root/.tmp/go-mod" \
    go build -o "$bin" "$repo_root/cmd/agent-fitness-functions"
fi

banner "SETUP — onboard a throwaway repo to block-mode governance"
echo "Repo:  $demo_repo"
echo "Daemon: $addr (private port, will not collide with a real :7890 daemon)"

git init -q "$demo_repo"
git -C "$demo_repo" config user.email "demo@example.com"
git -C "$demo_repo" config user.name "Governance Demo"

# Pre-authorize the dev client CN as an admin so cleanup can call POST /shutdown with the
# dev mTLS cert. `onboard` merges its caller entry into this file and preserves admins.
mkdir -p "$demo_repo/configs"
cat >"$demo_repo/caller-repos.json" <<'JSON'
{
  "callers": {},
  "admins": ["dev-hook-pool"]
}
JSON

# One command: repo-name detection, dev certs, server-side config scaffold, caller
# authorization, git + agent hook installation, local TLS daemon auto-start, doctor gate.
( cd "$demo_repo" && "$bin" client onboard --enforcement block --addr "$addr" )
resolve_selected_tls
echo
echo "Onboarding complete: '$repo_name' is governed in block mode."

agent_hook="$demo_repo/.git/hooks/agent-fitness-functions-pre-tool-use"
git_guard="$demo_repo/.git/hooks/agent-fitness-functions-git-guard"
target_file="internal/pricing/shipping.go"

# -------------------------------------------------------------------------------------
# Act 1 — the agent's edit is BLOCKED before it lands.
# -------------------------------------------------------------------------------------
banner "ACT 1 — Agent proposes a Write; complexity > 9 is BLOCKED"
echo "The agent wants to write $target_file with a deeply-branched ShippingCost()."
echo "This is the exact PreToolUse payload Claude Code sends the hook."

cat >"$tmp_dir/red.go" <<'EOF'
package pricing

// ShippingCost sums every surcharge with a long chain of independent branches.
func ShippingCost(weight int, zone string, express, insured, fragile bool) int {
	cost := 0
	if weight > 0 {
		cost += weight * 2
	}
	if weight > 10 {
		cost += 15
	}
	if weight > 50 {
		cost += 40
	}
	if weight > 100 {
		cost += 60
	}
	if zone == "intl" {
		cost += 30
	}
	if zone == "remote" {
		cost += 20
	}
	if express {
		cost += 25
	}
	if insured {
		cost += 10
	}
	if fragile {
		cost += 12
	}
	if express && insured {
		cost += 5
	}
	return cost
}
EOF

write_payload "$target_file" "$tmp_dir/red.go" "$tmp_dir/red.json"
run_hook "$agent_hook" "$tmp_dir/red.json"

echo
echo "--- what the agent sees on stderr (exit $HOOK_RC) ---"
echo "$HOOK_OUT"
echo "-----------------------------------------------------"
assert_exit "$HOOK_RC" 2 "Act 1 blocked edit"
echo
echo "WHY IT MATTERS: the violation never reached the repository. The agent got an"
echo "actionable, machine-readable report BEFORE writing — not a red build later."

# -------------------------------------------------------------------------------------
# Act 2 — the agent refactors; the edit now PASSES.
# -------------------------------------------------------------------------------------
banner "ACT 2 — Agent refactors to complexity <= 9; the Write PASSES"
echo "Same behavior, extracted helper + table-driven surcharges. Re-submitting the Write."

cat >"$tmp_dir/green.go" <<'EOF'
package pricing

// ShippingCost keeps the same behavior with a flat structure and one helper.
func ShippingCost(weight int, zone string, express, insured, fragile bool) int {
	cost := 0
	if weight > 0 {
		cost += weight * 2
	}
	cost += weightSurcharge(weight)
	cost += map[string]int{"intl": 30, "remote": 20}[zone]
	for _, opt := range []struct {
		on  bool
		add int
	}{{express, 25}, {insured, 10}, {fragile, 12}, {express && insured, 5}} {
		if opt.on {
			cost += opt.add
		}
	}
	return cost
}

func weightSurcharge(weight int) int {
	surcharge := 0
	for _, tier := range []struct{ over, add int }{{10, 15}, {50, 40}, {100, 60}} {
		if weight > tier.over {
			surcharge += tier.add
		}
	}
	return surcharge
}
EOF

write_payload "$target_file" "$tmp_dir/green.go" "$tmp_dir/green.json"
run_hook "$agent_hook" "$tmp_dir/green.json"

echo
echo "--- hook result (exit $HOOK_RC) ---"
if [[ -n "$HOOK_OUT" ]]; then echo "$HOOK_OUT"; else echo "(no violations — the edit is allowed to proceed)"; fi
echo "-----------------------------------"
assert_exit "$HOOK_RC" 0 "Act 2 passing edit"
echo
echo "WHY IT MATTERS: the agent self-corrected against the governance contract inside a"
echo "single tool call. The passing code is what actually gets written."

# -------------------------------------------------------------------------------------
# Act 3 — the bypass attempt is BLOCKED.
# -------------------------------------------------------------------------------------
banner "ACT 3 — Agent tries 'git commit --no-verify'; the git-guard BLOCKS it"
echo "Even if the agent tries to skip the hooks entirely, the git-guard PreToolUse hook"
echo "intercepts the Bash command."

cat >"$tmp_dir/bash.json" <<'JSON'
{"tool_name":"Bash","tool_input":{"command":"git commit --no-verify -m 'ship it anyway'"}}
JSON

run_hook "$git_guard" "$tmp_dir/bash.json"
echo
echo "--- what the agent sees on stderr (exit $HOOK_RC) ---"
echo "$HOOK_OUT"
echo "-----------------------------------------------------"
assert_exit "$HOOK_RC" 2 "Act 3 bypass block"
echo
echo "WHY IT MATTERS: enforcement can't be shrugged off. The bypass is denied with the"
echo "same 'fix the code, don't skip the check' guidance."

banner "DEMO COMPLETE — all three acts asserted their expected outcome"
echo "Block on bad edit -> explain -> pass on fix -> refuse the bypass. Governance lives"
echo "next to the agent, at the moment of the edit."
