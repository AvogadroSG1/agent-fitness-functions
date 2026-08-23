package calm_poc_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/devcerts"
)

// stubBridge writes a fake `stack-fitness-functions` binary that records the
// arguments of each invocation (one per line) into recordPath and emits a
// passing validation response so the helper completes without a real server.
func stubBridge(t *testing.T, dir, recordPath string) string {
	t.Helper()
	bridge := filepath.Join(dir, "stack-fitness-functions")
	script := "#!/usr/bin/env bash\n" +
		"if [[ \"$*\" == \"client resolve-dev-cert-version\" ]]; then printf '%s\\n' \"$*\" >> " + shellQuote(recordPath) + "; readlink \"$STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR/current\"; exit 0; fi\n" +
		"printf 'selector=%s|%s\\n' \"${STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR-unset}\" \"$*\" >> " + shellQuote(recordPath) + "\n" +
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
	version := publishManagedCertFixture(t, certDir)

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
	if calls := strings.Count(invocations, "client resolve-dev-cert-version"); calls != 1 {
		t.Fatalf("resolver calls = %d, want 1:\n%s", calls, invocations)
	}
	if !strings.Contains(invocations, "selector=unset|") {
		t.Fatalf("managed helper did not unset selector before validation:\n%s", invocations)
	}

	for _, want := range []string{
		"--addr https://",
		"--client-cert " + filepath.Join(certDir, filepath.FromSlash(version), "client.crt"),
		"--client-key " + filepath.Join(certDir, filepath.FromSlash(version), "client.key"),
		"--client-ca " + filepath.Join(certDir, filepath.FromSlash(version), "ca.crt"),
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
	if strings.Contains(invocations, "client resolve-dev-cert-version") {
		t.Fatalf("external TLS mode invoked managed resolver:\n%s", invocations)
	}
	if !strings.Contains(invocations, "selector=unset|") {
		t.Fatalf("external helper did not remove empty selector before validation:\n%s", invocations)
	}
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

// TestStackFitnessFunctionsTestErrorsWhenRepoNameUndetectable verifies the helper
// fails with an explicit remediation instead of silently falling back to calm-poc
// when the working tree has no configs/<basename> or .calm/config.json to infer the
// governance repo name from. Regression guard against masking a misconfigured repo.
func TestStackFitnessFunctionsTestErrorsWhenRepoNameUndetectable(t *testing.T) {
	helper, err := filepath.Abs(filepath.Join("bin", "stack-fitness-functions-test"))
	if err != nil {
		t.Fatalf("abs helper path: %v", err)
	}

	// A git repo whose basename has no server-side config and no .calm/config.json,
	// so detection cannot infer a name.
	_, file := newGitRepoWithFile(t)

	certDir := t.TempDir()
	publishManagedCertFixture(t, certDir)

	recordPath := filepath.Join(t.TempDir(), "invocations.log")
	bridge := stubBridge(t, t.TempDir(), recordPath)

	cmd := exec.Command(helper, file)
	cmd.Env = append(os.Environ(),
		"STACK_FITNESS_FUNCTIONS_BIN="+bridge,
		"STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR="+certDir,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("helper succeeded, want failure when repo name is undetectable; output=%s", out)
	}
	if !strings.Contains(string(out), "STACK_FITNESS_FUNCTIONS_REPO_NAME") {
		t.Fatalf("output = %s, want remediation naming STACK_FITNESS_FUNCTIONS_REPO_NAME", out)
	}
	if recorded, readErr := os.ReadFile(recordPath); readErr != nil || strings.Contains(string(recorded), "client validate") {
		t.Fatalf("client validate was invoked before repo-name detection; calls=%q error=%v", recorded, readErr)
	}
}

// TestStackFitnessFunctionsTestUsesExplicitRepoName verifies an explicit
// STACK_FITNESS_FUNCTIONS_REPO_NAME is forwarded to client validate, which is the
// supported way to name the governance repo when detection cannot infer it.
func TestStackFitnessFunctionsTestUsesExplicitRepoName(t *testing.T) {
	helper, err := filepath.Abs(filepath.Join("bin", "stack-fitness-functions-test"))
	if err != nil {
		t.Fatalf("abs helper path: %v", err)
	}

	_, file := newGitRepoWithFile(t)

	certDir := t.TempDir()
	publishManagedCertFixture(t, certDir)

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
	if !strings.Contains(string(recorded), "--repo calm-poc") {
		t.Fatalf("invocations = %s, want explicit --repo calm-poc", recorded)
	}
}

func TestStackFitnessFunctionsTestFailsWhenClientValidateFails(t *testing.T) {
	helper, err := filepath.Abs(filepath.Join("bin", "stack-fitness-functions-test"))
	if err != nil {
		t.Fatalf("abs helper path: %v", err)
	}

	_, file := newGitRepoWithFile(t)

	certDir := t.TempDir()
	publishManagedCertFixture(t, certDir)

	bridgeDir := t.TempDir()
	bridge := filepath.Join(bridgeDir, "stack-fitness-functions")
	script := "#!/usr/bin/env bash\n" +
		"if [[ \"$*\" == \"client resolve-dev-cert-version\" ]]; then readlink \"$STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR/current\"; exit 0; fi\n" +
		"echo 'check failed with HTTP 403: caller \"dev-hook-pool\" is not authorized for repository \"wrong-repo\"' >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(bridge, []byte(script), 0o755); err != nil {
		t.Fatalf("write failing stub bridge: %v", err)
	}

	cmd := exec.Command(helper, file)
	cmd.Env = append(os.Environ(),
		"STACK_FITNESS_FUNCTIONS_BIN="+bridge,
		"STACK_FITNESS_FUNCTIONS_REPO_NAME=wrong-repo",
		"STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR="+certDir,
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

func TestStackFitnessFunctionsTestManagedFailureMatrix(t *testing.T) {
	helper, err := filepath.Abs(filepath.Join("bin", "stack-fitness-functions-test"))
	if err != nil {
		t.Fatalf("Abs(helper): %v", err)
	}
	_, file := newGitRepoWithFile(t)
	certDir := t.TempDir()
	publishManagedCertFixture(t, certDir)
	tests := []struct {
		name     string
		resolver string
		extraEnv []string
		want     string
	}{
		{name: "ambiguity", resolver: "exit 99", extraEnv: []string{"STACK_FITNESS_FUNCTIONS_CLIENT_CERT=/external/client.crt"}, want: "cannot be combined with explicit client TLS inputs"},
		{name: "malformed", resolver: "printf 'versions/not-valid\\n'", want: "invalid managed certificate version"},
		{name: "resolver failure", resolver: "echo 'helper resolver failed safely' >&2; exit 7", want: "helper resolver failed safely"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bridge := writeBinHelperStub(t, tt.resolver)
			command := exec.Command(helper, file)
			command.Env = append(os.Environ(),
				"STACK_FITNESS_FUNCTIONS_BIN="+bridge,
				"STACK_FITNESS_FUNCTIONS_REPO_NAME=calm-poc",
				"STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR="+certDir,
				"STACK_FITNESS_FUNCTIONS_CLIENT_CERT=",
				"STACK_FITNESS_FUNCTIONS_CLIENT_KEY=",
				"STACK_FITNESS_FUNCTIONS_CLIENT_CA=",
			)
			command.Env = append(command.Env, tt.extraEnv...)
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("helper succeeded, want exit 1; output=%s", output)
			}
			exitErr, ok := err.(*exec.ExitError)
			if !ok || exitErr.ExitCode() != 1 {
				t.Fatalf("helper error = %v, want exit 1", err)
			}
			if !strings.Contains(string(output), tt.want) {
				t.Fatalf("output = %q, want %q", output, tt.want)
			}
		})
	}
}

func TestStackFitnessFunctionsTestKeepsResolvedPathsAfterRotation(t *testing.T) {
	helper, err := filepath.Abs(filepath.Join("bin", "stack-fitness-functions-test"))
	if err != nil {
		t.Fatalf("Abs(helper): %v", err)
	}
	_, file := newGitRepoWithFile(t)
	certDir := t.TempDir()
	resolved := publishManagedCertFixture(t, certDir)
	const rotated = "versions/v-dddddddddddddddddddddddddddddddd"
	copyBinHelperVersion(t, filepath.Join(certDir, filepath.FromSlash(resolved)), filepath.Join(certDir, filepath.FromSlash(rotated)))
	logPath := filepath.Join(t.TempDir(), "calls")
	bridge := filepath.Join(t.TempDir(), "stack-fitness-functions")
	script := `#!/usr/bin/env bash
if [[ "$*" == "client resolve-dev-cert-version" ]]; then
  target=$(readlink "$STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR/current")
  rm -f "$STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR/current"
  ln -s "` + rotated + `" "$STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR/current"
  printf '%s\n' "$target"
  exit 0
fi
printf 'selector=%s|%s\n' "${STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR-unset}" "$*" >>` + shellQuote(logPath) + `
printf '{"status":"pass"}\n'
`
	if err := os.WriteFile(bridge, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(bridge): %v", err)
	}
	command := exec.Command(helper, file)
	command.Env = append(os.Environ(),
		"STACK_FITNESS_FUNCTIONS_BIN="+bridge,
		"STACK_FITNESS_FUNCTIONS_REPO_NAME=calm-poc",
		"STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR="+certDir,
		"STACK_FITNESS_FUNCTIONS_CLIENT_CERT=",
		"STACK_FITNESS_FUNCTIONS_CLIENT_KEY=",
		"STACK_FITNESS_FUNCTIONS_CLIENT_CA=",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("helper failed: %v\n%s", err, output)
	}
	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(calls): %v", err)
	}
	resolvedClient := filepath.Join(certDir, filepath.FromSlash(resolved), "client.crt")
	rotatedClient := filepath.Join(certDir, filepath.FromSlash(rotated), "client.crt")
	if !strings.Contains(string(calls), "selector=unset|") || !strings.Contains(string(calls), resolvedClient) || strings.Contains(string(calls), rotatedClient) {
		t.Fatalf("helper calls did not preserve resolved paths with selector unset:\n%s", calls)
	}
}

func writeBinHelperStub(t *testing.T, resolver string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stack-fitness-functions")
	script := "#!/usr/bin/env bash\nif [[ \"$*\" == \"client resolve-dev-cert-version\" ]]; then " + resolver + "; resolver_rc=$?; exit \"$resolver_rc\"; fi\nprintf '{\"status\":\"pass\"}\\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(stub): %v", err)
	}
	return path
}

func copyBinHelperVersion(t *testing.T, source, destination string) {
	t.Helper()
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatalf("MkdirAll(destination): %v", err)
	}
	for _, file := range []struct {
		name string
		mode os.FileMode
	}{
		{name: "ca.crt", mode: 0o644}, {name: "client.crt", mode: 0o644}, {name: "client.key", mode: 0o600},
		{name: "server.crt", mode: 0o644}, {name: "server.key", mode: 0o600},
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

func publishManagedCertFixture(t *testing.T, root string) string {
	t.Helper()
	if err := devcerts.Publish(root, false); err != nil {
		t.Fatalf("Publish(%s): %v", root, err)
	}
	target, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil {
		t.Fatalf("Readlink(current): %v", err)
	}
	if !strings.HasPrefix(target, "versions/v-") || len(strings.TrimPrefix(target, "versions/v-")) != 32 {
		t.Fatalf("current target = %q, want generated first publication", target)
	}
	return target
}
