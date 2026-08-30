package calm_poc_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/devcerts"
)

// stubBridge writes a fake `agent-fitness-functions` binary that records the
// arguments of each invocation (one per line) into recordPath and emits a
// passing validation response so the helper completes without a real server.
func stubBridge(t *testing.T, dir, recordPath string) string {
	t.Helper()
	bridge := filepath.Join(dir, "agent-fitness-functions")
	script := "#!/usr/bin/env bash\n" +
		"if [[ \"$*\" == \"client resolve-dev-cert-version\" ]]; then printf '%s\\n' \"$*\" >> " + shellQuote(recordPath) + "; readlink \"$AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR/current\"; exit 0; fi\n" +
		"printf 'selector=%s|%s\\n' \"${AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR-unset}\" \"$*\" >> " + shellQuote(recordPath) + "\n" +
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

// TestStackFitnessFunctionsTestManagedModePassesSelectorNoTLSFlags is the
// ADR-0007 recast of the former TestStackFitnessFunctionsTestPassesMTLS
// (regression guard for calm-poc-qo7: the helper must be able to
// authenticate, must default to https). Under the new contract the client
// authenticates itself: managed mode passes NO --client-cert/key/ca flags and
// never calls the resolver — `client validate` resolves the machine
// governance root itself. The helper's only remaining job in managed mode is
// to default to an https addr and leave AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR
// untouched for the child to see.
func TestStackFitnessFunctionsTestManagedModePassesSelectorNoTLSFlags(t *testing.T) {
	helper, err := filepath.Abs(filepath.Join("bin", "agent-fitness-functions-test"))
	if err != nil {
		t.Fatalf("abs helper path: %v", err)
	}

	run := func(t *testing.T, devCertDir string) string {
		t.Helper()
		_, file := newGitRepoWithFile(t)
		recordPath := filepath.Join(t.TempDir(), "invocations.log")
		bridge := stubBridge(t, t.TempDir(), recordPath)

		env := append(os.Environ(),
			"AGENT_FITNESS_FUNCTIONS_BIN="+bridge,
			"AGENT_FITNESS_FUNCTIONS_REPO_NAME=calm-poc",
		)
		// Leave AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR genuinely absent (not
		// set-to-empty) so the stub bridge's "${VAR-unset}" probe can tell
		// the difference — set-to-empty is a distinct state from unset.
		if devCertDir != "" {
			env = append(env, "AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR="+devCertDir)
		}
		cmd := exec.Command(helper, file)
		cmd.Env = env
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
		return invocations
	}

	t.Run("selector set", func(t *testing.T) {
		certDir := t.TempDir()
		invocations := run(t, certDir)
		if strings.Contains(invocations, "client resolve-dev-cert-version") {
			t.Fatalf("managed mode must not call the resolver — `client validate` resolves the machine governance root itself (ADR-0007):\n%s", invocations)
		}
		if strings.Contains(invocations, "--client-cert") {
			t.Fatalf("managed mode must not pass --client-cert; the client resolves its own credentials:\n%s", invocations)
		}
		if !strings.Contains(invocations, "selector="+certDir+"|") {
			t.Fatalf("the AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR selector must pass through to client validate untouched:\n%s", invocations)
		}
		if !strings.Contains(invocations, "--addr https://") {
			t.Errorf("expected helper to default to an https addr; got invocations:\n%s", invocations)
		}
	})

	t.Run("selector unset", func(t *testing.T) {
		invocations := run(t, "")
		if strings.Contains(invocations, "client resolve-dev-cert-version") {
			t.Fatalf("managed mode must not call the resolver:\n%s", invocations)
		}
		if strings.Contains(invocations, "--client-cert") {
			t.Fatalf("managed mode must not pass --client-cert:\n%s", invocations)
		}
		if !strings.Contains(invocations, "selector=unset|") {
			t.Fatalf("expected selector to read as unset when AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR is not set:\n%s", invocations)
		}
	})
}

// TestStackFitnessFunctionsTestEnvOverridesCerts verifies explicit
// AGENT_FITNESS_FUNCTIONS_CLIENT_* env vars take precedence over directory
// discovery, following 12-factor config precedence.
func TestStackFitnessFunctionsTestEnvOverridesCerts(t *testing.T) {
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
	helper, err := filepath.Abs(filepath.Join("bin", "agent-fitness-functions-test"))
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
	// Under the ADR-0007 contract managed mode never calls the bridge before
	// repo-name detection (no resolver call happens either), so the log file
	// may legitimately not exist at all — that itself proves the bridge was
	// never invoked.
	recorded, readErr := os.ReadFile(recordPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read invocations: %v", readErr)
	}
	if strings.Contains(string(recorded), "client validate") {
		t.Fatalf("client validate was invoked before repo-name detection; calls=%q", recorded)
	}
}

// TestStackFitnessFunctionsTestUsesExplicitRepoName verifies an explicit
// AGENT_FITNESS_FUNCTIONS_REPO_NAME is forwarded to client validate, which is the
// supported way to name the governance repo when detection cannot infer it.
func TestStackFitnessFunctionsTestUsesExplicitRepoName(t *testing.T) {
	helper, err := filepath.Abs(filepath.Join("bin", "agent-fitness-functions-test"))
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

// TestStackFitnessFunctionsTestFailsWhenClientValidateFails verifies the helper
// surfaces a `client validate` failure instead of reporting a false PASS.
func TestStackFitnessFunctionsTestFailsWhenClientValidateFails(t *testing.T) {
	helper, err := filepath.Abs(filepath.Join("bin", "agent-fitness-functions-test"))
	if err != nil {
		t.Fatalf("abs helper path: %v", err)
	}

	_, file := newGitRepoWithFile(t)

	certDir := t.TempDir()
	publishManagedCertFixture(t, certDir)

	bridgeDir := t.TempDir()
	bridge := filepath.Join(bridgeDir, "agent-fitness-functions")
	// The resolver branch is dead under the ADR-0007 contract — managed mode
	// never calls `client resolve-dev-cert-version` from the helper — so the
	// stub only needs to fail `client validate`.
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

// TestStackFitnessFunctionsTestManagedFailureMatrix covers the failure modes
// the helper itself is still responsible for detecting under the ADR-0007
// contract. Only "ambiguity" (the managed-vs-explicit-TLS conflict guard)
// remains: managed cert resolution and version validation now live entirely
// inside `client validate`, so the "malformed resolver output" and "resolver
// failure" cases from the old resolve-then-unset contract no longer apply
// here — the helper never invokes the resolver at all.
func TestStackFitnessFunctionsTestManagedFailureMatrix(t *testing.T) {
	helper, err := filepath.Abs(filepath.Join("bin", "agent-fitness-functions-test"))
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
		{name: "ambiguity", resolver: "exit 99", extraEnv: []string{"AGENT_FITNESS_FUNCTIONS_CLIENT_CERT=/external/client.crt"}, want: "cannot be combined with explicit client TLS inputs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bridge := writeBinHelperStub(t, tt.resolver)
			command := exec.Command(helper, file)
			command.Env = append(os.Environ(),
				"AGENT_FITNESS_FUNCTIONS_BIN="+bridge,
				"AGENT_FITNESS_FUNCTIONS_REPO_NAME=calm-poc",
				"AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR="+certDir,
				"AGENT_FITNESS_FUNCTIONS_CLIENT_CERT=",
				"AGENT_FITNESS_FUNCTIONS_CLIENT_KEY=",
				"AGENT_FITNESS_FUNCTIONS_CLIENT_CA=",
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

func writeBinHelperStub(t *testing.T, resolver string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent-fitness-functions")
	script := "#!/usr/bin/env bash\nif [[ \"$*\" == \"client resolve-dev-cert-version\" ]]; then " + resolver + "; resolver_rc=$?; exit \"$resolver_rc\"; fi\nprintf '{\"status\":\"pass\"}\\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(stub): %v", err)
	}
	return path
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
