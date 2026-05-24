package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strings"
	"time"

	"github.com/poconnor/calm-poc/internal/analyzer"
	"github.com/poconnor/calm-poc/internal/calm"
	"github.com/poconnor/calm-poc/internal/report"
	"github.com/poconnor/calm-poc/patterns"
)

// Validator runs CALM validation for generated architecture documents.
type Validator interface {
	Validate(context.Context, string, string) (calm.ValidationResult, error)
}

// AnalysisRequest describes one temporary source analysis job.
type AnalysisRequest struct {
	Repo     string
	File     string
	Language string
	TempPath string
}

// SourceAnalyzer analyzes one temporary source file for a language.
type SourceAnalyzer interface {
	Analyze(context.Context, AnalysisRequest) (analyzer.AnalysisResult, error)
}

// AnalyzerFunc adapts a function to SourceAnalyzer.
type AnalyzerFunc func(context.Context, AnalysisRequest) (analyzer.AnalysisResult, error)

// Analyze implements SourceAnalyzer.
func (f AnalyzerFunc) Analyze(ctx context.Context, request AnalysisRequest) (analyzer.AnalysisResult, error) {
	if f == nil {
		return analyzer.AnalysisResult{}, infrastructureError("source analyzer is not configured", nil)
	}
	return f(ctx, request)
}

// Checker coordinates source analysis, CALM architecture generation, and validation.
type Checker struct {
	PatternPath     string
	TempDir         string
	Validator       Validator
	Analyzers       map[string]SourceAnalyzer
	State           *State
	DeferredContext context.Context
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
func (c *Checker) Check(ctx context.Context, request CheckRequest) (response CheckResponse, err error) {
	if c.State == nil {
		return CheckResponse{}, infrastructureError("checker state is not configured", nil)
	}
	config, repo, err := loadConfig(request.Repo)
	if err != nil {
		return CheckResponse{}, err
	}
	state := c.state()
	if config.EnforcementMode == EnforcementOff {
		state.ClearRepo(repo)
		return CheckResponse{Status: StatusPass}, nil
	}
	if request.Language == "csharp" && config.EnforcementMode == EnforcementBlock {
		if message, ok := state.TakeWarmupFailure("csharp"); ok {
			return CheckResponse{}, infrastructureError("csharp warm-up failed", errors.New(message))
		}
		if outstanding := state.Violations(repo); hasOtherFileViolation(outstanding, request.File) {
			return CheckResponse{Status: StatusBlock, Violations: outstanding}, nil
		}
	}
	if request.Language == "csharp" && config.EnforcementMode == EnforcementBlock && !state.IsWarm("csharp") {
		unlockRepo, err := state.LockRepo(ctx, repo)
		if err != nil {
			return CheckResponse{}, infrastructureError("check canceled while waiting for repository lock", err)
		}
		if state.IsWarm("csharp") {
			defer unlockRepo()
			return c.checkSynchronousLocked(ctx, request, repo, config, state)
		}
		if message, ok := state.TakeWarmupFailure("csharp"); ok {
			unlockRepo()
			return CheckResponse{}, infrastructureError("csharp warm-up failed", errors.New(message))
		}
		if outstanding := state.Violations(repo); hasOtherFileViolation(outstanding, request.File) {
			unlockRepo()
			return CheckResponse{Status: StatusBlock, Violations: outstanding}, nil
		}
		if state.BeginWarmup("csharp") {
			c.startDeferredCheck(request, repo, config, unlockRepo)
			return CheckResponse{Status: StatusPass, Warming: true}, nil
		}
		defer unlockRepo()
		return c.checkSynchronousLocked(ctx, request, repo, config, state)
	}
	return c.checkSynchronous(ctx, request, repo, config, state)
}

func (c *Checker) checkSynchronous(ctx context.Context, request CheckRequest, repo string, config Config, state *State) (response CheckResponse, err error) {
	unlockRepo, err := state.LockRepo(ctx, repo)
	if err != nil {
		return CheckResponse{}, infrastructureError("check canceled while waiting for repository lock", err)
	}
	defer unlockRepo()
	return c.checkSynchronousLocked(ctx, request, repo, config, state)
}

func (c *Checker) checkSynchronousLocked(ctx context.Context, request CheckRequest, repo string, config Config, state *State) (response CheckResponse, err error) {
	patternPath, cleanupPattern, err := c.resolvePatternPath()
	if err != nil {
		return CheckResponse{}, err
	}
	defer func() {
		if cleanupErr := cleanupPattern(); cleanupErr != nil {
			err = errors.Join(err, infrastructureError("cleaning temp pattern file", cleanupErr))
		}
	}()
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
	result, err := sourceAnalyzer.Analyze(ctx, AnalysisRequest{
		Repo:     repo,
		File:     request.File,
		Language: request.Language,
		TempPath: sourcePath,
	})
	if err != nil {
		var checkErr *CheckError
		if errors.As(err, &checkErr) {
			return CheckResponse{}, checkErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return CheckResponse{}, infrastructureError("check canceled during analysis", err)
		}
		if isAnalyzerInfrastructureError(err) {
			return CheckResponse{}, infrastructureError(fmt.Sprintf("running %s analyzer", request.Language), err)
		}
		return CheckResponse{}, inputError(fmt.Sprintf("analyzing %s file", request.Language), err)
	}
	if err := ctx.Err(); err != nil {
		return CheckResponse{}, infrastructureError("check canceled after analysis", err)
	}
	result = analyzer.EnsureModuleMetric(result)
	result.File = request.File
	result.CALMNode = calmNodeForRequest(request, result.CALMNode)

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
	violations := filterViolations(fitnessViolations(result, pattern), config)
	if len(violations) == 0 {
		if config.EnforcementMode == EnforcementBlock {
			state.ReplaceFile(repo, request.File, nil)
			outstanding := state.Violations(repo)
			if len(outstanding) > 0 {
				return CheckResponse{Status: StatusBlock, Violations: outstanding}, nil
			}
		} else {
			state.ClearRepo(repo)
		}
		return CheckResponse{Status: StatusPass}, nil
	}
	switch config.EnforcementMode {
	case EnforcementAdvisory:
		state.ClearRepo(repo)
		return CheckResponse{Status: StatusAdvisory, Violations: violations}, nil
	default:
		state.ReplaceFile(repo, request.File, violations)
		return CheckResponse{Status: StatusBlock, Violations: state.Violations(repo)}, nil
	}
}

func (c *Checker) state() *State {
	return c.State
}

func (c *Checker) startDeferredCheck(request CheckRequest, repo string, config Config, unlockRepo func()) {
	checker := *c
	go func() {
		defer unlockRepo()
		defer func() {
			if recovered := recover(); recovered != nil {
				checker.State.FailWarmup("csharp", fmt.Sprintf("panic during deferred csharp check: %v\n%s", recovered, debug.Stack()))
			}
		}()
		ctx := checker.DeferredContext
		if ctx == nil {
			ctx = context.Background()
		}
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		_, err := checker.checkSynchronousLocked(ctx, request, repo, config, checker.State)
		if err != nil {
			checker.State.FailWarmup("csharp", err.Error())
			return
		}
		checker.State.CompleteWarmup("csharp")
	}()
}

func hasOtherFileViolation(violations []Violation, file string) bool {
	for _, violation := range violations {
		if violation.File != file {
			return true
		}
	}
	return false
}

func isAnalyzerInfrastructureError(err error) bool {
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return true
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "running Roslyn analyzer") || strings.Contains(message, "parsing Roslyn analyzer output")
}

// resolvePatternPath returns the path to the governance pattern. When PatternPath is not set it
// writes the embedded default to a temp file. The caller must invoke the returned cleanup function.
func (c Checker) resolvePatternPath() (string, func() error, error) {
	noop := func() error { return nil }
	if c.PatternPath != "" {
		return c.PatternPath, noop, nil
	}
	f, err := os.CreateTemp(c.TempDir, "calm-pattern-*.json")
	if err != nil {
		return "", noop, infrastructureError("creating temp pattern file", err)
	}
	cleanup := func() error { return os.Remove(f.Name()) }
	if _, err := f.Write(patterns.GovernanceJSON); err != nil {
		_ = f.Close()
		_ = cleanup()
		return "", noop, infrastructureError("writing temp pattern file", err)
	}
	if err := f.Close(); err != nil {
		_ = cleanup()
		return "", noop, infrastructureError("closing temp pattern file", err)
	}
	return f.Name(), cleanup, nil
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

func fitnessViolations(result analyzer.AnalysisResult, pattern calm.Pattern) []Violation {
	violations := make([]Violation, 0)
	violations = append(violations, cyclomaticComplexityViolations(result, pattern)...)
	violations = append(violations, interfaceWidthViolations(result, pattern)...)
	violations = append(violations, implementationDepthViolations(result, pattern)...)
	violations = append(violations, logicDensityViolations(result, pattern)...)
	violations = append(violations, dependencyDisciplineViolations(result, pattern)...)
	return violations
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

func interfaceWidthViolations(result analyzer.AnalysisResult, pattern calm.Pattern) []Violation {
	rule, ok := pattern.FitnessFunctions["interface-width"]
	result = analyzer.EnsureModuleMetric(result)
	if !ok || rule.Operator != "lte" || float64(result.ModuleMetric.PublicMethods) <= rule.Threshold {
		return nil
	}
	return []Violation{{
		FitnessFunction: "interface_width",
		CALMNode:        result.CALMNode,
		File:            result.File,
		Value:           float64(result.ModuleMetric.PublicMethods),
		Limit:           rule.Threshold,
		Message: fmt.Sprintf(
			"Module '%s' exposes %d public methods, exceeding the limit of %.0f. Consolidate related operations or reduce the public surface area.",
			result.CALMNode,
			result.ModuleMetric.PublicMethods,
			rule.Threshold,
		),
	}}
}

func implementationDepthViolations(result analyzer.AnalysisResult, pattern calm.Pattern) []Violation {
	rule, ok := pattern.FitnessFunctions["implementation-depth"]
	result = analyzer.EnsureModuleMetric(result)
	value := result.ModuleMetric.AverageLOCPerPublicMethod
	if !ok || rule.Operator != "gte" || result.ModuleMetric.PublicMethods == 0 || value >= rule.Threshold {
		return nil
	}
	return []Violation{{
		FitnessFunction: "implementation_depth",
		CALMNode:        result.CALMNode,
		File:            result.File,
		Value:           value,
		Limit:           rule.Threshold,
		Message: fmt.Sprintf(
			"Module '%s' averages %.3f LOC per public method, below the minimum of %.3f. Methods with little implementation may be unnecessary pass-throughs.",
			result.CALMNode,
			value,
			rule.Threshold,
		),
	}}
}

func logicDensityViolations(result analyzer.AnalysisResult, pattern calm.Pattern) []Violation {
	rule, ok := pattern.FitnessFunctions["logic-density"]
	if !ok || rule.Operator != "gte" || result.FileMetric.TotalLOC == 0 || result.FileMetric.LDR >= rule.Threshold {
		return nil
	}
	return []Violation{{
		FitnessFunction: "logic_density",
		CALMNode:        result.CALMNode,
		File:            result.File,
		Value:           result.FileMetric.LDR,
		Limit:           rule.Threshold,
		Message: fmt.Sprintf(
			"File %q has a Logic Density Ratio of %.3f (minimum: %.3f). The file may contain excessive boilerplate relative to functional logic.",
			result.File,
			result.FileMetric.LDR,
			rule.Threshold,
		),
	}}
}

func dependencyDisciplineViolations(result analyzer.AnalysisResult, pattern calm.Pattern) []Violation {
	rule, ok := pattern.FitnessFunctions["dependency-discipline"]
	if !ok || rule.Operator != "gte" || result.Imports.Total == 0 || result.Imports.DDC >= rule.Threshold {
		return nil
	}
	unused := strings.Join(result.Imports.Unused, ", ")
	if unused == "" {
		unused = "none reported"
	}
	return []Violation{{
		FitnessFunction: "dependency_discipline",
		CALMNode:        result.CALMNode,
		File:            result.File,
		Value:           result.Imports.DDC,
		Limit:           rule.Threshold,
		Message: fmt.Sprintf(
			"File %q has a Dependency Discipline ratio of %.3f (minimum: %.3f). Unused imports: [%s].",
			result.File,
			result.Imports.DDC,
			rule.Threshold,
			unused,
		),
	}}
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
		return AnalyzerFunc(func(ctx context.Context, request AnalysisRequest) (analyzer.AnalysisResult, error) {
			return analyzeGoWithModuleContext(ctx, request)
		}), true
	}
	if language == "python" {
		return AnalyzerFunc(func(ctx context.Context, request AnalysisRequest) (analyzer.AnalysisResult, error) {
			return analyzer.AnalyzePythonFile(ctx, request.TempPath, "")
		}), true
	}
	if language == "csharp" {
		return AnalyzerFunc(func(ctx context.Context, request AnalysisRequest) (analyzer.AnalysisResult, error) {
			return analyzer.AnalyzeCSharpFile(ctx, request.TempPath, "")
		}), true
	}
	return nil, false
}

func analyzeGoWithModuleContext(ctx context.Context, request AnalysisRequest) (analyzer.AnalysisResult, error) {
	if err := ctx.Err(); err != nil {
		return analyzer.AnalysisResult{}, err
	}
	proposed, err := analyzer.AnalyzeGoFile(request.TempPath)
	if err != nil {
		return analyzer.AnalysisResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return analyzer.AnalysisResult{}, err
	}
	results := []analyzer.AnalysisResult{proposed}
	logicalPath := filepath.Join(request.Repo, request.File)
	dirEntries, err := os.ReadDir(filepath.Dir(logicalPath))
	if err != nil {
		return proposed, nil
	}
	for _, entry := range dirEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_generated.go") {
			continue
		}
		path := filepath.Join(filepath.Dir(logicalPath), entry.Name())
		if filepath.Clean(path) == filepath.Clean(logicalPath) {
			continue
		}
		existing, err := analyzer.AnalyzeGoFile(path)
		if err != nil {
			return analyzer.AnalysisResult{}, err
		}
		if existing.CALMNode == proposed.CALMNode {
			results = append(results, existing)
		}
	}
	return analyzer.AggregateModuleMetrics(results)[0], nil
}

func calmNodeForRequest(request CheckRequest, fallback string) string {
	base := strings.TrimSuffix(filepath.Base(request.File), filepath.Ext(request.File))
	if fallback == "" || strings.HasPrefix(fallback, "calm-check-") || request.Language == "python" {
		if base == "" {
			return fallback
		}
		return base
	}
	if base == "" {
		return fallback
	}
	return fallback
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
