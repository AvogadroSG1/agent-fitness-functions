package client

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
)

// S4 red-test contract (calm-poc-pxmm, ADR-0010): the managed-local client
// speaks plain HTTP by default and falls back between schemes in both
// directions, so hooks written against either generation keep working while
// the machine migrates off mTLS.

func TestManagedDefaultsUsePlainHTTP(t *testing.T) {
	opts, err := parseValidateFlags([]string{"--file", "x.go", "--repo", "/tmp/repo"})
	if err != nil {
		t.Fatalf("parseValidateFlags: %v", err)
	}
	if opts.addr != "http://127.0.0.1:7890" {
		t.Errorf("validate default addr = %q, want http://127.0.0.1:7890", opts.addr)
	}
	if defaultOnboardAddr != "http://127.0.0.1:7890" {
		t.Errorf("defaultOnboardAddr = %q, want http://127.0.0.1:7890", defaultOnboardAddr)
	}
}

func newPassingCheckServer(t *testing.T, tls bool) *httptest.Server {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/check":
			_ = json.NewEncoder(w).Encode(fitness.ValidationResult{Status: fitness.StatusPass})
		default:
			http.NotFound(w, r)
		}
	})
	if tls {
		return httptest.NewTLSServer(handler)
	}
	return httptest.NewServer(handler)
}

// A legacy hook env still says https://…; the daemon on that port now speaks
// plain HTTP. The client must fall back to http and validate successfully.
func TestRunCheckFallsBackFromHTTPSAddrToHTTPDaemon(t *testing.T) {
	server := newPassingCheckServer(t, false)
	defer server.Close()
	legacyAddr := "https://" + strings.TrimPrefix(server.URL, "http://")

	var stdout bytes.Buffer
	err := RunCheck(
		[]string{"--addr", legacyAddr, "--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n", "--language", "go"},
		&stdout, &http.Client{Timeout: time.Second}, func(DaemonStartConfig) error { return nil },
	)
	if err != nil {
		t.Fatalf("RunCheck with legacy https addr against HTTP daemon: %v", err)
	}
	if !strings.Contains(stdout.String(), `"status":"pass"`) {
		t.Fatalf("stdout = %q, want pass JSON", stdout.String())
	}
}

// The new http default meets a still-running legacy mTLS daemon. The client
// must retry over https with its configured transport instead of failing the
// commit.
func TestRunCheckFallsBackFromHTTPAddrToLegacyHTTPSDaemon(t *testing.T) {
	server := newPassingCheckServer(t, true)
	defer server.Close()
	newAddr := "http://" + strings.TrimPrefix(server.URL, "https://")

	var stdout bytes.Buffer
	err := RunCheck(
		[]string{"--addr", newAddr, "--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n", "--language", "go"},
		&stdout, server.Client(), func(DaemonStartConfig) error { return nil },
	)
	if err != nil {
		t.Fatalf("RunCheck with http addr against legacy HTTPS daemon: %v", err)
	}
	if !strings.Contains(stdout.String(), `"status":"pass"`) {
		t.Fatalf("stdout = %q, want pass JSON", stdout.String())
	}
}

// Managed-local auto-start must launch the daemon in the plain-HTTP local
// listen mode; non-local starts must not.
func TestDaemonStartArgsCarryLocalHTTPListenMode(t *testing.T) {
	localArgs := strings.Join(daemonStartArgs(DaemonStartConfig{Addr: "http://127.0.0.1:7890", Local: true}), " ")
	if !strings.Contains(localArgs, "--listen-mode local-http") {
		t.Errorf("local start args = %q, want --listen-mode local-http", localArgs)
	}
	remoteArgs := strings.Join(daemonStartArgs(DaemonStartConfig{Addr: "https://governance.example.com:7890"}), " ")
	if strings.Contains(remoteArgs, "local-http") {
		t.Errorf("non-local start args = %q, must not select local-http", remoteArgs)
	}
}
