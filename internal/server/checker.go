package server

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

	"github.com/AvogadroSG1/agent-fitness-functions/internal/analyzer"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/calm"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/report"
	"github.com/AvogadroSG1/agent-fitness-functions/patterns"
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
	ConfigStore     *ConfigStore
	DeferredContext context.Context
	BlockOnWarmup   bool
	AnalyzerTimeout time.Duration
	// ConcurrencyPermits is the per-repo concurrency cap managed by the HTTP
	// handler. When > 0, the handler's external N-permit semaphore already
	// serializes concurrent analyses, so checkSynchronous skips its own lock.
	ConcurrencyPermits int
}

// ErrorKind classifies checker failures for HTTP clients.
type ErrorKind string

const (
	// ErrorKindInput means the request or proposed content is unsupported or invalid.
	ErrorKindInput ErrorKind = "input"
	// ErrorKindInfrastructure means an internal dependency failed.
	ErrorKindInfrastructure ErrorKind = "infrastructure"
	// ErrorKindNotFound means the requested repository has no mounted configuration.
	ErrorKindNotFound ErrorKind = "not_found"
	// ErrorKindTimeout means an analyzer exceeded its configured deadline.
	ErrorKindTimeout ErrorKind = "timeout"
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
func (c *Checker) Check(ctx context.Context, request fitness.ValidationRequest) (response fitness.ValidationResult, err error) {
	if c.State == nil {
		return fitness.ValidationResult{}, infrastructureError("checker state is not configured", nil)
	}
	config, repo, err := loadConfig(c.ConfigStore, request.Repo)
	if err != nil {
		return fitness.ValidationResult{}, err
	}
	if config.IsExcluded(request.File) {
		return fitness.ValidationResult{Status: fitness.StatusPass}, nil
	}
	state := c.state()
	if config.EnforcementMode == EnforcementOff {
		newViolationLedger(state, request.DryRun).ClearRepo(repo)
		return fitness.ValidationResult{Status: fitness.StatusPass}, nil
	}
	if request.Language == "csharp" && config.EnforcementMode == EnforcementBlock {
		return c.checkWithCSharpWarmGuard(ctx, request, repo, config, state)
	}
	return c.checkSynchronous(ctx, request, repo, config, state)
}

// checkWithCSharpWarmGuard handles csharp warmup sequencing before delegating
// to the synchronous check path.
func (c *Checker) checkWithCSharpWarmGuard(ctx context.Context, request fitness.ValidationRequest, repo string, config Config, state *State) (fitness.ValidationResult, error) {
	if resp, err, done := c.checkCSharpEarlyOut(request, repo, state); done {
		return resp, err
	}
	if state.IsWarm("csharp") {
		return c.checkSynchronous(ctx, request, repo, config, state)
	}
	return c.checkCSharpWithLock(ctx, request, repo, config, state)
}

func (c *Checker) checkCSharpEarlyOut(request fitness.ValidationRequest, repo string, state *State) (fitness.ValidationResult, error, bool) {
	if message, ok := state.TakeWarmupFailure("csharp"); ok {
		return fitness.ValidationResult{}, infrastructureError("csharp warm-up failed", errors.New(message)), true
	}
	if outstanding := state.Violations(repo); hasOtherFileViolation(outstanding, request.File) {
		return fitness.ValidationResult{Status: fitness.StatusBlock, Violations: outstanding}, nil, true
	}
	return fitness.ValidationResult{}, nil, false
}

func (c *Checker) checkCSharpWithLock(ctx context.Context, request fitness.ValidationRequest, repo string, config Config, state *State) (fitness.ValidationResult, error) {
	unlockRepo, err := state.LockRepo(ctx, repo)
	if err != nil {
		return fitness.ValidationResult{}, infrastructureError("check canceled while waiting for repository lock", err)
	}
	if state.IsWarm("csharp") {
		defer unlockRepo()
		return c.checkSynchronousLocked(ctx, request, repo, config, state)
	}
	if resp, err, done := c.checkCSharpEarlyOut(request, repo, state); done {
		unlockRepo()
		return resp, err
	}
	if state.BeginWarmup("csharp") {
		return c.checkCSharpWarmup(ctx, request, repo, config, state, unlockRepo)
	}
	defer unlockRepo()
	return c.checkSynchronousLocked(ctx, request, repo, config, state)
}

func (c *Checker) checkCSharpWarmup(ctx context.Context, request fitness.ValidationRequest, repo string, config Config, state *State, unlockRepo func()) (fitness.ValidationResult, error) {
	if c.BlockOnWarmup {
		defer unlockRepo()
		resp, err := c.checkSynchronousLocked(ctx, request, repo, config, state)
		if err != nil {
			state.FailWarmup("csharp", err.Error())
			return fitness.ValidationResult{}, err
		}
		state.CompleteWarmup("csharp")
		return resp, nil
	}
	c.startDeferredCheck(request, repo, config, unlockRepo)
	return fitness.ValidationResult{Status: fitness.StatusPass, Warming: true}, nil
}

// checkSynchronous acquires a per-repo lock (when no external concurrency cap is
// configured) then delegates to checkSynchronousLocked. When ConcurrencyPermits
// > 0 the handler's external N-permit semaphore is the only lock needed.
func (c *Checker) checkSynchronous(ctx context.Context, request fitness.ValidationRequest, repo string, config Config, state *State) (response fitness.ValidationResult, err error) {
	if c.ConcurrencyPermits > 0 {
		return c.checkSynchronousLocked(ctx, request, repo, config, state)
	}
	unlockRepo, err := state.LockRepo(ctx, repo)
	if err != nil {
		return fitness.ValidationResult{}, infrastructureError("check canceled while waiting for repository lock", err)
	}
	defer unlockRepo()
	return c.checkSynchronousLocked(ctx, request, repo, config, state)
}

func (c *Checker) checkSynchronousLocked(ctx context.Context, request fitness.ValidationRequest, repo string, config Config, state *State) (response fitness.ValidationResult, err error) {
	patternPath, cleanupPattern, err := c.resolvePatternPath()
	if err != nil {
		return fitness.ValidationResult{}, err
	}
	defer func() {
		if cleanupErr := cleanupPattern(); cleanupErr != nil {
			err = errors.Join(err, infrastructureError("cleaning temp pattern file", cleanupErr))
		}
	}()
	sourcePath, cleanup, err := c.writeProposedContent(request)
	if err != nil {
		return fitness.ValidationResult{}, err
	}
	defer func() {
		if cleanupErr := cleanup(); cleanupErr != nil {
			err = errors.Join(err, infrastructureError("cleaning temporary source file", cleanupErr))
		}
	}()
	result, err := c.analyzeSource(ctx, request, repo, sourcePath)
	if err != nil {
		return c.routeAnalysisError(err, config)
	}
	result = analyzer.EnsureModuleMetric(result)
	result.File = request.File
	result.CALMNode = calmNodeForRequest(request, result.CALMNode)
	return c.runValidationAndScore(ctx, result, repo, request, patternPath, config, state)
}

// routeAnalysisError applies enforcement-on-error policy for non-input failures.
// Input errors are always returned as-is. For infrastructure/timeout failures,
// advisory and pass modes produce synthetic responses; block mode propagates the
// original error preserving its Kind for HTTP status mapping.
func (c *Checker) routeAnalysisError(err error, config Config) (fitness.ValidationResult, error) {
	var checkErr *CheckError
	if !errors.As(err, &checkErr) {
		return fitness.ValidationResult{}, err
	}
	if checkErr.Kind == ErrorKindInput {
		return fitness.ValidationResult{}, err
	}
	switch config.EnforcementOnError {
	case EnforcementOnErrorAdvisory:
		return fitness.ValidationResult{Status: fitness.StatusAdvisory}, nil
	case EnforcementOnErrorPass:
		return fitness.ValidationResult{Status: fitness.StatusPass}, nil
	default:
		return fitness.ValidationResult{}, err
	}
}

// analyzeSource runs the language-specific analyzer for the proposed file content.
func (c *Checker) analyzeSource(ctx context.Context, request fitness.ValidationRequest, repo, sourcePath string) (analyzer.AnalysisResult, error) {
	sourceAnalyzer, ok := c.sourceAnalyzer(request.Language)
	if !ok {
		return analyzer.AnalysisResult{}, inputError(fmt.Sprintf("unsupported language %q", request.Language), nil)
	}
	if err := ctx.Err(); err != nil {
		return analyzer.AnalysisResult{}, infrastructureError("check canceled before analysis", err)
	}

	analyzeCtx := ctx
	var cancel context.CancelFunc
	if c.AnalyzerTimeout > 0 {
		analyzeCtx, cancel = context.WithTimeout(ctx, c.AnalyzerTimeout)
		defer cancel()
	}

	result, err := sourceAnalyzer.Analyze(analyzeCtx, AnalysisRequest{
		Repo:     repo,
		File:     request.File,
		Language: request.Language,
		TempPath: sourcePath,
	})
	if err != nil {
		return analyzer.AnalysisResult{}, classifyAnalysisError(err, request.Language)
	}
	if err := analyzeCtx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return analyzer.AnalysisResult{}, timeoutError("analyzer timed out", err)
		}
		return analyzer.AnalysisResult{}, infrastructureError("check canceled after analysis", err)
	}
	return result, nil
}

func classifyAnalysisError(err error, language string) error {
	var checkErr *CheckError
	if errors.As(err, &checkErr) {
		return checkErr
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return timeoutError("analyzer timed out", err)
	}
	if errors.Is(err, context.Canceled) {
		return infrastructureError("check canceled during analysis", err)
	}
	if isAnalyzerInfrastructureError(err) {
		return infrastructureError(fmt.Sprintf("running %s analyzer", language), err)
	}
	return inputError(fmt.Sprintf("analyzing %s file", language), err)
}

// runValidationAndScore runs CALM validation and fitness scoring on an analyzed
// result. Content-scored fitness functions are scored first so their counts
// reach the generated architecture document.
func (c *Checker) runValidationAndScore(ctx context.Context, result analyzer.AnalysisResult, repo string, request fitness.ValidationRequest, patternPath string, config Config, state *State) (resp fitness.ValidationResult, err error) {
	pattern, err := calm.LoadPattern(patternPath)
	if err != nil {
		return fitness.ValidationResult{}, infrastructureError("loading governance pattern", err)
	}
	contentViolations := scoreContentFunctions(&result, request, config, pattern)
	architecturePath, cleanupArchitecture, err := c.writeArchitecture(report.BuildArchitecture(result))
	if err != nil {
		return fitness.ValidationResult{}, err
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
		return fitness.ValidationResult{}, infrastructureError("CALM validator is not configured", nil)
	}
	validation, err := validator.Validate(ctx, architecturePath, patternPath)
	if err != nil && !isValidationFailure(validation) {
		return fitness.ValidationResult{}, infrastructureError("running CALM validation", err)
	}
	violations := filterViolations(append(fitnessViolations(result, pattern), contentViolations...), config)
	ledger := newViolationLedger(state, request.DryRun)
	if len(violations) == 0 {
		return c.scoreClean(repo, request.File, config, ledger)
	}
	return c.scoreDirty(repo, request.File, violations, config, ledger)
}

func (c *Checker) scoreClean(repo, file string, config Config, ledger violationLedger) (fitness.ValidationResult, error) {
	if config.EnforcementMode == EnforcementBlock {
		ledger.ReplaceFile(repo, file, nil)
		// A clean file contributes nothing, so the outstanding set is whatever
		// the repository's other files still owe (the blackboard verdict).
		if outstanding := ledger.OutstandingAfter(repo, file, nil); len(outstanding) > 0 {
			return fitness.ValidationResult{Status: fitness.StatusBlock, Violations: outstanding}, nil
		}
	} else {
		ledger.ClearRepo(repo)
	}
	return fitness.ValidationResult{Status: fitness.StatusPass}, nil
}

func (c *Checker) scoreDirty(repo, file string, violations []fitness.Violation, config Config, ledger violationLedger) (fitness.ValidationResult, error) {
	switch config.EnforcementMode {
	case EnforcementAdvisory:
		ledger.ClearRepo(repo)
		return fitness.ValidationResult{Status: fitness.StatusAdvisory, Violations: violations}, nil
	default:
		ledger.ReplaceFile(repo, file, violations)
		// This file's violations replace its previous ones and merge with the
		// rest of the repository's; a dry run computes that merge without
		// having performed the write above.
		return fitness.ValidationResult{Status: fitness.StatusBlock, Violations: ledger.OutstandingAfter(repo, file, violations)}, nil
	}
}

func (c *Checker) state() *State {
	return c.State
}

func (c *Checker) startDeferredCheck(request fitness.ValidationRequest, repo string, config Config, unlockRepo func()) {
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
		fmt.Println("ready")
	}()
}

func hasOtherFileViolation(violations []fitness.Violation, file string) bool {
	for _, violation := range violations {
		if violation.File != file {
			return true
		}
	}
	return false
}

// analyzerToolingFailures are the message fragments an external analyzer
// (Roslyn, radon, the Python findings scan) produces when the tool itself is
// missing or broken, as opposed to the proposed source being unanalyzable. They
// are matched as text because the failures cross a process boundary and arrive
// without types.
var analyzerToolingFailures = []string{
	"running Roslyn analyzer",
	"parsing Roslyn analyzer output",
	"radon cc error",
	"radon raw error",
	"running radon",
	"parsing radon",
	"python findings error",
	"running findings scan",
	"radon python interpreter unavailable",
}

func isAnalyzerInfrastructureError(err error) bool {
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return true
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return true
	}
	return isAnalyzerToolingFailure(err.Error())
}

// isAnalyzerToolingFailure reports whether an analyzer message names a tooling
// failure rather than a problem with the file under analysis.
func isAnalyzerToolingFailure(message string) bool {
	for _, fragment := range analyzerToolingFailures {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
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

func (c Checker) writeProposedContent(request fitness.ValidationRequest) (string, func() error, error) {
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

func fitnessViolations(result analyzer.AnalysisResult, pattern calm.Pattern) []fitness.Violation {
	violations := make([]fitness.Violation, 0)
	violations = append(violations, cyclomaticComplexityViolations(result, pattern)...)
	violations = append(violations, interfaceWidthViolations(result, pattern)...)
	violations = append(violations, implementationDepthViolations(result, pattern)...)
	violations = append(violations, logicDensityViolations(result, pattern)...)
	violations = append(violations, dependencyDisciplineViolations(result, pattern)...)
	return violations
}

func cyclomaticComplexityViolations(result analyzer.AnalysisResult, pattern calm.Pattern) []fitness.Violation {
	rule, ok := pattern.FitnessFunctions["cyclomatic-complexity"]
	if !ok || rule.Operator != "lte" {
		return nil
	}
	violations := make([]fitness.Violation, 0)
	for _, function := range result.Functions {
		value := float64(function.CyclomaticComplexity)
		if value <= rule.Threshold {
			continue
		}
		violations = append(violations, fitness.Violation{
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

func interfaceWidthViolations(result analyzer.AnalysisResult, pattern calm.Pattern) []fitness.Violation {
	rule, ok := pattern.FitnessFunctions["interface-width"]
	result = analyzer.EnsureModuleMetric(result)
	if !ok || rule.Operator != "lte" || float64(result.ModuleMetric.PublicMethods) <= rule.Threshold {
		return nil
	}
	return []fitness.Violation{{
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

func implementationDepthViolations(result analyzer.AnalysisResult, pattern calm.Pattern) []fitness.Violation {
	rule, ok := pattern.FitnessFunctions["implementation-depth"]
	result = analyzer.EnsureModuleMetric(result)
	value := result.ModuleMetric.AverageLOCPerPublicMethod
	if !ok || rule.Operator != "gte" || result.ModuleMetric.PublicMethods == 0 || value >= rule.Threshold {
		return nil
	}
	return []fitness.Violation{{
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

func logicDensityViolations(result analyzer.AnalysisResult, pattern calm.Pattern) []fitness.Violation {
	rule, ok := pattern.FitnessFunctions["logic-density"]
	if !ok || rule.Operator != "gte" || result.FileMetric.TotalLOC == 0 || result.FileMetric.LDR >= rule.Threshold {
		return nil
	}
	return []fitness.Violation{{
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

func dependencyDisciplineViolations(result analyzer.AnalysisResult, pattern calm.Pattern) []fitness.Violation {
	rule, ok := pattern.FitnessFunctions["dependency-discipline"]
	if !ok || rule.Operator != "gte" || result.Imports.Total == 0 || result.Imports.DDC >= rule.Threshold {
		return nil
	}
	unused := strings.Join(result.Imports.Unused, ", ")
	if unused == "" {
		unused = "none reported"
	}
	return []fitness.Violation{{
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

func filterViolations(violations []fitness.Violation, config Config) []fitness.Violation {
	filtered := make([]fitness.Violation, 0, len(violations))
	for _, violation := range violations {
		if !config.Enabled(strings.ReplaceAll(violation.FitnessFunction, "_", "-")) {
			continue
		}
		filtered = append(filtered, violation)
	}
	return filtered
}

func analyzeWithCSharpProjectContext(ctx context.Context, request AnalysisRequest) (analyzer.AnalysisResult, error) {
	if err := ctx.Err(); err != nil {
		return analyzer.AnalysisResult{}, err
	}
	logicalPath := filepath.Join(request.Repo, request.File)
	csprojPath, err := analyzer.FindNearestCsproj(logicalPath, request.Repo)
	if err != nil {
		return analyzer.AnalyzeCSharpFile(ctx, request.TempPath, "")
	}
	return analyzer.AnalyzeCSharpFileWithProject(ctx, request.TempPath, csprojPath, "")
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
			return analyzeWithCSharpProjectContext(ctx, request)
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
	logicalPath := filepath.Join(request.Repo, request.File)
	peers, err := collectPeerGoFiles(filepath.Dir(logicalPath), logicalPath)
	if err != nil {
		return proposed, nil
	}
	results := []analyzer.AnalysisResult{proposed}
	for _, path := range peers {
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

// collectPeerGoFiles returns peer .go files in dir, excluding the proposed file,
// generated files, and test files.
func collectPeerGoFiles(dir, logicalPath string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var peers []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() ||
			!strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_generated.go") ||
			strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		if filepath.Clean(path) == filepath.Clean(logicalPath) {
			continue
		}
		peers = append(peers, path)
	}
	return peers, nil
}

func calmNodeForRequest(request fitness.ValidationRequest, fallback string) string {
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
		if !isValidExtensionChar(char) {
			return false
		}
	}
	return true
}

func isValidExtensionChar(char rune) bool {
	return char == '.' || char == '_' || char == '-' ||
		char >= '0' && char <= '9' ||
		char >= 'A' && char <= 'Z' ||
		char >= 'a' && char <= 'z'
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

func notFoundError(message string, err error) error {
	return &CheckError{Kind: ErrorKindNotFound, Message: message, Err: err}
}

func timeoutError(message string, err error) error {
	return &CheckError{Kind: ErrorKindTimeout, Message: message, Err: err}
}
