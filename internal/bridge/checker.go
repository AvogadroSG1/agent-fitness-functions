package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/poconnor/calm-poc/internal/analyzer"
	"github.com/poconnor/calm-poc/internal/calm"
	"github.com/poconnor/calm-poc/internal/report"
)

// Validator runs CALM validation for generated architecture documents.
type Validator interface {
	Validate(context.Context, string, string) (calm.ValidationResult, error)
}

// SourceAnalyzer analyzes one temporary source file for a language.
type SourceAnalyzer interface {
	Analyze(context.Context, string) (analyzer.AnalysisResult, error)
}

// AnalyzerFunc adapts a function to SourceAnalyzer.
type AnalyzerFunc func(context.Context, string) (analyzer.AnalysisResult, error)

// Analyze implements SourceAnalyzer.
func (f AnalyzerFunc) Analyze(ctx context.Context, path string) (analyzer.AnalysisResult, error) {
	if f == nil {
		return analyzer.AnalysisResult{}, infrastructureError("source analyzer is not configured", nil)
	}
	return f(ctx, path)
}

// Checker coordinates source analysis, CALM architecture generation, and validation.
type Checker struct {
	PatternPath string
	TempDir     string
	Validator   Validator
	Analyzers   map[string]SourceAnalyzer
	State       *State
}

// ErrorKind classifies checker failures for HTTP clients.
type ErrorKind string

const (
	// ErrorKindInput means the request or proposed content is unsupported or invalid.
	ErrorKindInput ErrorKind = "input"
	// ErrorKindInfrastructure means an internal dependency failed.
	ErrorKindInfrastructure ErrorKind = "infrastructure"
)

// CheckError is a client-safe, typed checker failure.
type CheckError struct {
	Kind    ErrorKind
	Message string
	Err     error
}

func (e *CheckError) Error() string {
	if e.Err == nil {
		return e.Message
	}
	return e.Message + ": " + e.Err.Error()
}

func (e *CheckError) Unwrap() error {
	return e.Err
}

// Check runs the synchronous check path for one proposed file.
func (c Checker) Check(ctx context.Context, request CheckRequest) (response CheckResponse, err error) {
	config, err := loadConfig(request.Repo)
	if err != nil {
		return CheckResponse{}, err
	}
	state := c.State
	if state == nil {
		state = NewState()
	}
	unlockRepo := state.LockRepo(request.Repo)
	defer unlockRepo()
	if config.EnforcementMode == EnforcementOff {
		state.ClearRepo(request.Repo)
		return CheckResponse{Status: StatusPass}, nil
	}
	patternPath := c.PatternPath
	if patternPath == "" {
		patternPath = filepath.Join("patterns", "governance.json")
	}
	pattern, err := calm.LoadPattern(patternPath)
	if err != nil {
		return CheckResponse{}, infrastructureError("loading governance pattern", err)
	}
	sourcePath, cleanup, err := c.writeProposedContent(request)
	if err != nil {
		return CheckResponse{}, err
	}
	defer func() {
		if cleanupErr := cleanup(); cleanupErr != nil {
			err = errors.Join(err, infrastructureError("cleaning temporary source file", cleanupErr))
		}
	}()

	sourceAnalyzer, ok := c.sourceAnalyzer(request.Language)
	if !ok {
		return CheckResponse{}, inputError(fmt.Sprintf("unsupported language %q", request.Language), nil)
	}
	if err := ctx.Err(); err != nil {
		return CheckResponse{}, infrastructureError("check canceled before analysis", err)
	}
	result, err := sourceAnalyzer.Analyze(ctx, sourcePath)
	if err != nil {
		var checkErr *CheckError
		if errors.As(err, &checkErr) {
			return CheckResponse{}, checkErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return CheckResponse{}, infrastructureError("check canceled during analysis", err)
		}
		return CheckResponse{}, inputError(fmt.Sprintf("analyzing %s file", request.Language), err)
	}
	if err := ctx.Err(); err != nil {
		return CheckResponse{}, infrastructureError("check canceled after analysis", err)
	}
	result.File = request.File

	architecturePath, cleanupArchitecture, err := c.writeArchitecture(report.BuildArchitecture(result))
	if err != nil {
		return CheckResponse{}, err
	}
	defer func() {
		if cleanupErr := cleanupArchitecture(); cleanupErr != nil {
			err = errors.Join(err, infrastructureError("cleaning temporary architecture file", cleanupErr))
		}
	}()

	validator := c.Validator
	switch {
	case validator == nil:
		validator = calm.Validator{}
	case isNilInterface(validator):
		return CheckResponse{}, infrastructureError("CALM validator is not configured", nil)
	}
	validation, err := validator.Validate(ctx, architecturePath, patternPath)
	if err != nil && !isValidationFailure(validation) {
		return CheckResponse{}, infrastructureError("running CALM validation", err)
	}
	violations := filterViolations(cyclomaticComplexityViolations(result, pattern), config)
	if len(violations) == 0 {
		if config.EnforcementMode == EnforcementBlock {
			state.ReplaceFile(request.Repo, request.File, nil)
			outstanding := state.Violations(request.Repo)
			if len(outstanding) > 0 {
				return CheckResponse{Status: StatusBlock, Violations: outstanding}, nil
			}
		} else {
			state.ClearRepo(request.Repo)
		}
		return CheckResponse{Status: StatusPass}, nil
	}
	switch config.EnforcementMode {
	case EnforcementAdvisory:
		state.ClearRepo(request.Repo)
		return CheckResponse{Status: StatusAdvisory, Violations: violations}, nil
	default:
		state.ReplaceFile(request.Repo, request.File, violations)
		return CheckResponse{Status: StatusBlock, Violations: state.Violations(request.Repo)}, nil
	}
}

func (c Checker) writeProposedContent(request CheckRequest) (string, func() error, error) {
	extension := filepath.Ext(request.File)
	if extension == "" {
		extension = ".go"
	}
	if !validSourceExtension(extension) {
		return "", func() error { return nil }, inputError("invalid source file extension", nil)
	}
	file, err := os.CreateTemp(c.TempDir, "calm-check-*"+extension)
	if err != nil {
		return "", func() error { return nil }, infrastructureError("creating temp source file", err)
	}
	cleanup := func() error {
		return os.Remove(file.Name())
	}
	if _, err := file.WriteString(request.ProposedContent); err != nil {
		_ = file.Close()
		cleanupErr := cleanup()
		return "", func() error { return nil }, errors.Join(inputError("writing temp source file", err), cleanupErr)
	}
	if err := file.Close(); err != nil {
		cleanupErr := cleanup()
		return "", func() error { return nil }, errors.Join(infrastructureError("closing temp source file", err), cleanupErr)
	}
	return file.Name(), cleanup, nil
}

func (c Checker) writeArchitecture(document report.ArchitectureDocument) (string, func() error, error) {
	file, err := os.CreateTemp(c.TempDir, "current-architecture-*.json")
	if err != nil {
		return "", func() error { return nil }, infrastructureError("creating temp architecture file", err)
	}
	cleanup := func() error {
		return os.Remove(file.Name())
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		_ = file.Close()
		cleanupErr := cleanup()
		return "", func() error { return nil }, errors.Join(infrastructureError("writing architecture file", err), cleanupErr)
	}
	if err := file.Close(); err != nil {
		cleanupErr := cleanup()
		return "", func() error { return nil }, errors.Join(infrastructureError("closing architecture file", err), cleanupErr)
	}
	return file.Name(), cleanup, nil
}

func cyclomaticComplexityViolations(result analyzer.AnalysisResult, pattern calm.Pattern) []Violation {
	rule, ok := pattern.FitnessFunctions["cyclomatic-complexity"]
	if !ok || rule.Operator != "lte" {
		return nil
	}
	violations := make([]Violation, 0)
	for _, function := range result.Functions {
		value := float64(function.CyclomaticComplexity)
		if value <= rule.Threshold {
			continue
		}
		violations = append(violations, Violation{
			FitnessFunction: "cyclomatic_complexity",
			CALMNode:        result.CALMNode,
			File:            result.File,
			Function:        function.Name,
			Value:           value,
			Limit:           rule.Threshold,
			Message: fmt.Sprintf(
				"Function %q in module %q has cyclomatic complexity %d, exceeding the limit of %.0f. Extract conditional branches into separate functions.",
				function.Name,
				result.CALMNode,
				function.CyclomaticComplexity,
				rule.Threshold,
			),
		})
	}
	return violations
}

func filterViolations(violations []Violation, config Config) []Violation {
	filtered := make([]Violation, 0, len(violations))
	for _, violation := range violations {
		if !config.enabled(strings.ReplaceAll(violation.FitnessFunction, "_", "-")) {
			continue
		}
		filtered = append(filtered, violation)
	}
	return filtered
}

func (c Checker) sourceAnalyzer(language string) (SourceAnalyzer, bool) {
	if sourceAnalyzer, ok := c.Analyzers[language]; ok {
		if isNilSourceAnalyzer(sourceAnalyzer) {
			return nil, false
		}
		return sourceAnalyzer, true
	}
	if language == "go" {
		return AnalyzerFunc(func(ctx context.Context, path string) (analyzer.AnalysisResult, error) {
			if err := ctx.Err(); err != nil {
				return analyzer.AnalysisResult{}, err
			}
			result, err := analyzer.AnalyzeGoFile(path)
			if err != nil {
				return analyzer.AnalysisResult{}, err
			}
			if err := ctx.Err(); err != nil {
				return analyzer.AnalysisResult{}, err
			}
			return result, nil
		}), true
	}
	return nil, false
}

func isNilSourceAnalyzer(sourceAnalyzer SourceAnalyzer) bool {
	return isNilInterface(sourceAnalyzer)
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func validSourceExtension(extension string) bool {
	if len(extension) > 16 || strings.ContainsRune(extension, 0) {
		return false
	}
	for _, char := range extension {
		if char == '.' || char == '_' || char == '-' || char >= '0' && char <= '9' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' {
			continue
		}
		return false
	}
	return true
}

func isValidationFailure(result calm.ValidationResult) bool {
	return !result.Valid && (strings.Contains(result.Output, `"hasErrors": true`) || strings.Contains(result.Output, `"hasErrors":true`))
}

func inputError(message string, err error) error {
	return &CheckError{Kind: ErrorKindInput, Message: message, Err: err}
}

func infrastructureError(message string, err error) error {
	return &CheckError{Kind: ErrorKindInfrastructure, Message: message, Err: err}
}
