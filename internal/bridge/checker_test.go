package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/poconnor/calm-poc/internal/analyzer"
	"github.com/poconnor/calm-poc/internal/calm"
)

func TestHandlerCheckRunsGoAnalyzerCALMAndBlocksCyclomaticComplexityViolation(t *testing.T) {
	repo := t.TempDir()
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
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		TempDir:     tempDir,
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: true, Output: `{"hasErrors":false}`}, nil
		}),
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": "/tmp/repo",
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
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: false, Output: ""}, errors.New("calm executable missing")
		}),
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": "/tmp/repo",
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
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": "/tmp/repo",
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
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": "/tmp/repo",
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
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Analyzers:   map[string]SourceAnalyzer{"go": AnalyzerFunc(nil)},
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": "/tmp/repo",
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
	var typedNil *typedNilAnalyzer
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Analyzers:   map[string]SourceAnalyzer{"go": typedNil},
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": "/tmp/repo",
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
		"repo": "/tmp/repo",
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
	var typedNil *typedNilValidator
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: writeTestPattern(t),
		Validator:   typedNil,
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": "/tmp/repo",
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
	patternPath := writeTestPattern(t)
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		PatternPath: patternPath,
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: true, Output: `{"hasErrors":false}`}, nil
		}),
	}, nil))
	defer server.Close()

	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": "/tmp/repo",
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
