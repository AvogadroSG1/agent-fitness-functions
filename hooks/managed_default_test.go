package hooks

// Red contract for calm-poc-mx5.4 (ADR-0007 hook-contract amendment,
// supersedes the q8d.11.3 resolve-then-unset handoff): managed-mode hooks no
// longer resolve, pin, or pass client TLS material. `client validate` resolves
// the machine governance root itself, so daemon auto-start always carries its
// managed root — the calm-poc-wgi dead-daemon failure cannot recur. Explicit
// AGENT_FITNESS_FUNCTIONS_CLIENT_* passthrough and the selector-vs-explicit
// conflict guard keep their existing behavior.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var allTLSHookScripts = []string{"pre-commit.sh", "pre-push.sh", "pre-tool-use.sh"}

func TestManagedHooksPassNoTLSMaterialAndNeverInvokeResolver(t *testing.T) {
	for _, script := range allTLSHookScripts {
		t.Run(script, func(t *testing.T) {
			repo, input := managedHookRepo(t, script)
			logPath := filepath.Join(t.TempDir(), "calls")
			binary := writeHookStub(t, `#!/usr/bin/env bash
if [[ "$*" == "client resolve-dev-cert-version" ]]; then
  printf 'resolver-invoked\n' >>"$CALL_LOG"
  printf 'versions/v-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
  exit 0
fi
printf 'selector=%s|%s\n' "${AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR-unset}" "$*" >>"$CALL_LOG"
printf '{"status":"pass"}\n'`)
			output, exit := runManagedHook(t, repo, script, binary, input, []string{
				"CALL_LOG=" + logPath,
				"AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR=",
			})
			if exit != 0 {
				t.Fatalf("managed hook exit = %d; output=%s", exit, output)
			}
			calls, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("ReadFile(calls): %v", err)
			}
			if strings.Contains(string(calls), "resolver-invoked") {
				t.Fatalf("hook still invokes client resolve-dev-cert-version; client validate owns managed resolution now: %s", calls)
			}
			for _, flag := range []string{"--client-cert", "--client-key", "--client-ca"} {
				if strings.Contains(string(calls), flag) {
					t.Fatalf("hook passed %s in managed mode; TLS material must not be resolved hook-side: %s", flag, calls)
				}
			}
		})
	}
}

func TestManagedHooksPassSelectorThroughToClient(t *testing.T) {
	for _, script := range allTLSHookScripts {
		t.Run(script, func(t *testing.T) {
			repo, input := managedHookRepo(t, script)
			certRoot := filepath.Join(repo, "pinned-certs")
			logPath := filepath.Join(t.TempDir(), "calls")
			binary := writeHookStub(t, `#!/usr/bin/env bash
printf 'selector=%s|%s\n' "${AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR-unset}" "$*" >>"$CALL_LOG"
printf '{"status":"pass"}\n'`)
			output, exit := runManagedHook(t, repo, script, binary, input, []string{
				"CALL_LOG=" + logPath,
				"AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR=" + certRoot,
			})
			if exit != 0 {
				t.Fatalf("managed hook exit = %d; output=%s", exit, output)
			}
			calls, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("ReadFile(calls): %v", err)
			}
			if !strings.Contains(string(calls), "selector="+certRoot+"|") {
				t.Fatalf("selector did not pass through to client validate (it owns managed resolution now): %s", calls)
			}
			if strings.Contains(string(calls), "--client-cert") {
				t.Fatalf("hook resolved TLS material from the selector instead of passing it through: %s", calls)
			}
		})
	}
}
