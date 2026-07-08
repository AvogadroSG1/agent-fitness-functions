package client

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveDoctorRepo(t *testing.T) {
	tests := []struct {
		name     string
		repoFlag string
		repoRoot string
		want     string
	}{
		{name: "flag wins", repoFlag: "graft", repoRoot: "/home/user/calm-poc", want: "graft"},
		{name: "basename fallback", repoFlag: "", repoRoot: "/home/user/calm-poc", want: "calm-poc"},
		{name: "empty when no root", repoFlag: "", repoRoot: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveDoctorRepo(tt.repoFlag, tt.repoRoot); got != tt.want {
				t.Fatalf("resolveDoctorRepo(%q, %q) = %q, want %q", tt.repoFlag, tt.repoRoot, got, tt.want)
			}
		})
	}
}

func TestCheckBinaryAlwaysPasses(t *testing.T) {
	result := checkBinary()
	if !result.passed {
		t.Fatalf("checkBinary passed = false, want true")
	}
	if result.detail == "" {
		t.Fatalf("checkBinary detail is empty, want a path")
	}
}

func TestClientCertificateResultExpired(t *testing.T) {
	leaf := &x509.Certificate{Subject: pkix.Name{CommonName: "dev-hook-pool"}, NotAfter: time.Now().Add(-time.Hour)}
	result := clientCertificateResult("/tmp/client.crt", leaf)
	if result.passed {
		t.Fatalf("expired cert result passed = true, want false")
	}
	if !strings.Contains(result.detail, "expired") {
		t.Fatalf("detail = %q, want to mention expiry", result.detail)
	}
	if result.remediation == "" {
		t.Fatalf("expired cert result has no remediation")
	}
}

func TestClientCertificateResultValidReportsCN(t *testing.T) {
	leaf := &x509.Certificate{Subject: pkix.Name{CommonName: "dev-hook-pool"}, NotAfter: time.Now().Add(time.Hour)}
	result := clientCertificateResult("/tmp/client.crt", leaf)
	if !result.passed {
		t.Fatalf("valid cert result passed = false, want true")
	}
	if !strings.Contains(result.detail, "CN=dev-hook-pool") {
		t.Fatalf("detail = %q, want CN reported", result.detail)
	}
}

func TestCheckClientCertificateWithGeneratedDevCerts(t *testing.T) {
	certDir := t.TempDir()
	if err := EnsureDevCerts(certDir); err != nil {
		t.Fatalf("EnsureDevCerts: %v", err)
	}
	cfg := doctorConfig{
		clientCert: filepath.Join(certDir, devClientCertName),
		clientKey:  filepath.Join(certDir, devClientKeyName),
	}
	result := checkClientCertificate(cfg)
	if !result.passed {
		t.Fatalf("checkClientCertificate passed = false, want true (detail=%q)", result.detail)
	}
	if !strings.Contains(result.detail, "CN="+devClientCommonName) {
		t.Fatalf("detail = %q, want CN=%s", result.detail, devClientCommonName)
	}
}

func TestCheckClientCertificateMissing(t *testing.T) {
	result := checkClientCertificate(doctorConfig{})
	if result.passed {
		t.Fatalf("missing cert result passed = true, want false")
	}
	if !strings.Contains(result.remediation, "generate-dev-certs.sh") {
		t.Fatalf("remediation = %q, want generate-dev-certs.sh hint", result.remediation)
	}
}

func TestCheckServerCABundle(t *testing.T) {
	certDir := t.TempDir()
	if err := EnsureDevCerts(certDir); err != nil {
		t.Fatalf("EnsureDevCerts: %v", err)
	}
	valid := checkServerCABundle(doctorConfig{clientCA: filepath.Join(certDir, devCACertName)})
	if !valid.passed {
		t.Fatalf("valid CA bundle passed = false, want true (detail=%q)", valid.detail)
	}

	garbage := filepath.Join(certDir, "garbage.pem")
	if err := os.WriteFile(garbage, []byte("not a pem"), 0o644); err != nil {
		t.Fatalf("write garbage: %v", err)
	}
	if checkServerCABundle(doctorConfig{clientCA: garbage}).passed {
		t.Fatalf("garbage CA bundle passed = true, want false")
	}

	if checkServerCABundle(doctorConfig{}).passed {
		t.Fatalf("empty CA bundle passed = true, want false")
	}
}

func TestPreflightStatusResult(t *testing.T) {
	if _, useFacts := preflightStatusResult(http.StatusOK); !useFacts {
		t.Fatalf("status 200 useFacts = false, want true")
	}
	unauthorized, useFacts := preflightStatusResult(http.StatusUnauthorized)
	if useFacts || unauthorized.passed {
		t.Fatalf("status 401 = %+v useFacts=%v, want failing result", unauthorized, useFacts)
	}
	if !strings.Contains(unauthorized.detail, "401") {
		t.Fatalf("401 detail = %q, want mention of 401", unauthorized.detail)
	}
	other, useFacts := preflightStatusResult(http.StatusInternalServerError)
	if useFacts || other.passed {
		t.Fatalf("status 500 = %+v useFacts=%v, want failing result", other, useFacts)
	}
}

func TestRepoConfiguredResult(t *testing.T) {
	cfg := doctorConfig{repo: "calm-poc"}
	notConfigured := repoConfiguredResult(cfg, preflightReport{RepoConfigured: false})
	if notConfigured.passed || !strings.Contains(notConfigured.remediation, "configs/calm-poc/config.json") {
		t.Fatalf("not-configured result = %+v, want failing with config path remediation", notConfigured)
	}
	invalid := repoConfiguredResult(cfg, preflightReport{RepoConfigured: true, RepoConfigValid: false})
	if invalid.passed || !strings.Contains(invalid.detail, "invalid config") {
		t.Fatalf("invalid-config result = %+v, want failing with invalid config detail", invalid)
	}
	valid := repoConfiguredResult(cfg, preflightReport{RepoConfigured: true, RepoConfigValid: true})
	if !valid.passed {
		t.Fatalf("valid result passed = false, want true")
	}
}

func TestCallerAuthorizedResult(t *testing.T) {
	cfg := doctorConfig{repo: "calm-poc"}
	unauthorized := callerAuthorizedResult(cfg, preflightReport{AuthenticatedCN: "dev-hook-pool"})
	if unauthorized.passed {
		t.Fatalf("unauthorized caller passed = true, want false")
	}
	if !strings.Contains(unauthorized.remediation, "caller-repos.json") || !strings.Contains(unauthorized.remediation, "dev-hook-pool") {
		t.Fatalf("remediation = %q, want CN + caller-repos.json", unauthorized.remediation)
	}
	authorized := callerAuthorizedResult(cfg, preflightReport{AuthenticatedCN: "dev-hook-pool", CallerAuthorized: true})
	if !authorized.passed {
		t.Fatalf("authorized caller passed = false, want true")
	}
}

func TestEnforcementModeResult(t *testing.T) {
	unknown := enforcementModeResult(preflightReport{})
	if unknown.passed || !unknown.warning {
		t.Fatalf("empty enforcement mode = %+v, want advisory warning", unknown)
	}
	known := enforcementModeResult(preflightReport{EnforcementMode: "block"})
	if !known.passed || known.detail != "block" {
		t.Fatalf("known enforcement mode = %+v, want passed with detail block", known)
	}
}

func TestCheckServerReachableUnreachable(t *testing.T) {
	result := checkServerReachable(doctorConfig{addr: "https://127.0.0.1:1", httpClient: &http.Client{Timeout: time.Second}})
	if result.passed {
		t.Fatalf("unreachable server passed = true, want false")
	}
	if !strings.Contains(result.remediation, "governance server") {
		t.Fatalf("remediation = %q, want start-server hint", result.remediation)
	}
}

func TestCheckServerReachableHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	result := checkServerReachable(doctorConfig{addr: server.URL, httpClient: server.Client()})
	if !result.passed {
		t.Fatalf("healthy server passed = false, want true (detail=%q)", result.detail)
	}
}

func TestCheckPreflightExpandsFacts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/preflight" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("repo") != "calm-poc" {
			http.Error(w, "unexpected repo", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(preflightReport{
			AuthenticatedCN:  "dev-hook-pool",
			RepoConfigured:   true,
			RepoConfigValid:  true,
			CallerAuthorized: true,
			EnforcementMode:  "block",
		})
	}))
	defer server.Close()

	results := checkPreflight(doctorConfig{addr: server.URL, repo: "calm-poc", httpClient: server.Client()})
	if len(results) != 4 {
		t.Fatalf("checkPreflight returned %d results, want 4: %+v", len(results), results)
	}
	for _, result := range results {
		if !result.passed {
			t.Fatalf("preflight result %q failed unexpectedly: %+v", result.name, result)
		}
	}
}

func TestCheckPreflightReportsUnauthorizedCaller(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(preflightReport{
			AuthenticatedCN: "ci-runner-graft",
			RepoConfigured:  true,
			RepoConfigValid: true,
		})
	}))
	defer server.Close()

	results := checkPreflight(doctorConfig{addr: server.URL, repo: "calm-poc", httpClient: server.Client()})
	var authorized *checkResult
	for i := range results {
		if results[i].name == "caller authorized for repo" {
			authorized = &results[i]
		}
	}
	if authorized == nil {
		t.Fatalf("no caller-authorized result in %+v", results)
	}
	if authorized.passed {
		t.Fatalf("caller-authorized passed = true, want false for unauthorized caller")
	}
}

func TestRunDoctorReturnsErrorWhenChecksFail(t *testing.T) {
	var stdout, stderr strings.Builder
	err := RunDoctor([]string{"--addr", "https://127.0.0.1:1", "--repo", "calm-poc"}, &stdout, &stderr, &http.Client{Timeout: time.Second})
	if err == nil {
		t.Fatalf("RunDoctor returned nil error, want failure when server unreachable")
	}
	if !strings.Contains(stdout.String(), "✘") {
		t.Fatalf("stdout = %q, want at least one ✘ line", stdout.String())
	}
	if !strings.Contains(stdout.String(), "server reachable") {
		t.Fatalf("stdout = %q, want server reachable check line", stdout.String())
	}
}

func TestRunDoctorRejectsUnknownFlag(t *testing.T) {
	var stdout, stderr strings.Builder
	err := RunDoctor([]string{"--nope"}, &stdout, &stderr, &http.Client{Timeout: time.Second})
	if err == nil {
		t.Fatalf("RunDoctor with unknown flag returned nil, want usage error")
	}
	if !IsUsageError(err) {
		t.Fatalf("error %v is not a usage error", err)
	}
}
