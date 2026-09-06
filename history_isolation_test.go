package calm_poc_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/client"
)

// Product and hook subprocesses MUST inherit a private fixture endpoint.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aff-rt-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("AGENT_FITNESS_FUNCTIONS_HISTORY_SOCKET", filepath.Join(dir, "s")); err != nil {
		panic(err)
	}
	// Capture the private endpoint before tests run. Real onboarding MAY start
	// a fixture writer; teardown MUST stop it before removing its directory.
	historyRuntime := client.NewHistoryRuntime(os.Getenv)
	code := m.Run()
	if err := historyRuntime.Stop(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "stop fixture history writer: %v\n", err)
		code = 1
	} else if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintf(os.Stderr, "remove fixture history directory: %v\n", err)
		code = 1
	}
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
		if err != nil || !parent.IsDir() || parent.Mode().Perm() != 0o700 {
			t.Fatalf("isolated endpoint MUST have a private fixture directory: %v", err)
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
