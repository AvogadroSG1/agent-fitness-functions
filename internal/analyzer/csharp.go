package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/installer"
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
	result.ModuleMetric = BuildModuleMetric(result.FileMetric, result.Functions)
	return result, nil
}

// AnalyzeCSharpFileWithProject analyzes one C# file with project context loaded from csprojPath.
// When csprojPath is empty it falls back to AnalyzeCSharpFile.
func AnalyzeCSharpFileWithProject(ctx context.Context, file, csprojPath, cliPath string) (AnalysisResult, error) {
	if csprojPath == "" {
		return AnalyzeCSharpFile(ctx, file, cliPath)
	}
	if cliPath == "" {
		cliPath = defaultRoslynCLI()
	}
	output, stderr, err := runToolOutput(ctx, cliPath, file, "--project", csprojPath)
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
	result.ModuleMetric = BuildModuleMetric(result.FileMetric, result.Functions)
	return result, nil
}

// managedRoslynAnalyzerPath returns the shipped self-contained Roslyn
// analyzer under this install's published binary version
// ($XDG_STATE_HOME/agent-fitness-functions/current/share/roslyn-analyzer/),
// per ADR-0005 slice 10: an installed root's own analyzer takes precedence
// over any source-tree-relative candidate. Returns "" when running from a
// source checkout with no such installed root (the common dev/test case),
// leaving defaultRoslynCLI's existing candidates and PATH fallback in
// effect.
func managedRoslynAnalyzerPath(getenv func(string) string) string {
	dir := filepath.Join(installer.StateRoot(getenv), "current", "share", "roslyn-analyzer")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.Contains(entry.Name(), "RoslynAnalyzer") {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.Mode()&0o111 != 0 {
			return filepath.Join(dir, entry.Name())
		}
	}
	return ""
}

// DefaultRoslynCLI returns the resolved path to the Roslyn analyzer CLI.
func DefaultRoslynCLI() string {
	return defaultRoslynCLI()
}

func defaultRoslynCLI() string {
	for _, envVar := range []string{"AGENT_FITNESS_FUNCTIONS_ROSLYN_PATH", "CALM_ROSLYN_ANALYZER_PATH"} {
		if val := os.Getenv(envVar); val != "" {
			if info, err := os.Stat(val); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				return val
			}
		}
	}
	if managed := managedRoslynAnalyzerPath(os.Getenv); managed != "" {
		return managed
	}
	home := os.Getenv("HOME")
	if home == "" {
		if userHome, err := os.UserHomeDir(); err == nil {
			home = userHome
		}
	}
	if home != "" {
		localCandidates := []string{
			filepath.Join(home, ".local", "share", "agent-fitness-functions", "roslyn-analyzer", "CalmRoslynAnalyzer"),
			filepath.Join(home, ".local", "share", "agent-fitness-functions", "roslyn-analyzer", "CalmRoslynAnalyzer.exe"),
			filepath.Join(home, ".dotnet", "tools", "CalmRoslynAnalyzer"),
			filepath.Join(home, ".dotnet", "tools", "CalmRoslynAnalyzer.exe"),
			filepath.Join(home, ".dotnet", "tools", "calm-roslyn-analyzer"),
		}
		for _, candidate := range localCandidates {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				return candidate
			}
		}
	}
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
	for _, name := range []string{"calm-roslyn-analyzer", "CalmRoslynAnalyzer"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return "calm-roslyn-analyzer"
}
