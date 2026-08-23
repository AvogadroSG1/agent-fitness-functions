#!/usr/bin/env bash
set -euo pipefail

# install.sh is the ADR-0005 bootstrap installer (rustup/uv-style): it verifies a
# release archive's checksum BEFORE extraction, publishes it under an
# XDG-state-rooted versions/<version>/ directory, writes a .verified completion
# sentinel last, and atomically repoints "current" at it. Every subsequent
# lifecycle operation (upgrade, rollback, uninstall, and now runtime
# provisioning) is a subcommand of the installed binary itself, not of this
# script: --provision-runtimes below is a thin delegation to `agent-fitness-
# functions runtime provision`, not a second provisioning implementation.

product_name="agent-fitness-functions"

usage() {
  cat <<'EOF' >&2
usage: install.sh --archive <path> --checksums <path> [--provision-runtimes]
       install.sh --help

  Options:
    --archive <path>       path to a agent-fitness-functions-<version>-darwin-arm64.tar.gz release archive
    --checksums <path>     path to the SHA256SUMS manifest covering that archive
    --provision-runtimes   after publishing, run the installed binary's
                           `runtime provision` to provision the managed CALM
                           CLI and Python (radon/pyyaml) runtimes. Requires a
                           host node/npm and python3 (documented, not
                           product-managed, bootstrap prerequisites).

  Environment:
    XDG_STATE_HOME   product-owned install root (default: ~/.local/state)
    XDG_CACHE_HOME   extraction scratch space   (default: ~/.cache)

  After install, add $XDG_STATE_HOME/agent-fitness-functions/current/bin to PATH.
EOF
  exit 0
}

archive=""
checksums=""
provision_runtimes=0
while [ $# -gt 0 ]; do
  case "$1" in
    --archive)
      archive="${2:-}"
      shift 2
      ;;
    --checksums)
      checksums="${2:-}"
      shift 2
      ;;
    --provision-runtimes)
      provision_runtimes=1
      shift
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

if [ -z "$archive" ] || [ -z "$checksums" ]; then
  echo "error: --archive and --checksums are required" >&2
  exit 2
fi
if [ ! -f "$archive" ]; then
  echo "error: archive not found: $archive" >&2
  exit 1
fi
if [ ! -f "$checksums" ]; then
  echo "error: checksums manifest not found: $checksums" >&2
  exit 1
fi

archive_name=$(basename "$archive")

# Verify checksum BEFORE any state-root mutation: a tampered archive must abort
# without creating, cleaning, or touching anything under the state root.
expected_sum=$(awk -v name="$archive_name" '$2 == name || $2 == "*" name { print $1; exit }' "$checksums")
if [ -z "$expected_sum" ]; then
  echo "error: no checksum entry for $archive_name in $checksums" >&2
  exit 1
fi
actual_sum=$(shasum -a 256 "$archive" | awk '{print $1}')
if [ "$actual_sum" != "$expected_sum" ]; then
  echo "error: checksum verification failed for $archive_name (expected $expected_sum, got $actual_sum)" >&2
  exit 1
fi

case "$archive_name" in
  "$product_name"-*-darwin-arm64.tar.gz) ;;
  *)
    echo "error: unrecognized archive name: $archive_name" >&2
    exit 1
    ;;
esac
version=${archive_name#"$product_name"-}
version=${version%-darwin-arm64.tar.gz}

state_home="${XDG_STATE_HOME:-$HOME/.local/state}"
state_root="$state_home/$product_name"
versions_dir="$state_root/versions"
current_link="$state_root/current"

cache_home="${XDG_CACHE_HOME:-$HOME/.cache}"
staging_root="$cache_home/$product_name"

mkdir -p "$versions_dir"
mkdir -p "$staging_root"

# Scan-and-remove unverified partial version directories before attempting a new
# publish (ADR-0005): a directory lacking .verified is never reused or left to
# accumulate.
for entry in "$versions_dir"/*/; do
  [ -d "$entry" ] || continue
  if [ ! -f "${entry}.verified" ]; then
    rm -rf "${entry%/}"
  fi
done

target_dir="$versions_dir/$version"

if [ ! -f "$target_dir/.verified" ]; then
  staging_dir=$(mktemp -d "$staging_root/install-staging-XXXXXX")
  cleanup_staging() {
    rm -rf "$staging_dir"
  }
  trap cleanup_staging EXIT

  tar -xzf "$archive" -C "$staging_dir"
  chmod +x "$staging_dir"/bin/* 2>/dev/null || true

  rm -rf "$target_dir"
  mv "$staging_dir" "$target_dir"

  # .verified is written last, only once the version directory is fully and
  # atomically in place, so a partial extraction is never mistaken for a
  # complete one.
  touch "$target_dir/.verified"
fi

# Atomic current-pointer swap. `ln -sfn` is used deliberately instead of a
# temp-symlink-plus-mv dance: mv(1) treats a destination that is itself a
# symlink to a directory as "move the source into that directory" rather than
# replacing the symlink, which would silently nest the new pointer inside the
# old target. -n disables exactly that dereference-and-follow behavior, so
# readers never observe anything but the old or the new pointer.
ln -sfn "versions/$version" "$current_link"

echo "installed $product_name $version at $state_root"
echo "add $current_link/bin to PATH to use it"

if [ "$provision_runtimes" -eq 1 ]; then
  "$current_link/bin/$product_name" runtime provision
fi
