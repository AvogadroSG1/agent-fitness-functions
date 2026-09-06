package client

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/devcerts"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
)

// Coverage for the managed dev-certificate candidate of the ADR-0010 scheme
// fallback: when the http default meets a still-running legacy mTLS daemon, the
// caller's own transport cannot verify that daemon, so the fallback must retry
// with the machine's managed client material — and must do so without publishing
// a new certificate generation as a side effect of a probe.

// managedCheckHandler answers the two endpoints a validate round trip needs.
func managedCheckHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/check":
			_ = json.NewEncoder(w).Encode(fitness.ValidationResult{Status: fitness.StatusPass})
		default:
			http.NotFound(w, r)
		}
	})
}

// managedVersionNames lists the published generations under a managed cert root,
// so a test can prove a probe published nothing.
func managedVersionNames(t *testing.T, certRoot string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(certRoot, "versions"))
	if err != nil {
		t.Fatalf("read managed versions dir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// TestRunCheckFallsBackToLegacyHTTPSDaemonWithManagedClientMaterial: given a
// legacy mTLS daemon serving the machine's managed dev certificates, and a base
// http client that does not trust that CA, when validate runs against the new
// http default, then the fallback retries https with the managed client material
// and the check succeeds — without auto-start and without publishing a new
// certificate generation (managed material is loaded with publish=false, because
// a probe must never mint certificates).
func TestRunCheckFallsBackToLegacyHTTPSDaemonWithManagedClientMaterial(t *testing.T) {
	certRoot := filepath.Join(t.TempDir(), "certs")
	if err := EnsureDevCerts(certRoot); err != nil {
		t.Fatalf("EnsureDevCerts(%q): %v", certRoot, err)
	}
	t.Setenv(envDevCertDir, certRoot)
	t.Setenv(envClientCert, "")
	t.Setenv(envClientKey, "")
	t.Setenv(envClientCA, "")

	version, err := devcerts.ResolveManagedVersion(certRoot)
	if err != nil {
		t.Fatalf("ResolveManagedVersion: %v", err)
	}
	paths := version.Paths()
	serverPair, err := tls.LoadX509KeyPair(paths.ServerCertificate, paths.ServerKey)
	if err != nil {
		t.Fatalf("load managed server keypair: %v", err)
	}
	server := httptest.NewUnstartedServer(managedCheckHandler())
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{serverPair}}
	server.StartTLS()
	defer server.Close()

	publishedBefore := managedVersionNames(t, certRoot)
	httpAddr := "http://" + strings.TrimPrefix(server.URL, "https://")
	starterCalled := false
	var stdout bytes.Buffer
	// A bare client: it trusts the system roots only, so it cannot verify the
	// dev CA. Only the managed candidate can complete this handshake.
	err = RunCheck(
		[]string{"--addr", httpAddr, "--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n", "--language", "go"},
		&stdout, &http.Client{Timeout: 5 * time.Second},
		func(DaemonStartConfig) error { starterCalled = true; return nil },
	)
	if err != nil {
		t.Fatalf("RunCheck with http addr against managed https daemon: %v\n%s", err, stdout.String())
	}
	if starterCalled {
		t.Error("auto-start attempted although the fallback reached a live daemon")
	}
	if !strings.Contains(stdout.String(), `"status":"pass"`) {
		t.Fatalf("stdout = %q, want pass JSON", stdout.String())
	}
	if publishedAfter := managedVersionNames(t, certRoot); !equalStringSlices(publishedBefore, publishedAfter) {
		t.Errorf("managed generations = %v, want %v unchanged: the fallback probe must load material with publish=false", publishedAfter, publishedBefore)
	}
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
