package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/architecture"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/installer"
)

// AnalyzeCSharpRepository invokes Roslyn's repository graph mode.
func AnalyzeCSharpRepository(ctx context.Context, repo, cliPath string) (architecture.Graph, error) {
	return AnalyzeCSharpRepositoryWithSolution(ctx, repo, cliPath, "")
}

// AnalyzeCSharpRepositoryWithSolution runs repository extraction with an
// optional repository-relative solution selector. An explicit selector is
// important when a checkout contains an aggregate and an application solution.
func AnalyzeCSharpRepositoryWithSolution(ctx context.Context, repo, cliPath, solution string) (architecture.Graph, error) {
	if cliPath == "" {
		cliPath = defaultRoslynCLI()
	}
	args := []string{"--repository", repo}
	if solution != "" {
		args = append(args, "--solution", solution)
	}
	output, stderr, err := runToolOutput(ctx, cliPath, args...)
	if err != nil {
		return architecture.Graph{}, fmt.Errorf("running Roslyn repository analyzer: %w", err)
	}
	var graph architecture.Graph
	if err := json.Unmarshal(output, &graph); err != nil {
		return graph, fmt.Errorf("parsing Roslyn repository output: %w (stderr: %s)", err, strings.TrimSpace(stderr))
	}
	return graph, nil
}

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
		if candidate, ok := executableCandidate(os.Getenv(envVar)); ok {
			return candidate
		}
	}
	if managed := managedRoslynAnalyzerPath(os.Getenv); managed != "" {
		return managed
	}
	if candidate := firstExecutable(localRoslynCandidates(resolveHome())); candidate != "" {
		return candidate
	}
	if cwd, err := os.Getwd(); err == nil {
		if candidate := firstExecutable(checkoutRoslynCandidates(cwd)); candidate != "" {
			return candidate
		}
	}
	if candidate := firstPathRoslyn(); candidate != "" {
		return candidate
	}
	return "calm-roslyn-analyzer"
}

func resolveHome() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	home, _ := os.UserHomeDir()
	return home
}

func localRoslynCandidates(home string) []string {
	if home == "" {
		return nil
	}
	base := filepath.Join(home, ".local", "share", "agent-fitness-functions", "roslyn-analyzer")
	dotnet := filepath.Join(home, ".dotnet", "tools")
	return []string{filepath.Join(base, "CalmRoslynAnalyzer"), filepath.Join(base, "CalmRoslynAnalyzer.exe"), filepath.Join(dotnet, "CalmRoslynAnalyzer"), filepath.Join(dotnet, "CalmRoslynAnalyzer.exe"), filepath.Join(dotnet, "calm-roslyn-analyzer")}
}

func checkoutRoslynCandidates(cwd string) []string {
	paths := []string{filepath.Join("tools", "roslyn-analyzer", "bin", "Release", "net8.0"), filepath.Join("tools", "roslyn-analyzer", "bin", "Debug", "net8.0")}
	root := filepath.Join(cwd, "..", "..", "tools", "roslyn-analyzer", "bin")
	paths = append(paths, filepath.Join(root, "Release", "net8.0"), filepath.Join(root, "Debug", "net8.0"))
	result := make([]string, 0, len(paths)*2)
	for _, path := range paths {
		result = append(result, filepath.Join(path, "CalmRoslynAnalyzer"), filepath.Join(path, "CalmRoslynAnalyzer.exe"))
	}
	return result
}

func executableCandidate(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	info, err := os.Stat(path)
	return path, err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func firstExecutable(candidates []string) string {
	for _, candidate := range candidates {
		if path, ok := executableCandidate(candidate); ok {
			return path
		}
	}
	return ""
}

func firstPathRoslyn() string {
	for _, name := range []string{"calm-roslyn-analyzer", "CalmRoslynAnalyzer"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}
