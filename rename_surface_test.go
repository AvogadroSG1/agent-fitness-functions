package calm_poc_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// calm-poc-q8d.8: given the accepted ADR-0002 rename decisions, when the
// active product surface is inspected, then binary, helpers, environment
// prefix, governance metadata, and the requirements manifest header all
// carry agent-fitness-functions, while protected surfaces stay exact.
func TestActiveProductSurfaceUsesAgentFitnessFunctions(t *testing.T) {
	for _, path := range []string{
		"cmd/agent-fitness-functions/main.go",
		"bin/agent-fitness-functions-serve",
		"bin/agent-fitness-functions-test",
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("renamed product surface missing: %s (%v)", path, err)
		}
	}
	for _, path := range []string{
		"cmd/stack-fitness-functions",
		"bin/stack-fitness-functions-serve",
		"bin/stack-fitness-functions-test",
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("predecessor product surface still present: %s", path)
		}
	}

	// The old environment prefix may survive only in immutable records
	// (accepted ADRs, legacy evidence, tracker history) — never in the
	// active surface.
	output, _ := exec.Command("git", "grep", "-l", "STACK_FITNESS_FUNCTIONS_",
		"--", ":!docs/adr", ":!LEGACY_REFERENCES.md", ":!.beads").Output()
	if listing := strings.TrimSpace(string(output)); listing != "" {
		t.Errorf("active surface still uses STACK_FITNESS_FUNCTIONS_ environment prefix:\n%s", listing)
	}

	lock, err := os.ReadFile("requirements.lock")
	if err != nil {
		t.Fatalf("read requirements.lock: %v", err)
	}
	lines := strings.SplitN(string(lock), "\n", 2)
	if !strings.Contains(lines[0], "agent-fitness-functions") {
		t.Errorf("requirements.lock line 1 = %q, want agent-fitness-functions product header", lines[0])
	}
	if strings.Contains(lines[0], "stack-fitness-functions") {
		t.Errorf("requirements.lock line 1 still names the predecessor: %q", lines[0])
	}
	if len(lines) < 2 || !strings.Contains(lines[1], "radon==6.0.1") {
		t.Error("requirements.lock dependency body must remain intact")
	}

	pattern, err := os.ReadFile("patterns/governance.json")
	if err != nil {
		t.Fatalf("read governance pattern: %v", err)
	}
	var governance struct {
		ID          string `json:"$id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(pattern, &governance); err != nil {
		t.Fatalf("parse governance pattern: %v", err)
	}
	if governance.ID != "https://stackoverflow.com/calm-poc/patterns/governance.json" {
		t.Errorf("governance $id changed to %q; it is a protected surface and must stay byte-identical", governance.ID)
	}
	if !strings.Contains(governance.Title, "Agent Fitness Functions") {
		t.Errorf("governance title = %q, want the agent-fitness-functions product identity", governance.Title)
	}
}
