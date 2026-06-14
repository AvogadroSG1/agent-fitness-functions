#!/usr/bin/env bash
set -euo pipefail

repo_root=$(git -C "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)" rev-parse --show-toplevel)
tmp_dir=$(mktemp -d)
demo_repo="$tmp_dir/go-red-green"
bridge_bin="$tmp_dir/calm-bridge"

cleanup() {
  if [[ -n "${bridge_pid:-}" ]]; then
    curl -fsS -X POST "$bridge_addr/shutdown" >/dev/null 2>&1 || true
    wait "$bridge_pid" 2>/dev/null || true
  fi
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

free_port=$(python3 - <<'PY'
import socket

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
PY
)
bridge_addr="http://127.0.0.1:$free_port"

go build -o "$bridge_bin" "$repo_root/cmd/stack-fitness-functions"
"$bridge_bin" server start --addr "127.0.0.1:$free_port" &
bridge_pid=$!

for _ in {1..40}; do
  if curl -fsS "$bridge_addr/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done
curl -fsS "$bridge_addr/health" >/dev/null

git init "$demo_repo" >/dev/null
git -C "$demo_repo" config user.email "calm-smoke@example.com"
git -C "$demo_repo" config user.name "CALM Smoke"
"$repo_root/scripts/install-hooks.sh" "$demo_repo" >/dev/null
mkdir -p "$demo_repo/.calm" "$demo_repo/internal/demo"

write_config() {
  local rule=$1
  cat > "$demo_repo/.calm/config.json" <<JSON
{
  "enforcement-mode": "block",
  "fitness-functions": {
    "cyclomatic-complexity": false,
    "interface-width": false,
    "implementation-depth": false,
    "logic-density": false,
    "dependency-discipline": false,
    "$rule": true
  }
}
JSON
}

# Print "Actual: X | Target: ≤/≥ Y" from a client validate JSON response.
fmt_metrics() {
  printf '%s' "$1" | python3 -c '
import json, sys
try:
    vs = json.load(sys.stdin).get("violations", [])
    if vs:
        v = vs[0]
        val, lim = float(v["value"]), float(v["limit"])
        op = "≤" if val > lim else "≥"
        fmt = lambda n: str(int(n)) if n == int(n) else f"{n:.3f}"
        print(f"Actual: {fmt(val)} | Target: {op} {fmt(lim)}", end="")
    else:
        print("no violation data", end="")
except Exception:
    print("?", end="")
'
}

expect_block() {
  local rule=$1
  local red=$2
  local green=$3
  local expected=$4

  write_config "$rule"

  # Probe red fixture directly to capture actual/target values for display.
  local red_json
  red_json=$("$bridge_bin" client validate \
    --addr "$bridge_addr" \
    --file "internal/demo/demo.go" \
    --repo "$demo_repo" \
    --content "$(cat "$repo_root/$red")" \
    --language go 2>/dev/null) || red_json='{}'
  local red_metrics
  red_metrics=$(fmt_metrics "$red_json")

  cp -f "$repo_root/$red" "$demo_repo/internal/demo/demo.go"
  git -C "$demo_repo" add .calm/config.json internal/demo/demo.go
  if CALM_BRIDGE_BIN="$bridge_bin" CALM_BRIDGE_ADDR="$bridge_addr" git -C "$demo_repo" commit -m "red $rule" >"$tmp_dir/red.out" 2>&1; then
    cat "$tmp_dir/red.out"
    echo "expected red commit to fail for $rule" >&2
    exit 1
  fi
  if ! grep -q "$expected" "$tmp_dir/red.out"; then
    cat "$tmp_dir/red.out"
    echo "red commit for $rule did not mention $expected" >&2
    exit 1
  fi

  cp -f "$repo_root/$green" "$demo_repo/internal/demo/demo.go"
  git -C "$demo_repo" add .calm/config.json internal/demo/demo.go
  CALM_BRIDGE_BIN="$bridge_bin" CALM_BRIDGE_ADDR="$bridge_addr" git -C "$demo_repo" commit -m "green $rule" >/dev/null

  printf 'PASS  %-28s  [Go]  red: blocked (%s)  green: pass\n' "$rule" "$red_metrics"
}

echo "Testing: Go fitness functions"
expect_block "cyclomatic-complexity" "fixtures/violations/go/cyclomatic-complexity.go" "fixtures/green/go/cyclomatic-complexity.go" "cyclomatic complexity"
expect_block "interface-width" "fixtures/violations/go/interface-width.go" "fixtures/green/go/interface-width.go" "exposes"
expect_block "logic-density" "fixtures/violations/go/logic-density.go" "fixtures/green/go/logic-density.go" "Logic Density Ratio"
expect_block "dependency-discipline" "fixtures/violations/go/dependency-discipline.go" "fixtures/green/go/dependency-discipline.go" "Dependency Discipline ratio"

echo "Go red-green smoke test passed"
