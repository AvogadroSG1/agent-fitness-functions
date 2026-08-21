# Joint Brief Direct-Root Certificate Publication

## Joint Escalation Brief: Distinguish Certificate No-Op from Storage Migration

Agents: GPT-5.6-Sol specification reviewer and GPT-5.6-Sol standards reviewer · Triggered: 2026-08-20 · Trigger: reviewers disagreed on whether a valid direct-root target could be a no-op

## 1. The Outcome We Both Want

Existing valid development certificates retain their trust identity while every managed TLS reader receives one coherent version through the required atomic publication model.

## 2. What We Agree On

- A valid published target MUST be a complete filesystem no-op.
- Every managed TLS reader MUST pin one version below `<managed-root>/current` for a complete load operation.
- Existing direct-root certificate bytes MUST NOT rotate merely to adopt the versioned storage model.
- The MADR MUST distinguish identity preservation from publication-structure migration.

## 3. What Horizon Each Side Is Optimizing For

- Specification reviewer: preserve valid certificate bytes and the approved no-rotation behavior.
- Standards reviewer: provide one implementable steady state in which all managed readers can resolve `current` without ambiguity.

## 4. The Options

**Option A: Treat every valid target as a complete no-op.** The standards reviewer's steelman of the specification position: **byte-for-byte no-op** can mean preserving the five certificate files while independently publishing structural metadata. This minimizes trust churn, but the word **no-op** leaves filesystem mutation ambiguous.

**Option B: Distinguish published and legacy direct-root targets.** The specification reviewer's steelman of the standards position: an already published valid target is a complete no-op; a legacy direct-root target copies the same five bytes into a validated version directory and atomically publishes `current`. This adds one state and tests, but removes the implementation contradiction without rotating certificates.

## 5. The Exact Decision Needed

Should only a valid published target be a complete no-op, while a valid legacy direct-root target preserves certificate bytes and publishes the versioned structure?

## 6. Cost of Delay

Without an explicit distinction, implementation can either violate the managed-reader contract or leave valid existing installations unusable after readers switch to `current`.

## 7. Status

- Specification reviewer: Resolved
- Standards reviewer: Resolved

## 8. Escalation Decision

- Arbitration Decision: Not required
- Arbitration Reason: Both reviewers endorsed Option B in communication round 1.

---

## Communication Ledger

- GPT-5.6-Sol specification reviewer - Resolution 4 The Options - Endorsed separate published-target and legacy direct-root target states because the distinction preserves certificate fingerprints while completing storage migration.
- GPT-5.6-Sol standards reviewer - Resolution 4 The Options - Confirmed the distinction resolves the no-op ambiguity and requires prose, state-diagram, and confirmation-test updates.

## Resolved Decision

A valid published target is a complete filesystem no-op. A valid legacy direct-root target MUST preserve all five certificate bytes, validate and publish them through a version directory and atomic `current` replacement, and remain untouched until publication succeeds.

*Authored By Peter O'Connor with Assistance from OpenCode (openai/gpt-5.6-sol) · 2026-08-20 · calm-poc-q8d.1 certificate publication joint framing*
