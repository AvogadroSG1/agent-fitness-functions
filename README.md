# Calm PoC

Calm PoC is a local proof-of-concept architecture-as-code system that uses CALM fitness functions to evaluate proposed source changes before they are written or committed.

See [docs/spec/why-and-what.md](docs/spec/why-and-what.md) and [docs/spec/engineering-spec.md](docs/spec/engineering-spec.md) for the product and engineering specification.

## Tool Requirements

- Go 1.22 or newer for `calm-bridge`
- `radon` 6.0.1 on `PATH`, or pass `--radon <path>`, for Python baseline analysis
- .NET 8 SDK for `tools/roslyn-analyzer`
