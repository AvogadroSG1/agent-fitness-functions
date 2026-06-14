#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CALM_BRIDGE="${CALM_BRIDGE:-$ROOT/calm-bridge}"

if [ ! -x "$CALM_BRIDGE" ]; then
  (cd "$ROOT" && go build -o "$CALM_BRIDGE" ./cmd/stack-fitness-functions)
fi

"$CALM_BRIDGE" baseline "$@"
