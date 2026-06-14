#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
stack_fitness_functions_bin="${STACK_FITNESS_FUNCTIONS_BIN:-$ROOT/stack-fitness-functions}"

if [ ! -x "$stack_fitness_functions_bin" ]; then
  (cd "$ROOT" && go build -o "$stack_fitness_functions_bin" ./cmd/stack-fitness-functions)
fi

"$stack_fitness_functions_bin" baseline "$@"
