package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AnalyzeCSharpFile analyzes one C# file through the Roslyn analyzer CLI.
func AnalyzeCSharpFile(ctx context.Context, file, cliPath string) (AnalysisResult, error) {
	if cliPath == "" {
		cliPath = defaultRoslynCLI()
	}
	output, stderr, err := runToolOutput(ctx, cliPath, file)
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("running Roslyn analyzer: %w", err)
	}
	var result AnalysisResult
	if err := json.Unmarshal(output, &result); err != nil {
		if strings.TrimSpace(stderr) != "" {
			return AnalysisResult{}, fmt.Errorf("parsing Roslyn analyzer output: %w: stderr: %s", err, strings.TrimSpace(stderr))
		}
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

func defaultRoslynCLI() string {
	candidates := []string{
		filepath.Join("tools", "roslyn-analyzer", "bin", "Release", "net8.0", "CalmRoslynAnalyzer"),
		filepath.Join("tools", "roslyn-analyzer", "bin", "Release", "net8.0", "CalmRoslynAnalyzer.exe"),
		filepath.Join("tools", "roslyn-analyzer", "bin", "Debug", "net8.0", "CalmRoslynAnalyzer"),
		filepath.Join("tools", "roslyn-analyzer", "bin", "Debug", "net8.0", "CalmRoslynAnalyzer.exe"),
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "..", "..", "tools", "roslyn-analyzer", "bin", "Release", "net8.0", "CalmRoslynAnalyzer"),
			filepath.Join(cwd, "..", "..", "tools", "roslyn-analyzer", "bin", "Release", "net8.0", "CalmRoslynAnalyzer.exe"),
			filepath.Join(cwd, "..", "..", "tools", "roslyn-analyzer", "bin", "Debug", "net8.0", "CalmRoslynAnalyzer"),
			filepath.Join(cwd, "..", "..", "tools", "roslyn-analyzer", "bin", "Debug", "net8.0", "CalmRoslynAnalyzer.exe"),
		)
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate
		}
	}
	return "calm-roslyn-analyzer"
}
