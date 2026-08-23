package calm_poc_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDemoAgentGovernedEditTLSModeMatrix(t *testing.T) {
	tests := []struct {
		name       string
		resolver   string
		env        []string
		wantExit   int
		wantOutput []string
		wantCalls  int
	}{
		{
			name: "managed default", resolver: "printf 'versions/v-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\n'", wantCalls: 1,
			wantOutput: []string{"mode=managed", "/agent-demo/certs/versions/v-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/client.crt", "selector=unset"},
		},
		{
			name: "managed selected root", resolver: "printf 'versions/v-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\\n'", wantCalls: 1,
			env:        []string{"STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR=/selected/certs"},
			wantOutput: []string{"mode=managed", "client_cert=/selected/certs/versions/v-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/client.crt", "selector=unset"},
		},
		{
			name: "external", wantCalls: 0,
			env: []string{
				"STACK_FITNESS_FUNCTIONS_CLIENT_CERT=/external/client.crt",
				"STACK_FITNESS_FUNCTIONS_CLIENT_KEY=/external/client.key",
				"STACK_FITNESS_FUNCTIONS_CLIENT_CA=/external/ca.crt",
			},
			wantOutput: []string{"mode=external", "client_cert=/external/client.crt", "client_key=/external/client.key", "client_ca=/external/ca.crt", "selector=unset"},
		},
		{
			name: "ambiguity", wantExit: 2, wantCalls: 0,
			env:        []string{"STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR=/managed", "STACK_FITNESS_FUNCTIONS_CLIENT_CERT=/external/client.crt"},
			wantOutput: []string{"STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR cannot be combined with explicit client TLS inputs"},
		},
		{
			name: "malformed resolver output", resolver: "printf 'versions/not-valid\\n'", wantExit: 1, wantCalls: 1,
			wantOutput: []string{"stack-fitness-functions returned an invalid managed certificate version"},
		},
		{
			name: "resolver failure", resolver: "echo 'demo resolver failed safely' >&2; exit 7", wantExit: 7, wantCalls: 1,
			wantOutput: []string{"demo resolver failed safely"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logPath := filepath.Join(t.TempDir(), "calls")
			tempRoot := t.TempDir()
			stubDir := t.TempDir()
			mktempStub := filepath.Join(stubDir, "mktemp")
			mktempScript := "#!/usr/bin/env bash\nset -euo pipefail\npath=\"$DEMO_TEST_TMP_ROOT/created\"\nmkdir \"$path\"\nprintf '%s\\n' \"$path\"\n"
			if err := os.WriteFile(mktempStub, []byte(mktempScript), 0o755); err != nil {
				t.Fatalf("WriteFile(mktemp stub): %v", err)
			}
			resolver := tt.resolver
			if resolver == "" {
				resolver = "exit 99"
			}
			binary := writeDemoStub(t, logPath, resolver)
			command := exec.Command("bash", filepath.Join("scripts", "demo-agent-governed-edit.sh"))
			command.Env = append(os.Environ(),
				"STACK_FITNESS_FUNCTIONS_BIN="+binary,
				"STACK_FITNESS_FUNCTIONS_DEMO_TLS_CONTRACT_ONLY=1",
				"STACK_FITNESS_FUNCTIONS_ADDR=",
				"STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR=",
				"STACK_FITNESS_FUNCTIONS_CLIENT_CERT=",
				"STACK_FITNESS_FUNCTIONS_CLIENT_KEY=",
				"STACK_FITNESS_FUNCTIONS_CLIENT_CA=",
				"DEMO_TEST_TMP_ROOT="+tempRoot,
				"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
			)
			command.Env = append(command.Env, tt.env...)
			output, err := command.CombinedOutput()
			gotExit := 0
			if err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatalf("run demo: %v", err)
				}
				gotExit = exitErr.ExitCode()
			}
			if gotExit != tt.wantExit {
				t.Fatalf("exit = %d, want %d; output=%s", gotExit, tt.wantExit, output)
			}
			for _, want := range tt.wantOutput {
				if !strings.Contains(string(output), want) {
					t.Errorf("output missing %q:\n%s", want, output)
				}
			}
			calls, readErr := os.ReadFile(logPath)
			if readErr != nil && !os.IsNotExist(readErr) {
				t.Fatalf("ReadFile(calls): %v", readErr)
			}
			if got := strings.Count(string(calls), "client resolve-dev-cert-version"); got != tt.wantCalls {
				t.Fatalf("resolver calls = %d, want %d; calls=%s", got, tt.wantCalls, calls)
			}
			entries, readDirErr := os.ReadDir(tempRoot)
			if readDirErr != nil {
				t.Fatalf("ReadDir(demo temp root): %v", readDirErr)
			}
			if len(entries) != 0 {
				t.Fatalf("demo left temporary directories after exit %d: %v", gotExit, entries)
			}
		})
	}
}

func TestDemoAgentGovernedEditKeepsResolvedPathsAfterRotation(t *testing.T) {
	root := t.TempDir()
	const resolved = "versions/v-eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	const rotated = "versions/v-ffffffffffffffffffffffffffffffff"
	if err := os.Symlink(resolved, filepath.Join(root, "current")); err != nil {
		t.Fatalf("Symlink(current): %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "calls")
	resolver := "target=$(readlink \"$STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR/current\"); rm -f \"$STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR/current\"; ln -s \"" + rotated + "\" \"$STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR/current\"; printf '%s\\n' \"$target\""
	binary := writeDemoStub(t, logPath, resolver)
	command := exec.Command("bash", filepath.Join("scripts", "demo-agent-governed-edit.sh"))
	command.Env = append(os.Environ(),
		"STACK_FITNESS_FUNCTIONS_BIN="+binary,
		"STACK_FITNESS_FUNCTIONS_DEMO_TLS_CONTRACT_ONLY=1",
		"STACK_FITNESS_FUNCTIONS_ADDR=",
		"STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR="+root,
		"STACK_FITNESS_FUNCTIONS_CLIENT_CERT=",
		"STACK_FITNESS_FUNCTIONS_CLIENT_KEY=",
		"STACK_FITNESS_FUNCTIONS_CLIENT_CA=",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run demo rotation: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), filepath.Join(root, filepath.FromSlash(resolved), "client.crt")) || strings.Contains(string(output), filepath.Join(root, filepath.FromSlash(rotated), "client.crt")) {
		t.Fatalf("demo did not preserve resolved paths after rotation:\n%s", output)
	}
	current, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil || current != rotated {
		t.Fatalf("current = %q/%v, want %q", current, err, rotated)
	}
}

func writeDemoStub(t *testing.T, logPath, resolver string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stack-fitness-functions")
	script := "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >>" + shellQuote(logPath) + "\nif [[ \"$*\" == \"client resolve-dev-cert-version\" ]]; then " + resolver + "; resolver_rc=$?; exit \"$resolver_rc\"; fi\nexit 99\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(demo stub): %v", err)
	}
	return path
}
