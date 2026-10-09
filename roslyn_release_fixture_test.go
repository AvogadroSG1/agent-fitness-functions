package calm_poc_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/roslyntest"
)

// privateReleaseScript runs the unchanged packaging script with private Roslyn
// inputs. Only read-only Go sources and packaged assets point at the worktree.
func privateReleaseScript(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve release repository root: %v", err)
	}
	fixtureRoot := t.TempDir()
	if err := roslyntest.CopyProject(repoRoot, filepath.Join(fixtureRoot, "tools", "roslyn-analyzer")); err != nil {
		t.Fatalf("copy release Roslyn project: %v", err)
	}
	for _, name := range []string{"cmd", "internal", "patterns", "bin", "tools/calm-runtime"} {
		if err := os.Symlink(filepath.Join(repoRoot, name), filepath.Join(fixtureRoot, name)); err != nil {
			t.Fatalf("link read-only release input %s: %v", name, err)
		}
	}
	for _, name := range []string{"go.mod", "go.sum", "requirements.lock", "scripts/package-release.sh"} {
		content, err := os.ReadFile(filepath.Join(repoRoot, name))
		if err != nil {
			t.Fatalf("read release input %s: %v", name, err)
		}
		destination := filepath.Join(fixtureRoot, name)
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			t.Fatalf("create release fixture directory: %v", err)
		}
		if err := os.WriteFile(destination, content, 0o644); err != nil {
			t.Fatalf("copy release input %s: %v", name, err)
		}
	}
	return filepath.Join(fixtureRoot, "scripts", "package-release.sh")
}
