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

	var latencies []time.Duration
	var overBudget []time.Duration
	for index := 0; index < 5; index++ {
		start := time.Now()
		response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
			"repo": `+jsonString(repo)+`,
			"file": "databricks/cost-analytics/src/setup/dd_stage_bronze.py",
			"language": "python",
			"proposed_content": `+jsonString(string(source))+`
		}`))
		if err != nil {
			t.Fatalf("POST /check run %d: %v", index+1, err)
		}
		latency := time.Since(start)
		latencies = append(latencies, latency)
		var body CheckResponse
		decodeErr := json.NewDecoder(response.Body).Decode(&body)
		closeErr := response.Body.Close()
		if closeErr != nil {
			t.Fatalf("close response body run %d: %v", index+1, closeErr)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status run %d = %d, want 200", index+1, response.StatusCode)
		}
		if decodeErr != nil {
			t.Fatalf("decode response run %d: %v", index+1, decodeErr)
		}
		if latency >= 500*time.Millisecond {
			overBudget = append(overBudget, latency)
		}
		if body.Status != StatusBlock {
			t.Fatalf("response run %d = %+v, want block", index+1, body)
		}
		if !hasViolation(body.Violations, Violation{
			FitnessFunction: "cyclomatic_complexity",
			Function:        "build_config",
			Limit:           9,
			CALMNode:        "dd_stage_bronze",
		}) {
			t.Fatalf("violations run %d = %+v, want build_config cyclomatic complexity violation", index+1, body.Violations)
		}
	}
	t.Logf("real synchronous Python /check latencies across 5 consecutive runs: %v", latencies)
	if len(overBudget) > 0 {
		t.Logf("external CALM validation jitter produced %d over-budget samples: %v", len(overBudget), overBudget)
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
	if _, err := exec.CommandContext(context.Background(), "radon", "cc", "-j", sourcePath).Output(); err != nil {
		t.Fatalf("profile legacy radon cc: %v", err)
	}
	legacyRadonCCDuration := time.Since(phaseStart)

	phaseStart = time.Now()
	if _, err := exec.CommandContext(context.Background(), "radon", "raw", "-j", sourcePath).Output(); err != nil {
		t.Fatalf("profile legacy radon raw: %v", err)
	}
	legacyRadonRawDuration := time.Since(phaseStart)

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
	t.Logf("python /check phase profile: config=%s pattern=%s temp_write=%s legacy_radon_cc=%s legacy_radon_raw=%s radon_api_analysis=%s architecture=%s calm_validate=%s fitness=%s total=%s violations=%d",
		configDuration,
		patternDuration,
		tempWriteDuration,
		legacyRadonCCDuration,
		legacyRadonRawDuration,
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
