# Calm PoC

Calm PoC is a local proof-of-concept architecture-as-code system that uses CALM fitness functions to evaluate proposed source changes before they are written or committed.

See [docs/spec/why-and-what.md](docs/spec/why-and-what.md) and [docs/spec/engineering-spec.md](docs/spec/engineering-spec.md) for the product and engineering specification.

## Tool Requirements

- Go 1.22 or newer for the `stack-fitness-functions` binary
- FINOS CALM CLI 1.40.0 via `npm install -g @finos/calm-cli@1.40.0`
- `radon` 6.0.1 on `PATH`, or pass `--radon <path>`, for Python baseline analysis
- .NET 8 SDK for `tools/roslyn-analyzer`; `stack-fitness-functions baseline --language csharp` builds the local analyzer automatically when `--roslyn <path>` is omitted
- `pyyaml` 6+ for hook violation formatting: `python3 -m pip install -r hooks/requirements.txt`
- Docker with BuildKit for validating the container image; the image packages the Go server, FINOS CALM CLI 1.40.0, Python `radon==6.0.1`, and the self-contained .NET 8 Roslyn analyzer.

## Container Image

The repository includes a multi-stage `Dockerfile` for the containerized stack-fitness-functions service. It builds the Go server, publishes the .NET analyzer, installs FINOS CALM CLI 1.40.0, installs Python plus `radon==6.0.1`, runs as non-root `appuser` uid 1001, and starts with `/app/stack-fitness-functions server start`.

Use a Docker-enabled environment to verify the image contract:

```bash
docker build --build-arg GIT_SHA="$(git rev-parse --short HEAD)" --build-arg BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" -t stack-fitness-functions:local .
```

`docker-compose.yml` provides the local/staging deployment contract. It mounts `./configs`, `./certs`, and `./caller-repos.json` read-only, runs the container as a hardened service, and passes TLS flags to `/app/stack-fitness-functions server start`.

Before running Compose, provide these local certificate files for the mounted TLS volume:

```text
certs/server.crt
certs/server.key
certs/ca.crt
```

For local verification, generate non-production development certificates with:

```bash
scripts/generate-dev-certs.sh
```

The script also creates `certs/client.crt` and `certs/client.key` with CN `dev-hook-pool` for authenticated local hook checks. Generated files under `certs/` are ignored by git and excluded from the Docker build context.

Then verify the deployment artifact with:

```bash
docker compose config --quiet
docker compose up --build
```

Compose resource limits are local/staging guidance; Swarm enforces `deploy.resources`, and Kubernetes production deployments should treat the Helm chart as authoritative.

## Governance Layer Deployment

The stack-fitness-functions server is an enterprise organization-wide governance layer. The containerized service is the **primary production path**. Every governed repository connects to a shared, centrally operated container instance; governance thresholds and enforcement configuration are authoritative only when served from the container.

The local `.calm` mode (described in the CLI tools section below) is a **sandbox environment** for developer iteration and demonstration. It does not substitute for the container layer in any production or CI context.

### Deployment topology

```
┌─────────────────────────────────┐
│  Container (authoritative)      │
│  stack-fitness-functions server start │
│  configs/<repo>/config.json  ←─ governance source of truth
│  certs/{server,ca}.{crt,key}    │
└──────────────┬──────────────────┘
               │ HTTPS + mTLS
       ┌───────┴────────┐
       │                │
  pre-commit.sh    pre-tool-use.sh
  (developer git)  (AI agent hook)
```

### Environment variables for remote mode

| Variable | Required | Purpose |
|----------|----------|---------|
| `STACK_FITNESS_FUNCTIONS_ADDR` | Yes | Full HTTPS URL, e.g. `https://calm-governance.example:7890` |
| `STACK_FITNESS_FUNCTIONS_ALLOW_REMOTE` | Yes (set to `1`) | Opt-in to non-loopback bridge addresses |
| `STACK_FITNESS_FUNCTIONS_CLIENT_CERT` | Yes (mTLS) | Path to PEM-encoded client certificate |
| `STACK_FITNESS_FUNCTIONS_CLIENT_KEY` | Yes (mTLS) | Path to PEM-encoded client private key |
| `STACK_FITNESS_FUNCTIONS_CLIENT_CA` | Yes (mTLS) | Path to PEM-encoded CA bundle for server verification |
| `STACK_FITNESS_FUNCTIONS_REPO_NAME` | Recommended | Logical repository name (overrides working-tree basename) |

All hooks enforce HTTPS when `STACK_FITNESS_FUNCTIONS_ALLOW_REMOTE=1` is set. Connections over plain HTTP to a non-loopback address are rejected at the hook layer.

## CLI Tools

The `bin/` directory contains helper scripts. Add it to your PATH once after cloning:

```bash
export PATH="$(pwd)/bin:$PATH"
```

Or symlink into `~/.local/bin` for a permanent install:

```bash
ln -sf "$(pwd)/bin"/stack-fitness-functions-* ~/.local/bin/
```

| Command | Purpose |
|---------|---------|
| `stack-fitness-functions client install-hooks [repo]` | Install the embedded Git hooks into a repository |
| `stack-fitness-functions-serve [--build]` | Start the stack-fitness-functions server container via Docker Compose (Docker Desktop) |
| `stack-fitness-functions-test <file>` | Validate a file's fitness functions against the running server |

### Remote Container Hook Mode

For a containerized server, configure hooks with an HTTPS endpoint, mTLS client credentials, and an optional logical repository override:

```bash
export STACK_FITNESS_FUNCTIONS_ADDR=https://calm-governance.example:7890
export STACK_FITNESS_FUNCTIONS_ALLOW_REMOTE=1
export STACK_FITNESS_FUNCTIONS_CLIENT_CERT=/path/to/client.crt
export STACK_FITNESS_FUNCTIONS_CLIENT_KEY=/path/to/client.key
export STACK_FITNESS_FUNCTIONS_CLIENT_CA=/path/to/ca.crt
export STACK_FITNESS_FUNCTIONS_REPO_NAME=graft
```

When `STACK_FITNESS_FUNCTIONS_ADDR` points at a remote server, `hooks/pre-commit.sh` sends staged content through a temporary content file and uses `STACK_FITNESS_FUNCTIONS_REPO_NAME` or the working-tree basename as the logical `--repo` value.

## Baseline Analysis

Generate a C# baseline from a fresh checkout with:

```bash
go run ./cmd/stack-fitness-functions baseline --repo /path/to/repo --language csharp --output baseline-report.json
```

The Roslyn analyzer is also packageable as a local .NET tool:

```bash
dotnet pack tools/roslyn-analyzer/CalmRoslynAnalyzer.csproj
```

Measured cold start for the Debug Roslyn analyzer on 2026-05-18 was 0.12 seconds for a one-file C# fixture.
