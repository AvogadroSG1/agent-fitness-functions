---
status: proposed
date: 2026-08-29
scope: repository
authors: "Peter O'Connor with Claude Code assistance"
type: decision
---

# Longitudinal fitness history: local and governance-level validation records

## Metadata

| Field | Value |
|---|---|
| Date | 2026-08-29 |
| Status | Proposed |
| Scope | Repository |
| Accountable owner | Peter O'Connor, repository maintainer |
| Authors | Peter O'Connor with Claude Code assistance |
| Type | Decision (feature worthiness — deliberately does not design the implementation) |

This document records an idea, its refinement, the pressure testing applied to it from several perspectives, and the resulting decision on whether it is worthy of implementation. It is the agreed outcome of that conversation. It does **not** decide schemas, storage engines, endpoints, or file formats — a follow-up MADR must do that before any implementation begins.

---

## 1. The Idea (as proposed)

Fitness functions exist to create an **evolutionary architecture**: standards that let us verify we are moving in the right direction, on a journey that never ends and should only get better on aggregate.

Therefore the system should keep **longitudinal records at two levels**:

- **Local level** — each onboarded (governed) repository keeps a log of its own validation history, so the agents and humans working in it get fast feedback and can track that individual system over time.
- **Governance level** — the server (and the CI/CD path that runs through it) persists and monitors validation history across all governed repositories over time.

Claimed benefits:

1. Enables fitness functions that are about **iterative improvement**, not just point-in-time thresholds.
2. Tracked data over time can inform the human — or a specialized agent — when the coding agent **is not being guided properly**.
3. Helps identify **patterns of divergent architecture** that go against the repository's standards.
4. The accumulated data can be used to **derive new architectural fitness functions**.

The proposal is deliberately a *why*, not a *how*: history at both levels feeds the core promise of fitness functions — get better over time.

---

## 2. Context: what the system actually is today

The pressure test below is grounded in these verifiable facts about the current implementation, not in the aspiration documents:

- **The server keeps no history.** `internal/server/state.go` holds only the *latest* violations per repo/file in an in-memory map, lost on restart. `GET /state` reports current violations, not a record over time.
- **But the documentation already promises one.** `CONTEXT.md` names an "Audit trail — the server logs every validation, so the *absence* of a validation on a commit is itself a signal" as one of the three real enforcement layers. Today that layer is process stdout at best. The claim is currently unbacked.
- **The spec already requires trend measurement.** `docs/spec/why-and-what.md` success criterion 4: "Violation count in a repository trends down across three or more consecutive agent task iterations." Without persisted history this criterion cannot be evaluated at all.
- **The idea has internal lineage.** `docs/thoughts/gradientff.md` (gradient fitness functions) already argues that the most valuable signal is *trajectory* ("the last three edits moved this code toward the duplication rule") and that this requires "storing a little temporal state."
- **Verdicts are pure functions.** `POST /check` computes a `ValidationResult` from exactly (file content, per-repo config, calibrated thresholds). Same inputs, same verdict, every time. The fixture suite (`fixtures/green/`, `fixtures/violations/`) and the red-green demo depend on this determinism.
- **The trust split already has a precedent.** `configs/<repo>/config.json` on the server is the source of truth; a repo-local `.calm/config.json` is a developer sandbox that "has no effect on container governance." Local artifacts are convenience; the server is authority.
- **The wire contract has thin identity.** `ValidationRequest` (`internal/fitness/contract.go`) carries repo, file path, content, and language. No commit SHA, no timestamp, no branch. File identity is path-based, so a rename severs any lineage.
- **All metrics are intra-file by design.** The server never touches a repository on disk; each check analyzes one file's content in isolation.

---

## 3. Refinement: restating the idea in this system's vocabulary

Pressure testing exposed that "keep logs at both levels" is actually **four separable claims**, and they do not stand or fall together:

- **R1 — Governance ledger.** The server persists every `ValidationResult` it issues (append-only), surviving restarts, queryable per repo over time.
- **R2 — Local history.** A governed repository keeps its own record of the verdicts it received, for fast feedback and self-tracking.
- **R3 — Trend-based fitness functions.** New fitness functions whose verdicts depend on history — "this must not get worse," "aggregate violations must trend down."
- **R4 — History as calibration input.** Accumulated data informs humans (or a specialized reviewing agent) about mis-guidance, divergence patterns, and candidate new fitness functions.

Each is pressure tested separately below.

---

## 4. Pressure Testing

### Perspective A — Evolutionary architecture theory

*Does the idea even belong to the discipline it invokes?*

Yes — squarely. The fitness-function literature (Ford/Parsons/Kua, *Building Evolutionary Architectures*) explicitly distinguishes **triggered** fitness functions (run at a gate — what this system does today) from **continual/monitoring** fitness functions (evaluated over time), and treats trend measurement as a first-class use. A fitness-function system that can only answer "is this file acceptable right now?" and never "is this system getting healthier?" implements half of the discipline it is named after.

**But the challenge lands on the phrasing, not the direction:** "get better on aggregate" is a *goal*, not a fitness function. A fitness function must be objective and mechanically evaluable. The honest translations of "better on aggregate" are things like:

- **Ratchet**: no file's metric may exceed its value in a pinned reference measurement (a baseline artifact), even where it is already over the global threshold.
- **Debt monotonicity**: total violations against a pinned baseline may not increase.

Both are legitimate, well-known fitness functions ("no new violations" ratchets are standard practice when onboarding legacy code). Both need a **reference measurement** — which is exactly what the existing `baseline` command produces. The refinement: R3 is not a new kind of magic; it is `baseline` output promoted from one-time calibration input to a versioned, pinned comparison artifact.

*Verdict: the idea is in-tradition; the aggregate-improvement language must be operationalized as ratchets against pinned baselines before it means anything enforceable.*

### Perspective B — The system's own trust model

*Who can write the log, and who is judged by it?*

The local log (R2) is written on a machine where the governed agent — the very actor the fitness functions exist to constrain — has write access. `CONTEXT.md` is blunt that the hook layer is "a shift-left convenience, not a security boundary" and that a developer can delete the hook. A local history file has exactly the same property: **an audit record the subject can edit has zero audit value.**

This does not kill R2; it demotes it. The repository already has the precedent: `.calm/config.json` is useful *and* explicitly powerless over governance. The same split applies cleanly:

- **Local history = feedback cache.** Useful for fast agent/human feedback, trajectory hints (the `gradientff.md` use case), and self-tracking. Never an input to any enforcement decision, never trusted by the server, never a substitute for the governance ledger.
- **Server ledger (R1) = the record.** Written by the authority at the moment it issues the verdict, from data it already holds. Nothing new is trusted.

A useful consequence: the local cache can be *derived* — it is just the client persisting the verdicts the server returned. It needs no independent measurement machinery, so it cannot drift from the server's view except by tampering, which it is by definition not trusted against.

*Verdict: R1 is the trustworthy half; R2 survives only as an explicitly untrusted convenience, mirroring the `.calm/config.json` precedent.*

### Perspective C — Determinism of verdicts

*What does history do to the purity of `POST /check`?*

This is the strongest objection to the idea as originally framed. Today a verdict is a pure function of (content, config, thresholds). That property is load-bearing:

- The fixture suite asserts specific files pass/fail specific functions — meaningless if verdicts depend on hidden server state.
- The red-green demo ("introduce violation → blocked → fix → passes") assumes replayability.
- A developer disputing a blocked commit can reproduce the verdict exactly.
- Two server instances (or a restarted one) agree without state synchronization.

A naive trend-based fitness function — "blocked because this repo's rolling average got worse this week" — destroys all of this: the same file content passes on Monday and fails on Tuesday for reasons invisible in the request. That is not governance; that is weather.

Two design commitments dissolve the objection without giving up R3:

1. **Record, don't consult.** The `/check` path *writes* to the ledger and never *reads* from it. Persistence is observationally invisible to today's verdict semantics.
2. **Pin, don't roll.** Any future history-aware fitness function compares against a **pinned, versioned baseline artifact** — an explicit input, named in config, changed only by deliberate act (exactly how thresholds work now) — never against a rolling log. The verdict becomes a pure function of (content, config, thresholds, **baseline-version**): still deterministic, still replayable, still disputable.

*Verdict: history-dependent verdicts against rolling state are rejected outright; ratchets against pinned baseline artifacts preserve every determinism property the system relies on.*

### Perspective D — Data-model honesty

*Can the data we could actually record support the conclusions we want to draw?*

Two real gaps:

1. **Identity.** `ValidationRequest` has no commit SHA, no timestamp, no branch; file identity is a path. A rename severs lineage; a long-lived series keyed by path quietly measures different code over time. Any ledger needs at least server-side timestamps and the caller CN (already authenticated) on day one, and the contract eventually needs optional commit/branch identity — an additive, backward-compatible change, but a change to the shared contract that must be decided deliberately (the `internal/fitness` package is owned by neither side).
2. **Sampling bias.** Commit-time checks observe only *files being changed*. A repo can "trend better" in the ledger purely because agents happened to touch easy files, while untouched debt sits invisible. Aggregate repo health claims from commit-event data alone are statistically dishonest. The complete measurement already exists: `baseline` scans the whole repository. The honest aggregate signal is a **time series of periodic baseline runs** (a CI cadence job), with the per-check ledger serving a different purpose: it is an *event stream about behavior* — what was attempted, what was blocked, how many attempts a fix took, whether validations are absent (the audit signal `CONTEXT.md` already promises).

This perspective also produced the idea's most elegant defense: a time series of intra-file metrics yields **repo-level and org-level insight without breaking the intra-file compute model**. The server still never touches a repository on disk; time, not cross-file analysis, provides the wider view. The idea is more architecturally compatible with this system than it first appears.

*Verdict: benefits 2 and 3 survive but split across two instruments — the ledger measures behavior, baseline series measure health. Conflating them was the refinement's most important correction.*

### Perspective E — Goodhart's law and agent psychology

*What happens when the measured start seeing the measurements?*

"When a measure becomes a target, it ceases to be a good measure." Three concrete risks:

- **Metric gaming.** Intra-file metrics are gameable (split a function to duck complexity; scatter logic to lower density). Trend targets amplify the incentive: an agent told "the aggregate must improve" can churn cosmetic refactors that improve numbers and nothing else. Mitigation: trend data informs *humans and calibration* (R4) long before it gates anything (R3); ratchets, when they come, are per-file floors ("don't get worse"), which are much harder to game than aggregate targets ("look better").
- **Context pollution.** Feeding raw long-term logs into an agent's context is expensive and distracting. The local cache must surface *summaries and deltas* (the `gradientff.md` shape: score before, score after, trend), not history dumps.
- **Self-fulfilling misguidance.** If the "are agents being guided properly?" analysis (benefit 2) is itself performed by an agent reading tamperable local logs, the loop is circular. That analysis must read the governance ledger, not local caches — which Perspective B already requires.

*Verdict: real risks, all addressable by ordering (observe → inform → enforce) and by keeping enforcement per-file and pinned. None invalidates the idea.*

### Perspective F — Operational cost

*What does this do to a deliberately thin deployment?*

Today the container needs mounted configs and nothing else. R1 adds the first real datastore: a volume, retention policy, growth management, backup/restore, and a migration story. That is a genuine step in operational weight and must not be waved away — but an append-only verdict ledger is the *smallest possible* stateful addition (no read path on the hot loop, no consistency requirements across instances beyond append), and it is the price of the audit-trail promise the documentation has already made. A system that markets "the server logs every validation" as an enforcement layer either pays this cost or retracts the claim.

R2's cost is a placement decision: committed history pollutes diffs and invites merge conflicts; a gitignored location is per-machine and lossy — acceptable *because* Perspective B already ruled the local record untrusted and derivable. (The forge addendum in ADR-0006 accepted the identical per-machine tradeoff for `.claude/settings.local.json`.)

*Verdict: costs are real, bounded, and in R1's case already owed.*

### Perspective G — Scope discipline

*Is this a PoC feature or scope creep?*

`why-and-what.md` says this PoC "does not ship a production-ready tool." A full observability stack is out. But three paper commitments already presuppose minimal history: the audit-trail enforcement claim (`CONTEXT.md`), success criterion 4 (unmeasurable today), and the gradient-fitness-function direction (`docs/thoughts/gradientff.md`). R1 in minimal form is therefore *closing a gap between what is written and what is built*, not opening new scope. R3 in enforcement form, by contrast, genuinely is new scope and belongs after the PoC has data to justify it.

*Verdict: minimal R1 is owed; R3 enforcement is post-PoC.*

---

## 5. What survives, what is rejected

| Original claim | Outcome after pressure testing |
|---|---|
| Local per-system history for fast feedback and self-tracking | **Survives, demoted** — an untrusted, derivable feedback cache of server verdicts; summaries/deltas for agents, never raw dumps; never an enforcement input (mirrors `.calm/config.json` precedent) |
| Governance-level persistence and monitoring | **Survives, strengthened** — the server ledger is the trustworthy record and makes the already-documented audit-trail claim true; append-only, record-don't-consult |
| Fitness functions about iterative improvement | **Survives, reshaped and deferred** — only as ratchets against pinned, versioned baseline artifacts (deterministic verdicts preserved); never against rolling logs; enforcement waits for observed data |
| Data over time reveals mis-guided agents | **Survives** — but the analysis must read the governance ledger (behavior stream), not local caches, and is an inform-humans capability before it is any agent's trigger |
| Identify divergent-architecture patterns | **Survives, split** — repo-health trends come from a periodic `baseline` time series (complete measurement); the per-check ledger contributes the behavioral/audit dimension. Commit-event data alone is a biased sample and must not be presented as repo health |
| Derive new fitness functions from data | **Survives as a human calibration activity** — the ledger and baseline series feed the existing threshold-calibration workflow; nothing is auto-derived |
| History-dependent verdicts from rolling state | **Rejected** — same content must always yield the same verdict given the same explicit inputs |
| Local history as any form of record or audit | **Rejected** — a log the governed agent can edit has no audit value |

---

## 6. Decision

**The feature is worthy of implementation — in its refined form, in this order, and not before a design MADR.**

The idea passed its strongest tests: it is orthodox evolutionary-architecture practice, it extends rather than violates the intra-file compute model (time supplies the wider view, not cross-file analysis), it fits the existing local-convenience/server-authority trust split without inventing a new one, and part of it is already promised by this repository's own documentation. The pressure test materially reshaped it — enforcement via rolling history is out, local logs are demoted to caches, and repo-health claims move to baseline time series — but the core ("history at both levels feeds getting better over time") stands.

Adoption is sequenced so that each stage must earn the next:

1. **Governance ledger (R1)** — server persists every verdict it issues; append-only; the `/check` verdict path never reads it. Closes the audit-trail gap and makes success criterion 4 measurable. First, because everything downstream consumes it.
2. **Local feedback cache (R2)** — client persists received verdicts locally (gitignored), surfacing summaries/deltas to agents and humans. Explicitly untrusted.
3. **Baseline time series** — periodic full-repo `baseline` runs recorded at the governance level; the honest repo-health trend signal.
4. **Calibration from data (R4)** — humans (optionally assisted by a reviewing agent reading the *ledger*) use the accumulated record to spot divergence and mis-guidance and to propose new fitness functions through the existing calibration workflow.
5. **Ratchet fitness functions (R3)** — only after stages 1–4 have produced data justifying them, and only as deterministic comparisons against pinned, versioned baseline artifacts.

**Guard rails carried into any design:** record-don't-consult on the hot path; pin-don't-roll for any history-aware enforcement; local is never authority; aggregate claims come only from complete measurements; contract changes (timestamps, commit identity) are additive and deliberate.

**Exit condition:** if, after the ledger and baseline series exist, they are not actually consulted — no calibration decision, no mis-guidance finding, no success-criterion measurement cites them within a reasonable evaluation window — stage 5 must not proceed and the storage cost of stages 1–3 should be revisited rather than defended.

Per the scope of this conversation, this document is the sole deliverable: no implementation, schema, endpoint, or storage decision is made here. A follow-up MADR must design R1 before any code lands.

## 30-Second Summary

Keeping longitudinal fitness history locally and at the governance level is a worthy feature: it is textbook evolutionary architecture, it is partly already promised by this repo's docs (audit trail, trend success criterion), and time series of intra-file metrics give system-level insight without breaking the intra-file model. Pressure testing forced three corrections: local logs are untrusted feedback caches, never records; verdicts must stay deterministic, so improvement-oriented fitness functions compare against pinned baseline artifacts, never rolling logs; and repo-health trends come from periodic full-repo baselines, because commit-time events are a biased sample. Build the server-side verdict ledger first; let data earn each later stage.

## Supporting Evidence

- `CONTEXT.md` — audit-trail enforcement claim; `.calm/config.json` vs `configs/` authority precedent; intra-file design rationale
- `docs/spec/why-and-what.md` — success criterion 4 (violation trend across iterations)
- `docs/thoughts/gradientff.md` — gradient fitness functions; trajectory over verdicts; "storing a little temporal state"
- `internal/server/state.go` — current in-memory, latest-only state
- `internal/fitness/contract.go` — wire contract identity fields
- `docs/threshold-calibration.md` — the existing human calibration workflow stage 4 feeds into
- ADR-0006 — precedent for the per-machine/gitignored tradeoff (`.claude/settings.local.json`)
- Ford, Parsons, Kua — *Building Evolutionary Architectures* (triggered vs. continual fitness functions)

*Authored By Peter O'Connor with Claude Code assistance · 2026-08-29 · Longitudinal fitness history — worthiness decision*
