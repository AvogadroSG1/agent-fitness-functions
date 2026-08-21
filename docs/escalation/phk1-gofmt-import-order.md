# Joint Brief PHK.1 Gofmt Import Order

## Joint Escalation Brief: Authorize One Mechanical Import Reorder

Agents: GPT-5.6-Sol specification reviewer and GPT-5.6-Sol standards reviewer · Triggered: 2026-08-21 · Trigger: reviewer disagreement between byte-exact migration scope and mandatory Go formatting

## 1. The Outcome We Both Want

Commit the canonical module migration with auditable scope while leaving every changed Go file in standard `gofmt` form.

## 2. What We Agree On

- The accepted migration authorizes exact module-literal substitutions and four exact documentation lines.
- Changing import casing makes `internal/server/server_test.go` fail `gofmt -d` unless one import line is reordered.
- `gofmt -d` reports no other formatting consequence across the changed Go files.
- The reorder changes no runtime or test semantics.
- Any additional byte-level scope MUST be explicit and verifier-enforced.

## 3. What Horizon Each Side Is Optimizing For

- Specification reviewer: preserve byte-exact compliance and prevent formatting tools from silently expanding migration authority.
- Standards reviewer: preserve mandatory Go formatting so the committed source is canonical and stable under standard tooling.

## 4. The Options

**Option A: Preserve the literal-only diff.** The standards reviewer's steelman of the specification position: keeping the exact substitution makes the migration maximally auditable and prevents an apparently mechanical tool from authorizing unrelated drift. The cost is a deliberate formatting exception that standard tooling will later modify.

**Option B: Authorize one exact mechanical reorder.** The specification reviewer's steelman of the standards position: apply only the `gofmt`-required import reorder in `internal/server/server_test.go`, record it as a mechanical hunk, and encode an exact verifier projection. The cost is one additional authorized hunk and verifier maintenance; semantic scope remains unchanged.

## 5. The Exact Decision Needed

Should PHK.1 preserve the 48-hunk literal/semantic contract and accept a formatting exception, or authorize exactly one `gofmt` import reorder for a 49-hunk mechanical contract?

## 6. Cost of Delay

Without a decision, the module migration cannot pass both specification and formatting review, and the dependent wire-contract and product-rename work remain blocked.

## 7. Status

- Specification reviewer: Resolved
- Standards reviewer: Escalate

## 8. Escalation Decision

- Arbitration Decision: Option B - authorize exactly one mechanical import reorder in `internal/server/server_test.go`, producing a 49-hunk contract.
- Arbitration Reason: The exact projection preserves byte-level auditability while canonical `gofmt` output avoids immediate tooling drift. The verifier and both reviews MUST prove that no additional formatting, runtime, or test-semantic change enters scope.

---

## Communication Ledger

- GPT-5.6-Sol specification reviewer - Resolution 4 The Options - Endorsed Option B if the exact reorder, verifier projection, and review evidence are added.
- GPT-5.6-Sol standards reviewer - Escalation 4 The Options - Confirmed Option B would satisfy formatting but requested arbitration because it expands accepted byte-level scope.
- GPT-5.6 Terra High arbitration agent - Decision 8 Escalation Decision - Selected Option B with exact verifier, `gofmt -d`, specification-review, and standards-review conditions.

*Authored By Peter O'Connor with Assistance from OpenCode (openai/gpt-5.6-sol) · 2026-08-21 · calm-poc-phk.1 gofmt import-order joint framing*
