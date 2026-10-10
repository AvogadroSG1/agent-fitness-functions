//go:build integration

package server

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/calm"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/roslyntest"
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
	if first.Status != fitness.StatusPass || !first.Warming {
		t.Fatalf("first response = %+v, want pass with warming", first)
	}
	violations := waitForStateViolations(t, server.URL, repo, 1)
	if violations[0].Function != "Render" || violations[0].CALMNode != "Sample" {
		t.Fatalf("violations = %+v, want default Roslyn Sample.Render violation", violations)
	}
	second := postCheckForLanguage(t, server.URL, repo, "src/Other.cs", "csharp", cleanCSharpSource())
	if second.Status != fitness.StatusBlock || second.Warming || len(second.Violations) != 1 || second.Violations[0].File != "src/Widget.cs" {
		t.Fatalf("second response = %+v, want deferred Widget violation to block next C# check", second)
	}
	cleared := postCheckForLanguage(t, server.URL, repo, "src/Widget.cs", "csharp", cleanCSharpSource())
	if cleared.Status != fitness.StatusPass || cleared.Warming || len(cleared.Violations) != 0 {
		t.Fatalf("cleared response = %+v, want warm synchronous pass after fixing Widget", cleared)
	}
}

func ensureDefaultRoslynAnalyzer(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dotnet"); err != nil {
		t.Skip("dotnet not installed")
	}
	path, err := roslyntest.Build("../..")
	if err != nil {
		t.Fatalf("build Roslyn analyzer: %v", err)
	}
	t.Setenv("AGENT_FITNESS_FUNCTIONS_ROSLYN_PATH", path)
}

func TestCheckerCSharpProjectContextResolvesLocalNamespace(t *testing.T) {
	ensureDefaultRoslynAnalyzer(t)

	repoRoot := t.TempDir()
	csprojPath := filepath.Join(repoRoot, "MyApp.csproj")
	if err := os.WriteFile(csprojPath, []byte(`<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
    <Nullable>enable</Nullable>
  </PropertyGroup>
</Project>`), 0o644); err != nil {
		t.Fatalf("write csproj: %v", err)
	}
	widgetSrc := `namespace MyApp.Domain;
public class Widget { public int Id { get; set; } }`
	if err := os.WriteFile(filepath.Join(repoRoot, "Widget.cs"), []byte(widgetSrc), 0o644); err != nil {
		t.Fatalf("write Widget.cs: %v", err)
	}
	consumerSrc := `using System;
using MyApp.Domain;

namespace MyApp.App;

public class Consumer
{
    public Widget Get()
    {
        Console.WriteLine("fetching");
        return new Widget { Id = 1 };
    }
}
`
	if err := os.WriteFile(filepath.Join(repoRoot, "Consumer.cs"), []byte(consumerSrc), 0o644); err != nil {
		t.Fatalf("write Consumer.cs: %v", err)
	}

	req := AnalysisRequest{
		Repo:     "logical-repo",
		RepoPath: repoRoot,
		File:     "Consumer.cs",
		Language: "csharp",
		TempPath: filepath.Join(repoRoot, "Consumer.cs"),
	}
	result, err := analyzeWithCSharpProjectContext(context.Background(), req)
	if err != nil {
		t.Fatalf("analyzeWithCSharpProjectContext returned error: %v", err)
	}
	if result.Imports.Total != 2 {
		t.Fatalf("imports.total = %d, want 2", result.Imports.Total)
	}
	if result.Imports.Used != 2 {
		t.Fatalf("imports.used = %d, want 2 — MyApp.Domain must resolve with project context; got unused: %v",
			result.Imports.Used, result.Imports.Unused)
	}
	if result.Imports.DDC != 1.0 {
		t.Fatalf("imports.ddc = %.3f, want 1.0", result.Imports.DDC)
	}
}

func TestCheckerLocalHTTPPreservesAuthorizedPhysicalRepoForCSharpDDC(t *testing.T) {
	ensureDefaultRoslynAnalyzer(t)

	workspace := t.TempDir()
	repoRoot := filepath.Join(workspace, "repo-one")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "MyApp.csproj"), []byte(`<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
  </PropertyGroup>
</Project>`), 0o644); err != nil {
		t.Fatalf("write csproj: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "Types.cs"), []byte(`namespace MyApp.Domain;
public class Widget { }
public class WidgetBase<T> { }`), 0o644); err != nil {
		t.Fatalf("write Types.cs: %v", err)
	}

	store := newTestConfigStore(t)
	writeRepoConfig(t, store, "repo-one", EnforcementBlock, map[string]bool{
		"dependency-discipline": true,
		"logic-density":         false,
	})
	server := httptest.NewServer(NewHandlerWithOptions(Checker{
		ConfigStore:   store,
		PatternPath:   writeTestPattern(t),
		BlockOnWarmup: true,
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: true, Output: `{"hasErrors":false}`}, nil
		}),
	}, nil, HandlerOptions{RequireAuthentication: true, LocalHTTP: true}))
	defer server.Close()

	source := `using MyApp.Domain;

namespace MyApp.App;

public class Consumer : WidgetBase<Widget>
{
    public Widget Get() => new();
}
`
	body := postCheckForLanguage(t, server.URL, repoRoot, "Consumer.cs", "csharp", source)
	if body.Status != fitness.StatusPass || body.Warming || len(body.Violations) != 0 {
		t.Fatalf("response = %+v, want a synchronous pass with project-local generic import used", body)
	}
}
