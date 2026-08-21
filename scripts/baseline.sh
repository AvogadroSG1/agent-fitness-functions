#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
AGENT_FITNESS_FUNCTIONS_bin="${AGENT_FITNESS_FUNCTIONS_BIN:-$ROOT/agent-fitness-functions}"

if [ ! -x "$AGENT_FITNESS_FUNCTIONS_bin" ]; then
  (cd "$ROOT" && go build -o "$AGENT_FITNESS_FUNCTIONS_bin" ./cmd/agent-fitness-functions)
fi

"$AGENT_FITNESS_FUNCTIONS_bin" baseline "$@"
