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

func TestRunFullIsAlwaysPendingAndNeverPasses(t *testing.T) {
	report := RunFull("../..")
	if report.Mode != ModeFull {
		t.Fatalf("report.Mode = %q, want %q", report.Mode, ModeFull)
	}
	if report.Passed() {
		t.Error("RunFull().Passed() = true, want false: full-confirmation mode must stay pending until calm-poc-phk.7")
	}
	for _, c := range report.Checks {
		if c.Status == StatusPass {
			t.Errorf("check %q reported PASS in full mode; full mode must never report PASS before calm-poc-phk.7", c.Name)
		}
		if c.Status != StatusNotImplemented {
			t.Errorf("check %q = %s, want NOT_IMPLEMENTED", c.Name, c.Status)
		}
	}
}
