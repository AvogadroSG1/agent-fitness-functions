package agent_fitness_functions_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentFitnessFunctionsBinHelpersContract(t *testing.T) {
	for _, oldName := range []string{"calm-serve", "calm-test"} {
		if _, err := os.Stat(filepath.Join("bin", oldName)); !os.IsNotExist(err) {
			t.Fatalf("legacy helper %s must not exist; stat error = %v", oldName, err)
		}
	}

	for _, helper := range []struct {
		name        string
		usage       string
		description string
	}{
		{
			name:        "agent-fitness-functions-serve",
			usage:       "usage: agent-fitness-functions-serve [--build]",
			description: "Starts the agent-fitness-functions container via Docker Compose",
		},
		{
			name:        "agent-fitness-functions-test",
			usage:       "usage: agent-fitness-functions-test <file>",
			description: "LOCAL SANDBOX ONLY",
		},
	} {
		helperPath := filepath.Join("bin", helper.name)
		info, err := os.Stat(helperPath)
		if err != nil {
			t.Fatalf("stat %s: %v", helperPath, err)
		}
		if info.Mode()&0o111 == 0 {
			t.Fatalf("%s must be executable", helperPath)
		}

		content, err := os.ReadFile(helperPath)
		if err != nil {
			t.Fatalf("read %s: %v", helperPath, err)
		}
		script := string(content)
		for _, legacy := range []string{"calm-bridge", "calm-serve", "calm-test"} {
			if strings.Contains(script, legacy) {
				t.Fatalf("%s contains legacy name %q", helperPath, legacy)
			}
		}
		if !strings.Contains(script, "agent-fitness-functions") {
			t.Fatalf("%s must invoke or document agent-fitness-functions", helperPath)
		}
		if !strings.Contains(script, "AGENT_FITNESS_FUNCTIONS_") {
			t.Fatalf("%s must use AGENT_FITNESS_FUNCTIONS_* environment names", helperPath)
		}

		command := exec.Command(helperPath, "--help")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("%s --help failed: %v\n%s", helperPath, err, output)
		}
		helpText := string(output)
		if !strings.Contains(helpText, helper.usage) {
			t.Fatalf("%s --help missing usage %q:\n%s", helperPath, helper.usage, helpText)
		}
		if !strings.Contains(helpText, helper.description) {
			t.Fatalf("%s --help missing description %q:\n%s", helperPath, helper.description, helpText)
		}
	}
}
