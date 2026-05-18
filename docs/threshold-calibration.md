# CALM PoC Threshold Calibration

This document records the proposed governance thresholds derived from the four baseline reports generated on 2026-05-18. It is the HITL review artifact for `patterns/governance.json`.

## Calibration Rule

The governance file is shared across repositories, so the proposed threshold uses the most conservative repository percentile:

- Upper-bound rules use the maximum repository P90.
- Lower-bound rules use the minimum repository P10.
- Dependency Discipline is set to `0` for this baseline because the current C# analyzer cannot reliably resolve every project-local namespace import without project compilation context. This keeps the rule concrete but effectively advisory until the analyzer gains project-reference awareness.
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
| Dependency Discipline | `gte` | 0 | Minimum repository P10 from C# baselines |

## Known Exceptions

The exhaustive exception appendix is [threshold-exceptions.md](threshold-exceptions.md). These existing files/functions SHOULD be treated as baseline exceptions unless a future change worsens them.

Exception counts from the appendix:

| Repository | CC > 9 | Public Methods > 20 | Avg LOC/Public < 0.722 | LDR < 0.255 |
| --- | ---: | ---: | ---: | ---: |
| graft | 49 | 2 | 0 | 0 |
| ringstation | 137 | 36 | 0 | 18 |
| SlackStatus | 2 | 1 | 6 | 6 |
| StackOverflow.Api.V3 | 23 | 31 | 7 | 31 |

### Dependency Discipline

The current C# baseline still contains many `0` values because project-local symbols cannot be fully resolved from a single-file Roslyn analysis. The threshold is concrete at `0` for this task, but this is a no-op rule for valid ratio output. Peter approved this as a temporary PoC threshold on 2026-05-18, and follow-up Bead `calm-poc-oeu` tracks project-aware C# import resolution before Dependency Discipline is made meaningful.

## Human Approval

Status: Approved by Peter on 2026-05-18.

Open approval questions:

Approved decisions:

1. Use shared thresholds across Go, Python, and C# for the PoC, instead of per-language thresholds.
2. Use `dependency-discipline` as an advisory/no-op threshold at `0` until project-aware C# import resolution exists.
3. Use the exhaustive exception appendix and the policy that existing exceptions block only when worsened by a change.
4. Use the rounding policy: upper integer thresholds remain exact, lower-bound ratios round down to three decimals.

Footer: Authored By Peter O'Connor with Assistance from Codex (GPT-5) · 2026-05-18 · Calm-POC threshold calibration
