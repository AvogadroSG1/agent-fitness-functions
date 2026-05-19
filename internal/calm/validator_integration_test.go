//go:build integration

package calm

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestValidatorWithRealCalmCLI(t *testing.T) {
	cli, err := exec.LookPath("calm")
	if err != nil {
		t.Skip("calm CLI not installed")
	}
	validator := Validator{CLIPath: cli}

	pass, err := validator.Validate(context.Background(), "../../fixtures/calm/current-architecture-pass.json", "../../patterns/governance.json")
	if err != nil {
		t.Fatalf("passing fixture failed validation: %v\nstdout=%s\nstderr=%s", err, pass.Output, pass.ErrorOutput)
	}
	if !pass.Valid || !strings.Contains(pass.Output, `"hasErrors": false`) {
		t.Fatalf("pass result = %+v, want valid output without errors", pass)
	}

	failures := []struct {
		name    string
		fixture string
		want    string
	}{
		{name: "cyclomatic complexity", fixture: "current-architecture-fail-cyclomatic-complexity.json", want: "must be <= 9"},
		{name: "interface width", fixture: "current-architecture-fail-interface-width.json", want: "must be <= 20"},
		{name: "implementation depth", fixture: "current-architecture-fail-implementation-depth.json", want: "must be >= 0.722"},
		{name: "logic density", fixture: "current-architecture-fail-logic-density.json", want: "must be >= 0.255"},
		{
			name:    "dependency discipline",
			fixture: "current-architecture-fail-dependency-discipline.json",
			want:    "must be >= 0.8",
		},
	}
	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validator.Validate(context.Background(), "../../fixtures/calm/"+tt.fixture, "../../patterns/governance.json")
			if err == nil {
				t.Fatalf("%s returned nil error: %+v", tt.fixture, result)
			}
			if result.Valid || !strings.Contains(result.Output, tt.want) {
				t.Fatalf("%s result = %+v, want violation %q", tt.fixture, result, tt.want)
			}
		})
	}
}
