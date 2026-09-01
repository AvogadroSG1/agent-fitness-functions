package server

import (
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
)

// Red tests for calm-poc-k0l (S6): the Python analyzer emits
// sql-composition-safety findings for f-string, %-format, and .format()
// strings passed as the first argument to .execute()/.executemany(); the
// checker counts them into one lte-0 violation.

func TestHandlerCheckBlocksInterpolatedSQLExecution(t *testing.T) {
	requireRadon(t)
	repo := "repo-sqlsafety"
	server := newContentScoringServer(t, repo, `{
		"enforcement-mode": "block",
		"fitness-functions": {`+contentScoredOnlyFunctions+`,
			"sql-composition-safety": true
		}
	}`)

	source := "def refresh(cursor, table):\n" +
		"    cursor.execute(f\"DELETE FROM {table}\")\n" +
		"    cursor.execute(\"INSERT INTO %s VALUES (1)\" % table)\n" +
		"    cursor.executemany(\"UPDATE {} SET x = 1\".format(table), [()])\n"
	body := postCheckForLanguage(t, server.URL, repo, "src/pipeline/refresh.py", "python", source)
	if body.Status != fitness.StatusBlock {
		t.Fatalf("status = %q (violations %+v), want block for interpolated SQL", body.Status, body.Violations)
	}
	if len(body.Violations) != 1 {
		t.Fatalf("violations = %+v, want one sql-composition-safety violation", body.Violations)
	}
	violation := body.Violations[0]
	if violation.FitnessFunction != "sql_composition_safety" || violation.Value != 3 || violation.Limit != 0 {
		t.Fatalf("violation = %+v, want sql_composition_safety value 3 limit 0", violation)
	}
}

func TestHandlerCheckPassesParameterizedSQLExecution(t *testing.T) {
	requireRadon(t)
	repo := "repo-sqlsafety-pass"
	server := newContentScoringServer(t, repo, `{
		"enforcement-mode": "block",
		"fitness-functions": {`+contentScoredOnlyFunctions+`,
			"sql-composition-safety": true
		}
	}`)

	source := "def refresh(cursor, event_id):\n" +
		"    cursor.execute(\"DELETE FROM events WHERE id = %s\", (event_id,))\n" +
		"    cursor.executemany(\"INSERT INTO events (id) VALUES (%s)\", [(event_id,)])\n"
	body := postCheckForLanguage(t, server.URL, repo, "src/pipeline/refresh.py", "python", source)
	if body.Status != fitness.StatusPass {
		t.Fatalf("status = %q (violations %+v), want pass for parameterized SQL", body.Status, body.Violations)
	}
}
