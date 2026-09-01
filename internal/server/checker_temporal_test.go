package server

import (
	"os/exec"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
)

// Red tests for calm-poc-mcc (S5): the Python analyzer emits temporal-purity
// findings (naive datetime construction) on AnalysisResult.Findings; the
// checker counts enabled findings into one lte-0 violation and the RuleCounts
// map. Unsupported languages contribute zero findings and pass silently.

func requireRadon(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("radon"); err != nil {
		t.Skipf("radon is not installed: %v", err)
	}
}

func TestHandlerCheckBlocksNaivePythonDatetime(t *testing.T) {
	requireRadon(t)
	repo := "repo-temporal"
	server := newContentScoringServer(t, repo, `{
		"enforcement-mode": "block",
		"fitness-functions": {`+contentScoredOnlyFunctions+`,
			"temporal-purity": true
		}
	}`)

	source := "from datetime import datetime\n\n\ndef stamp(event):\n    event[\"created\"] = datetime.now()\n    event[\"updated\"] = datetime.utcnow()\n    return event\n"
	body := postCheckForLanguage(t, server.URL, repo, "src/pipeline/stamp.py", "python", source)
	if body.Status != fitness.StatusBlock {
		t.Fatalf("status = %q (violations %+v), want block for naive datetime construction", body.Status, body.Violations)
	}
	if len(body.Violations) != 1 {
		t.Fatalf("violations = %+v, want one temporal-purity violation", body.Violations)
	}
	violation := body.Violations[0]
	if violation.FitnessFunction != "temporal_purity" || violation.Value != 2 || violation.Limit != 0 {
		t.Fatalf("violation = %+v, want temporal_purity value 2 limit 0", violation)
	}
}

func TestHandlerCheckPassesTimezoneAwarePython(t *testing.T) {
	requireRadon(t)
	repo := "repo-temporal-pass"
	server := newContentScoringServer(t, repo, `{
		"enforcement-mode": "block",
		"fitness-functions": {`+contentScoredOnlyFunctions+`,
			"temporal-purity": true
		}
	}`)

	source := "from datetime import datetime, timezone\n\n\ndef stamp(event):\n    event[\"created\"] = datetime.now(timezone.utc)\n    return event\n"
	body := postCheckForLanguage(t, server.URL, repo, "src/pipeline/stamp.py", "python", source)
	if body.Status != fitness.StatusPass {
		t.Fatalf("status = %q (violations %+v), want pass for timezone-aware datetime", body.Status, body.Violations)
	}
}

func TestHandlerCheckPassesGoForTemporalPurity(t *testing.T) {
	repo := "repo-temporal-go"
	server := newContentScoringServer(t, repo, `{
		"enforcement-mode": "block",
		"fitness-functions": {`+contentScoredOnlyFunctions+`,
			"temporal-purity": true
		}
	}`)

	body := postCheck(t, server.URL, repo, "internal/clock/clock.go",
		"package clock\n\nimport \"time\"\n\nfunc Now() time.Time { return time.Now() }\n")
	if body.Status != fitness.StatusPass {
		t.Fatalf("status = %q (violations %+v), want silent pass for unsupported language", body.Status, body.Violations)
	}
}
