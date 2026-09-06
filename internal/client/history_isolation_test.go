package client

import (
	"os"
	"path/filepath"
	"testing"
)

// Fixture validations MUST never reach the developer's running history writer.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aff-client-history-")
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
