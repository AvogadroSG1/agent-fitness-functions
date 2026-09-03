package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/analyzer"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/calm"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/client"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/server"
)

type validatorFunc func(context.Context, string, string) (calm.ValidationResult, error)

func (f validatorFunc) Validate(ctx context.Context, archPath, patPath string) (calm.ValidationResult, error) {
	return f(ctx, archPath, patPath)
}

func ensureDefaultRoslynAnalyzer(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dotnet"); err != nil {
		t.Skip("dotnet not installed")
	}
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	exe := filepath.Join(repoRoot, "tools", "roslyn-analyzer", "bin", "Release", "net8.0", "CalmRoslynAnalyzer")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if info, err := os.Stat(exe); err == nil && !info.IsDir() {
		return
	}
	project := filepath.Join(repoRoot, "tools", "roslyn-analyzer", "CalmRoslynAnalyzer.csproj")
	command := exec.Command("dotnet", "build", "-c", "Release", project)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("dotnet build failed: %v\n%s", err, output)
	}
}

func setupTestConfig(t *testing.T, repo string, mode server.EnforcementMode, onError server.ErrorEnforcementMode, fitnessFuncs map[string]bool) (*server.ConfigStore, string) {
	t.Helper()
	configDir := t.TempDir()
	repoDir := filepath.Join(configDir, repo)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	cfg := server.Config{
		EnforcementMode:    mode,
		EnforcementOnError: onError,
		FitnessFunctions:   fitnessFuncs,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "config.json"), data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	store, err := server.NewConfigStore(context.Background(), configDir)
	if err != nil {
		t.Fatalf("NewConfigStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, configDir
}

func createGovernanceServer(t *testing.T, store *server.ConfigStore, mutateChecker ...func(*server.Checker)) (*httptest.Server, *server.State) {
	t.Helper()
	state := server.NewState()
	checker := server.Checker{
		ConfigStore: store,
		State:       state,
		Validator: validatorFunc(func(ctx context.Context, archPath, patPath string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: true, Output: `{"hasErrors":false}`}, nil
		}),
	}
	for _, fn := range mutateChecker {
		fn(&checker)
	}
	handler := server.NewHandlerWithChecker(checker, nil)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, state
}

func fixturePath(t *testing.T, filename string) string {
	t.Helper()
	path := filepath.Join("..", "fixtures", "resilience", filename)
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve fixture path %q: %v", filename, err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("fixture %q not found at %s: %v", filename, abs, err)
	}
	return abs
}

// BDD Scenario: Validate Observatory google.py fixture containing Python 3.12 type statements
func TestObservatoryGooglePyFixture_ClientValidate(t *testing.T) {
	googlePyPath := fixturePath(t, "google.py")
	repo := "observatory-repo"
	store, _ := setupTestConfig(t, repo, server.EnforcementBlock, server.EnforcementOnErrorBlock, map[string]bool{
		"cyclomatic-complexity": true,
		"dependency-discipline": true,
	})
	ts, _ := createGovernanceServer(t, store)

	var stdout bytes.Buffer
	err := client.RunCheck([]string{
		"--addr", ts.URL,
		"--repo", repo,
		"--file", "sources/google.py",
		"--content-file", googlePyPath,
		"--language", "python",
	}, &stdout, ts.Client(), nil)

	if err != nil {
		t.Fatalf("client validate returned unexpected error: %v (stdout: %s)", err, stdout.String())
	}

	var result fitness.ValidationResult
	if decodeErr := json.Unmarshal(stdout.Bytes(), &result); decodeErr != nil {
		t.Fatalf("failed to parse validation result JSON: %v, raw stdout: %s", decodeErr, stdout.String())
	}

	if result.Status != fitness.StatusPass && result.Status != fitness.StatusAdvisory {
		t.Errorf("status = %q, want %q or %q", result.Status, fitness.StatusPass, fitness.StatusAdvisory)
	}
}

func TestObservatoryGooglePyFixture_DirectHTTPCheck_DoesNotReturn400(t *testing.T) {
	googlePyPath := fixturePath(t, "google.py")
	content, err := os.ReadFile(googlePyPath)
	if err != nil {
		t.Fatalf("read google.py: %v", err)
	}

	repo := "observatory-repo"
	store, _ := setupTestConfig(t, repo, server.EnforcementBlock, server.EnforcementOnErrorBlock, map[string]bool{
		"cyclomatic-complexity": true,
		"dependency-discipline": true,
	})
	ts, _ := createGovernanceServer(t, store)

	reqBody, err := json.Marshal(fitness.ValidationRequest{
		Repo:            repo,
		File:            "sources/google.py",
		Language:        "python",
		ProposedContent: string(content),
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	resp, err := ts.Client().Post(ts.URL+"/check", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST /check failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /check returned HTTP 400 Bad Request for Python 3.12 syntax: %s", string(body))
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /check status = %d, body = %s; want 200 OK", resp.StatusCode, string(body))
	}

	var result fitness.ValidationResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if result.Status != fitness.StatusPass && result.Status != fitness.StatusAdvisory {
		t.Errorf("result status = %q, want pass or advisory", result.Status)
	}
}

func TestObservatoryGooglePyFixture_AnalyzerDirectParsing(t *testing.T) {
	googlePyPath := fixturePath(t, "google.py")
	result, err := analyzer.AnalyzePythonFile(context.Background(), googlePyPath, "")
	if err != nil {
		t.Fatalf("AnalyzePythonFile failed on Python 3.12 fixture: %v", err)
	}
	if result.FileMetric.TotalLOC == 0 {
		t.Errorf("expected non-zero TotalLOC, got %d", result.FileMetric.TotalLOC)
	}
	if len(result.Functions) == 0 {
		t.Errorf("expected functions to be detected in GoogleSource class, got 0")
	}
}

// BDD Scenario: Validate Observatory HealthReporter.cs fixture
func TestObservatoryHealthReporterCSharpFixture_ClientValidate(t *testing.T) {
	ensureDefaultRoslynAnalyzer(t)
	healthReporterPath := fixturePath(t, "HealthReporter.cs")
	repo := "observatory-csharp-repo"
	store, _ := setupTestConfig(t, repo, server.EnforcementBlock, server.EnforcementOnErrorBlock, map[string]bool{
		"cyclomatic-complexity": true,
		"interface-width":       true,
	})
	ts, _ := createGovernanceServer(t, store)

	// In EnforcementBlock mode, first cold C# check returns pass with warming=true
	var stdout bytes.Buffer
	err := client.RunCheck([]string{
		"--addr", ts.URL,
		"--repo", repo,
		"--file", "HealthReporter.cs",
		"--content-file", healthReporterPath,
		"--language", "csharp",
	}, &stdout, ts.Client(), nil)

	if err != nil {
		t.Fatalf("client validate returned unexpected error: %v (stdout: %s)", err, stdout.String())
	}

	var firstResult fitness.ValidationResult
	if decodeErr := json.Unmarshal(stdout.Bytes(), &firstResult); decodeErr != nil {
		t.Fatalf("failed to parse first validation result JSON: %v", decodeErr)
	}

	if firstResult.Status != fitness.StatusPass {
		t.Errorf("status = %q, want %q", firstResult.Status, fitness.StatusPass)
	}
	if !firstResult.Warming {
		t.Errorf("Warming = %v, want true on first cold C# check", firstResult.Warming)
	}
}

func TestObservatoryHealthReporterCSharpFixture_AdvisoryModeProducesVerdict(t *testing.T) {
	healthReporterPath := fixturePath(t, "HealthReporter.cs")
	repo := "observatory-csharp-repo"
	store, _ := setupTestConfig(t, repo, server.EnforcementAdvisory, server.EnforcementOnErrorAdvisory, map[string]bool{
		"cyclomatic-complexity": true,
		"interface-width":       true,
	})
	ts, _ := createGovernanceServer(t, store, func(c *server.Checker) {
		c.Analyzers = map[string]server.SourceAnalyzer{
			"csharp": server.AnalyzerFunc(func(ctx context.Context, req server.AnalysisRequest) (analyzer.AnalysisResult, error) {
				return analyzer.AnalysisResult{
					CALMNode: "HealthReporter",
					File:     req.File,
					Language: "csharp",
				}, nil
			}),
		}
	})

	var stdout bytes.Buffer
	err := client.RunCheck([]string{
		"--addr", ts.URL,
		"--repo", repo,
		"--file", "HealthReporter.cs",
		"--content-file", healthReporterPath,
		"--language", "csharp",
	}, &stdout, ts.Client(), nil)

	if err != nil {
		t.Fatalf("client validate failed in advisory mode: %v (stdout: %s)", err, stdout.String())
	}

	var result fitness.ValidationResult
	if decodeErr := json.Unmarshal(stdout.Bytes(), &result); decodeErr != nil {
		t.Fatalf("failed to parse validation result JSON: %v", decodeErr)
	}

	if result.Status != fitness.StatusPass && result.Status != fitness.StatusAdvisory {
		t.Errorf("status = %q, want pass or advisory", result.Status)
	}
}

func TestObservatoryHealthReporterCSharp_AnalyzerDirectParsing(t *testing.T) {
	ensureDefaultRoslynAnalyzer(t)
	healthReporterPath := fixturePath(t, "HealthReporter.cs")
	result, err := analyzer.AnalyzeCSharpFile(context.Background(), healthReporterPath, "")
	if err != nil {
		t.Fatalf("AnalyzeCSharpFile failed on HealthReporter.cs: %v", err)
	}
	if result.FileMetric.TotalLOC == 0 {
		t.Errorf("expected non-zero TotalLOC, got %d", result.FileMetric.TotalLOC)
	}
	if len(result.Functions) == 0 {
		t.Errorf("expected functions to be detected in HealthReporter class, got 0")
	}
}

// Resilience Scenario: enforcement_on_error: advisory gracefully handles analyzer failures
func TestEnforcementOnError_Advisory_GracefulDegradationOnAnalyzerFailure(t *testing.T) {
	repo := "advisory-degradation-repo"
	store, _ := setupTestConfig(t, repo, server.EnforcementBlock, server.EnforcementOnErrorAdvisory, map[string]bool{
		"cyclomatic-complexity": true,
	})
	ts, _ := createGovernanceServer(t, store, func(c *server.Checker) {
		c.Analyzers = map[string]server.SourceAnalyzer{
			"python": server.AnalyzerFunc(func(ctx context.Context, req server.AnalysisRequest) (analyzer.AnalysisResult, error) {
				return analyzer.AnalysisResult{}, errors.New("running radon: executable file not found in $PATH")
			}),
		}
	})

	var stdout bytes.Buffer
	err := client.RunCheck([]string{
		"--addr", ts.URL,
		"--repo", repo,
		"--file", "sources/google.py",
		"--content", "type FlowCredentials = dict[str, str]\n",
		"--language", "python",
	}, &stdout, ts.Client(), nil)

	if err != nil {
		t.Fatalf("expected advisory mode to not fail client check, got error: %v", err)
	}

	var result fitness.ValidationResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v (stdout: %s)", err, stdout.String())
	}

	if result.Status != fitness.StatusAdvisory {
		t.Fatalf("status = %q, want %q on toolchain degradation", result.Status, fitness.StatusAdvisory)
	}
}

// Resilience Scenario: enforcement_on_error: block surfaces HTTP 503 and exit code 3 in client
func TestEnforcementOnError_Block_SurfacesInfrastructureFailure(t *testing.T) {
	repo := "block-infrastructure-repo"
	store, _ := setupTestConfig(t, repo, server.EnforcementBlock, server.EnforcementOnErrorBlock, map[string]bool{
		"cyclomatic-complexity": true,
	})
	ts, _ := createGovernanceServer(t, store, func(c *server.Checker) {
		c.Analyzers = map[string]server.SourceAnalyzer{
			"python": server.AnalyzerFunc(func(ctx context.Context, req server.AnalysisRequest) (analyzer.AnalysisResult, error) {
				return analyzer.AnalysisResult{}, errors.New("running radon: executable file not found in $PATH")
			}),
		}
	})

	var stdout bytes.Buffer
	err := client.RunCheck([]string{
		"--addr", ts.URL,
		"--repo", repo,
		"--file", "sources/google.py",
		"--content", "type FlowCredentials = dict[str, str]\n",
		"--language", "python",
	}, &stdout, ts.Client(), nil)

	if err == nil {
		t.Fatal("expected client check to fail under enforcement_on_error: block, but it passed")
	}

	if !client.IsInfraError(err) {
		t.Errorf("expected error to be classified as InfraError (exit code 3), got: %T (%v)", err, err)
	}

	// Verify structured machine-readable error report emitted to stdout
	var infraReport struct {
		Status      string `json:"status"`
		ErrorKind   string `json:"error_kind"`
		Message     string `json:"message"`
		Remediation string `json:"remediation"`
	}

	if unmarshalErr := json.Unmarshal(stdout.Bytes(), &infraReport); unmarshalErr != nil {
		t.Fatalf("failed to parse stdout machine-readable error JSON: %v, raw: %s", unmarshalErr, stdout.String())
	}

	if infraReport.Status != "error" {
		t.Errorf("infraReport.Status = %q, want %q", infraReport.Status, "error")
	}
	if infraReport.ErrorKind != "server_error" {
		t.Errorf("infraReport.ErrorKind = %q, want %q", infraReport.ErrorKind, "server_error")
	}
	if !strings.Contains(infraReport.Remediation, "agent-fitness-functions doctor") {
		t.Errorf("infraReport.Remediation = %q, want mention of doctor", infraReport.Remediation)
	}
}

func TestEnforcementOnError_CSharp_Advisory_GracefulDegradation(t *testing.T) {
	repo := "advisory-csharp-repo"
	store, _ := setupTestConfig(t, repo, server.EnforcementBlock, server.EnforcementOnErrorAdvisory, map[string]bool{
		"cyclomatic-complexity": true,
	})
	ts, _ := createGovernanceServer(t, store, func(c *server.Checker) {
		c.BlockOnWarmup = true
		c.Analyzers = map[string]server.SourceAnalyzer{
			"csharp": server.AnalyzerFunc(func(ctx context.Context, req server.AnalysisRequest) (analyzer.AnalysisResult, error) {
				return analyzer.AnalysisResult{}, errors.New("running Roslyn analyzer: exit status 1")
			}),
		}
	})

	var stdout bytes.Buffer
	err := client.RunCheck([]string{
		"--addr", ts.URL,
		"--repo", repo,
		"--file", "HealthReporter.cs",
		"--content", "public class HealthReporter {}\n",
		"--language", "csharp",
	}, &stdout, ts.Client(), nil)

	if err != nil {
		t.Fatalf("expected advisory mode to not fail client check on C# analyzer error, got: %v", err)
	}

	var result fitness.ValidationResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v (stdout: %s)", err, stdout.String())
	}

	if result.Status != fitness.StatusAdvisory {
		t.Fatalf("status = %q, want %q on C# toolchain degradation", result.Status, fitness.StatusAdvisory)
	}
}

func TestEnforcementOnError_CSharp_Block_SurfacesInfrastructureFailure(t *testing.T) {
	repo := "block-csharp-repo"
	store, _ := setupTestConfig(t, repo, server.EnforcementBlock, server.EnforcementOnErrorBlock, map[string]bool{
		"cyclomatic-complexity": true,
	})
	ts, _ := createGovernanceServer(t, store, func(c *server.Checker) {
		c.BlockOnWarmup = true
		c.Analyzers = map[string]server.SourceAnalyzer{
			"csharp": server.AnalyzerFunc(func(ctx context.Context, req server.AnalysisRequest) (analyzer.AnalysisResult, error) {
				return analyzer.AnalysisResult{}, errors.New("running Roslyn analyzer: exit status 1")
			}),
		}
	})

	var stdout bytes.Buffer
	err := client.RunCheck([]string{
		"--addr", ts.URL,
		"--repo", repo,
		"--file", "HealthReporter.cs",
		"--content", "public class HealthReporter {}\n",
		"--language", "csharp",
	}, &stdout, ts.Client(), nil)

	if err == nil {
		t.Fatal("expected client check to fail under enforcement_on_error: block for C#, but it passed")
	}

	if !client.IsInfraError(err) {
		t.Errorf("expected error to be classified as InfraError (exit code 3), got: %T (%v)", err, err)
	}

	var infraReport struct {
		Status      string `json:"status"`
		ErrorKind   string `json:"error_kind"`
		Message     string `json:"message"`
		Remediation string `json:"remediation"`
	}

	if unmarshalErr := json.Unmarshal(stdout.Bytes(), &infraReport); unmarshalErr != nil {
		t.Fatalf("failed to parse stdout machine-readable error JSON: %v, raw: %s", unmarshalErr, stdout.String())
	}

	if infraReport.Status != "error" {
		t.Errorf("infraReport.Status = %q, want %q", infraReport.Status, "error")
	}
	if infraReport.ErrorKind != "server_error" {
		t.Errorf("infraReport.ErrorKind = %q, want %q", infraReport.ErrorKind, "server_error")
	}
}
