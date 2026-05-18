# Calm PoC

Calm PoC is a local proof-of-concept architecture-as-code system that uses CALM fitness functions to evaluate proposed source changes before they are written or committed.

See [docs/spec/why-and-what.md](docs/spec/why-and-what.md) and [docs/spec/engineering-spec.md](docs/spec/engineering-spec.md) for the product and engineering specification.

## Tool Requirements

- Go 1.22 or newer for `calm-bridge`
- FINOS CALM CLI 1.40.0 via `npm install -g @finos/calm-cli@1.40.0`
- `radon` 6.0.1 on `PATH`, or pass `--radon <path>`, for Python baseline analysis
- .NET 8 SDK for `tools/roslyn-analyzer`; `calm-bridge baseline --language csharp` builds the local analyzer automatically when `--roslyn <path>` is omitted

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
