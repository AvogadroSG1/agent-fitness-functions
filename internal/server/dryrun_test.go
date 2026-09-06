package server

import (
	"context"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
)

// S6 red-test contract (calm-poc-cpvk): a dry-run proposal — agent pre-write
// validation or a doctor probe — gets a full verdict but must never persist
// into the repository's outstanding-violation state. Twice in one session a
// rejected proposal (once for a file that never existed on disk) poisoned the
// ledger and blocked every subsequent edit in block mode.

func TestCheckerDryRunDoesNotPersistState(t *testing.T) {
	repo := "repo-one"
	store := newTestConfigStore(t)
	writeRepoConfig(t, store, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	checker := Checker{
		ConfigStore: store,
		PatternPath: writeTestPattern(t),
		State:       NewState(),
	}

	rejected, err := checker.Check(context.Background(), fitness.ValidationRequest{
		Repo:            repo,
		File:            "internal/parser/parser.go",
		Language:        "go",
		ProposedContent: complexGoSource(),
		DryRun:          true,
	})
	if err != nil {
		t.Fatalf("dry-run check: %v", err)
	}
	if rejected.Status != fitness.StatusBlock || len(rejected.Violations) == 0 {
		t.Fatalf("dry-run response = %+v, want the real block verdict", rejected)
	}
	if outstanding := checker.State.Violations(repo); len(outstanding) != 0 {
		t.Fatalf("state after dry-run = %+v, want empty: rejected proposals must not persist", outstanding)
	}

	clean, err := checker.Check(context.Background(), fitness.ValidationRequest{
		Repo:            repo,
		File:            "internal/other/other.go",
		Language:        "go",
		ProposedContent: cleanGoSource(),
	})
	if err != nil {
		t.Fatalf("follow-up check: %v", err)
	}
	if clean.Status != fitness.StatusPass || len(clean.Violations) != 0 {
		t.Fatalf("follow-up response = %+v, want pass untainted by the dry-run", clean)
	}
}
