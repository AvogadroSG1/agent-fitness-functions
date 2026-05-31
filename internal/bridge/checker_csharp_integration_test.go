//go:build integration

package bridge

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/poconnor/calm-poc/internal/calm"
)

func TestCheckerWithDefaultRoslynAnalyzerDefersThenBlocksCSharpFixture(t *testing.T) {
	ensureDefaultRoslynAnalyzer(t)
	repo := "repo-one"
	store := newTestConfigStore(t)
	writeRepoConfig(t, store, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		ConfigStore: store,
		PatternPath: writeTestPattern(t),
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: false, Output: `{"hasErrors":true}`}, errors.New("calm validate failed")
		}),
	}, nil))
	defer server.Close()

	start := time.Now()
	first := postCheckForLanguage(t, server.URL, repo, "src/Widget.cs", "csharp", complexCSharpSource())
	t.Logf("real Roslyn deferred C# cold response latency: %s", time.Since(start))
	if first.Status != StatusPass || !first.Warming {
		t.Fatalf("first response = %+v, want pass with warming", first)
	}
	violations := waitForStateViolations(t, server.URL, repo, 1)
	if violations[0].Function != "Render" || violations[0].CALMNode != "Sample" {
		t.Fatalf("violations = %+v, want default Roslyn Sample.Render violation", violations)
	}
	second := postCheckForLanguage(t, server.URL, repo, "src/Other.cs", "csharp", cleanCSharpSource())
	if second.Status != StatusBlock || second.Warming || len(second.Violations) != 1 || second.Violations[0].File != "src/Widget.cs" {
		t.Fatalf("second response = %+v, want deferred Widget violation to block next C# check", second)
	}
	cleared := postCheckForLanguage(t, server.URL, repo, "src/Widget.cs", "csharp", cleanCSharpSource())
	if cleared.Status != StatusPass || cleared.Warming || len(cleared.Violations) != 0 {
		t.Fatalf("cleared response = %+v, want warm synchronous pass after fixing Widget", cleared)
	}
}

func ensureDefaultRoslynAnalyzer(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dotnet"); err != nil {
		t.Skip("dotnet not installed")
	}
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	exe := filepath.Join(repoRoot, "tools", "roslyn-analyzer", "bin", "Release", "net8.0", "CalmRoslynAnalyzer")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if info, err := os.Stat(exe); err == nil && !info.IsDir() {
		return
	}
	project := filepath.Join(repoRoot, "tools", "roslyn-analyzer", "CalmRoslynAnalyzer.csproj")
	command := exec.Command("dotnet", "build", "-c", "Release", project)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("dotnet build failed: %v\n%s", err, output)
	}
}
