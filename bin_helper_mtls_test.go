package agent_fitness_functions_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// stubBridge writes a fake `agent-fitness-functions` binary that records the
// arguments of each invocation (one per line) into recordPath and emits a
// passing validation response so the helper completes without a real server.
func stubBridge(t *testing.T, dir, recordPath string) string {
	t.Helper()
	bridge := filepath.Join(dir, "agent-fitness-functions")
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

// TestAgentFitnessFunctionsTestPassesMTLS verifies the helper authenticates:
// it must default to an https addr and forward discovered mTLS client
// credentials to `client validate`. Regression guard for calm-poc-qo7, where
// the helper defaulted to plain HTTP with no certs and every check returned
// HTTP 401.
func TestAgentFitnessFunctionsTestPassesMTLS(t *testing.T) {
	helper, err := filepath.Abs(filepath.Join("bin", "agent-fitness-functions-test"))
	if err != nil {
		t.Fatalf("abs helper path: %v", err)
	}

	_, file := newGitRepoWithFile(t)

	// A cert directory the helper should auto-discover via AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR.
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
		"AGENT_FITNESS_FUNCTIONS_BIN="+bridge,
		"AGENT_FITNESS_FUNCTIONS_REPO_NAME=calm-poc",
		"AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR="+certDir,
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

// TestAgentFitnessFunctionsTestEnvOverridesCerts verifies explicit
// AGENT_FITNESS_FUNCTIONS_CLIENT_* env vars take precedence over directory
// discovery, following 12-factor config precedence.
func TestAgentFitnessFunctionsTestEnvOverridesCerts(t *testing.T) {
	helper, err := filepath.Abs(filepath.Join("bin", "agent-fitness-functions-test"))
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
		"AGENT_FITNESS_FUNCTIONS_BIN="+bridge,
		"AGENT_FITNESS_FUNCTIONS_REPO_NAME=calm-poc",
		"AGENT_FITNESS_FUNCTIONS_CLIENT_CERT="+filepath.Join(envCertDir, "hook.crt"),
		"AGENT_FITNESS_FUNCTIONS_CLIENT_KEY="+filepath.Join(envCertDir, "hook.key"),
		"AGENT_FITNESS_FUNCTIONS_CLIENT_CA="+filepath.Join(envCertDir, "roots.crt"),
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

// TestAgentFitnessFunctionsTestErrorsWhenRepoNameUndetectable verifies the helper
// fails with an explicit remediation instead of silently falling back to calm-poc
// when the working tree has no configs/<basename> or .calm/config.json to infer the
// governance repo name from. Regression guard against masking a misconfigured repo.
func TestAgentFitnessFunctionsTestErrorsWhenRepoNameUndetectable(t *testing.T) {
	helper, err := filepath.Abs(filepath.Join("bin", "agent-fitness-functions-test"))
	if err != nil {
		t.Fatalf("abs helper path: %v", err)
	}

	// A git repo whose basename has no server-side config and no .calm/config.json,
	// so detection cannot infer a name.
	_, file := newGitRepoWithFile(t)

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
		"AGENT_FITNESS_FUNCTIONS_BIN="+bridge,
		"AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR="+certDir,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("helper succeeded, want failure when repo name is undetectable; output=%s", out)
	}
	if !strings.Contains(string(out), "AGENT_FITNESS_FUNCTIONS_REPO_NAME") {
		t.Fatalf("output = %s, want remediation naming AGENT_FITNESS_FUNCTIONS_REPO_NAME", out)
	}
	if _, statErr := os.Stat(recordPath); statErr == nil {
		t.Fatalf("stub bridge was invoked; helper should fail before calling client validate")
	}
}

// TestAgentFitnessFunctionsTestUsesExplicitRepoName verifies an explicit
// AGENT_FITNESS_FUNCTIONS_REPO_NAME is forwarded to client validate, which is the
// supported way to name the governance repo when detection cannot infer it.
func TestAgentFitnessFunctionsTestUsesExplicitRepoName(t *testing.T) {
	helper, err := filepath.Abs(filepath.Join("bin", "agent-fitness-functions-test"))
	if err != nil {
		t.Fatalf("abs helper path: %v", err)
	}

	_, file := newGitRepoWithFile(t)

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
		"AGENT_FITNESS_FUNCTIONS_BIN="+bridge,
		"AGENT_FITNESS_FUNCTIONS_REPO_NAME=calm-poc",
		"AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR="+certDir,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, out)
	}

	recorded, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read invocations: %v", err)
	}
	if !strings.Contains(string(recorded), "--repo calm-poc") {
		t.Fatalf("invocations = %s, want explicit --repo calm-poc", recorded)
	}
}

func TestAgentFitnessFunctionsTestFailsWhenClientValidateFails(t *testing.T) {
	helper, err := filepath.Abs(filepath.Join("bin", "agent-fitness-functions-test"))
	if err != nil {
		t.Fatalf("abs helper path: %v", err)
	}

	_, file := newGitRepoWithFile(t)

	certDir := t.TempDir()
	for _, name := range []string{"client.crt", "client.key", "ca.crt"} {
		if err := os.WriteFile(filepath.Join(certDir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	bridgeDir := t.TempDir()
	bridge := filepath.Join(bridgeDir, "agent-fitness-functions")
	script := "#!/usr/bin/env bash\n" +
		"echo 'check failed with HTTP 403: caller \"dev-hook-pool\" is not authorized for repository \"wrong-repo\"' >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(bridge, []byte(script), 0o755); err != nil {
		t.Fatalf("write failing stub bridge: %v", err)
	}

	cmd := exec.Command(helper, file)
	cmd.Env = append(os.Environ(),
		"AGENT_FITNESS_FUNCTIONS_BIN="+bridge,
		"AGENT_FITNESS_FUNCTIONS_REPO_NAME=wrong-repo",
		"AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR="+certDir,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("helper succeeded, want failure; output=%s", out)
	}
	if strings.Contains(string(out), "  PASS       ") {
		t.Fatalf("output = %s, want helper to fail instead of reporting PASS", out)
	}
	if !strings.Contains(string(out), "check failed with HTTP 403") {
		t.Fatalf("output = %s, want client validate error surfaced", out)
	}
}
