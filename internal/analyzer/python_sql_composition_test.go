package analyzer

// Red contract for calm-poc-dx9x: sql-composition-safety must distinguish
// psycopg2's safe composition API from genuine string interpolation.
//
// _sql_composition_kind flagged any execute(X.format(...)) without inspecting
// X, so `sql.SQL("... {c} ...").format(c=sql.Identifier(col))` — the exact
// pattern the violation message tells users to adopt — was reported as unsafe.
// Every one of the 49 sites this fired on in the observatory repo was that
// safe form, and in block mode the repo-wide ratchet turned those unfixable
// findings into a total commit lockout.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func sqlCompositionFindings(t *testing.T, source string) []Finding {
	t.Helper()
	radonPath, ok := resolvePythonRadonPath(os.Getenv)
	if !ok {
		t.Skip("radon not available")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "model.py")
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := pythonFileFindings(context.Background(), file, radonPath)
	if err != nil {
		t.Fatalf("pythonFileFindings: %v", err)
	}
	var sqlFindings []Finding
	for _, finding := range findings {
		if finding.Rule == "sql-composition-safety" {
			sqlFindings = append(sqlFindings, finding)
		}
	}
	return sqlFindings
}

func TestSQLCompositionAcceptsPsycopg2SafeComposition(t *testing.T) {
	safe := `from psycopg2 import sql

_TEMPLATE = sql.SQL("SELECT {col} FROM t")

def apply(cur, col):
    cur.execute(
        sql.SQL("SELECT {col} FROM raw.t").format(col=sql.Identifier(col))
    )
    cur.execute(_TEMPLATE.format(col=sql.Identifier(col)))
`
	if findings := sqlCompositionFindings(t, safe); len(findings) != 0 {
		t.Fatalf("safe psycopg2 composition produced %d finding(s): %+v; want none", len(findings), findings)
	}
}

func TestSQLCompositionStillCatchesGenuineInterpolation(t *testing.T) {
	for name, source := range map[string]string{
		"str-format": `def apply(cur, col):
    cur.execute("SELECT {} FROM t".format(col))
`,
		"fstring": `def apply(cur, col):
    cur.execute(f"SELECT {col} FROM t")
`,
		"percent": `def apply(cur, col):
    cur.execute("SELECT %s FROM t" % col)
`,
	} {
		t.Run(name, func(t *testing.T) {
			if findings := sqlCompositionFindings(t, source); len(findings) == 0 {
				t.Fatalf("%s produced no finding; the rule must still catch genuine interpolation", name)
			}
		})
	}
}
