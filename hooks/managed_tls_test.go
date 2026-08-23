package hooks

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedResolverFailuresHaveHookSpecificExits(t *testing.T) {
	tests := []struct {
		name     string
		script   string
		wantExit int
	}{
		{name: "pre-commit", script: "pre-commit.sh", wantExit: 2},
		{name: "pre-push", script: "pre-push.sh", wantExit: 1},
		{name: "pre-tool-use", script: "pre-tool-use.sh", wantExit: 2},
	}
	resolverCases := []struct {
		name       string
		resolution string
		want       string
	}{
		{name: "malformed", resolution: "printf 'versions/not-valid\\n'", want: "invalid managed certificate version"},
		{name: "failure", resolution: "echo 'managed resolution failed safely' >&2; exit 9", want: "managed resolution failed safely"},
	}
	for _, tt := range tests {
		for _, resolver := range resolverCases {
			t.Run(tt.name+"/"+resolver.name, func(t *testing.T) {
				repo, input := managedHookRepo(t, tt.script)
				binary := writeHookStub(t, "#!/usr/bin/env bash\nif [[ \"$*\" == \"client resolve-dev-cert-version\" ]]; then "+resolver.resolution+"; exit 0; fi\nprintf '{\"status\":\"pass\"}\\n'")
				output, exit := runManagedHook(t, repo, tt.script, binary, input, []string{"STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR=" + filepath.Join(repo, "certs")})
				if exit != tt.wantExit {
					t.Fatalf("exit = %d, want %d; output=%s", exit, tt.wantExit, output)
				}
				if !strings.Contains(string(output), resolver.want) || strings.Contains(string(output), "PRIVATE KEY") {
					t.Fatalf("output = %q, want safe %q diagnostic", output, resolver.want)
				}
			})
		}
	}
}

func TestManagedSelectorConflictHasHookSpecificExits(t *testing.T) {
	tests := []struct {
		name     string
		wantExit int
	}{
		{name: "pre-commit.sh", wantExit: 2},
		{name: "pre-push.sh", wantExit: 1},
		{name: "pre-tool-use.sh", wantExit: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, input := managedHookRepo(t, tt.name)
			binary := writeHookStub(t, "#!/usr/bin/env bash\nexit 99\n")
			output, exit := runManagedHook(t, repo, tt.name, binary, input, []string{
				"STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR=" + filepath.Join(repo, "certs"),
				"STACK_FITNESS_FUNCTIONS_CLIENT_CERT=/external/client.crt",
			})
			if exit != tt.wantExit || !strings.Contains(string(output), "cannot be combined with explicit client TLS inputs") {
				t.Fatalf("exit/output = %d/%q, want %d selector conflict", exit, output, tt.wantExit)
			}
		})
	}
}

func TestExternalHookTLSNeverInvokesManagedResolver(t *testing.T) {
	for _, script := range []string{"pre-commit.sh", "pre-push.sh", "pre-tool-use.sh"} {
		t.Run(script, func(t *testing.T) {
			repo, input := managedHookRepo(t, script)
			logPath := filepath.Join(t.TempDir(), "calls")
			external := t.TempDir()
			for _, name := range []string{"client.crt", "client.key", "ca.crt"} {
				if err := os.WriteFile(filepath.Join(external, name), []byte("external"), 0o600); err != nil {
					t.Fatalf("WriteFile(%s): %v", name, err)
				}
			}
			binary := writeHookStub(t, "#!/usr/bin/env bash\nprintf 'selector=%s|%s\\n' \"${STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR-unset}\" \"$*\" >>\"$CALL_LOG\"\nprintf '{\"status\":\"pass\"}\\n'")
			output, exit := runManagedHook(t, repo, script, binary, input, []string{
				"CALL_LOG=" + logPath,
				"STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR=",
				"STACK_FITNESS_FUNCTIONS_CLIENT_CERT=" + filepath.Join(external, "client.crt"),
				"STACK_FITNESS_FUNCTIONS_CLIENT_KEY=" + filepath.Join(external, "client.key"),
				"STACK_FITNESS_FUNCTIONS_CLIENT_CA=" + filepath.Join(external, "ca.crt"),
			})
			if exit != 0 {
				t.Fatalf("external hook exit = %d; output=%s", exit, output)
			}
			calls, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("ReadFile(calls): %v", err)
			}
			if bytes.Contains(calls, []byte("resolve-dev-cert-version")) || !bytes.Contains(calls, []byte("--client-cert "+filepath.Join(external, "client.crt"))) {
				t.Fatalf("external hook calls = %q, want validate with exact paths and no resolver", calls)
			}
		})
	}
}

func TestManagedHooksUnsetSelectorAndKeepResolvedPathsAcrossRotation(t *testing.T) {
	const rotatedTarget = "versions/v-cccccccccccccccccccccccccccccccc"
	for _, script := range []string{"pre-commit.sh", "pre-push.sh", "pre-tool-use.sh"} {
		t.Run(script, func(t *testing.T) {
			repo, input := managedHookRepo(t, script)
			certRoot := filepath.Join(repo, "certs")
			resolvedTarget := publishManagedCerts(t, certRoot)
			copyManagedHookVersion(t, filepath.Join(certRoot, filepath.FromSlash(resolvedTarget)), filepath.Join(certRoot, filepath.FromSlash(rotatedTarget)))
			logPath := filepath.Join(t.TempDir(), "calls")
			binary := writeHookStub(t, `#!/usr/bin/env bash
if [[ "$*" == "client resolve-dev-cert-version" ]]; then
  target=$(readlink "$STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR/current")
  rm -f "$STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR/current"
  ln -s "`+rotatedTarget+`" "$STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR/current"
  printf '%s\n' "$target"
  exit 0
fi
printf 'selector=%s|%s\n' "${STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR-unset}" "$*" >>"$CALL_LOG"
printf '{"status":"pass"}\n'
`)
			output, exit := runManagedHook(t, repo, script, binary, input, []string{
				"CALL_LOG=" + logPath,
				"STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR=" + certRoot,
			})
			if exit != 0 {
				t.Fatalf("managed hook exit = %d; output=%s", exit, output)
			}
			calls, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("ReadFile(calls): %v", err)
			}
			if !bytes.Contains(calls, []byte("selector=unset|")) {
				t.Fatalf("selector remained set for child invocation: %s", calls)
			}
			resolvedClient := filepath.Join(certRoot, filepath.FromSlash(resolvedTarget), "client.crt")
			rotatedClient := filepath.Join(certRoot, filepath.FromSlash(rotatedTarget), "client.crt")
			if !bytes.Contains(calls, []byte("--client-cert "+resolvedClient)) || bytes.Contains(calls, []byte(rotatedClient)) {
				t.Fatalf("hook did not keep resolved generation after rotation: %s", calls)
			}
			current, err := os.Readlink(filepath.Join(certRoot, "current"))
			if err != nil || current != rotatedTarget {
				t.Fatalf("current after resolver rotation = %q/%v, want %q", current, err, rotatedTarget)
			}
		})
	}
}

func copyManagedHookVersion(t *testing.T, source, destination string) {
	t.Helper()
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", destination, err)
	}
	for _, file := range []struct {
		name string
		mode os.FileMode
	}{
		{name: "ca.crt", mode: 0o644},
		{name: "client.crt", mode: 0o644},
		{name: "client.key", mode: 0o600},
		{name: "server.crt", mode: 0o644},
		{name: "server.key", mode: 0o600},
	} {
		content, err := os.ReadFile(filepath.Join(source, file.name))
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", file.name, err)
		}
		if err := os.WriteFile(filepath.Join(destination, file.name), content, file.mode); err != nil {
			t.Fatalf("WriteFile(%s): %v", file.name, err)
		}
	}
}

func managedHookRepo(t *testing.T, script string) (string, string) {
	t.Helper()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	gitBin := filepath.Join(repo, ".test-bin")
	if err := os.Mkdir(gitBin, 0o755); err != nil {
		t.Fatalf("Mkdir(fake git bin): %v", err)
	}
	fakeGit := `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "-C" ]]; then shift 2; fi
case "${1:-}" in
  rev-parse) printf '%s\n' "$FAKE_GIT_REPO" ;;
  diff)
    if [[ " $* " == *" -z "* ]]; then printf 'sample.go\0'; else printf 'sample.go\n'; fi
    ;;
  show) cat "$FAKE_GIT_REPO/sample.go" ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(gitBin, "git"), []byte(fakeGit), 0o755); err != nil {
		t.Fatalf("WriteFile(fake git): %v", err)
	}
	input := ""
	if script == "pre-push.sh" {
		input = "refs/heads/main head refs/heads/main base\n"
	}
	if script == "pre-tool-use.sh" {
		input = `{"tool_input":{"file_path":"sample.go","content":"package sample\n"}}`
	}
	return repo, input
}

func writeHookStub(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stack-fitness-functions")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(stub): %v", err)
	}
	return path
}

func runManagedHook(t *testing.T, repo, script, binary, input string, extraEnv []string) ([]byte, int) {
	t.Helper()
	command := exec.Command("bash", hookScriptPathFor(t, script))
	command.Dir = repo
	command.Stdin = strings.NewReader(input)
	command.Env = append(os.Environ(),
		"STACK_FITNESS_FUNCTIONS_BIN="+binary,
		"FAKE_GIT_REPO="+repo,
		"PATH="+filepath.Join(repo, ".test-bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	command.Env = append(command.Env, extraEnv...)
	output, err := command.CombinedOutput()
	if err == nil {
		return output, 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run hook: %v", err)
	}
	return output, exitErr.ExitCode()
}
