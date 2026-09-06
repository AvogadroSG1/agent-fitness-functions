package server

// S6 (calm-poc-cpvk): the dry-run ledger contract beyond the single red case in
// dryrun_test.go. A dry run must return exactly the verdict a wet check would —
// including the repository-wide aggregation the blackboard model is built on —
// while leaving the outstanding-violation ledger byte-for-byte as it found it.
// Each case pins the dry behaviour against the wet behaviour it must mirror in
// the verdict and must NOT mirror in the ledger.

import (
	"context"
	"reflect"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
)

const (
	ledgerFileA = "internal/alpha/alpha.go"
	ledgerFileB = "internal/bravo/bravo.go"
)

// newLedgerChecker builds a checker governing repo in one enforcement mode, with
// only cyclomatic complexity active so the fixtures score predictably.
func newLedgerChecker(t *testing.T, repo string, mode EnforcementMode) Checker {
	t.Helper()
	store := newTestConfigStore(t)
	writeRepoConfig(t, store, repo, mode, map[string]bool{"cyclomatic-complexity": true})
	return Checker{ConfigStore: store, PatternPath: writeTestPattern(t), State: NewState()}
}

// ledgerRequest is one validation of file with content, wet or dry.
func ledgerRequest(repo, file, content string, dryRun bool) fitness.ValidationRequest {
	return fitness.ValidationRequest{Repo: repo, File: file, Language: "go", ProposedContent: content, DryRun: dryRun}
}

// recordViolation performs a wet check that leaves file's violations outstanding,
// returning the ledger contents it produced.
func recordViolation(t *testing.T, checker Checker, repo, file string) []fitness.Violation {
	t.Helper()
	result, err := checker.Check(context.Background(), ledgerRequest(repo, file, complexGoSource(), false))
	if err != nil {
		t.Fatalf("wet check of %s: %v", file, err)
	}
	if result.Status != fitness.StatusBlock || len(result.Violations) == 0 {
		t.Fatalf("wet check of %s = %+v, want a recorded block", file, result)
	}
	return checker.State.Violations(repo)
}

// seedOutstanding puts one violation in the ledger directly, for the modes whose
// check path never records one but must still not clear what is already there.
func seedOutstanding(t *testing.T, checker Checker, repo, file string) []fitness.Violation {
	t.Helper()
	checker.State.ReplaceFile(repo, file, []fitness.Violation{{
		FitnessFunction: "cyclomatic_complexity",
		CALMNode:        "bravo",
		File:            file,
		Function:        "Parse",
		Value:           11,
		Limit:           9,
		Message:         "recorded by an earlier wet check",
	}})
	return checker.State.Violations(repo)
}

// A clean file in block mode is still blocked by another file's outstanding
// violations — the blackboard verdict — and a dry run must report exactly that
// without touching the ledger it read.
func TestBlockModeDryRunOfCleanFileReportsOutstandingWithoutRecording(t *testing.T) {
	repo := "repo-one"
	checker := newLedgerChecker(t, repo, EnforcementBlock)
	outstanding := recordViolation(t, checker, repo, ledgerFileB)

	dry, err := checker.Check(context.Background(), ledgerRequest(repo, ledgerFileA, cleanGoSource(), true))
	if err != nil {
		t.Fatalf("dry-run check: %v", err)
	}
	if dry.Status != fitness.StatusBlock || !reflect.DeepEqual(dry.Violations, outstanding) {
		t.Fatalf("dry-run verdict = %+v, want the outstanding block %+v", dry, outstanding)
	}
	if after := checker.State.Violations(repo); !reflect.DeepEqual(after, outstanding) {
		t.Fatalf("ledger after dry run = %+v, want %+v unchanged", after, outstanding)
	}

	wet, err := checker.Check(context.Background(), ledgerRequest(repo, ledgerFileA, cleanGoSource(), false))
	if err != nil {
		t.Fatalf("wet check: %v", err)
	}
	if !reflect.DeepEqual(dry, wet) {
		t.Fatalf("dry-run verdict = %+v, want it identical to the wet verdict %+v", dry, wet)
	}
}

// A violating file in block mode merges into the repository-wide set: the dry
// run must produce the same merged, sorted violations as the wet sequence, and
// only the wet one may leave them behind.
func TestBlockModeDryRunMergesAndSortsWithOutstandingViolations(t *testing.T) {
	repo := "repo-one"
	dryChecker := newLedgerChecker(t, repo, EnforcementBlock)
	wetChecker := newLedgerChecker(t, repo, EnforcementBlock)
	outstanding := recordViolation(t, dryChecker, repo, ledgerFileB)
	recordViolation(t, wetChecker, repo, ledgerFileB)

	dry, err := dryChecker.Check(context.Background(), ledgerRequest(repo, ledgerFileA, complexGoSource(), true))
	if err != nil {
		t.Fatalf("dry-run check: %v", err)
	}
	wet, err := wetChecker.Check(context.Background(), ledgerRequest(repo, ledgerFileA, complexGoSource(), false))
	if err != nil {
		t.Fatalf("wet check: %v", err)
	}
	if !reflect.DeepEqual(dry, wet) {
		t.Fatalf("dry-run verdict = %+v, want the merged wet verdict %+v", dry, wet)
	}
	if len(dry.Violations) <= len(outstanding) {
		t.Fatalf("dry-run violations = %+v, want the proposal merged with the outstanding %+v", dry.Violations, outstanding)
	}
	if after := dryChecker.State.Violations(repo); !reflect.DeepEqual(after, outstanding) {
		t.Fatalf("ledger after dry run = %+v, want %+v unchanged", after, outstanding)
	}
	if after := wetChecker.State.Violations(repo); !reflect.DeepEqual(after, wet.Violations) {
		t.Fatalf("ledger after wet check = %+v, want the merged set %+v recorded", after, wet.Violations)
	}
}

// Enforcement-off passes every file and a wet pass clears the repository. A dry
// run passes too, but clearing is a write: it must not happen.
func TestEnforcementOffDryRunLeavesOutstandingStateIntact(t *testing.T) {
	repo := "repo-one"
	dryChecker := newLedgerChecker(t, repo, EnforcementOff)
	seeded := seedOutstanding(t, dryChecker, repo, ledgerFileB)

	dry, err := dryChecker.Check(context.Background(), ledgerRequest(repo, ledgerFileA, cleanGoSource(), true))
	if err != nil {
		t.Fatalf("dry-run check: %v", err)
	}
	if dry.Status != fitness.StatusPass {
		t.Fatalf("dry-run verdict = %+v, want pass in off mode", dry)
	}
	if after := dryChecker.State.Violations(repo); !reflect.DeepEqual(after, seeded) {
		t.Fatalf("ledger after dry run = %+v, want %+v unchanged", after, seeded)
	}

	wetChecker := newLedgerChecker(t, repo, EnforcementOff)
	seedOutstanding(t, wetChecker, repo, ledgerFileB)
	if _, err := wetChecker.Check(context.Background(), ledgerRequest(repo, ledgerFileA, cleanGoSource(), false)); err != nil {
		t.Fatalf("wet check: %v", err)
	}
	if after := wetChecker.State.Violations(repo); len(after) != 0 {
		t.Fatalf("ledger after wet off-mode check = %+v, want cleared: the dry run must differ from this", after)
	}
}

// Advisory reports violations and clears the repository on every wet check. A
// dry run reports the same advisory verdict and clears nothing.
func TestAdvisoryDryRunKeepsOutstandingStateThatWetAdvisoryClears(t *testing.T) {
	repo := "repo-one"
	dryChecker := newLedgerChecker(t, repo, EnforcementAdvisory)
	seeded := seedOutstanding(t, dryChecker, repo, ledgerFileB)

	dry, err := dryChecker.Check(context.Background(), ledgerRequest(repo, ledgerFileA, complexGoSource(), true))
	if err != nil {
		t.Fatalf("dry-run check: %v", err)
	}
	if dry.Status != fitness.StatusAdvisory || len(dry.Violations) == 0 {
		t.Fatalf("dry-run verdict = %+v, want an advisory verdict with violations", dry)
	}
	if after := dryChecker.State.Violations(repo); !reflect.DeepEqual(after, seeded) {
		t.Fatalf("ledger after dry run = %+v, want %+v unchanged", after, seeded)
	}

	wetChecker := newLedgerChecker(t, repo, EnforcementAdvisory)
	seedOutstanding(t, wetChecker, repo, ledgerFileB)
	wet, err := wetChecker.Check(context.Background(), ledgerRequest(repo, ledgerFileA, complexGoSource(), false))
	if err != nil {
		t.Fatalf("wet check: %v", err)
	}
	if !reflect.DeepEqual(dry, wet) {
		t.Fatalf("dry-run verdict = %+v, want the wet advisory verdict %+v", dry, wet)
	}
	if after := wetChecker.State.Violations(repo); len(after) != 0 {
		t.Fatalf("ledger after wet advisory check = %+v, want cleared: the dry run must differ from this", after)
	}
}
