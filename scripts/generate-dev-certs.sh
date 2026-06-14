#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cert_dir=${STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR:-"$repo_root/certs"}
openssl_bin=${OPENSSL:-openssl}
force=0

usage() {
  cat <<'USAGE'
Usage: scripts/generate-dev-certs.sh [--force]

Creates local-only development TLS material for docker compose:
  certs/ca.crt
  certs/server.crt
  certs/server.key
  certs/client.crt
  certs/client.key

The client certificate uses CN=dev-hook-pool, which is allowed by caller-repos.json.
Generated files are ignored by git and MUST NOT be used for production.
USAGE
}

for arg in "$@"; do
  case "$arg" in
    --force)
      force=1
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $arg" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if ! command -v "$openssl_bin" >/dev/null 2>&1; then
  echo "openssl is required; set OPENSSL=/path/to/openssl if it is not on PATH" >&2
  exit 1
fi

mkdir -p "$cert_dir"

required_files=(
  "$cert_dir/ca.crt"
  "$cert_dir/server.crt"
  "$cert_dir/server.key"
  "$cert_dir/client.crt"
  "$cert_dir/client.key"
)

existing=0
missing=0
for file in "${required_files[@]}"; do
  if [[ -e "$file" ]]; then
    existing=1
  else
    missing=1
  fi
done

if [[ "$existing" -eq 1 && "$missing" -eq 0 && "$force" -eq 0 ]]; then
  echo "development certificates already exist in $cert_dir"
  echo "use --force to rotate them"
  exit 0
fi

if [[ "$existing" -eq 1 && "$missing" -eq 1 && "$force" -eq 0 ]]; then
  echo "partial certificate set found in $cert_dir" >&2
  echo "remove the partial files or rerun with --force to regenerate local dev certs" >&2
  exit 1
fi

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

cat > "$tmp_dir/server-ext.cnf" <<'EOF'
subjectAltName = @alt_names
extendedKeyUsage = serverAuth
keyUsage = digitalSignature, keyEncipherment

[alt_names]
DNS.1 = localhost
DNS.2 = stack-fitness-functions
IP.1 = 127.0.0.1
IP.2 = ::1
EOF

cat > "$tmp_dir/client-ext.cnf" <<'EOF'
extendedKeyUsage = clientAuth
keyUsage = digitalSignature, keyEncipherment
EOF

"$openssl_bin" req -x509 -newkey rsa:2048 -sha256 -days 365 -nodes \
  -subj "/CN=calm-poc-dev-ca" \
  -keyout "$tmp_dir/ca.key" \
  -out "$tmp_dir/ca.crt" >/dev/null 2>&1

"$openssl_bin" req -newkey rsa:2048 -nodes \
  -subj "/CN=localhost" \
  -keyout "$tmp_dir/server.key" \
  -out "$tmp_dir/server.csr" >/dev/null 2>&1

"$openssl_bin" x509 -req -sha256 -days 365 \
  -in "$tmp_dir/server.csr" \
  -CA "$tmp_dir/ca.crt" \
  -CAkey "$tmp_dir/ca.key" \
  -CAcreateserial \
  -extfile "$tmp_dir/server-ext.cnf" \
  -out "$tmp_dir/server.crt" >/dev/null 2>&1

"$openssl_bin" req -newkey rsa:2048 -nodes \
  -subj "/CN=dev-hook-pool" \
  -keyout "$tmp_dir/client.key" \
  -out "$tmp_dir/client.csr" >/dev/null 2>&1

"$openssl_bin" x509 -req -sha256 -days 365 \
  -in "$tmp_dir/client.csr" \
  -CA "$tmp_dir/ca.crt" \
  -CAkey "$tmp_dir/ca.key" \
  -CAcreateserial \
  -extfile "$tmp_dir/client-ext.cnf" \
  -out "$tmp_dir/client.crt" >/dev/null 2>&1

install -m 0644 "$tmp_dir/ca.crt" "$cert_dir/ca.crt"
install -m 0644 "$tmp_dir/server.crt" "$cert_dir/server.crt"
install -m 0600 "$tmp_dir/server.key" "$cert_dir/server.key"
install -m 0644 "$tmp_dir/client.crt" "$cert_dir/client.crt"
install -m 0600 "$tmp_dir/client.key" "$cert_dir/client.key"

cat <<EOF
Generated local development certificates in $cert_dir:
  certs/ca.crt
  certs/server.crt
  certs/server.key
  certs/client.crt
  certs/client.key

These files are for local docker compose verification only and MUST NOT be used for production.
EOF
