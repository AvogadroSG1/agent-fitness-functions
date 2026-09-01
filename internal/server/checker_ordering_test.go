package server

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/calm"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
)

// Red tests for calm-poc-aj2 (S3): deterministic-ordering is scored in the
// checker against request content (regex over SQL window functions), counted
// against the pattern's lte-0 rule, with the tie-breaker token list sourced
// from fitness-function-settings. The scored count must also reach the CALM
// architecture document via AnalysisResult.RuleCounts.

func passingValidator() validatorFunc {
	return func(context.Context, string, string) (calm.ValidationResult, error) {
		return calm.ValidationResult{Valid: true}, nil
	}
}

// contentScoredOnlyFunctions disables the five metric functions so tests
// isolate the content-scored generalized functions.
const contentScoredOnlyFunctions = `
		"cyclomatic-complexity": false,
		"interface-width": false,
		"implementation-depth": false,
		"logic-density": false,
		"dependency-discipline": false`

func newContentScoringServer(t *testing.T, repo, configJSON string) *httptest.Server {
	t.Helper()
	store := newTestConfigStore(t)
	writeRepoConfigContent(t, store, repo, configJSON)
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		ConfigStore: store,
		PatternPath: writeTestPattern(t),
		Validator:   passingValidator(),
	}, nil))
	t.Cleanup(server.Close)
	return server
}

func TestHandlerCheckBlocksWindowFunctionWithoutTieBreaker(t *testing.T) {
	repo := "repo-ordering"
	server := newContentScoringServer(t, repo, `{
		"enforcement-mode": "block",
		"fitness-functions": {`+contentScoredOnlyFunctions+`,
			"deterministic-ordering": true
		}
	}`)

	source := "package store\n\nconst rankQuery = `SELECT ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY created_at) FROM events`\n"
	body := postCheck(t, server.URL, repo, "internal/store/query.go", source)
	if body.Status != fitness.StatusBlock {
		t.Fatalf("status = %q, want block for window function without tie-breaker", body.Status)
	}
	if len(body.Violations) != 1 {
		t.Fatalf("violations = %+v, want one deterministic-ordering violation", body.Violations)
	}
	violation := body.Violations[0]
	if violation.FitnessFunction != "deterministic_ordering" || violation.Value != 1 || violation.Limit != 0 {
		t.Fatalf("violation = %+v, want deterministic_ordering value 1 limit 0", violation)
	}
}

func TestHandlerCheckPassesWindowFunctionWithTieBreaker(t *testing.T) {
	repo := "repo-ordering-pass"
	server := newContentScoringServer(t, repo, `{
		"enforcement-mode": "block",
		"fitness-functions": {`+contentScoredOnlyFunctions+`,
			"deterministic-ordering": true
		}
	}`)

	source := "package store\n\nconst rankQuery = `SELECT ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY created_at, event_id) FROM events`\n"
	body := postCheck(t, server.URL, repo, "internal/store/query.go", source)
	if body.Status != fitness.StatusPass {
		t.Fatalf("status = %q (violations %+v), want pass for tie-broken ORDER BY", body.Status, body.Violations)
	}
}

func TestHandlerCheckHonorsCustomTieBreakerTokens(t *testing.T) {
	repo := "repo-ordering-custom"
	server := newContentScoringServer(t, repo, `{
		"enforcement-mode": "block",
		"fitness-functions": {`+contentScoredOnlyFunctions+`,
			"deterministic-ordering": true
		},
		"fitness-function-settings": {
			"deterministic-ordering": {"tie-breaker-tokens": ["event_no"]}
		}
	}`)

	blocked := postCheck(t, server.URL, repo, "internal/store/query.go",
		"package store\n\nconst q = `SELECT RANK() OVER (ORDER BY created_at, event_id) FROM events`\n")
	if blocked.Status != fitness.StatusBlock {
		t.Fatalf("status = %q, want block when ORDER BY lacks configured token", blocked.Status)
	}
	// Re-validating the same file with the configured token clears its
	// outstanding violation (block-state semantics track per file).
	passed := postCheck(t, server.URL, repo, "internal/store/query.go",
		"package store\n\nconst q = `SELECT RANK() OVER (ORDER BY created_at, event_no) FROM events`\n")
	if passed.Status != fitness.StatusPass {
		t.Fatalf("status = %q (violations %+v), want pass for configured token", passed.Status, passed.Violations)
	}
}

func TestHandlerCheckIgnoresWindowFunctionsWhenOrderingDisabled(t *testing.T) {
	repo := "repo-ordering-off"
	server := newContentScoringServer(t, repo, `{
		"enforcement-mode": "block",
		"fitness-functions": {`+contentScoredOnlyFunctions+`,
			"deterministic-ordering": false
		}
	}`)

	body := postCheck(t, server.URL, repo, "internal/store/query.go",
		"package store\n\nconst q = `SELECT ROW_NUMBER() OVER (ORDER BY created_at) FROM events`\n")
	if body.Status != fitness.StatusPass {
		t.Fatalf("status = %q, want pass when deterministic-ordering disabled", body.Status)
	}
}
