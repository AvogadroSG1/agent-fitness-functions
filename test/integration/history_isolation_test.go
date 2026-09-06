package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Public client calls MUST NOT inherit the developer's history endpoint.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aff-it-")
	if err != nil {
		panic(err)
	}
	// A regular-file parent also prevents future onboarding fixtures from
	// starting a persistent writer during package tests.
	parent := filepath.Join(dir, "unavailable")
	if err := os.WriteFile(parent, nil, 0o600); err != nil {
		panic(err)
	}
	if err := os.Setenv("AGENT_FITNESS_FUNCTIONS_HISTORY_SOCKET", filepath.Join(parent, "s")); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestHistoryEnvironmentIsolation(t *testing.T) {
	const inherited = "/inherited/developer-writer.sock"
	if os.Getenv("AFF_HISTORY_ISOLATION_CHILD") == "1" {
		endpoint := os.Getenv("AGENT_FITNESS_FUNCTIONS_HISTORY_SOCKET")
		if endpoint == "" || endpoint == inherited {
			t.Fatalf("package inherited an unisolated history endpoint %q", endpoint)
		}
		parent, err := os.Stat(filepath.Dir(endpoint))
		if err != nil || !parent.Mode().IsRegular() {
			t.Fatalf("isolated endpoint MUST prevent fixture writer startup: %v", err)
		}
		return
	}
	// Only this environment assertion runs in the child. It MUST NOT dial the
	// harmless sentinel or run a validation against any writer.
	command := exec.Command(os.Args[0], "-test.run=^TestHistoryEnvironmentIsolation$")
	command.Env = append(os.Environ(), "AFF_HISTORY_ISOLATION_CHILD=1", "AGENT_FITNESS_FUNCTIONS_HISTORY_SOCKET="+inherited)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("package startup did not isolate inherited writer configuration: %v\n%s", err, output)
	}
}
