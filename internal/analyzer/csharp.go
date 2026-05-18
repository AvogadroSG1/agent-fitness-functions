package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
)

// AnalyzeCSharpFile analyzes one C# file through the Roslyn analyzer CLI.
func AnalyzeCSharpFile(ctx context.Context, file, cliPath string) (AnalysisResult, error) {
	if cliPath == "" {
		cliPath = "calm-roslyn-analyzer"
	}
	output, err := runTool(ctx, cliPath, file)
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("running Roslyn analyzer: %w", err)
	}
	var result AnalysisResult
	if err := json.Unmarshal(output, &result); err != nil {
		return AnalysisResult{}, fmt.Errorf("parsing Roslyn analyzer output: %w", err)
	}
	if result.Language == "" {
		result.Language = "csharp"
	}
	if result.File == "" {
		result.File = file
	}
	return result, nil
}
