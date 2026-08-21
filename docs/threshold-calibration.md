# agent-fitness-functions Threshold Calibration

This document records the proposed governance thresholds derived from the four baseline reports generated on 2026-05-18. It is the HITL review artifact for `patterns/governance.json`.

## Calibration Rule

The governance file is shared across repositories, so the proposed threshold uses the most conservative repository percentile:

- Upper-bound rules use the maximum repository P90.
- Lower-bound rules use the minimum repository P10.
- Dependency Discipline now follows the Step 5.3 PoC threshold, `0.8`, so files with excessive unused imports are actionable. C# project-aware import resolution remains tracked as follow-up work before treating C# DDC results as final.
- Ratio thresholds round down to three decimal places so the concrete governance values are stable and do not become stricter than the measured baseline tail.

This deliberately refines the Step 0 shorthand of setting thresholds at the 90th percentile: ceiling metrics violate when they rise above the upper tail, while floor metrics violate when they fall below the lower tail.

## Repository Percentiles

| Repository | Language | CC P90 | CC Max | Interface P90 | Interface Max | Impl Depth P10 | LDR P10 | DDC P10 |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| graft | Go | 9 | 24 | 14 | 33 | 5.00 | 0.529 | 1.000 |
| ringstation | Python | 6 | 36 | 18 | 82 | 5.40 | 0.348 | 0.667 |
| SlackStatus | C# | 3 | 12 | 10 | 36 | 0.722 | 0.286 | 0.000 |
| StackOverflow.Api.V3 | C# | 2 | 28 | 20 | 137 | 1.17 | 0.255 | 0.000 |

## Proposed Thresholds

| Fitness Function | Operator | Threshold | Source |
| --- | --- | ---: | --- |
| Cyclomatic Complexity | `lte` | 9 | Maximum repository P90 from graft |
| Interface Width | `lte` | 20 | Maximum repository P90 from StackOverflow.Api.V3 |
| Implementation Depth | `gte` | 0.722 | Minimum repository P10 from SlackStatus |
| Logic Density | `gte` | 0.255 | Minimum repository P10 from StackOverflow.Api.V3 |
| Dependency Discipline | `gte` | 0.8 | Step 5.3 PoC threshold |

## Known Exceptions

The exhaustive exception appendix is [threshold-exceptions.md](threshold-exceptions.md). These existing files/functions SHOULD be treated as baseline exceptions unless a future change worsens them.

Exception counts from the appendix:

| Repository | CC > 9 | Public Methods > 20 | Avg LOC/Public < 0.722 | LDR < 0.255 | DDC < 0.8 |
| --- | ---: | ---: | ---: | ---: | ---: |
| graft | 49 | 2 | 0 | 0 | 0 |
| ringstation | 137 | 36 | 0 | 18 | 15 |
| SlackStatus | 2 | 1 | 6 | 6 | 45 |
| StackOverflow.Api.V3 | 23 | 31 | 7 | 31 | 205 |

### Dependency Discipline

The current C# baseline still contains many `0` values because project-local symbols cannot be fully resolved from a single-file Roslyn analysis. `patterns/governance.json` now uses the Step 5.3 PoC threshold, `0.8`, so DDC can flag unused-import slop in Go/Python and any C# files the current analyzer can resolve. Follow-up Bead `calm-poc-oeu` tracks project-aware C# import resolution before Dependency Discipline is treated as final for C# repositories.

## Onboarding a New Repository

To scaffold a per-repo governance config from a fresh baseline, run `baseline` with
`--emit-config`:

```bash
agent-fitness-functions baseline \
  --repo /path/to/repo --language go \
  --output baseline-report.json \
  --emit-config configs/<repo>/config.json --name <repo>
```

Alongside the usual baseline report this writes a ready-to-use
`configs/<repo>/config.json` (all five fitness functions enabled) and prints:

- an **enforcement-mode recommendation** — `block` when zero files/functions violate the
  current global thresholds, otherwise `advisory` — with the violation count so the
  reviewer sees why; and
- a **threshold-delta report** listing, per fitness function, the repository's relevant
  percentile (P90 for ceiling metrics, P10 for floor metrics), the current global
  embedded threshold, the delta, and whether the repository's tail would need a looser
  threshold.

The threshold-delta report is a diagnostic only. Thresholds are **global** and compiled
into the binary (`patterns/governance.json`); a per-repo `config.json` can toggle
fitness functions and the enforcement mode but **cannot change any threshold**. If the
report flags "needs looser? yes" for a function, either accept advisory mode for that
repository or recalibrate the global threshold by editing `patterns/governance.json`
(per the Calibration Rule above) and rebuilding the binary. Per-repo thresholds are a
separate future epic.

## Human Approval

Status: Approved by Peter on 2026-05-18.

Approved decisions:

1. Use shared thresholds across Go, Python, and C# for the PoC, instead of per-language thresholds.
2. Use `dependency-discipline` at the Step 5.3 threshold (`0.8`) while tracking project-aware C# import resolution in `calm-poc-oeu`.
3. Use the exhaustive exception appendix and the policy that existing exceptions block only when worsened by a change.
4. Use the rounding policy: upper integer thresholds remain exact, lower-bound ratios round down to three decimals.

Footer: Authored By Peter O'Connor with Assistance from Codex (GPT-5) · 2026-05-18 · Calm-POC threshold calibration
