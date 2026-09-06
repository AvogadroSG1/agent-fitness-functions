# Agent Fitness Functions

Agent Fitness Functions is a local proof-of-concept architecture-as-code system that uses FINOS CALM fitness functions to evaluate proposed source changes before they are written or committed. The source repository, Go module, product, binary, and — since ADR-0009 — the logical governance key are all `agent-fitness-functions`.

See [docs/spec/why-and-what.md](docs/spec/why-and-what.md) and [docs/spec/engineering-spec.md](docs/spec/engineering-spec.md) for the product and engineering specification.

**New to the tool?** The [5-minute quickstart](docs/quickstart-0-to-governed.md) takes a
fresh repo to a governed coding agent with one command
(`agent-fitness-functions client onboard`). The
[onboarding runbook](docs/runbooks/onboard-new-repository.md) is the authoritative
operator reference for both local and production onboarding.

## Tool Requirements

- Go 1.22 or newer for the `agent-fitness-functions` binary
- FINOS CALM CLI 1.40.0 via `npm install -g @finos/calm-cli@1.40.0`
- `radon` 6.0.1 on `PATH`, or pass `--radon <path>`, for Python baseline analysis
- .NET 8 SDK for `tools/roslyn-analyzer`; `agent-fitness-functions baseline --language csharp` builds the local analyzer automatically when `--roslyn <path>` is omitted
- `pyyaml` 6+ for hook violation formatting: `python3 -m pip install -r hooks/requirements.txt`
- Docker with BuildKit for validating the container image; the image packages the Go server, FINOS CALM CLI 1.40.0, Python `radon==6.0.1`, and the self-contained .NET 8 Roslyn analyzer.

## Container Image

The repository includes a multi-stage `Dockerfile` for the containerized agent-fitness-functions service. It builds the Go server, publishes the .NET analyzer, installs FINOS CALM CLI 1.40.0, installs Python plus `radon==6.0.1`, runs as non-root `appuser` uid 1001, and starts with `/app/agent-fitness-functions server start`.

Use a Docker-enabled environment to verify the image contract:

```bash
docker build --build-arg GIT_SHA="$(git rev-parse --short HEAD)" --build-arg BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" -t agent-fitness-functions:local .
```

`docker-compose.yml` provides the local/staging deployment contract. It mounts `./configs`, `./certs`, and `./caller-repos.json` read-only, runs the container as a hardened service, and configures TLS through the `AGENT_FITNESS_FUNCTIONS_TLS_CERT/KEY/CA` environment variables.

`server start` resolves its TLS material from those environment variables (the `--tls-cert/--tls-key/--tls-ca` flags override them when set). All three must be provided together or the server refuses to start; setting only some — or none while expecting HTTPS — is a configuration error rather than a silent plain-HTTP fallback.

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

The agent-fitness-functions server is an enterprise organization-wide governance layer. The containerized service is the **primary production path**. Every governed repository connects to a shared, centrally operated container instance; governance thresholds and enforcement configuration are authoritative only when served from the container.

The local `.calm` mode (described in the CLI tools section below) is a **sandbox environment** for developer iteration and demonstration. It does not substitute for the container layer in any production or CI context.

### Deployment topology

```
┌─────────────────────────────────┐
│  Container (authoritative)      │
│  agent-fitness-functions server start │
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
| `AGENT_FITNESS_FUNCTIONS_ADDR` | Yes | Full HTTPS URL, e.g. `https://calm-governance.example:7890` |
| `AGENT_FITNESS_FUNCTIONS_ALLOW_REMOTE` | Yes (set to `1`) | Opt-in to non-loopback server addresses |
| `AGENT_FITNESS_FUNCTIONS_CLIENT_CERT` | Optional (mTLS) | External PEM-encoded client certificate path |
| `AGENT_FITNESS_FUNCTIONS_CLIENT_KEY` | Optional (mTLS) | External PEM-encoded client private key path |
| `AGENT_FITNESS_FUNCTIONS_CLIENT_CA` | Optional (mTLS) | External PEM-encoded CA bundle for server verification |
| `AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR` | Optional | Managed development certificate root (default: the machine governance root `${XDG_STATE_HOME:-~/.local/state}/agent-fitness-functions/governance/certs`, ADR-0007) |
| `AGENT_FITNESS_FUNCTIONS_REPO_NAME` | Recommended | Logical repository name (overrides working-tree basename) |
| `AGENT_FITNESS_FUNCTIONS_ON_ERROR` | Optional | `block` (default) or `advisory` — whether an infrastructure/setup failure blocks the commit or agent edit. Mirrors the server's `enforcement-on-error`; distinct from a real architecture violation. |

With no explicit client TLS input, the client resolves one immutable managed version
beneath `AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR` or the machine governance root
(`${XDG_STATE_HOME:-~/.local/state}/agent-fitness-functions/governance/certs`, ADR-0007) — the hooks pass
no TLS material at all and leave that resolution to `client validate`. Client TLS
flags or `AGENT_FITNESS_FUNCTIONS_CLIENT_*` variables select external mode and retain
flag-over-environment precedence. The managed selector cannot be combined with an
explicit client TLS input. `client onboard` generates managed dev certs (CN
`dev-hook-pool`) into the shared machine governance root automatically — one dev CA
serves every governed repository on the machine.

Full onboarding with external client TLS validates the certificate/key pair, CA chain,
client-auth profile, and non-empty leaf CN before changing the repository. The external
daemon MUST already be healthy because automatic local daemon startup requires managed
development certificates; onboarding authorizes the verified external leaf CN.

All hooks enforce HTTPS when `AGENT_FITNESS_FUNCTIONS_ALLOW_REMOTE=1` is set. Connections over plain HTTP to a non-loopback address are rejected at the hook layer.

### Setup failures vs. architecture violations

Infrastructure/setup failures (server unreachable, TLS/cert problem, unauthenticated,
unauthorized, repo not configured) are **distinct from** fitness-function blocks. `client
validate` exits `3` with a machine-readable
`{"status":"error","error_kind":...,"message":...,"remediation":...}` object, and the
hooks print a labeled `agent-fitness-functions SETUP problem ... (NOT an architecture
violation)` block with the fix. A real block is a successful check (exit 0, status
`block`). Run `agent-fitness-functions doctor` to diagnose setup failures; the error-kind
table is in the [onboarding runbook](docs/runbooks/onboard-new-repository.md).

## CLI Tools

The `bin/` directory contains helper scripts. Add it to your PATH once after cloning:

```bash
export PATH="$(pwd)/bin:$PATH"
```

Or symlink into `~/.local/bin` for a permanent install:

```bash
ln -sf "$(pwd)/bin"/agent-fitness-functions-* ~/.local/bin/
```

| Command | Purpose |
|---------|---------|
| `agent-fitness-functions client onboard [repo]` | **Single-command 0-to-governed** — dev certs, per-repo config scaffold, caller authorization, hook installation, local daemon auto-start, and a `doctor` gate. Idempotent. Flags: `--repo`, `--enforcement advisory\|block` (default `advisory`), `--addr`. See the [quickstart](docs/quickstart-0-to-governed.md). |
| `agent-fitness-functions doctor` | Ordered ✔/✘/⚠ readiness checks (binary, python3/pyyaml, client cert, CA, server reachability, `/preflight` facts, installed hooks), each with a one-line remediation. Run it anytime to diagnose setup. Flags: `--addr`, `--repo`, `--client-cert/--client-key/--client-ca`. |
| `agent-fitness-functions client install-hooks [repo]` | Install the embedded Git hooks **and** the agent Edit/Write validation hook into a repository, configuring `PreToolUse` hooks across Claude Code (`.claude/settings.json`), OpenAI Codex (`.codex/hooks.json`), and OpenCode (`.opencode/plugins/agent-fitness-functions.js`) with zero manual authoring. See [Onboarding a New Repository](docs/runbooks/onboard-new-repository.md). |
| `agent-fitness-functions client resolve-dev-cert-version` | Print the current validated managed `versions/v-...` path for shell handoff; accepts no arguments. |
| `agent-fitness-functions-serve [--build]` | Start the agent-fitness-functions server container via Docker Compose (Docker Desktop) |
| `agent-fitness-functions-test <file>` | Validate a file's fitness functions against the running server |

### Remote Container Hook Mode

For a containerized server, configure hooks with an HTTPS endpoint, mTLS client credentials, and an optional logical repository override:

```bash
export AGENT_FITNESS_FUNCTIONS_ADDR=https://calm-governance.example:7890
export AGENT_FITNESS_FUNCTIONS_ALLOW_REMOTE=1
export AGENT_FITNESS_FUNCTIONS_CLIENT_CERT=/path/to/client.crt
export AGENT_FITNESS_FUNCTIONS_CLIENT_KEY=/path/to/client.key
export AGENT_FITNESS_FUNCTIONS_CLIENT_CA=/path/to/ca.crt
export AGENT_FITNESS_FUNCTIONS_REPO_NAME=graft
```

When `AGENT_FITNESS_FUNCTIONS_ADDR` points at a remote server, `hooks/pre-commit.sh` sends staged content through a temporary content file and uses `AGENT_FITNESS_FUNCTIONS_REPO_NAME` or the working-tree basename as the logical `--repo` value.

## Baseline Analysis

Generate a C# baseline from a fresh checkout with:

```bash
go run ./cmd/agent-fitness-functions baseline --repo /path/to/repo --language csharp --output baseline-report.json
```

The Roslyn analyzer is also packageable as a local .NET tool:

```bash
dotnet pack tools/roslyn-analyzer/CalmRoslynAnalyzer.csproj
```

Measured cold start for the Debug Roslyn analyzer on 2026-05-18 was 0.12 seconds for a one-file C# fixture.
