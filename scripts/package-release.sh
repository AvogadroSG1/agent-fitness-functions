#!/usr/bin/env bash
set -euo pipefail

# package-release.sh builds the agent-fitness-functions darwin-arm64 release
# archive per ADR-0005 slice 1: the Go binary plus the bin/ helper scripts,
# packaged as agent-fitness-functions-<version>-darwin-arm64.tar.gz alongside a
# SHA256SUMS manifest that `shasum -a 256 -c` can verify.
#
# Roslyn/CALM/python runtime provisioning are later slices of calm-poc-phk.2
# and are deliberately NOT included by this script.

usage() {
  cat <<'EOF' >&2
usage: package-release.sh --output <dir> [--version <version>]
       package-release.sh --help

  Options:
    --output <dir>       directory to write the archive and SHA256SUMS into (required)
    --version <version>  override the derived version (default: git describe --always --dirty)

  Environment:
    GOCACHE, GOMODCACHE   Go build/module caches (default: <repo>/.tmp/go-build, <repo>/.tmp/go-mod)
EOF
  exit 0
}

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
product_name="agent-fitness-functions"
target_os="darwin"
target_arch="arm64"

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

archive_name="${product_name}-${version}-${target_os}-${target_arch}.tar.gz"
tar -czf "$output_dir/$archive_name" -C "$work_dir" bin

(
  cd "$output_dir"
  shasum -a 256 "$archive_name" > SHA256SUMS
)

echo "wrote $output_dir/$archive_name"
echo "wrote $output_dir/SHA256SUMS"
