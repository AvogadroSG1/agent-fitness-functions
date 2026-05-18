package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
			"go": AnalyzerFunc(func(context.Context, string) (analyzer.AnalysisResult, error) {
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
			"go": AnalyzerFunc(func(context.Context, string) (analyzer.AnalysisResult, error) {
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

func TestCheckerDirectUsageInitializesStateOnceForConcurrentCalls(t *testing.T) {
	repo := t.TempDir()
	writeRepoConfig(t, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	checker := Checker{
		PatternPath: writeTestPattern(t),
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
	response, err := http.Post(serverURL+"/check", "application/json", strings.NewReader(`{
		"repo": `+jsonString(repo)+`,
		"file": `+jsonString(file)+`,
		"language": "go",
		"proposed_content": `+jsonString(source)+`
	}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d body = %q, want 200", response.StatusCode, body)
	}
	var body CheckResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body
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

type validatorFunc func(context.Context, string, string) (calm.ValidationResult, error)

func (f validatorFunc) Validate(ctx context.Context, architecturePath, patternPath string) (calm.ValidationResult, error) {
	return f(ctx, architecturePath, patternPath)
}

type typedNilAnalyzer struct{}

func (*typedNilAnalyzer) Analyze(context.Context, string) (analyzer.AnalysisResult, error) {
	panic("typed nil analyzer should be rejected before Analyze")
}

type typedNilValidator struct{}

func (*typedNilValidator) Validate(context.Context, string, string) (calm.ValidationResult, error) {
	panic("typed nil validator should be rejected before Validate")
}
