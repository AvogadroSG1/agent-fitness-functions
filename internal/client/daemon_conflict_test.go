package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The scenarios below pin the calm-poc-l9u fix: in managed local mode the
// client's own dev-cert material has already loaded cleanly by the time
// ensureDaemon probes, so a TLS handshake failure from a live listener on the
// shared default port means a daemon that does not trust this repository's dev
// CA — almost always another repository's daemon (or a stale pre-rotation
// one) — owns the port. That must surface as an actionable port conflict, not
// as generic certificate corruption.

// TestEnsureDaemonWrapsManagedLoopbackTLSProbeFailureAsPortConflict: given a
// foreign TLS server already listening on the probed loopback address, when
// ensureDaemon runs in managed local mode, then it returns a typed
// daemonConflictError naming the address and the real cause, and never
// attempts an auto-start that would race the existing listener.
func TestEnsureDaemonWrapsManagedLoopbackTLSProbeFailureAsPortConflict(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	starterCalled := false
	starter := func(DaemonStartConfig) error { starterCalled = true; return nil }
	cfg := DaemonStartConfig{Addr: server.URL, Local: true, ManagedRoot: t.TempDir()}

	err := ensureDaemon(&http.Client{Timeout: time.Second}, server.URL, cfg, starter)
	if err == nil {
		t.Fatal("ensureDaemon succeeded against an untrusted listener, want port-conflict error")
	}
	if starterCalled {
		t.Fatal("auto-start attempted while a conflicting daemon is listening, want short-circuit")
	}
	var conflict daemonConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error = %v (%T), want daemonConflictError", err, err)
	}
	for _, want := range []string{server.URL, "another repository", "--addr"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want to contain %q", err.Error(), want)
		}
	}
}

// TestEnsureDaemonKeepsRawTLSErrorOutsideManagedLocalMode: given the same
// foreign listener, when the client runs with external (non-managed) TLS
// material, then the raw TLS error is preserved — an external CA mismatch
// really can be a certificate problem, so the port-conflict diagnosis must
// stay scoped to managed local mode.
func TestEnsureDaemonKeepsRawTLSErrorOutsideManagedLocalMode(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DaemonStartConfig{Addr: server.URL, Local: false}
	err := ensureDaemon(&http.Client{Timeout: time.Second}, server.URL, cfg, noopStarter)
	if err == nil {
		t.Fatal("ensureDaemon succeeded against an untrusted listener, want TLS error")
	}
	var conflict daemonConflictError
	if errors.As(err, &conflict) {
		t.Fatalf("external-mode TLS failure classified as port conflict: %v", err)
	}
	if !isTLSError(err) {
		t.Fatalf("error = %v, want a TLS error", err)
	}
}

// TestClassifyValidationErrorPortConflict: a daemonConflictError must classify
// as its own machine-readable kind — distinct from tls_failure — whose message
// names the contested address and whose remediation names the two real fixes:
// stop the conflicting daemon or choose a distinct --addr.
func TestClassifyValidationErrorPortConflict(t *testing.T) {
	if errorKindPortConflict != "port_conflict" {
		t.Fatalf("errorKindPortConflict = %q, want %q", errorKindPortConflict, "port_conflict")
	}
	cause := errors.New("tls: failed to verify certificate: x509: certificate signed by unknown authority")
	err := daemonConflictError{addr: "https://127.0.0.1:7890", cause: cause}
	ierr, ok := classifyValidationError(err, "sample")
	if !ok {
		t.Fatal("classifyValidationError(daemonConflictError) ok = false, want true")
	}
	if ierr.kind != errorKindPortConflict {
		t.Fatalf("kind = %q, want %q", ierr.kind, errorKindPortConflict)
	}
	if !strings.Contains(ierr.message, "127.0.0.1:7890") {
		t.Fatalf("message = %q, want to name the contested address", ierr.message)
	}
	for _, want := range []string{"--addr", "lsof -i"} {
		if !strings.Contains(ierr.remediation, want) {
			t.Fatalf("remediation = %q, want to contain %q", ierr.remediation, want)
		}
	}
}

// TestDaemonConflictErrorUnwrapsCause keeps the underlying TLS error reachable
// for errors.As/Is callers so no diagnostic information is lost by wrapping.
func TestDaemonConflictErrorUnwrapsCause(t *testing.T) {
	cause := errors.New("x509: certificate signed by unknown authority")
	err := daemonConflictError{addr: "https://127.0.0.1:7890", cause: cause}
	if !errors.Is(err, cause) {
		t.Fatal("daemonConflictError does not unwrap to its cause")
	}
}
