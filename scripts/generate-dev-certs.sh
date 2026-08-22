#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/generate-dev-certs.sh [--force]

Delegates local development certificate publication to stack-fitness-functions.
USAGE
}

args=(client onboard --certificates-only)
case "${1:-}" in
  "") ;;
  --force) args+=(--force-dev-cert-rotation) ;;
  -h|--help) usage; exit 0 ;;
  *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
esac
[[ $# -le 1 ]] || { echo "too many arguments" >&2; usage >&2; exit 2; }

exec stack-fitness-functions "${args[@]}"
