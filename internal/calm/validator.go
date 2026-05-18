package calm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Validator invokes the FINOS calm CLI.
type Validator struct {
	CLIPath string
	Env     []string
	Timeout time.Duration
}

// ValidationResult is the raw result returned by calm validate.
type ValidationResult struct {
	Valid       bool
	Output      string
	ErrorOutput string
}

// Validate runs `calm validate` for one architecture document and pattern.
func (v Validator) Validate(ctx context.Context, architecturePath, patternPath string) (ValidationResult, error) {
	cliPath := v.CLIPath
	if cliPath == "" {
		cliPath = "calm"
	}
	timeout := v.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command := exec.CommandContext(runCtx, cliPath, "validate", "--architecture", architecturePath, "--pattern", patternPath)
	command.Env = append(os.Environ(), v.Env...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	result := ValidationResult{
		Valid:       err == nil,
		Output:      stdout.String(),
		ErrorOutput: stderr.String(),
	}
	if err == nil {
		return result, nil
	}
	detail := firstNonBlank(stderr.String(), stdout.String())
	wrapped := fmt.Errorf("calm validate failed: %w: %s", err, detail)
	if runCtx.Err() != nil {
		wrapped = errors.Join(runCtx.Err(), wrapped)
	}
	return result, wrapped
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
