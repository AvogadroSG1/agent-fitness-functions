#!/usr/bin/env bash
#
# demo-unifi-video.sh — Re-runnable video demo script and end-to-end smoke test
# demonstrating agent-fitness-functions governance with ~/peter_code/unifi-cli.
#
# Narrative Arc:
#   Act 0: Onboard unifi-cli with agent-fitness-functions in block mode on a private port.
#   Act 1: Agent proposes a naive monolithic RF link health diagnosis (Cyclomatic Complexity = 13 > 9).
#          The PreToolUse hook intercepts before disk write, returning structured YAML guidance (exit 2).
#   Act 2: Agent reads guidance and refactors to modular predicate helpers (Complexity <= 3).
#          The PreToolUse hook passes silently (exit 0) and the file is written.
#   Act 3: Agent attempts to bypass governance with `git commit --no-verify`.
#          The git-guard hook blocks the bypass command (exit 2).
#   Act 4: Commit and verification with pre-commit hook and unit tests.
#
set -euo pipefail

repo_root=$(git -C "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)" rev-parse --show-toplevel)
bin=${AGENT_FITNESS_FUNCTIONS_BIN:-$repo_root/.tmp/agent-fitness-functions}
target_repo=${UNIFI_REPO:-$HOME/peter_code/unifi-cli}

if [[ ! -d "$target_repo/.git" ]]; then
  echo "Error: target git repository $target_repo not found" >&2
  exit 1
fi

# Build binary if missing
if [[ ! -x "$bin" ]]; then
  echo "Building agent-fitness-functions into $bin ..."
  mkdir -p "$repo_root/.tmp"
  go build -o "$bin" "$repo_root/cmd/agent-fitness-functions"
fi

# Allocate a private dynamic port to avoid collision with any running daemon on :7890
host="127.0.0.1"
port=$(python3 - <<'PY'
import socket

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
PY
)
addr="https://$host:$port"

tmp_dir=$(mktemp -d)
cert_dir="$target_repo/certs"
client_cert=""
client_key=""
client_ca=""

shutdown_daemon() {
  if [[ -f "$client_cert" && -f "$client_key" && -f "$client_ca" ]]; then
    python3 - "$host" "$port" "$client_cert" "$client_key" "$client_ca" <<'PY' || true
import http.client
import ssl
import sys

host, port, cert, key, ca = sys.argv[1], int(sys.argv[2]), sys.argv[3], sys.argv[4], sys.argv[5]
ctx = ssl.create_default_context(cafile=ca)
ctx.check_hostname = False
ctx.load_cert_chain(cert, key)
try:
    conn = http.client.HTTPSConnection(host, port, context=ctx, timeout=3)
    conn.request("POST", "/shutdown")
    conn.getresponse().read()
    conn.close()
except Exception:
    pass
PY
  fi
}

cleanup() {
  echo
  echo "🧹 Cleaning up demo environment..."
  shutdown_daemon
  if command -v lsof >/dev/null 2>&1; then
    for _ in 1 2 3 4 5; do
      pid=$(lsof -ti "tcp:$port" 2>/dev/null || true)
      [[ -z "$pid" ]] && break
      kill "$pid" 2>/dev/null || true
      sleep 0.2
    done
  fi
  rm -rf "$tmp_dir"
  # Reset unifi-cli git state safely
  if [[ -d "$target_repo" ]]; then
    rm -f "$target_repo/internal/service/client_health.go"
    git -C "$target_repo" checkout main -q 2>/dev/null || true
    git -C "$target_repo" branch -D demo/fitness-video 2>/dev/null || true
    # Clean up generated certs / configs for the throwaway test if present
    rm -rf "$target_repo/certs" "$target_repo/configs" "$target_repo/caller-repos.json" "$target_repo/.claude" 2>/dev/null || true
    git -C "$target_repo" checkout -- . 2>/dev/null || true
    git -C "$target_repo" clean -fdq 2>/dev/null || true
  fi
  echo "✅ Target repository restored."
}
trap cleanup EXIT

banner() {
  echo
  echo "=================================================================="
  echo "$1"
  echo "=================================================================="
}

run_hook() {
  local hook=$1 payload=$2
  set +e
  HOOK_OUT=$(cd "$target_repo" && \
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

# ------------------------------------------------------------------------------
# SETUP: Prepare unifi-cli branch and onboard
# ------------------------------------------------------------------------------
banner "ACT 0: 0-to-Governed Onboarding on unifi-cli"
echo "Repository: $target_repo"
echo "Daemon URL: $addr"

git -C "$target_repo" checkout -b demo/fitness-video -q

# Pre-authorize dev client CN as admin so cleanup can invoke /shutdown
mkdir -p "$target_repo/configs"
cat >"$target_repo/caller-repos.json" <<'JSON'
{
  "callers": {},
  "admins": ["dev-hook-pool"]
}
JSON

( cd "$target_repo" && "$bin" client onboard --enforcement block --repo unifi-cli --addr "$addr" )

# Resolve managed dev cert paths for admin shutdown
managed_version=$(AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR="$cert_dir" "$bin" client resolve-dev-cert-version)
client_cert="$cert_dir/$managed_version/client.crt"
client_key="$cert_dir/$managed_version/client.key"
client_ca="$cert_dir/$managed_version/ca.crt"

echo
echo "▶ Running agent-fitness-functions doctor"
( cd "$target_repo" && "$bin" doctor --repo unifi-cli --addr "$addr" )

agent_hook="$target_repo/.git/hooks/agent-fitness-functions-pre-tool-use"
git_guard="$target_repo/.git/hooks/agent-fitness-functions-git-guard"
target_file="internal/service/client_health.go"

# ------------------------------------------------------------------------------
# ACT 1: Naive Monolithic Implementation (Complexity 13 > 9) is Blocked
# ------------------------------------------------------------------------------
banner "ACT 1: Agent Proposes Naive Write (Complexity 13 > 9) — BLOCKED"
echo "The agent wants to write $target_file with a monolithic DiagnoseClientHealth()."
echo "PreToolUse hook intercepts before the file is written to disk."

cat >"$tmp_dir/red.go" <<'EOF'
package service

import "github.com/AvogadroSG1/unifi-cli/internal/domain"

func DiagnoseClientHealth(c domain.Client) string {
	if c.IsWired {
		return "EXCELLENT"
	}
	if c.Signal < -85 {
		if c.Radio == "ng" {
			return "CRITICAL_2G"
		} else if c.Radio == "na" {
			return "CRITICAL_5G"
		} else {
			return "CRITICAL"
		}
	} else if c.Signal < -75 {
		if c.Noise != 0 && (c.Signal-c.Noise) < 15 {
			return "POOR_SNR"
		}
		if c.TxRetries > 1000 && c.TxPackets > 0 && (float64(c.TxRetries)/float64(c.TxPackets)) > 0.3 {
			return "HIGH_RETRY"
		}
		return "DEGRADED"
	} else {
		if c.TxPackets > 0 && (float64(c.TxRetries)/float64(c.TxPackets)) > 0.5 {
			return "UNSTABLE"
		}
		return "EXCELLENT"
	}
}
EOF

python3 - "$target_file" "$tmp_dir/red.go" "$tmp_dir/red.json" <<'PY'
import json, sys
rel, content_file, out = sys.argv[1], sys.argv[2], sys.argv[3]
with open(content_file, encoding="utf-8") as handle:
    content = handle.read()
payload = {"tool_name": "Write", "tool_input": {"file_path": rel, "content": content}}
with open(out, "w", encoding="utf-8") as handle:
    handle.write(json.dumps(payload))
PY

run_hook "$agent_hook" "$tmp_dir/red.json"

echo
echo "--- What the agent sees on stderr (exit $HOOK_RC) ---"
echo "$HOOK_OUT"
echo "-----------------------------------------------------"
assert_exit "$HOOK_RC" 2 "Act 1 blocked edit"
echo "✔ ACT 1 SUCCESS: Monolithic edit blocked before disk write."

# ------------------------------------------------------------------------------
# ACT 2: Agent Refactors to Modular Helpers (Complexity <= 3) — PASSES
# ------------------------------------------------------------------------------
banner "ACT 2: Agent Refactors to Modular Helpers (Complexity <= 3) — PASSES"
echo "The agent reads the YAML guidance, creates pure predicate helpers, and re-submits."

cat >"$tmp_dir/green.go" <<'EOF'
package service

import "github.com/AvogadroSG1/unifi-cli/internal/domain"

type HealthStatus string

const (
	HealthExcellent HealthStatus = "EXCELLENT"
	HealthDegraded  HealthStatus = "DEGRADED"
	HealthCritical  HealthStatus = "CRITICAL"
)

func DiagnoseClientHealth(c domain.Client) HealthStatus {
	if c.IsWired {
		return HealthExcellent
	}
	if isCriticalSignal(c.Signal) || isCriticalRetry(c.TxRetries, c.TxPackets) {
		return HealthCritical
	}
	if isDegradedRF(c.Signal, c.Noise, c.Radio) {
		return HealthDegraded
	}
	return HealthExcellent
}

func isCriticalSignal(signal int) bool {
	return signal != 0 && signal < -80
}

func isCriticalRetry(retries, packets int64) bool {
	pct, ok := RetryPct(retries, packets)
	return ok && pct > 25.0
}

func isDegradedRF(signal, noise int, radio string) bool {
	if signal != 0 && noise != 0 && (signal-noise) < 20 {
		return true
	}
	return radio == "ng" || radio == "na"
}
EOF

python3 - "$target_file" "$tmp_dir/green.go" "$tmp_dir/green.json" <<'PY'
import json, sys
rel, content_file, out = sys.argv[1], sys.argv[2], sys.argv[3]
with open(content_file, encoding="utf-8") as handle:
    content = handle.read()
payload = {"tool_name": "Write", "tool_input": {"file_path": rel, "content": content}}
with open(out, "w", encoding="utf-8") as handle:
    handle.write(json.dumps(payload))
PY

run_hook "$agent_hook" "$tmp_dir/green.json"

echo
echo "--- Hook result (exit $HOOK_RC) ---"
if [[ -n "$HOOK_OUT" ]]; then echo "$HOOK_OUT"; else echo "(Pass: 0 violations, write permitted)"; fi
echo "-----------------------------------"
assert_exit "$HOOK_RC" 0 "Act 2 passing edit"
echo "✔ ACT 2 SUCCESS: Clean decomposed code passed inspection."

# Write the passing code to disk
cp -f "$tmp_dir/green.go" "$target_repo/$target_file"

# ------------------------------------------------------------------------------
# ACT 3: Agent Attempts Bypass with `git commit --no-verify` — BLOCKED
# ------------------------------------------------------------------------------
banner "ACT 3: Agent Tries 'git commit --no-verify' — BLOCKED by Git-Guard"
echo "Testing git-guard defense against hook evasion."

cat >"$tmp_dir/bypass.json" <<'JSON'
{"tool_name":"Bash","tool_input":{"command":"git commit --no-verify -m 'skip checks'"}}
JSON

run_hook "$git_guard" "$tmp_dir/bypass.json"

echo
echo "--- What the agent sees on stderr (exit $HOOK_RC) ---"
echo "$HOOK_OUT"
echo "-----------------------------------------------------"
assert_exit "$HOOK_RC" 2 "Act 3 bypass block"
echo "✔ ACT 3 SUCCESS: Bypass attempt denied by git-guard."

# ------------------------------------------------------------------------------
# ACT 4: Commit and Final Verification
# ------------------------------------------------------------------------------
banner "ACT 4: Clean Commit & Verification"
git -C "$target_repo" add "$target_file"
env AGENT_FITNESS_FUNCTIONS_BIN="$bin" AGENT_FITNESS_FUNCTIONS_ADDR="$addr" \
  git -C "$target_repo" commit -m "feat(service): add client RF health diagnostics"

banner "🎉 DEMO COMPLETE: All 4 acts executed and verified successfully!"
