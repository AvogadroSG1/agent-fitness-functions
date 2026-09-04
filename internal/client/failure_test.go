package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func noopStarter(DaemonStartConfig) error { return nil }

func TestClassifyValidationErrorHTTPStatuses(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantKind string
		wantText string
	}{
		{name: "400", status: http.StatusBadRequest, body: "invalid check request", wantKind: errorKindInvalidRequest, wantText: "--language"},
		{name: "401", status: http.StatusUnauthorized, body: "authentication required", wantKind: errorKindUnauthenticated, wantText: "doctor"},
		{name: "403", status: http.StatusForbidden, body: "caller not authorized", wantKind: errorKindUnauthorized, wantText: "caller-repos.json"},
		{name: "404", status: http.StatusNotFound, body: `repository "sample" is not configured`, wantKind: errorKindNotConfigured, wantText: "client onboard"},
		{name: "500", status: http.StatusInternalServerError, body: "check failed", wantKind: errorKindServerError, wantText: "server logs"},
		{name: "503", status: http.StatusServiceUnavailable, body: "running python analyzer", wantKind: errorKindServerError, wantText: "server logs"},
		{name: "504", status: http.StatusGatewayTimeout, body: "analyzer timeout", wantKind: errorKindTimeout, wantText: "timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ierr, ok := classifyValidationError(httpStatusError{status: tt.status, body: tt.body}, "sample")
			if !ok {
				t.Fatalf("classifyValidationError(%d) ok = false, want true", tt.status)
			}
			if ierr.kind != tt.wantKind {
				t.Fatalf("kind = %q, want %q", ierr.kind, tt.wantKind)
			}
			if !bytes.Contains([]byte(ierr.remediation), []byte(tt.wantText)) {
				t.Fatalf("remediation = %q, want to contain %q", ierr.remediation, tt.wantText)
			}
		})
	}
}

func TestClassifyNotConfiguredNamesRepoInRemediation(t *testing.T) {
	ierr, _ := classifyValidationError(httpStatusError{status: http.StatusNotFound}, "widget-svc")
	for _, want := range []string{"configs/widget-svc/config.json", "client onboard", "onboard-new-repository.md"} {
		if !bytes.Contains([]byte(ierr.remediation), []byte(want)) {
			t.Fatalf("remediation = %q, want to contain %q", ierr.remediation, want)
		}
	}
}

func TestClassifyValidationErrorConnectionRefused(t *testing.T) {
	ierr, ok := classifyValidationError(transportError{err: syscall.ECONNREFUSED}, "sample")
	if !ok || ierr.kind != errorKindServerUnreachable {
		t.Fatalf("kind = %q, ok = %v; want server_unreachable, true", ierr.kind, ok)
	}
}

func TestClassifyValidationErrorTimeout(t *testing.T) {
	for name, err := range map[string]error{
		"context deadline exceeded": context.DeadlineExceeded,
		"wrapped deadline":          fmt.Errorf("Post https://127.0.0.1:7890/check: %w", context.DeadlineExceeded),
		"custom timeout text":       errors.New("Post \"https://127.0.0.1:7890/check\": context deadline exceeded"),
		"client timeout text":       errors.New("net/http: request canceled (Client.Timeout exceeded while awaiting headers)"),
	} {
		t.Run(name, func(t *testing.T) {
			ierr, ok := classifyValidationError(transportError{err: err}, "sample")
			if !ok || ierr.kind != errorKindTimeout {
				t.Fatalf("kind = %q, ok = %v; want %s, true", ierr.kind, ok, errorKindTimeout)
			}
			if !bytes.Contains([]byte(ierr.remediation), []byte("AGENT_FITNESS_FUNCTIONS_CLIENT_TIMEOUT")) &&
				!bytes.Contains([]byte(ierr.remediation), []byte("--timeout")) {
				t.Fatalf("remediation = %q, want mention of timeout configuration", ierr.remediation)
			}
		})
	}
}

func TestClassifyValidationErrorTLSTyped(t *testing.T) {
	for name, err := range map[string]error{
		"cert-verification": &tls.CertificateVerificationError{},
		"unknown-authority": x509.UnknownAuthorityError{},
		"record-header":     tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"},
	} {
		t.Run(name, func(t *testing.T) {
			ierr, ok := classifyValidationError(transportError{err: err}, "sample")
			if !ok || ierr.kind != errorKindTLSFailure {
				t.Fatalf("kind = %q, ok = %v; want tls_failure, true", ierr.kind, ok)
			}
		})
	}
}

func TestClassifyValidationErrorTLSText(t *testing.T) {
	err := errors.New("Get \"https://127.0.0.1:7890/check\": tls: failed to verify certificate: x509: certificate signed by unknown authority")
	ierr, ok := classifyValidationError(err, "sample")
	if !ok || ierr.kind != errorKindTLSFailure {
		t.Fatalf("kind = %q, ok = %v; want tls_failure, true", ierr.kind, ok)
	}
}

func TestClassifyValidationErrorUsageIsNotInfra(t *testing.T) {
	if _, ok := classifyValidationError(usageError{err: errors.New("bad flag")}, "sample"); ok {
		t.Fatalf("usage error classified as infra, want ok=false")
	}
	if _, ok := classifyValidationError(nil, "sample"); ok {
		t.Fatalf("nil error classified as infra, want ok=false")
	}
}

func TestIsInfraError(t *testing.T) {
	if !IsInfraError(infraError{kind: errorKindServerUnreachable}) {
		t.Fatal("IsInfraError(infraError) = false, want true")
	}
	if IsInfraError(usageError{err: errors.New("x")}) {
		t.Fatal("IsInfraError(usageError) = true, want false")
	}
	if IsInfraError(nil) {
		t.Fatal("IsInfraError(nil) = true, want false")
	}
}

func TestReportInfraErrorWritesMachineReadableObject(t *testing.T) {
	var stdout bytes.Buffer
	err := reportInfraError(&stdout, infraError{
		kind:        errorKindNotConfigured,
		message:     "repository \"sample\" is not configured",
		remediation: "run onboard",
	})
	if !IsInfraError(err) {
		t.Fatalf("reportInfraError returned non-infra error: %v", err)
	}
	var report infraErrorReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
	}
	if report.Status != "error" || report.ErrorKind != errorKindNotConfigured || report.Remediation != "run onboard" {
		t.Fatalf("report = %+v, want status=error kind=not_configured remediation set", report)
	}
}

// TestRunCheckEmitsInfraObjectForHTTPStatus drives the full validate path against a
// healthy server that answers /check with an error status, asserting the machine-
// readable object lands on stdout and the returned error maps to the infra exit code.
func TestRunCheckEmitsInfraObjectForHTTPStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantKind string
	}{
		{name: "not configured", status: http.StatusNotFound, body: `repository "sample" is not configured`, wantKind: errorKindNotConfigured},
		{name: "forbidden", status: http.StatusForbidden, body: "caller not authorized", wantKind: errorKindUnauthorized},
		{name: "server error", status: http.StatusServiceUnavailable, body: "running python analyzer", wantKind: errorKindServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/health" {
					w.WriteHeader(http.StatusOK)
					return
				}
				http.Error(w, tt.body, tt.status)
			}))
			defer server.Close()

			var stdout bytes.Buffer
			err := RunCheck(
				[]string{"--addr", server.URL, "--file", "x.go", "--repo", "sample", "--content", "package main\n", "--language", "go"},
				&stdout, &http.Client{Timeout: time.Second}, noopStarter,
			)
			if !IsInfraError(err) {
				t.Fatalf("RunCheck error = %v, want infra error", err)
			}
			var report infraErrorReport
			if jsonErr := json.Unmarshal(stdout.Bytes(), &report); jsonErr != nil {
				t.Fatalf("stdout not JSON: %v\n%s", jsonErr, stdout.String())
			}
			if report.Status != "error" || report.ErrorKind != tt.wantKind {
				t.Fatalf("report = %+v, want status=error kind=%s", report, tt.wantKind)
			}
			if report.Remediation == "" {
				t.Fatalf("report.Remediation empty, want a fix hint")
			}
		})
	}
}

// TestRunCheckReportsServerUnreachable proves the establishDaemon path (server down,
// auto-start unavailable) surfaces as server_unreachable rather than a block.
func TestRunCheckReportsServerUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := server.URL
	server.Close() // nothing listens now → connection refused

	var stdout bytes.Buffer
	starter := func(DaemonStartConfig) error { return errors.New("daemon unavailable") }
	err := RunCheck(
		[]string{"--addr", addr, "--file", "x.go", "--repo", "sample", "--content", "package main\n", "--language", "go"},
		&stdout, &http.Client{Timeout: time.Second}, starter,
	)
	if !IsInfraError(err) {
		t.Fatalf("RunCheck error = %v, want infra error", err)
	}
	var report infraErrorReport
	if jsonErr := json.Unmarshal(stdout.Bytes(), &report); jsonErr != nil {
		t.Fatalf("stdout not JSON: %v\n%s", jsonErr, stdout.String())
	}
	if report.ErrorKind != errorKindServerUnreachable {
		t.Fatalf("kind = %q, want server_unreachable", report.ErrorKind)
	}
}

// TestRunCheckReportsTimeout proves a slow check call exceeding the configured timeout
// surfaces as check_timeout with actionable remediation rather than server_unreachable.
func TestRunCheckReportsTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		// Simulate a slow validation that exceeds the short client timeout
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"verdict":{"status":"pass"}}`))
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := RunCheck(
		[]string{"--addr", server.URL, "--file", "x.go", "--repo", "sample", "--content", "package main\n", "--language", "go", "--timeout", "20ms"},
		&stdout, &http.Client{Timeout: 10 * time.Second}, noopStarter,
	)
	if !IsInfraError(err) {
		t.Fatalf("RunCheck error = %v, want infra error", err)
	}
	var report infraErrorReport
	if jsonErr := json.Unmarshal(stdout.Bytes(), &report); jsonErr != nil {
		t.Fatalf("stdout not JSON: %v\n%s", jsonErr, stdout.String())
	}
	if report.ErrorKind != errorKindTimeout {
		t.Fatalf("kind = %q, want %s", report.ErrorKind, errorKindTimeout)
	}
	if !bytes.Contains([]byte(report.Remediation), []byte("AGENT_FITNESS_FUNCTIONS_CLIENT_TIMEOUT")) &&
		!bytes.Contains([]byte(report.Remediation), []byte("--timeout")) {
		t.Fatalf("remediation = %q, want timeout configuration hints", report.Remediation)
	}
}

// TestRunCheckReportsTLSFailure proves a bad-CA handshake against a live TLS server is
// classified as tls_failure (not server_unreachable) and does not trigger auto-start.
func TestRunCheckReportsTLSFailure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	bogusCA := writeSelfSignedCAFile(t)
	var stdout bytes.Buffer
	starterCalled := false
	starter := func(DaemonStartConfig) error { starterCalled = true; return nil }
	err := RunCheck(
		[]string{"--addr", server.URL, "--file", "x.go", "--repo", "sample", "--content", "package main\n", "--language", "go", "--client-ca", bogusCA},
		&stdout, &http.Client{Timeout: 2 * time.Second}, starter,
	)
	if !IsInfraError(err) {
		t.Fatalf("RunCheck error = %v, want infra error", err)
	}
	if starterCalled {
		t.Fatalf("auto-start attempted on a TLS handshake failure, want short-circuit")
	}
	var report infraErrorReport
	if jsonErr := json.Unmarshal(stdout.Bytes(), &report); jsonErr != nil {
		t.Fatalf("stdout not JSON: %v\n%s", jsonErr, stdout.String())
	}
	if report.ErrorKind != errorKindTLSFailure {
		t.Fatalf("kind = %q, want tls_failure", report.ErrorKind)
	}
}

func TestInfraErrorRemediationContainsDoctorAndAdvisoryMode(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "400 Bad Request", status: http.StatusBadRequest, body: "invalid request"},
		{name: "401 Unauthorized", status: http.StatusUnauthorized, body: "cert rejected"},
		{name: "404 Not Found", status: http.StatusNotFound, body: "repo not found"},
		{name: "500 Internal Server Error", status: http.StatusInternalServerError, body: "internal error"},
		{name: "503 Service Unavailable", status: http.StatusServiceUnavailable, body: "running roslyn analyzer"},
		{name: "504 Gateway Timeout", status: http.StatusGatewayTimeout, body: "analyzer timeout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ierr, ok := classifyValidationError(httpStatusError{status: tt.status, body: tt.body}, "test-repo")
			if !ok {
				t.Fatalf("expected infra error for HTTP %d", tt.status)
			}
			if !bytes.Contains([]byte(ierr.remediation), []byte("agent-fitness-functions doctor")) &&
				!bytes.Contains([]byte(ierr.remediation), []byte("agent-fitness-functions client onboard")) {
				t.Errorf("expected remediation to contain doctor or onboard command, got: %q", ierr.remediation)
			}
			if !bytes.Contains([]byte(ierr.remediation), []byte("AGENT_FITNESS_FUNCTIONS_ON_ERROR=advisory")) {
				t.Errorf("expected remediation to contain 'AGENT_FITNESS_FUNCTIONS_ON_ERROR=advisory', got: %q", ierr.remediation)
			}
		})
	}
}

func writeSelfSignedCAFile(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "unrelated-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	path := filepath.Join(t.TempDir(), "bogus-ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}
	return path
}
