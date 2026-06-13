# Handoff: Observability Fitness Functions

## Session Summary

Grill-with-docs session designing **observability fitness functions** for calm-poc. The framing: a good engineer builds an automobile with clear metrics from the computer so a mechanic (human or AI) can diagnose problems. These fitness functions guard that commitment at commit time.

## Core Framing Decision

**Option B wins over Option A.** Instrumentation *correctness* over instrumentation *presence*. Presence checks are superficially gameable (no-op spans pass). Correctness checks guard what the mechanic actually needs.

---

## Decisions Made

### "Provable" splits into three fitness functions

**1. Surfacability**
Caught errors must be surfaced through at least one durable signal — a structured log at ERROR level, a span with error status, or a metric increment. A caught error with no downstream signal is a violation.

Canonical smell:
```go
span.RecordError(err)
// missing: span.SetStatus(codes.Error, err.Error())
return nil  // error swallowed — caller sees success
```

**2. Cardinality-Constantability**
Metric label/tag values must be constants or enums at the call site — not variables derived from runtime input. Unbounded cardinality (user IDs, file paths, error message strings as tag values) corrupts metrics backends.

Canonical smell:
```go
metric.WithLabelValues(userID)       // violation: unbounded variable
metric.WithLabelValues("cache_hit")  // correct: constant
```

**3. Nomenclature-ability**
Metric names must match the agreed naming convention: `<service>.<component>.<operation>.<unit>`. Detectable as a string literal regex check at the call site. Enforces that the engineer thinks about meaning at declaration time.

---

### Trace Connectivity ("Provable Traces")

A trace is provably connected IFF it incorporates the union of all spans within its **trace scope**, contains no orphaned spans, and contains no spans from outside its trace scope.

**Trace scope** is the canonical term for the unit that owns a trace root. Examples:
- An HTTP request handler is a trace scope
- A background worker is a trace scope

A goroutine or async operation that crosses a trace scope boundary MUST start a new root span — it must NOT inherit the parent's context.

Canonical violation:
```go
func HandleRequest(ctx context.Context, ...) {
    go backgroundWorker(ctx)  // violation: background spans bleed into request trace
}
```

**Scope of "trace scope":**
- For now: a CALM Node (single microservice)
- Future: modules within a monorepo (not a monolith) — not in scope yet
- Within a node, trace scopes are logical operations: request/response cycles, background services, batch jobs

---

## Fitness Functions Identified This Session

| Name | Category | Detection | Status |
|---|---|---|---|
| Surfacability | Observability | AST (error handling + span API) | To be filed |
| Cardinality-Constantability | Observability | AST (label value is variable vs constant) | To be filed |
| Nomenclature-ability | Observability | AST (string literal regex) | To be filed |
| Trace Scope Integrity | Observability | AST (context propagation) | To be filed |

---

## Previously Filed Issues (from prior sessions)

### Structural Guardrails
| ID | Title |
|----|-------|
| calm-poc-8c8 | Coupling Direction Enforcement |
| calm-poc-8g5 | Responsibility Drift Detection |
| calm-poc-8jp | Change Propagation Risk |
| calm-poc-54i | Cohesion Decay Detection |

### Intent Preservation
| ID | Title |
|----|-------|
| calm-poc-292 | Contract Stability Gate |
| calm-poc-nt5 | Boundary Crossing Density |
| calm-poc-l53 | Naming Coherence / Domain Vocabulary |
| calm-poc-3sz | Symmetry Enforcement / Semantic Duplication |

### Temporal / Evolutionary
| ID | Title |
|----|-------|
| calm-poc-jhj | Churn-Coupling Correlation |
| calm-poc-0k9 | Accretion Rate / Interior Monolith |
| calm-poc-bxd | Decision Decay / ADR Drift |
| calm-poc-73s | Architectural Staleness / Propagation Lag |

### Agent-Specific / Pattern Contracts
| ID | Title |
|----|-------|
| calm-poc-9mp | Novel Dependency Justification Gate |
| calm-poc-99m | Invariant Bypass Detection (Pattern Circumvention) |
| calm-poc-e9n | Pattern Contract Category: Consistency Patterns |

---

## Next Session Focus

1. **File beads issues** for the 4 new observability fitness functions above
2. **Continue the observability grill** — "usable," "just enough," and "correlated" properties not yet fully explored
3. **Prioritization** — which of the now ~19 ideas to design/implement first
4. **CALM model extensions** — what new first-class concepts the CALM spec needs (trace scope declaration, vocabulary registries, pattern contracts)
5. **Detection feasibility** — AST-only vs. requiring runtime/git-history data

---

*Authored By Peter O'Connor with Assistance from Claude Code (claude-sonnet-4-6) · 2026-06-07 · Observability Fitness Functions Grill Session*
