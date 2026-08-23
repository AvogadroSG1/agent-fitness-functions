#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
stack_fitness_functions_bin="${AGENT_FITNESS_FUNCTIONS_BIN:-$ROOT/agent-fitness-functions}"

if [ ! -x "$stack_fitness_functions_bin" ]; then
  (cd "$ROOT" && go build -o "$stack_fitness_functions_bin" ./cmd/agent-fitness-functions)
fi

"$stack_fitness_functions_bin" baseline "$@"
