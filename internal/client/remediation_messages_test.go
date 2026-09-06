package client

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// S1 red-test contract (calm-poc-vdxf): setup-failure messages must steer a local
// developer at the managed flow, not at container operations, and must not print
// unmanaged-TLS placeholder noise.

// The doctor server-reachable remediation must name `client onboard` (which
// auto-starts the local daemon) instead of docker compose, while keeping the
// --addr escape hatch for remote daemons.
func TestCheckServerReachableRemediationNamesOnboard(t *testing.T) {
	cfg := doctorConfig{
		addr:       "https://127.0.0.1:1",
		tlsLoaded:  true,
		httpClient: &http.Client{Timeout: 500 * time.Millisecond},
	}
	result := checkServerReachable(cfg)
	if result.passed {
		t.Fatalf("expected unreachable server, got passed=%v", result.passed)
	}
	if !strings.Contains(result.remediation, "client onboard") {
		t.Errorf("remediation %q must point at `client onboard`", result.remediation)
	}
	if !strings.Contains(result.remediation, "--addr") {
		t.Errorf("remediation %q must keep the --addr escape hatch", result.remediation)
	}
	if strings.Contains(result.remediation, "docker compose") {
		t.Errorf("remediation %q must not point local developers at docker compose", result.remediation)
	}
}

// A health-wait failure outside managed dev TLS must say so in words instead of
// rendering tls=off with <none> placeholders — and must only claim explicit client
// certs are in use when the caller actually supplied them (ManagedRoot is also empty
// when a remote --addr disabled managed TLS with no certs given at all).
func TestDescribeDaemonFailureUnmanagedDetail(t *testing.T) {
	cause := errors.New("daemon at https://127.0.0.1:7890 did not become healthy within 5s")

	bare := describeDaemonFailure(DaemonStartConfig{Addr: "https://governance.example.com:7890"}, cause).Error()
	if !strings.Contains(bare, "dev-tls=unmanaged") {
		t.Errorf("message %q must describe the unmanaged TLS mode in words", bare)
	}
	if strings.Contains(bare, "explicit client certs in use") {
		t.Errorf("message %q must not claim explicit certs when none were supplied", bare)
	}

	explicit := describeDaemonFailure(DaemonStartConfig{
		Addr:              "https://governance.example.com:7890",
		ExplicitClientTLS: true,
	}, cause).Error()
	if !strings.Contains(explicit, "dev-tls=unmanaged (explicit client certs in use)") {
		t.Errorf("message %q must name the explicit client TLS material", explicit)
	}

	for _, message := range []string{bare, explicit} {
		for _, noise := range []string{"tls=off", "dev-cert-dir=<none>", "configs-dir=<none>"} {
			if strings.Contains(message, noise) {
				t.Errorf("message %q must not contain placeholder noise %q", message, noise)
			}
		}
	}
}

// A TLS probe failure against a live local listener is at least as likely to be this
// repository's own stale pre-ADR-0007 certs as a stale daemon; the conflict error
// must say so and point at doctor.
func TestDaemonConflictErrorMentionsStaleRepoLocalCerts(t *testing.T) {
	err := daemonConflictError{addr: "https://127.0.0.1:7890", cause: errors.New("tls: bad certificate")}
	message := err.Error()
	if !strings.Contains(message, "repo-local certs") {
		t.Errorf("message %q must name stale repo-local certs as a likely cause", message)
	}
	if !strings.Contains(message, "doctor") {
		t.Errorf("message %q must point at doctor for diagnosis", message)
	}
}
