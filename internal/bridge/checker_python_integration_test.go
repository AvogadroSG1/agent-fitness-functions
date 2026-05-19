//go:build integration

package bridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/poconnor/calm-poc/internal/analyzer"
	"github.com/poconnor/calm-poc/internal/calm"
	"github.com/poconnor/calm-poc/internal/report"
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

func TestPythonSynchronousCheckPhaseProfile(t *testing.T) {
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
	checker := Checker{
		PatternPath: filepath.Join("..", "..", "patterns", "governance.json"),
		State:       NewState(),
	}
	request := CheckRequest{
		Repo:            repo,
		File:            "databricks/cost-analytics/src/setup/dd_stage_bronze.py",
		Language:        "python",
		ProposedContent: string(source),
	}

	start := time.Now()
	config, canonicalRepo, err := loadConfig(request.Repo)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	configDuration := time.Since(start)

	phaseStart := time.Now()
	pattern, err := calm.LoadPattern(checker.PatternPath)
	if err != nil {
		t.Fatalf("load pattern: %v", err)
	}
	patternDuration := time.Since(phaseStart)

	phaseStart = time.Now()
	sourcePath, cleanup, err := checker.writeProposedContent(request)
	if err != nil {
		t.Fatalf("write proposed content: %v", err)
	}
	defer cleanup()
	tempWriteDuration := time.Since(phaseStart)

	phaseStart = time.Now()
	result, err := analyzer.AnalyzePythonFile(context.Background(), sourcePath, "")
	if err != nil {
		t.Fatalf("analyze python: %v", err)
	}
	analysisDuration := time.Since(phaseStart)
	result = analyzer.EnsureModuleMetric(result)
	result.File = request.File
	result.CALMNode = calmNodeForRequest(request, result.CALMNode)

	phaseStart = time.Now()
	architecturePath, cleanupArchitecture, err := checker.writeArchitecture(report.BuildArchitecture(result))
	if err != nil {
		t.Fatalf("write architecture: %v", err)
	}
	defer cleanupArchitecture()
	architectureDuration := time.Since(phaseStart)

	phaseStart = time.Now()
	validation, err := (calm.Validator{}).Validate(context.Background(), architecturePath, checker.PatternPath)
	if err != nil && !isValidationFailure(validation) {
		t.Fatalf("validate architecture: %v", err)
	}
	calmDuration := time.Since(phaseStart)

	phaseStart = time.Now()
	violations := filterViolations(fitnessViolations(result, pattern), config)
	fitnessDuration := time.Since(phaseStart)
	totalDuration := time.Since(start)
	if canonicalRepo == "" {
		t.Fatalf("canonical repo is empty")
	}
	t.Logf("python /check phase profile: config=%s pattern=%s temp_write=%s analysis=%s architecture=%s calm_validate=%s fitness=%s total=%s violations=%d",
		configDuration,
		patternDuration,
		tempWriteDuration,
		analysisDuration,
		architectureDuration,
		calmDuration,
		fitnessDuration,
		totalDuration,
		len(violations),
	)
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
