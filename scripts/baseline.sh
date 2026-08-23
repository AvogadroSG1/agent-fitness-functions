#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
agent_fitness_functions_bin="${AGENT_FITNESS_FUNCTIONS_BIN:-$ROOT/agent-fitness-functions}"

if [ ! -x "$agent_fitness_functions_bin" ]; then
  (cd "$ROOT" && go build -o "$agent_fitness_functions_bin" ./cmd/agent-fitness-functions)
fi

"$agent_fitness_functions_bin" baseline "$@"
