package calm_poc_test

import (
	"os/exec"
	"strings"
	"testing"
)

// calm-poc-phk.3: given the repository index, when cert paths are
// inspected, then only certs/.gitignore is tracked — generated
// development certificates and private keys are ephemeral local state.
func TestOnlyGitignoreIsTrackedUnderCerts(t *testing.T) {
	output, err := exec.Command("git", "ls-files", "--", "certs").Output()
	if err != nil {
		t.Fatalf("git ls-files -- certs: %v", err)
	}
	tracked := strings.Fields(strings.TrimSpace(string(output)))
	if len(tracked) != 1 || tracked[0] != "certs/.gitignore" {
		t.Fatalf("tracked cert paths = %v, want exactly [certs/.gitignore]; generated certificates and private keys must never be tracked", tracked)
	}
}
