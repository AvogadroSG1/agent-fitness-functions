package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Command fixture validations MUST never write into the developer's history.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aff-command-history-")
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
