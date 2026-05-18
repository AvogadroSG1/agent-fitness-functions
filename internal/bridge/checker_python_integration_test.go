//go:build integration

package bridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheckerWithRealCALMBlocksRingstationPythonCyclomaticComplexityFixture(t *testing.T) {
	if _, err := exec.LookPath("calm"); err != nil {
		t.Skip("calm CLI not installed")
	}
	if _, err := exec.LookPath("radon"); err != nil {
		t.Skip("radon not installed")
	}
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	source, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "violations", "python", "ringstation-dd-stage-bronze.py"))
	if err != nil {
		t.Fatalf("read ringstation fixture: %v", err)
	}
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: filepath.Join("..", "..", "patterns", "governance.json"),
	}, nil))
	defer server.Close()

	start := time.Now()
	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": `+jsonString(repo)+`,
		"file": "databricks/cost-analytics/src/setup/dd_stage_bronze.py",
		"language": "python",
		"proposed_content": `+jsonString(string(source))+`
	}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	t.Logf("real synchronous Python /check latency: %s", time.Since(start))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	var body CheckResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != StatusBlock {
		t.Fatalf("response = %+v, want block", body)
	}
	if !hasViolation(body.Violations, Violation{
		FitnessFunction: "cyclomatic_complexity",
		Function:        "build_config",
		Limit:           9,
		CALMNode:        "dd_stage_bronze",
	}) {
		t.Fatalf("violations = %+v, want build_config cyclomatic complexity violation", body.Violations)
	}
}

func hasViolation(violations []Violation, expected Violation) bool {
	for _, violation := range violations {
		if violation.FitnessFunction == expected.FitnessFunction &&
			violation.Function == expected.Function &&
			violation.Limit == expected.Limit &&
			violation.CALMNode == expected.CALMNode &&
			violation.Value > expected.Limit {
			return true
		}
	}
	return false
}
