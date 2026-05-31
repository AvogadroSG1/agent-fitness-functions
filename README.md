# Calm PoC

Calm PoC is a local proof-of-concept architecture-as-code system that uses CALM fitness functions to evaluate proposed source changes before they are written or committed.

See [docs/spec/why-and-what.md](docs/spec/why-and-what.md) and [docs/spec/engineering-spec.md](docs/spec/engineering-spec.md) for the product and engineering specification.

## Tool Requirements

- Go 1.22 or newer for `calm-bridge`
- FINOS CALM CLI 1.40.0 via `npm install -g @finos/calm-cli@1.40.0`
- `radon` 6.0.1 on `PATH`, or pass `--radon <path>`, for Python baseline analysis
- .NET 8 SDK for `tools/roslyn-analyzer`; `calm-bridge baseline --language csharp` builds the local analyzer automatically when `--roslyn <path>` is omitted
- `pyyaml` 6+ for hook violation formatting: `python3 -m pip install -r hooks/requirements.txt`
- Docker with BuildKit for validating the container image; the image packages the Go bridge, Python `radon==6.0.1`, and the self-contained .NET 8 Roslyn analyzer.

## Container Image

The repository includes a multi-stage `Dockerfile` for the containerized `calm-bridge` service. It builds the Go daemon, publishes the .NET analyzer, installs Python plus `radon==6.0.1`, runs as non-root `appuser` uid 1001, and starts with `/app/calm-bridge serve`.

Use a Docker-enabled environment to verify the image contract:

```bash
docker build --build-arg GIT_SHA="$(git rev-parse --short HEAD)" --build-arg BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" -t calm-bridge:local .
```

## CLI Tools

The `bin/` directory contains helper scripts. Add it to your PATH once after cloning:

```bash
export PATH="$(pwd)/bin:$PATH"
```

Or symlink into `~/.local/bin` for a permanent install:

```bash
ln -sf "$(pwd)/bin"/calm-* ~/.local/bin/
```

| Command | Purpose |
|---------|---------|
| `calm-install-hooks [repo]` | Install the CALM pre-commit hook into a git repository |
| `calm-serve [--no-build]` | Build and start the CALM bridge daemon on loopback |
| `calm-test <file>` | Check a file's fitness functions against the running bridge |

## Baseline Analysis

Generate a C# baseline from a fresh checkout with:

```bash
go run ./cmd/calm-bridge baseline --repo /path/to/repo --language csharp --output baseline-report.json
```

The Roslyn analyzer is also packageable as a local .NET tool:

```bash
dotnet pack tools/roslyn-analyzer/CalmRoslynAnalyzer.csproj
```

Measured cold start for the Debug Roslyn analyzer on 2026-05-18 was 0.12 seconds for a one-file C# fixture.
