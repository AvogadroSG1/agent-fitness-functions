package renamecheck

import (
	"testing"
)

// calm-poc-q8d.8: the rename-phase verifier must actually run its checks
// against this repository and report the one known, deliberately unresolved
// gap (.beads/issues.jsonl, a protected database file outside the RED
// test's docs/adr + LEGACY_REFERENCES.md exclusion set) rather than a false
// pass, while every other rename-phase check is green.
func TestRunRenamePhaseAgainstRepo(t *testing.T) {
	report := RunRenamePhase("../..")
	if report.Mode != ModeRenamePhase {
		t.Fatalf("report.Mode = %q, want %q", report.Mode, ModeRenamePhase)
	}
	if len(report.Checks) == 0 {
		t.Fatal("RunRenamePhase produced no checks")
	}

	results := make(map[string]CheckResult, len(report.Checks))
	for _, c := range report.Checks {
		results[c.Name] = c
	}

	wantPass := []string{
		"active-surface-product-name",
		"governance-json-projection",
		"requirements-lock-line-one-only-diff",
		"marker-product-prefix-correctness",
		"protected-path-exactness",
	}
	for _, name := range wantPass {
		c, ok := results[name]
		if !ok {
			t.Errorf("missing check %q", name)
			continue
		}
		if c.Status != StatusPass {
			t.Errorf("check %q = %s: %s", name, c.Status, c.Detail)
		}
	}

	// The env-prefix exclusion set mirrors rename_surface_test.go exactly:
	// immutable records (accepted ADRs, legacy evidence, and .beads/ tracker
	// history) may retain the predecessor prefix; the active surface may not.
	envCheck, ok := results["active-surface-env-prefix"]
	if !ok {
		t.Fatal("missing check \"active-surface-env-prefix\"")
	}
	if envCheck.Status != StatusPass {
		t.Errorf("active-surface-env-prefix = %s, want PASS: %s", envCheck.Status, envCheck.Detail)
	}

	if !report.Passed() {
		t.Error("report.Passed() = false, want true with the rename-phase surface complete")
	}
}

// calm-poc-phk.7: full-confirmation mode now runs to completion rather than
// staying deliberately pending — every rename-phase check, the
// separator-insensitive predecessor sweep, and the marker-history
// source-level assertion must all report PASS against this repository now
// that predecessor hook recognition/replacement/cleanup/idempotent-upgrade
// has landed.
func TestRunFullPassesAgainstRepoAfterHookUpgradeLands(t *testing.T) {
	report := RunFull("../..")
	if report.Mode != ModeFull {
		t.Fatalf("report.Mode = %q, want %q", report.Mode, ModeFull)
	}
	if len(report.Checks) == 0 {
		t.Fatal("RunFull produced no checks")
	}
	for _, c := range report.Checks {
		if c.Status != StatusPass {
			t.Errorf("check %q = %s: %s", c.Name, c.Status, c.Detail)
		}
	}
	if !report.Passed() {
		t.Error("report.Passed() = false, want true: full-confirmation mode should pass now that calm-poc-phk.7 has landed")
	}
}
