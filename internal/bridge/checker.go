package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/poconnor/calm-poc/internal/analyzer"
	"github.com/poconnor/calm-poc/internal/calm"
	"github.com/poconnor/calm-poc/internal/report"
)

// Validator runs CALM validation for generated architecture documents.
type Validator interface {
	Validate(context.Context, string, string) (calm.ValidationResult, error)
}

// Checker coordinates source analysis, CALM architecture generation, and validation.
type Checker struct {
	PatternPath string
	TempDir     string
	Validator   Validator
	GoAnalyzer  func(string) (analyzer.AnalysisResult, error)
}

// Check runs the synchronous check path for one proposed file.
func (c Checker) Check(ctx context.Context, request CheckRequest) (CheckResponse, error) {
	if request.Language != "go" {
		return CheckResponse{}, fmt.Errorf("unsupported language %q", request.Language)
	}
	patternPath := c.PatternPath
	if patternPath == "" {
		patternPath = filepath.Join("patterns", "governance.json")
	}
	pattern, err := calm.LoadPattern(patternPath)
	if err != nil {
		return CheckResponse{}, err
	}
	sourcePath, cleanup, err := c.writeProposedContent(request)
	if err != nil {
		return CheckResponse{}, err
	}
	defer cleanup()

	analyzeGo := c.GoAnalyzer
	if analyzeGo == nil {
		analyzeGo = analyzer.AnalyzeGoFile
	}
	result, err := analyzeGo(sourcePath)
	if err != nil {
		return CheckResponse{}, fmt.Errorf("analyzing Go file: %w", err)
	}
	result.File = request.File

	architecturePath, cleanupArchitecture, err := c.writeArchitecture(report.BuildArchitecture(result))
	if err != nil {
		return CheckResponse{}, err
	}
	defer cleanupArchitecture()

	validator := c.Validator
	if validator == nil {
		validator = calm.Validator{}
	}
	validation, err := validator.Validate(ctx, architecturePath, patternPath)
	if err == nil && validation.Valid {
		return CheckResponse{Status: StatusPass}, nil
	}
	violations := cyclomaticComplexityViolations(result, pattern)
	if len(violations) == 0 && err != nil {
		return CheckResponse{}, err
	}
	return CheckResponse{Status: StatusBlock, Violations: violations}, nil
}

func (c Checker) writeProposedContent(request CheckRequest) (string, func(), error) {
	extension := filepath.Ext(request.File)
	if extension == "" {
		extension = ".go"
	}
	file, err := os.CreateTemp(c.TempDir, "calm-check-*"+extension)
	if err != nil {
		return "", func() {}, fmt.Errorf("creating temp source file: %w", err)
	}
	cleanup := func() {
		_ = os.Remove(file.Name())
	}
	if _, err := file.WriteString(request.ProposedContent); err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("writing temp source file: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("closing temp source file: %w", err)
	}
	return file.Name(), cleanup, nil
}

func (c Checker) writeArchitecture(document report.ArchitectureDocument) (string, func(), error) {
	file, err := os.CreateTemp(c.TempDir, "current-architecture-*.json")
	if err != nil {
		return "", func() {}, fmt.Errorf("creating temp architecture file: %w", err)
	}
	cleanup := func() {
		_ = os.Remove(file.Name())
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("writing architecture file: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("closing architecture file: %w", err)
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

func normalizeFitnessFunction(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}
