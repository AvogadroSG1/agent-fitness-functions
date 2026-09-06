package calm_poc_test

import (
	"os/exec"
	"strings"
	"testing"
)

// calm-poc-phk.3, revised for ADR-0007: given the repository index, when cert
// paths are inspected, then nothing at all is tracked under certs/. The
// development CA now lives in the machine governance root, so the repository
// carries no certs/ directory — and generated certificates and private keys
// remain ephemeral local state that must never be committed.
func TestNoCertificateMaterialIsTracked(t *testing.T) {
	output, err := exec.Command("git", "ls-files", "--", "certs").Output()
	if err != nil {
		t.Fatalf("git ls-files -- certs: %v", err)
	}
	tracked := strings.Fields(strings.TrimSpace(string(output)))
	if len(tracked) != 0 {
		t.Fatalf("tracked cert paths = %v, want none; ADR-0007 moved the development CA to the machine governance root and certificates must never be tracked", tracked)
	}
}
