# Technical Specification Evaluation Update

Source specification: `/Users/poconnor/peter_code/calm-poc/docs/superpowers/specs/2026-05-29-calm-bridge-container-design.md`
Parent issue: `calm-poc-04s`
Review lanes:
- `calm-poc-3c5` (`/cloud:docker`)
- `calm-poc-deg` (`/golang:golang-dependency-management`)
- `calm-poc-4g4` (`/golang:golang-design-patterns`)
- `calm-poc-lce` (`/golang:golang-safety`)
- `calm-poc-5nk` (`/golang:golang-security`)

## Consolidated Findings

### MUST Fix
1. **Fix Dockerfile execution correctness before rollout.**
- `COPY --chown=appuser:appuser` appears before user creation (spec lines 168-170), and `ENTRYPOINT ["calm-bridge", "serve"]` may not resolve `/app/calm-bridge` without `WORKDIR` or absolute path (line 176).

2. **Eliminate silent unknown-repo fallback behavior.**
- Current behavior falls back to `defaultConfig()` for missing repo config (line 125, line 52).
- This allows typo-driven or spoofed policy routing and hides boundary failures.

3. **Specify and enforce config-store concurrency safety.**
- Hot-reload + `/configs` exposure requires explicit thread-safe snapshot semantics to avoid `concurrent map iteration and map write` panic risks (lines 45-53, 127-136, 274).

4. **Define lifecycle ownership for config watcher/store.**
- Spec adds watcher/store but omits explicit start/stop/cancel/close behavior (lines 45-50, 274-275).
- Must define constructor + shutdown contract to avoid leaked goroutines/file descriptors.

5. **Fail closed on config subsystem startup failure.**
- If initial config load or watcher startup fails (missing/unreadable mount), service must not quietly operate with implicit defaults (lines 54, 125, 227-229).

6. **Add mandatory service authentication/authorization.**
- `/check`, `/state`, `/configs` are exposed with no auth model specified (lines 91, 117-147, 225).

7. **Bind caller identity to repo authorization.**
- Request-controlled `repo` currently selects policy (lines 37, 56-69, 109, 120).
- Must enforce server-side allowlisting by authenticated caller identity.

8. **Require transport security for governance traffic.**
- Examples use cleartext HTTP (lines 174, 231, 249-250).
- Must require TLS in transit (prefer mTLS) or authenticated trusted proxy termination.

9. **Define explicit Go dependency governance for new modules.**
- `fsnotify`-driven design (line 45, line 274) introduces dependency change without explicit `go.mod`/`go.sum` policy, verification gate, or pinning workflow.

### SHOULD Fix
1. **Document abuse controls for `/check`.**
- Define max request size, per-client/per-repo rate limits, concurrency caps, analyzer timeouts, bounded worker pools.

2. **Narrow `/configs` exposure.**
- Make admin-only (or disabled in production), add sanitized/snapshot contract fields (`version`, `loaded_at`).

3. **Define robust hot-reload semantics.**
- Specify atomic-write expectations, debounce/retry behavior, and event normalization for rename/write storms.

4. **Clarify deployment target semantics for resource limits.**
- `deploy.resources` behavior differs across Compose/Swarm; specify enforceable runtime strategy.

5. **Improve multi-arch portability strategy.**
- Current build is x64-specific (`GOARCH=amd64`, `linux-x64`). Define supported platform matrix and buildx/RID strategy.

6. **Align container default config path with deployment reality.**
- Default `./configs` conflicts with mounted `/app/configs` model; define container-safe default.

7. **Add vulnerability and upgrade gates across Go/Python/.NET + image.**
- Monthly cadence alone is insufficient; define CI gates and failure thresholds.

8. **Harden Python dependency reproducibility.**
- `radon==6.0.1` pins top-level only; add transitive lock + hash strategy.

9. **Define ownership/SLA for OS package patching.**
- Apt-installed runtime packages need explicit operational ownership and CVE response expectations.

10. **Validate and normalize repo-name inputs.**
- Reject empty/invalid identifiers and normalize casing/charset.

11. **Define analyzer sandbox boundaries.**
- Add execution isolation requirements (egress policy, temp workspace isolation, process limits).

### MAY Fix
1. **Add stronger runtime hardening defaults in deployment examples.**
- `read_only`, `tmpfs`, `cap_drop: [ALL]`, `no-new-privileges`, tighter seccomp/apparmor guidance.

2. **Publish SBOM/dependency inventory artifacts per build.**
- Improves auditability and incident response.

3. **Use typed boundary values and option-based constructors as system grows.**
- Improves maintainability and reduces stringly-typed drift.

4. **Add config integrity checks for mounted governance files.**
- Optional checksum/signature + startup allowlist verification.

5. **Define deterministic analyzer-failure mapping behavior.**
- Explicit timeout/error translation contract for predictable enforcement behavior.

## Conflicts and Resolutions

### Conflict 1: Unknown repo behavior
- Option A: Keep current fallback to `defaultConfig()`.
- Option B: Reject unknown repo with explicit error.
- Option C: Allow fallback only for pre-declared onboarding window.

**Recommendation:** Option B. Reject unknown repo names by default and require explicit registration. This minimizes spoofing and policy ambiguity.

### Conflict 2: `/configs` operability vs exposure
- Option A: Keep endpoint broadly accessible for troubleshooting.
- Option B: Keep endpoint but restrict to authenticated admin/operator identities.
- Option C: Disable endpoint in production and expose only via internal diagnostics path.

**Recommendation:** Option B now, with optional Option C in hardened environments. Preserves operability while reducing reconnaissance risk.

### Conflict 3: Behavior when repo config is deleted/invalid at runtime
- Option A: Fallback to `defaultConfig()`.
- Option B: Keep last-known-good forever.
- Option C: Mark repo config invalid and fail that repo’s checks explicitly until repaired.

**Recommendation:** Option C. Never silently switch policy; return explicit error/block for affected repo while keeping unaffected repos healthy.

### Conflict 4: Compose portability vs strict resource governance
- Option A: Keep current `deploy.resources` example.
- Option B: Declare Swarm/Kubernetes as required runtime.
- Option C: Provide runtime-specific limit examples (Compose + Swarm/K8s) and enforce one in production.

**Recommendation:** Option C. Avoid ambiguous enforcement by publishing explicit target-runtime patterns.

## Recommended Decision
Adopt all MUST fixes before implementation proceeds. Track SHOULD fixes in the same delivery wave unless consciously deferred with a written rationale. MAY fixes can be batched into hardening follow-ups.

## Output Status
- Beads child reviews recorded: complete
- Consolidated report written: complete
- Markdown update file written: complete (`.update.md`)
