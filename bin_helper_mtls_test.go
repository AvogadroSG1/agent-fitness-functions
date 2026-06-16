package calm_poc_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// stubBridge writes a fake `stack-fitness-functions` binary that records the
// arguments of each invocation (one per line) into recordPath and emits a
// passing validation response so the helper completes without a real server.
func stubBridge(t *testing.T, dir, recordPath string) string {
	t.Helper()
	bridge := filepath.Join(dir, "stack-fitness-functions")
	script := "#!/usr/bin/env bash\n" +
		"printf '%s\\n' \"$*\" >> " + shellQuote(recordPath) + "\n" +
		"echo '{\"status\":\"pass\"}'\n"
	if err := os.WriteFile(bridge, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub bridge: %v", err)
	}
	return bridge
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// newGitRepoWithFile initializes a git repo containing a single Go fixture and
// returns the repo root and the fixture path.
func newGitRepoWithFile(t *testing.T) (string, string) {
	t.Helper()
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	file := filepath.Join(repo, "sample.go")
	if err := os.WriteFile(file, []byte("package sample\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return repo, file
}

// TestStackFitnessFunctionsTestPassesMTLS verifies the helper authenticates:
// it must default to an https addr and forward discovered mTLS client
// credentials to `client validate`. Regression guard for calm-poc-qo7, where
// the helper defaulted to plain HTTP with no certs and every check returned
// HTTP 401.
func TestStackFitnessFunctionsTestPassesMTLS(t *testing.T) {
	helper, err := filepath.Abs(filepath.Join("bin", "stack-fitness-functions-test"))
	if err != nil {
		t.Fatalf("abs helper path: %v", err)
	}

	_, file := newGitRepoWithFile(t)

	// A cert directory the helper should auto-discover via STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR.
	certDir := t.TempDir()
	for _, name := range []string{"client.crt", "client.key", "ca.crt"} {
		if err := os.WriteFile(filepath.Join(certDir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	recordPath := filepath.Join(t.TempDir(), "invocations.log")
	bridge := stubBridge(t, t.TempDir(), recordPath)

	cmd := exec.Command(helper, file)
	cmd.Env = append(os.Environ(),
		"STACK_FITNESS_FUNCTIONS_BIN="+bridge,
		"STACK_FITNESS_FUNCTIONS_REPO_NAME=calm-poc",
		"STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR="+certDir,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, out)
	}

	recorded, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read invocations: %v", err)
	}
	invocations := string(recorded)
	if invocations == "" {
		t.Fatalf("stub bridge was never invoked; helper output:\n%s", out)
	}

	for _, want := range []string{
		"--addr https://",
		"--client-cert " + filepath.Join(certDir, "client.crt"),
		"--client-key " + filepath.Join(certDir, "client.key"),
		"--client-ca " + filepath.Join(certDir, "ca.crt"),
	} {
		if !strings.Contains(invocations, want) {
			t.Errorf("expected helper to pass %q to client validate; got invocations:\n%s", want, invocations)
		}
	}
}

// TestStackFitnessFunctionsTestEnvOverridesCerts verifies explicit
// STACK_FITNESS_FUNCTIONS_CLIENT_* env vars take precedence over directory
// discovery, following 12-factor config precedence.
func TestStackFitnessFunctionsTestEnvOverridesCerts(t *testing.T) {
	helper, err := filepath.Abs(filepath.Join("bin", "stack-fitness-functions-test"))
	if err != nil {
		t.Fatalf("abs helper path: %v", err)
	}

	_, file := newGitRepoWithFile(t)

	envCertDir := t.TempDir()
	for _, name := range []string{"hook.crt", "hook.key", "roots.crt"} {
		if err := os.WriteFile(filepath.Join(envCertDir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	recordPath := filepath.Join(t.TempDir(), "invocations.log")
	bridge := stubBridge(t, t.TempDir(), recordPath)

	cmd := exec.Command(helper, file)
	cmd.Env = append(os.Environ(),
		"STACK_FITNESS_FUNCTIONS_BIN="+bridge,
		"STACK_FITNESS_FUNCTIONS_REPO_NAME=calm-poc",
		"STACK_FITNESS_FUNCTIONS_CLIENT_CERT="+filepath.Join(envCertDir, "hook.crt"),
		"STACK_FITNESS_FUNCTIONS_CLIENT_KEY="+filepath.Join(envCertDir, "hook.key"),
		"STACK_FITNESS_FUNCTIONS_CLIENT_CA="+filepath.Join(envCertDir, "roots.crt"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, out)
	}

	recorded, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read invocations: %v", err)
	}
	invocations := string(recorded)
	for _, want := range []string{
		"--client-cert " + filepath.Join(envCertDir, "hook.crt"),
		"--client-key " + filepath.Join(envCertDir, "hook.key"),
		"--client-ca " + filepath.Join(envCertDir, "roots.crt"),
	} {
		if !strings.Contains(invocations, want) {
			t.Errorf("expected env-provided cert %q; got invocations:\n%s", want, invocations)
		}
	}
}
