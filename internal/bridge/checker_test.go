package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/poconnor/calm-poc/internal/analyzer"
	"github.com/poconnor/calm-poc/internal/calm"
)

func TestHandlerCheckRunsGoAnalyzerCALMAndBlocksCyclomaticComplexityViolation(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	patternPath := writeTestPattern(t)
	var called bool
	validator := validatorFunc(func(_ context.Context, architecturePath, patternPathArg string) (calm.ValidationResult, error) {
		called = true
		if patternPathArg != patternPath {
			t.Fatalf("pattern path = %q, want %q", patternPathArg, patternPath)
		}
		content, err := os.ReadFile(architecturePath)
		if err != nil {
			t.Fatalf("read architecture: %v", err)
		}
		if !bytes.Contains(content, []byte(`"cyclomatic-complexity": 10`)) {
			t.Fatalf("architecture = %s, want cyclomatic complexity 10", content)
		}
		return calm.ValidationResult{Valid: false, Output: `{"hasErrors":true}`}, errors.New("calm validate failed")
	})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: patternPath,
		Validator:   validator,
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": `+jsonString(repo)+`,
		"file": "internal/parser/parser.go",
		"language": "go",
		"proposed_content": `+jsonString(complexGoSource())+`
	}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	if !called {
		t.Fatal("validator was not called")
	}
	rawBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var jsonBody map[string]any
	if err := json.Unmarshal(rawBody, &jsonBody); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	violations, ok := jsonBody["violations"].([]any)
	if !ok || len(violations) != 1 {
		t.Fatalf("raw response = %s, want one violations entry", rawBody)
	}
	violationFields, ok := violations[0].(map[string]any)
	if !ok {
		t.Fatalf("raw response = %s, want violation object", rawBody)
	}
	for _, field := range []string{"fitness_function", "calm_node", "function", "value", "limit", "message"} {
		if _, ok := violationFields[field]; !ok {
			t.Fatalf("raw response = %s, want violation field %q", rawBody, field)
		}
	}
	var body CheckResponse
	if err := json.Unmarshal(rawBody, &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != StatusBlock {
		t.Fatalf("status = %q, want block", body.Status)
	}
	if len(body.Violations) != 1 {
		t.Fatalf("violations = %+v, want one violation", body.Violations)
	}
	violation := body.Violations[0]
	if violation.FitnessFunction != "cyclomatic_complexity" || violation.Function != "Parse" || violation.Value != 10 || violation.Limit != 9 {
		t.Fatalf("violation = %+v, want cyclomatic complexity violation for Parse", violation)
	}
}

func TestHandlerCheckCleansTemporaryFiles(t *testing.T) {
	tempDir := t.TempDir()
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		TempDir:     tempDir,
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: true, Output: `{"hasErrors":false}`}, nil
		}),
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": `+jsonString(repo)+`,
		"file": "internal/parser/parser.go",
		"language": "go",
		"proposed_content": "package parser\n\nfunc Parse() error {\n\treturn nil\n}\n"
	}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("temp entries = %v, want cleanup after check", entries)
	}
}

func TestHandlerCheckReturnsServiceUnavailableForCALMInfrastructureFailure(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: false, Output: ""}, errors.New("calm executable missing")
		}),
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": `+jsonString(repo)+`,
		"file": "internal/parser/parser.go",
		"language": "go",
		"proposed_content": `+jsonString(complexGoSource())+`
	}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %q, want 503", response.StatusCode, body)
	}
	if strings.Contains(string(body), "calm executable missing") || !strings.Contains(string(body), "running CALM validation") {
		t.Fatalf("body = %q, want client-safe infrastructure message", body)
	}
}

func TestHandlerCheckReturnsBadRequestForUnsupportedLanguage(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": `+jsonString(repo)+`,
		"file": "index.ts",
		"language": "typescript",
		"proposed_content": "const value = 1;\n"
	}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d body = %q, want 400", response.StatusCode, body)
	}
	if !strings.Contains(string(body), `unsupported language "typescript"`) {
		t.Fatalf("body = %q, want unsupported language message", body)
	}
}

func TestHandlerCheckReturnsBadRequestForTrailingJSON(t *testing.T) {
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": "/tmp/repo",
		"file": "internal/parser/parser.go",
		"language": "go",
		"proposed_content": "package parser\n"
	}{"extra":true}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.StatusCode)
	}
}

func TestHandlerCheckReturnsBadRequestForInvalidFileExtension(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": `+jsonString(repo)+`,
		"file": "bad.go?cachebuster",
		"language": "go",
		"proposed_content": "package parser\n"
	}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "invalid source file extension") {
		t.Fatalf("status = %d body = %q, want invalid extension 400", response.StatusCode, body)
	}
}

func TestHandlerCheckRejectsNilAnalyzerWithoutPanic(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Analyzers:   map[string]SourceAnalyzer{"go": AnalyzerFunc(nil)},
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": `+jsonString(repo)+`,
		"file": "internal/parser/parser.go",
		"language": "go",
		"proposed_content": "package parser\n"
	}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 unsupported analyzer", response.StatusCode)
	}
}

func TestHandlerCheckRejectsTypedNilAnalyzerWithoutPanic(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	var typedNil *typedNilAnalyzer
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Analyzers:   map[string]SourceAnalyzer{"go": typedNil},
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": `+jsonString(repo)+`,
		"file": "internal/parser/parser.go",
		"language": "go",
		"proposed_content": "package parser\n"
	}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 unsupported analyzer", response.StatusCode)
	}
}

func TestHandlerCheckReturnsServiceUnavailableForAnalyzerCancellation(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Analyzers: map[string]SourceAnalyzer{
			"go": AnalyzerFunc(func(context.Context, AnalysisRequest) (analyzer.AnalysisResult, error) {
				return analyzer.AnalysisResult{}, context.Canceled
			}),
		},
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": `+jsonString(repo)+`,
		"file": "internal/parser/parser.go",
		"language": "go",
		"proposed_content": "package parser\n"
	}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.StatusCode)
	}
}

func TestHandlerCheckRejectsTypedNilValidatorWithoutPanic(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	var typedNil *typedNilValidator
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Validator:   typedNil,
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": `+jsonString(repo)+`,
		"file": "internal/parser/parser.go",
		"language": "go",
		"proposed_content": "package parser\n\nfunc Parse() error {\n\treturn nil\n}\n"
	}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.StatusCode)
	}
}

func TestHandlerCheckPassesCleanGoContent(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	patternPath := writeTestPattern(t)
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: patternPath,
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: true, Output: `{"hasErrors":false}`}, nil
		}),
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": `+jsonString(repo)+`,
		"file": "internal/parser/parser.go",
		"language": "go",
		"proposed_content": "package parser\n\nfunc Parse() error {\n\treturn nil\n}\n"
	}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	var body CheckResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != StatusPass || len(body.Violations) != 0 {
		t.Fatalf("response = %+v, want pass without violations", body)
	}
}

func TestHandlerCheckRunsPythonAnalyzerAndRoutesAdvisoryViolation(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementAdvisory, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Analyzers: map[string]SourceAnalyzer{
			"python": fakePythonAnalyzer(pythonAnalysisWithComplexFunction("build_config", 10)),
		},
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: false, Output: `{"hasErrors":true}`}, errors.New("calm validate failed")
		}),
	}, nil))
	defer server.Close()

	body := postCheckForLanguage(t, server.URL, repo, "databricks/cost-analytics/src/setup/dd_stage_bronze.py", "python", complexPythonSource())
	if body.Status != StatusAdvisory || len(body.Violations) != 1 {
		t.Fatalf("response = %+v, want advisory violation", body)
	}
	violation := body.Violations[0]
	if violation.FitnessFunction != "cyclomatic_complexity" || violation.Function != "build_config" || violation.Value <= 9 || violation.Limit != 9 {
		t.Fatalf("violation = %+v, want Python build_config CC violation", violation)
	}
	if violation.CALMNode != "dd_stage_bronze" {
		t.Fatalf("calm node = %q, want logical Python module node", violation.CALMNode)
	}
}

func TestHandlerCheckPassesCleanPythonContent(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Analyzers: map[string]SourceAnalyzer{
			"python": fakePythonAnalyzer(analyzer.AnalysisResult{Language: "python"}),
		},
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: true, Output: `{"hasErrors":false}`}, nil
		}),
	}, nil))
	defer server.Close()

	body := postCheckForLanguage(t, server.URL, repo, "src/setup/clean.py", "python", cleanPythonSource())
	if body.Status != StatusPass || len(body.Violations) != 0 {
		t.Fatalf("response = %+v, want pass", body)
	}
}

func TestHandlerCheckReturnsServiceUnavailableForMissingRadon(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Analyzers: map[string]SourceAnalyzer{
			"python": AnalyzerFunc(func(ctx context.Context, request AnalysisRequest) (analyzer.AnalysisResult, error) {
				return analyzer.AnalyzePythonFile(ctx, request.TempPath, filepath.Join(t.TempDir(), "missing-radon"))
			}),
		},
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": `+jsonString(repo)+`,
		"file": "src/setup/clean.py",
		"language": "python",
		"proposed_content": `+jsonString(cleanPythonSource())+`
	}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(body), "running python analyzer") {
		t.Fatalf("status = %d body = %q, want python analyzer infrastructure failure", response.StatusCode, body)
	}
}

func TestHandlerCheckReturnsServiceUnavailableForCSharpAnalyzerFailure(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	state := NewState()
	state.CompleteWarmup("csharp")
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		State:       state,
		Analyzers: map[string]SourceAnalyzer{
			"csharp": AnalyzerFunc(func(context.Context, AnalysisRequest) (analyzer.AnalysisResult, error) {
				return analyzer.AnalysisResult{}, errors.New("running Roslyn analyzer: boom")
			}),
		},
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": `+jsonString(repo)+`,
		"file": "src/Widget.cs",
		"language": "csharp",
		"proposed_content": `+jsonString(cleanCSharpSource())+`
	}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(body), "running csharp analyzer") {
		t.Fatalf("status = %d body = %q, want csharp analyzer infrastructure failure", response.StatusCode, body)
	}
}

func TestHandlerCheckCSharpDeferredAnalyzerFailureSurfacesOnNextCall(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	failed := make(chan struct{})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Analyzers: map[string]SourceAnalyzer{
			"csharp": AnalyzerFunc(func(context.Context, AnalysisRequest) (analyzer.AnalysisResult, error) {
				close(failed)
				return analyzer.AnalysisResult{}, errors.New("running Roslyn analyzer: boom")
			}),
		},
	}, nil))
	defer server.Close()

	first := postCheckForLanguage(t, server.URL, repo, "src/Widget.cs", "csharp", cleanCSharpSource())
	if first.Status != StatusPass || !first.Warming {
		t.Fatalf("first response = %+v, want deferred warming pass", first)
	}
	select {
	case <-failed:
	case <-time.After(time.Second):
		t.Fatal("deferred C# analyzer failure did not run")
	}
	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": `+jsonString(repo)+`,
		"file": "src/Retry.cs",
		"language": "csharp",
		"proposed_content": `+jsonString(cleanCSharpSource())+`
	}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(body), "csharp warm-up failed") {
		t.Fatalf("status = %d body = %q, want warm-up infrastructure failure", response.StatusCode, body)
	}
}

func TestHandlerCheckCSharpWarmupFailurePrecedesOutstandingViolation(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	state := NewState()
	state.FailWarmup("csharp", "running csharp analyzer: boom")
	state.ReplaceFile(repo, "src/Existing.cs", []Violation{{
		FitnessFunction: "cyclomatic_complexity",
		CALMNode:        "Existing",
		File:            "src/Existing.cs",
		Function:        "Render",
		Value:           10,
		Limit:           9,
		Message:         "existing violation",
	}})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		State:       state,
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": `+jsonString(repo)+`,
		"file": "src/Retry.cs",
		"language": "csharp",
		"proposed_content": `+jsonString(cleanCSharpSource())+`
	}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(body), "csharp warm-up failed") {
		t.Fatalf("status = %d body = %q, want warm-up infrastructure failure before outstanding block", response.StatusCode, body)
	}
}

func TestHandlerCheckCSharpColdDefersAnalysisAndBlocksNextCall(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	started := make(chan AnalysisRequest, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Analyzers: map[string]SourceAnalyzer{
			"csharp": AnalyzerFunc(func(ctx context.Context, request AnalysisRequest) (analyzer.AnalysisResult, error) {
				call := calls.Add(1)
				if call == 1 {
					started <- request
					select {
					case <-release:
					case <-ctx.Done():
						return analyzer.AnalysisResult{}, ctx.Err()
					}
					return csharpAnalysisWithComplexFunction("Render", 10), nil
				}
				return analyzer.AnalysisResult{Language: "csharp"}, nil
			}),
		},
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: false, Output: `{"hasErrors":true}`}, errors.New("calm validate failed")
		}),
	}, nil))
	defer server.Close()

	start := time.Now()
	first := postCheckForLanguage(t, server.URL, repo, "src/Widget.cs", "csharp", complexCSharpSource())
	coldLatency := time.Since(start)
	t.Logf("deferred C# cold response latency: %s", coldLatency)
	if first.Status != StatusPass || !first.Warming || len(first.Violations) != 0 {
		t.Fatalf("first response = %+v, want pass with warming", first)
	}
	select {
	case request := <-started:
		if request.File != "src/Widget.cs" || request.Language != "csharp" {
			t.Fatalf("analysis request = %+v, want deferred C# request", request)
		}
	case <-time.After(time.Second):
		t.Fatal("deferred C# analyzer did not start")
	}
	close(release)
	violations := waitForStateViolations(t, server.URL, repo, 1)
	if violations[0].Function != "Render" || violations[0].CALMNode != "Widget" {
		t.Fatalf("violations = %+v, want deferred Widget.Render violation", violations)
	}

	second := postCheckForLanguage(t, server.URL, repo, "src/Other.cs", "csharp", cleanCSharpSource())
	if second.Status != StatusBlock || second.Warming || len(second.Violations) != 1 || second.Violations[0].File != "src/Widget.cs" {
		t.Fatalf("second response = %+v, want block from outstanding deferred violation", second)
	}
	if calls.Load() != 1 {
		t.Fatalf("analyzer calls = %d, want next call to block outstanding state without analysis", calls.Load())
	}
}

func TestHandlerCheckCSharpColdBlocksExistingOutstandingViolation(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	state := NewState()
	state.ReplaceFile(repo, "src/Existing.cs", []Violation{{
		FitnessFunction: "cyclomatic_complexity",
		CALMNode:        "Existing",
		File:            "src/Existing.cs",
		Function:        "Render",
		Value:           10,
		Limit:           9,
		Message:         "existing violation",
	}})
	var calls atomic.Int32
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		State:       state,
		Analyzers: map[string]SourceAnalyzer{
			"csharp": AnalyzerFunc(func(context.Context, AnalysisRequest) (analyzer.AnalysisResult, error) {
				calls.Add(1)
				return analyzer.AnalysisResult{Language: "csharp"}, nil
			}),
		},
	}, nil))
	defer server.Close()

	body := postCheckForLanguage(t, server.URL, repo, "src/New.cs", "csharp", cleanCSharpSource())
	if body.Status != StatusBlock || body.Warming || len(body.Violations) != 1 || body.Violations[0].File != "src/Existing.cs" {
		t.Fatalf("response = %+v, want existing violation block before cold warming", body)
	}
	if calls.Load() != 0 {
		t.Fatalf("analyzer calls = %d, want no cold analyzer while existing block is outstanding", calls.Load())
	}
}

func TestCheckerCheckCSharpCanceledWarmupLockDoesNotLeaveLanguageRunning(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	state := NewState()
	unlock, err := state.LockRepo(context.Background(), repo)
	if err != nil {
		t.Fatalf("lock repo: %v", err)
	}
	defer unlock()
	checker := Checker{
		PatternPath: writeTestPattern(t),
		State:       state,
		Analyzers: map[string]SourceAnalyzer{
			"csharp": fakeCSharpAnalyzer(analyzer.AnalysisResult{Language: "csharp"}),
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = checker.Check(ctx, CheckRequest{
		Repo:            repo,
		File:            "src/Widget.cs",
		Language:        "csharp",
		ProposedContent: cleanCSharpSource(),
	})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled while waiting for repo lock", err)
	}
	if state.IsWarm("csharp") {
		t.Fatal("csharp should not be marked warm after canceled warm-up lock")
	}
	if state.BeginWarmup("csharp") {
		return
	}
	t.Fatal("csharp warm-up should be claimable after canceled lock acquisition")
}

func TestHandlerCheckCSharpColdAdvisoryClearsStaleBlockState(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementAdvisory, map[string]bool{"cyclomatic-complexity": true})
	state := NewState()
	state.ReplaceFile(repo, "src/Existing.cs", []Violation{{
		FitnessFunction: "cyclomatic_complexity",
		CALMNode:        "Existing",
		File:            "src/Existing.cs",
		Function:        "Render",
		Value:           10,
		Limit:           9,
		Message:         "existing violation",
	}})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		State:       state,
		Analyzers: map[string]SourceAnalyzer{
			"csharp": fakeCSharpAnalyzer(analyzer.AnalysisResult{Language: "csharp"}),
		},
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: true, Output: `{"hasErrors":false}`}, nil
		}),
	}, nil))
	defer server.Close()

	body := postCheckForLanguage(t, server.URL, repo, "src/New.cs", "csharp", cleanCSharpSource())
	if body.Status != StatusPass || body.Warming || len(body.Violations) != 0 {
		t.Fatalf("response = %+v, want pass without stale block in advisory mode", body)
	}
	if state := getState(t, server.URL, repo); len(state.Violations) != 0 {
		t.Fatalf("state = %+v, want stale block state cleared", state)
	}
}

func TestHandlerCheckCSharpColdAdvisoryReturnsGuidanceSynchronously(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementAdvisory, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Analyzers: map[string]SourceAnalyzer{
			"csharp": fakeCSharpAnalyzer(csharpAnalysisWithComplexFunction("Render", 10)),
		},
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: false, Output: `{"hasErrors":true}`}, errors.New("calm validate failed")
		}),
	}, nil))
	defer server.Close()

	body := postCheckForLanguage(t, server.URL, repo, "src/Widget.cs", "csharp", complexCSharpSource())
	if body.Status != StatusAdvisory || body.Warming || len(body.Violations) != 1 || body.Violations[0].Function != "Render" {
		t.Fatalf("response = %+v, want synchronous advisory guidance", body)
	}
}

func TestHandlerCheckCSharpSecondColdCallAnalyzesSynchronouslyWhileWarmupRuns(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseSecond := make(chan struct{})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Analyzers: map[string]SourceAnalyzer{
			"csharp": AnalyzerFunc(func(ctx context.Context, request AnalysisRequest) (analyzer.AnalysisResult, error) {
				switch request.File {
				case "src/Warmup.cs":
					close(firstStarted)
					select {
					case <-releaseFirst:
					case <-ctx.Done():
						return analyzer.AnalysisResult{}, ctx.Err()
					}
					return analyzer.AnalysisResult{Language: "csharp"}, nil
				case "src/Widget.cs":
					close(secondStarted)
					select {
					case <-releaseSecond:
					case <-ctx.Done():
						return analyzer.AnalysisResult{}, ctx.Err()
					}
					return csharpAnalysisWithComplexFunction("Render", 10), nil
				default:
					return analyzer.AnalysisResult{Language: "csharp"}, nil
				}
			}),
		},
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: false, Output: `{"hasErrors":true}`}, errors.New("calm validate failed")
		}),
	}, nil))
	defer server.Close()

	first := postCheckForLanguage(t, server.URL, repo, "src/Warmup.cs", "csharp", cleanCSharpSource())
	if first.Status != StatusPass || !first.Warming {
		t.Fatalf("first response = %+v, want deferred warming pass", first)
	}
	responseCh := make(chan checkResult, 1)
	go func() {
		response, err := postCheckForLanguageResult(server.URL, repo, "src/Widget.cs", "csharp", complexCSharpSource())
		responseCh <- checkResult{response: response, err: err}
	}()
	select {
	case <-secondStarted:
		t.Fatal("second cold C# call entered analyzer before deferred warm-up held the repo lock")
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first deferred C# analysis did not start")
	}
	select {
	case result := <-responseCh:
		if result.err != nil {
			t.Fatalf("second cold C# call failed: %v", result.err)
		}
		t.Fatalf("second cold C# call returned before analyzer completed: %+v", result.response)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseFirst)
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second cold C# call did not enter synchronous analyzer")
	}
	close(releaseSecond)
	result := <-responseCh
	if result.err != nil {
		t.Fatalf("second cold C# call failed: %v", result.err)
	}
	response := result.response
	if response.Status != StatusBlock || response.Warming || len(response.Violations) != 1 || response.Violations[0].Function != "Render" {
		t.Fatalf("second response = %+v, want synchronous C# block while warmup is running", response)
	}
}

func TestHandlerCheckCSharpWarmPathIsSynchronous(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	state := NewState()
	firstAnalyzed := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseSecond := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		State:       state,
		Analyzers: map[string]SourceAnalyzer{
			"csharp": AnalyzerFunc(func(ctx context.Context, request AnalysisRequest) (analyzer.AnalysisResult, error) {
				call := calls.Add(1)
				switch call {
				case 1:
					close(firstAnalyzed)
					return analyzer.AnalysisResult{Language: "csharp"}, nil
				case 2:
					close(secondStarted)
					select {
					case <-releaseSecond:
					case <-ctx.Done():
						return analyzer.AnalysisResult{}, ctx.Err()
					}
					return csharpAnalysisWithComplexFunction("Render", 10), nil
				default:
					return analyzer.AnalysisResult{Language: "csharp"}, nil
				}
			}),
		},
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: false, Output: `{"hasErrors":true}`}, errors.New("calm validate failed")
		}),
	}, nil))
	defer server.Close()

	first := postCheckForLanguage(t, server.URL, repo, "src/Warmup.cs", "csharp", cleanCSharpSource())
	if first.Status != StatusPass || !first.Warming {
		t.Fatalf("first response = %+v, want deferred warming pass", first)
	}
	select {
	case <-firstAnalyzed:
	case <-time.After(time.Second):
		t.Fatal("first C# analysis did not run")
	}
	waitForWarmLanguage(t, state, "csharp")

	responseCh := make(chan checkResult, 1)
	go func() {
		response, err := postCheckForLanguageResult(server.URL, repo, "src/Widget.cs", "csharp", complexCSharpSource())
		responseCh <- checkResult{response: response, err: err}
	}()
	select {
	case result := <-responseCh:
		if result.err != nil {
			t.Fatalf("warm C# path failed: %v", result.err)
		}
		t.Fatalf("warm C# path returned before analyzer completed: %+v", result.response)
	case <-secondStarted:
	}
	close(releaseSecond)
	result := <-responseCh
	if result.err != nil {
		t.Fatalf("warm C# path failed: %v", result.err)
	}
	response := result.response
	if response.Status != StatusBlock || response.Warming || len(response.Violations) != 1 || response.Violations[0].Function != "Render" {
		t.Fatalf("warm response = %+v, want synchronous block", response)
	}
}

func TestHandlerCheckRoutesViolationsByEnforcementMode(t *testing.T) {
	tests := []struct {
		name string
		mode EnforcementMode
		want CheckStatus
	}{
		{name: "block", mode: EnforcementBlock, want: StatusBlock},
		{name: "advisory", mode: EnforcementAdvisory, want: StatusAdvisory},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			writeRepoConfig(t, repo, tt.mode, map[string]bool{"cyclomatic-complexity": true})
			server := httptest.NewServer(NewHandlerWithChecker(Checker{
				PatternPath: writeTestPattern(t),
				Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
					return calm.ValidationResult{Valid: false, Output: `{"hasErrors":true}`}, errors.New("calm validate failed")
				}),
			}, nil))
			defer server.Close()

			body := postCheck(t, server.URL, repo, "internal/parser/parser.go", complexGoSource())
			if body.Status != tt.want || len(body.Violations) != 1 {
				t.Fatalf("response = %+v, want %s with one violation", body, tt.want)
			}
		})
	}
}

func TestHandlerCheckOffModeSkipsAnalysis(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementOff, map[string]bool{"cyclomatic-complexity": true})
	called := false
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: "/does/not/exist.json",
		Analyzers: map[string]SourceAnalyzer{
			"go": AnalyzerFunc(func(context.Context, AnalysisRequest) (analyzer.AnalysisResult, error) {
				called = true
				return analyzer.AnalysisResult{}, nil
			}),
		},
	}, nil))
	defer server.Close()

	body := postCheck(t, server.URL, repo, "internal/parser/parser.go", complexGoSource())
	if body.Status != StatusPass || called {
		t.Fatalf("response = %+v called = %v, want pass without analysis", body, called)
	}
}

func TestHandlerCheckAccumulatesOutstandingViolationsUntilFilePasses(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: false, Output: `{"hasErrors":true}`}, errors.New("calm validate failed")
		}),
	}, nil))
	defer server.Close()

	first := postCheck(t, server.URL, repo, "internal/parser/parser.go", complexGoSource())
	if first.Status != StatusBlock || len(first.Violations) != 1 || first.Violations[0].File != "internal/parser/parser.go" {
		t.Fatalf("first response = %+v, want stored parser violation", first)
	}
	second := postCheck(t, server.URL, repo, "internal/other/other.go", cleanGoSource())
	if second.Status != StatusBlock || len(second.Violations) != 1 || second.Violations[0].File != "internal/parser/parser.go" {
		t.Fatalf("second response = %+v, want outstanding parser violation", second)
	}
	cleared := postCheck(t, server.URL, repo, "internal/parser/parser.go", cleanGoSource())
	if cleared.Status != StatusPass || len(cleared.Violations) != 0 {
		t.Fatalf("cleared response = %+v, want pass after offending file passes", cleared)
	}
	state := getState(t, server.URL, repo)
	if len(state.Violations) != 0 {
		t.Fatalf("state = %+v, want no outstanding violations", state)
	}
}

func TestHandlerCheckAccumulatesMultipleOutstandingViolationsAndClearsIndependently(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: false, Output: `{"hasErrors":true}`}, errors.New("calm validate failed")
		}),
	}, nil))
	defer server.Close()

	first := postCheck(t, server.URL, repo, "internal/parser/parser.go", complexGoSource())
	if first.Status != StatusBlock || len(first.Violations) != 1 {
		t.Fatalf("first response = %+v, want one violation", first)
	}
	second := postCheck(t, server.URL, repo, "internal/other/other.go", complexGoSource())
	if second.Status != StatusBlock || len(second.Violations) != 2 {
		t.Fatalf("second response = %+v, want two accumulated violations", second)
	}
	state := getState(t, server.URL, repo)
	if len(state.Violations) != 2 {
		t.Fatalf("state = %+v, want two outstanding violations", state)
	}
	stillBlocked := postCheck(t, server.URL, repo, "internal/parser/parser.go", cleanGoSource())
	if stillBlocked.Status != StatusBlock || len(stillBlocked.Violations) != 1 || stillBlocked.Violations[0].File != "internal/other/other.go" {
		t.Fatalf("stillBlocked response = %+v, want remaining other.go violation", stillBlocked)
	}
	cleared := postCheck(t, server.URL, repo, "internal/other/other.go", cleanGoSource())
	if cleared.Status != StatusPass || len(cleared.Violations) != 0 {
		t.Fatalf("cleared response = %+v, want pass after both files pass", cleared)
	}
}

func TestHandlerCheckClearsBlockStateWhenModeChangesToAdvisoryOrOff(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: false, Output: `{"hasErrors":true}`}, errors.New("calm validate failed")
		}),
	}, nil))
	defer server.Close()

	_ = postCheck(t, server.URL, repo, "internal/parser/parser.go", complexGoSource())
	writeRepoConfig(t, repo, EnforcementAdvisory, map[string]bool{"cyclomatic-complexity": true})
	advisory := postCheck(t, server.URL, repo, "internal/parser/parser.go", cleanGoSource())
	if advisory.Status != StatusPass {
		t.Fatalf("advisory clean response = %+v, want pass and stale state cleared", advisory)
	}
	if state := getState(t, server.URL, repo); len(state.Violations) != 0 {
		t.Fatalf("state after advisory = %+v, want stale state cleared", state)
	}
	_ = postCheck(t, server.URL, repo, "internal/parser/parser.go", complexGoSource())
	writeRepoConfig(t, repo, EnforcementOff, map[string]bool{"cyclomatic-complexity": true})
	off := postCheck(t, server.URL, repo, "internal/parser/parser.go", complexGoSource())
	if off.Status != StatusPass {
		t.Fatalf("off response = %+v, want pass", off)
	}
	if state := getState(t, server.URL, repo); len(state.Violations) != 0 {
		t.Fatalf("state after off = %+v, want stale state cleared", state)
	}
}

func TestHandlerCheckUsesCanonicalRepoPathForOutstandingState(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: false, Output: `{"hasErrors":true}`}, errors.New("calm validate failed")
		}),
	}, nil))
	defer server.Close()

	dirtyRepoPath := filepath.Join(repo, ".")
	first := postCheck(t, server.URL, dirtyRepoPath, "internal/parser/parser.go", complexGoSource())
	if first.Status != StatusBlock || len(first.Violations) != 1 {
		t.Fatalf("first response = %+v, want block", first)
	}
	second := postCheck(t, server.URL, repo, "internal/other/other.go", cleanGoSource())
	if second.Status != StatusBlock || len(second.Violations) != 1 || second.Violations[0].File != "internal/parser/parser.go" {
		t.Fatalf("second response = %+v, want canonical outstanding parser violation", second)
	}
	state := getState(t, server.URL, dirtyRepoPath)
	if state.Repo != repo || len(state.Violations) != 1 || state.Violations[0].File != "internal/parser/parser.go" {
		t.Fatalf("state = %+v, want canonical parser violation", state)
	}
}

func TestCheckerDirectUsagePreservesStateAcrossCalls(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	checker := Checker{
		PatternPath: writeTestPattern(t),
		State:       NewState(),
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: false, Output: `{"hasErrors":true}`}, errors.New("calm validate failed")
		}),
	}

	first, err := checker.Check(context.Background(), CheckRequest{
		Repo:            repo,
		File:            "internal/parser/parser.go",
		Language:        "go",
		ProposedContent: complexGoSource(),
	})
	if err != nil {
		t.Fatalf("first check: %v", err)
	}
	if first.Status != StatusBlock || len(first.Violations) != 1 {
		t.Fatalf("first response = %+v, want one violation", first)
	}
	second, err := checker.Check(context.Background(), CheckRequest{
		Repo:            repo,
		File:            "internal/other/other.go",
		Language:        "go",
		ProposedContent: cleanGoSource(),
	})
	if err != nil {
		t.Fatalf("second check: %v", err)
	}
	if second.Status != StatusBlock || len(second.Violations) != 1 || second.Violations[0].File != "internal/parser/parser.go" {
		t.Fatalf("second response = %+v, want preserved parser violation", second)
	}
}

func TestCheckerDirectUsageRequiresConfiguredState(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	checker := Checker{PatternPath: writeTestPattern(t)}

	_, err := checker.Check(context.Background(), CheckRequest{
		Repo:            repo,
		File:            "internal/parser/parser.go",
		Language:        "go",
		ProposedContent: cleanGoSource(),
	})
	if err == nil || !strings.Contains(err.Error(), "checker state is not configured") {
		t.Fatalf("error = %v, want configured-state error", err)
	}
}

func TestCheckerDirectUsageInitializesStateOnceForConcurrentCalls(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	checker := Checker{
		PatternPath: writeTestPattern(t),
		State:       NewState(),
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: false, Output: `{"hasErrors":true}`}, errors.New("calm validate failed")
		}),
	}
	errs := make(chan error, 2)
	for _, file := range []string{"internal/parser/parser.go", "internal/other/other.go"} {
		file := file
		go func() {
			_, err := checker.Check(context.Background(), CheckRequest{
				Repo:            repo,
				File:            file,
				Language:        "go",
				ProposedContent: complexGoSource(),
			})
			errs <- err
		}()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("check returned error: %v", err)
		}
	}
	if state := checker.State.Violations(repo); len(state) != 2 {
		t.Fatalf("state = %+v, want two violations from concurrent checks", state)
	}
}

func TestCheckerCheckRespectsCancellationWhileWaitingForRepoLock(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	checker := Checker{
		PatternPath: writeTestPattern(t),
		State:       NewState(),
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: true, Output: `{"hasErrors":false}`}, nil
		}),
	}
	unlock, err := checker.State.LockRepo(context.Background(), repo)
	if err != nil {
		t.Fatalf("lock repo: %v", err)
	}
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = checker.Check(ctx, CheckRequest{
		Repo:            repo,
		File:            "internal/parser/parser.go",
		Language:        "go",
		ProposedContent: cleanGoSource(),
	})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled while waiting for repo lock", err)
	}
}

func TestStateLockRepoReleasesUnusedLockEntries(t *testing.T) {
	state := NewState()
	repo := t.TempDir()

	unlock, err := state.LockRepo(context.Background(), repo)
	if err != nil {
		t.Fatalf("lock repo: %v", err)
	}
	unlock()

	state.mu.RLock()
	lockCount := len(state.repoLocks)
	state.mu.RUnlock()
	if lockCount != 0 {
		t.Fatalf("repo lock count = %d, want released lock removed", lockCount)
	}
}

func TestHandlerStateReturnsOutstandingViolationsForRepo(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: false, Output: `{"hasErrors":true}`}, errors.New("calm validate failed")
		}),
	}, nil))
	defer server.Close()

	_ = postCheck(t, server.URL, repo, "internal/parser/parser.go", complexGoSource())
	state := getState(t, server.URL, repo)
	if state.Repo != repo || len(state.Violations) != 1 || state.Violations[0].Function != "Parse" {
		t.Fatalf("state = %+v, want Parse violation for repo", state)
	}
}

func TestHandlerCheckRespectsDisabledFitnessFunctions(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": false})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: false, Output: `{"hasErrors":true}`}, errors.New("calm validate failed")
		}),
	}, nil))
	defer server.Close()

	body := postCheck(t, server.URL, repo, "internal/parser/parser.go", complexGoSource())
	if body.Status != StatusPass || len(body.Violations) != 0 {
		t.Fatalf("response = %+v, want pass when cyclomatic complexity disabled", body)
	}
}

func TestHandlerCheckDefaultsMissingFitnessFunctionKeysToEnabled(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: false, Output: `{"hasErrors":true}`}, errors.New("calm validate failed")
		}),
	}, nil))
	defer server.Close()

	body := postCheck(t, server.URL, repo, "internal/parser/parser.go", complexGoSource())
	if body.Status != StatusBlock || len(body.Violations) != 1 {
		t.Fatalf("response = %+v, want block when fitness map is empty", body)
	}
}

func TestHandlerCheckRejectsUnknownFitnessFunctionKeys(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexityy": true})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": `+jsonString(repo)+`,
		"file": "internal/parser/parser.go",
		"language": "go",
		"proposed_content": "package parser\n"
	}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.StatusCode)
	}
}

func TestHandlerCheckRejectsInvalidRepositoryPath(t *testing.T) {
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": "",
		"file": "internal/parser/parser.go",
		"language": "go",
		"proposed_content": "package parser\n"
	}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.StatusCode)
	}
}

func TestHandlerStateRequiresRepo(t *testing.T) {
	server := httptest.NewServer(NewHandlerWithChecker(Checker{}, nil))
	defer server.Close()

	response, err := http.Get(server.URL + "/state")
	if err != nil {
		t.Fatalf("GET /state: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.StatusCode)
	}
}

func writeTestPattern(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "governance.json")
	content, err := os.ReadFile(filepath.Join("..", "..", "patterns", "governance.json"))
	if err != nil {
		t.Fatalf("read governance pattern: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write pattern: %v", err)
	}
	return path
}

func jsonString(value string) string {
	content, _ := json.Marshal(value)
	return string(content)
}

func writeRepoConfig(t *testing.T, repo string, mode EnforcementMode, fitness map[string]bool) {
	t.Helper()
	dir := filepath.Join(repo, ".calm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	content, err := json.Marshal(Config{EnforcementMode: mode, FitnessFunctions: fitness})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func postCheck(t *testing.T, serverURL, repo, file, source string) CheckResponse {
	t.Helper()
	return postCheckForLanguage(t, serverURL, repo, file, "go", source)
}

func postCheckForLanguage(t *testing.T, serverURL, repo, file, language, source string) CheckResponse {
	t.Helper()
	body, err := postCheckForLanguageResult(serverURL, repo, file, language, source)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

type checkResult struct {
	response CheckResponse
	err      error
}

func postCheckForLanguageResult(serverURL, repo, file, language, source string) (CheckResponse, error) {
	response, err := http.Post(serverURL+"/check", "application/json", strings.NewReader(`{
		"repo": `+jsonString(repo)+`,
		"file": `+jsonString(file)+`,
		"language": `+jsonString(language)+`,
		"proposed_content": `+jsonString(source)+`
	}`))
	if err != nil {
		return CheckResponse{}, fmt.Errorf("POST /check: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return CheckResponse{}, fmt.Errorf("status = %d body = %q, want 200", response.StatusCode, body)
	}
	var body CheckResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return CheckResponse{}, fmt.Errorf("decode response: %w", err)
	}
	return body, nil
}

func getState(t *testing.T, serverURL, repo string) StateResponse {
	t.Helper()
	response, err := http.Get(serverURL + "/state?repo=" + url.QueryEscape(repo))
	if err != nil {
		t.Fatalf("GET /state: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	var state StateResponse
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	return state
}

func waitForStateViolations(t *testing.T, serverURL, repo string, count int) []Violation {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state := getState(t, serverURL, repo)
		if len(state.Violations) == count {
			return state.Violations
		}
		time.Sleep(10 * time.Millisecond)
	}
	state := getState(t, serverURL, repo)
	t.Fatalf("state violations = %+v, want %d violations", state.Violations, count)
	return nil
}

func waitForWarmLanguage(t *testing.T, state *State, language string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if state.IsWarm(language) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("language %q did not become warm", language)
}

func complexGoSource() string {
	return `package parser

func Parse(value int) string {
	if value == 0 {
		return "zero"
	}
	if value == 1 {
		return "one"
	}
	if value == 2 {
		return "two"
	}
	if value == 3 {
		return "three"
	}
	if value == 4 {
		return "four"
	}
	if value == 5 {
		return "five"
	}
	if value == 6 {
		return "six"
	}
	if value == 7 {
		return "seven"
	}
	if value == 8 {
		return "eight"
	}
	return "many"
}
`
}

func cleanGoSource() string {
	return `package parser

func Parse() error {
	return nil
}
`
}

func complexPythonSource() string {
	return `from datetime import date

def build_config(value, table_value=None, fallback=None):
    if value == "a":
        return "a"
    if value == "b":
        return "b"
    if value == "c":
        return "c"
    if value == "d":
        return "d"
    if value == "e":
        return "e"
    if value == "f":
        return "f"
    if value == "g":
        return "g"
    if value == "h":
        return "h"
    if table_value:
        return table_value
    return fallback or date.today().isoformat()
`
}

func cleanPythonSource() string {
	return `def build_config():
    return {"status": "ok"}
`
}

func complexCSharpSource() string {
	return `namespace Sample;

public class Widget
{
    public string Render(int value)
    {
        if (value == 0) return "zero";
        if (value == 1) return "one";
        if (value == 2) return "two";
        if (value == 3) return "three";
        if (value == 4) return "four";
        if (value == 5) return "five";
        if (value == 6) return "six";
        if (value == 7) return "seven";
        if (value == 8) return "eight";
        return "many";
    }
}
`
}

func cleanCSharpSource() string {
	return `namespace Sample;

public class Widget
{
    public string Render() => "ok";
}
`
}

func fakePythonAnalyzer(result analyzer.AnalysisResult) SourceAnalyzer {
	return AnalyzerFunc(func(context.Context, AnalysisRequest) (analyzer.AnalysisResult, error) {
		return result, nil
	})
}

func fakeCSharpAnalyzer(result analyzer.AnalysisResult) SourceAnalyzer {
	return AnalyzerFunc(func(context.Context, AnalysisRequest) (analyzer.AnalysisResult, error) {
		return result, nil
	})
}

func pythonAnalysisWithComplexFunction(name string, complexity int) analyzer.AnalysisResult {
	return analyzer.AnalysisResult{
		Language: "python",
		Functions: []analyzer.FunctionMetric{{
			Name:                 name,
			CyclomaticComplexity: complexity,
			IsPublic:             true,
			LOC:                  20,
		}},
		FileMetric: analyzer.FileMetric{
			TotalLOC:      20,
			LogicLOC:      12,
			PublicMethods: 1,
			LDR:           0.6,
		},
		Imports: analyzer.ImportMetric{
			Total: 1,
			Used:  1,
			DDC:   1,
		},
	}
}

func csharpAnalysisWithComplexFunction(name string, complexity int) analyzer.AnalysisResult {
	return analyzer.AnalysisResult{
		Language: "csharp",
		Functions: []analyzer.FunctionMetric{{
			Name:                 name,
			CyclomaticComplexity: complexity,
			IsPublic:             true,
			LOC:                  20,
		}},
		FileMetric: analyzer.FileMetric{
			TotalLOC:      20,
			LogicLOC:      12,
			PublicMethods: 1,
			LDR:           0.6,
		},
		Imports: analyzer.ImportMetric{
			Total: 1,
			Used:  1,
			DDC:   1,
		},
	}
}

type validatorFunc func(context.Context, string, string) (calm.ValidationResult, error)

func (f validatorFunc) Validate(ctx context.Context, architecturePath, patternPath string) (calm.ValidationResult, error) {
	return f(ctx, architecturePath, patternPath)
}

type typedNilAnalyzer struct{}

func (*typedNilAnalyzer) Analyze(context.Context, AnalysisRequest) (analyzer.AnalysisResult, error) {
	panic("typed nil analyzer should be rejected before Analyze")
}

type typedNilValidator struct{}

func (*typedNilValidator) Validate(context.Context, string, string) (calm.ValidationResult, error) {
	panic("typed nil validator should be rejected before Validate")
}
