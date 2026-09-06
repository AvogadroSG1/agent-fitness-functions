package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

// Hook fixture subprocesses MUST inherit an isolated history endpoint.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aff-hooks-history-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("AGENT_FITNESS_FUNCTIONS_HISTORY_SOCKET", filepath.Join(dir, "absent.sock")); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
