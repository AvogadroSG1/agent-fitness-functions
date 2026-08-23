#!/usr/bin/env bash
set -euo pipefail

# package-release.sh builds the agent-fitness-functions darwin-arm64 release
# archive per ADR-0005: the Go binary and bin/ helper scripts, plus (slices
# 4-8) the release-side assets managed runtime provisioning needs at install
# time: a self-contained Roslyn analyzer built with the pinned .NET 8 SDK
# exactly as the Dockerfile's dotnet-build stage does, and copies of the
# committed tools/calm-runtime/{package.json,package-lock.json} and
# requirements.lock so `agent-fitness-functions runtime provision` can find
# them without a source checkout. Packaged as
# agent-fitness-functions-<version>-darwin-arm64.tar.gz alongside a
# SHA256SUMS manifest that `shasum -a 256 -c` can verify.
#
# CALM_RUNTIME_PIN and PYTHON_RUNTIME_PIN below MUST agree with
# internal/installer/pins.go (CALMCLIVersion, RadonVersion); they are
# recorded into each release's share/pins.json so `rollback` can reconcile
# runtimes/<tool>/current pointers to the version being restored.

usage() {
  cat <<'EOF' >&2
usage: package-release.sh --output <dir> [--version <version>]
       package-release.sh --help

  Options:
    --output <dir>       directory to write the archive and SHA256SUMS into (required)
    --version <version>  override the derived version (default: git describe --always --dirty)

  Environment:
    GOCACHE, GOMODCACHE   Go build/module caches (default: <repo>/.tmp/go-build, <repo>/.tmp/go-mod)
    DOTNET_ROOT           pinned .NET 8 SDK root used ONLY for the Roslyn analyzer publish
                          step below (e.g. $HOME/.dotnet); the host's default `dotnet` on
                          PATH is never relied on, so a newer/older host SDK cannot silently
                          produce a mismatched analyzer.
EOF
  exit 0
}

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
product_name="agent-fitness-functions"
target_os="darwin"
target_arch="arm64"
dotnet_rid="osx-arm64"
# Keep in sync with internal/installer/pins.go; TestManagedToolPinsAgree...
# in internal/installer/pins_drift_test.go does not check this script
# directly, but a mismatch here would still produce a release whose
# runtimes/*/current pointers doctor and rollback disagree with.
calm_runtime_pin="1.40.0"
python_runtime_pin="6.0.1"

output_dir=""
version=""
while [ $# -gt 0 ]; do
  case "$1" in
    --output)
      output_dir="${2:-}"
      shift 2
      ;;
    --version)
      version="${2:-}"
      shift 2
      ;;
    --help|-h)
      usage
      ;;
    *)
      echo "error: unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

if [ -z "$output_dir" ]; then
  echo "error: --output is required" >&2
  exit 2
fi
mkdir -p "$output_dir"
output_dir=$(cd "$output_dir" && pwd)

if [ -z "$version" ]; then
  version=$(cd "$repo_root" && git describe --tags --always --dirty 2>/dev/null || true)
fi
if [ -z "$version" ]; then
  version="0.0.0"
fi

work_dir=$(mktemp -d)
cleanup() {
  rm -rf "$work_dir"
}
trap cleanup EXIT

mkdir -p "$work_dir/bin"

: "${GOCACHE:=$repo_root/.tmp/go-build}"
: "${GOMODCACHE:=$repo_root/.tmp/go-mod}"
export GOCACHE
export GOMODCACHE

(
  cd "$repo_root"
  GOOS="$target_os" GOARCH="$target_arch" CGO_ENABLED=0 \
    go build -o "$work_dir/bin/$product_name" ./cmd/agent-fitness-functions
)

cp "$repo_root/bin/agent-fitness-functions-serve" "$work_dir/bin/"
cp "$repo_root/bin/agent-fitness-functions-test" "$work_dir/bin/"
chmod +x "$work_dir"/bin/*

# share/calm-runtime and share/requirements.lock: the exact committed files
# `agent-fitness-functions runtime provision` needs to reproduce the
# Dockerfile's pinned CALM CLI and radon/pyyaml provisioning without a source
# checkout (ADR-0005 "Managed Tool Components and Integrity").
mkdir -p "$work_dir/share/calm-runtime"
cp "$repo_root/tools/calm-runtime/package.json" "$work_dir/share/calm-runtime/"
cp "$repo_root/tools/calm-runtime/package-lock.json" "$work_dir/share/calm-runtime/"
cp "$repo_root/requirements.lock" "$work_dir/share/requirements.lock"

cat > "$work_dir/share/pins.json" <<EOF
{
  "calm": "$calm_runtime_pin",
  "python": "$python_runtime_pin"
}
EOF

# Self-contained Roslyn analyzer (ADR-0005 "doctor and Repair" /
# "Managed Tool Components and Integrity"): built exactly as the Dockerfile's
# dotnet-build stage does, so C# governance checks at install time never
# invoke the host `dotnet` or depend on DOTNET_ROOT. DOTNET_ROOT is prepended
# to PATH only for this one publish invocation in a subshell, never exported
# to the rest of this script or the packaged artifacts.
mkdir -p "$work_dir/share/roslyn-analyzer"
(
  if [ -n "${DOTNET_ROOT:-}" ]; then
    export PATH="$DOTNET_ROOT:$PATH"
  fi
  cd "$repo_root/tools/roslyn-analyzer"
  dotnet publish -c Release --self-contained true -r "$dotnet_rid" \
    -o "$work_dir/share/roslyn-analyzer"
)

archive_name="${product_name}-${version}-${target_os}-${target_arch}.tar.gz"
tar -czf "$output_dir/$archive_name" -C "$work_dir" bin share

(
  cd "$output_dir"
  shasum -a 256 "$archive_name" > SHA256SUMS
)

echo "wrote $output_dir/$archive_name"
echo "wrote $output_dir/SHA256SUMS"
