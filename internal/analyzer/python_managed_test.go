package analyzer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// ADR-0005 slice 10 parity with the C# analyzer: when no explicit radon
// path is configured and an installed state root carries the managed
// python runtime, its pinned radon MUST be preferred over host PATH or
// host-python API fallbacks.
func TestAnalyzePythonFilePrefersManagedRadon(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	binDir := filepath.Join(stateHome, "agent-fitness-functions", "runtimes", "python", "current", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir managed bin: %v", err)
	}
	marker := filepath.Join(t.TempDir(), "managed-radon-invoked")
	stub := "#!/bin/sh\n: > \"" + marker + "\"\n" +
		"case \"$1\" in\n" +
		"cc) printf '{\"%s\": []}' \"$3\" ;;\n" +
		"raw) printf '{\"%s\": {\"loc\":1,\"lloc\":1,\"sloc\":1,\"comments\":0,\"multi\":0,\"blank\":0,\"single_comments\":0}}' \"$3\" ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(binDir, "radon"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write radon stub: %v", err)
	}

	source := filepath.Join(t.TempDir(), "sample.py")
	if err := os.WriteFile(source, []byte("def sample():\n    return 1\n"), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	if _, err := AnalyzePythonFile(context.Background(), source, ""); err != nil {
		t.Logf("AnalyzePythonFile returned %v (stub output may be minimal); invocation is what this test asserts", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("managed radon was not invoked; analyzer must prefer the installed runtime over host fallbacks: %v", err)
	}
}
