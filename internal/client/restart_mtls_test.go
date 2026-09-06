package client

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/devcerts"
)

// S5 (calm-poc-5lku): the restart that matters in practice is the one that
// replaces the legacy mTLS daemon a machine is migrating off (ADR-0010). The
// client speaks the new plain-HTTP default, so its very first POST /shutdown
// meets a TLS listener — and only the machine's managed dev-certificate
// material can complete that handshake. This locks the shutdown path onto the
// same S4 candidate machinery validate uses, including its publish=false rule.

// legacyMTLSDaemon is a stale https daemon that requires the managed client
// certificate, records who reached POST /shutdown, and closes itself when it
// does — the observable behaviour of the daemon generation being replaced.
type legacyMTLSDaemon struct {
	server           *httptest.Server
	httpAddr         string
	mutex            sync.Mutex
	shutdownCallers  []string
	stopOnce         sync.Once
	stopped          chan struct{}
	staleIdentity    daemonIdentity
	freshExpectation daemonExpectations
}

func newLegacyMTLSDaemon(t *testing.T, certRoot string) *legacyMTLSDaemon {
	t.Helper()
	daemon := &legacyMTLSDaemon{
		stopped:          make(chan struct{}),
		staleIdentity:    daemonIdentity{BuildRevision: "0523ea04afd8", ListenMode: "mtls", ConfigsDir: "/gov/configs"},
		freshExpectation: daemonExpectations{BuildRevision: "fresh0000000", ListenMode: "local-http", ConfigsDir: "/gov/configs"},
	}
	daemon.server = httptest.NewUnstartedServer(http.HandlerFunc(daemon.serve))
	daemon.server.TLS = managedServerTLSConfig(t, certRoot)
	daemon.server.StartTLS()
	daemon.httpAddr = "http://" + strings.TrimPrefix(daemon.server.URL, "https://")
	go func() {
		<-daemon.stopped
		daemon.server.Close()
	}()
	t.Cleanup(daemon.server.Close)
	return daemon
}

func (d *legacyMTLSDaemon) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/shutdown" {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "build_revision": d.staleIdentity.BuildRevision,
			"listen_mode": d.staleIdentity.ListenMode, "configs_dir": d.staleIdentity.ConfigsDir, "pid": os.Getpid(),
		})
		return
	}
	d.mutex.Lock()
	d.shutdownCallers = append(d.shutdownCallers, peerCommonName(r))
	d.mutex.Unlock()
	_, _ = w.Write([]byte(`{"status":"shutting_down"}`))
	d.stopOnce.Do(func() { close(d.stopped) })
}

func (d *legacyMTLSDaemon) callers() []string {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return append([]string(nil), d.shutdownCallers...)
}

func peerCommonName(r *http.Request) string {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return ""
	}
	return r.TLS.PeerCertificates[0].Subject.CommonName
}

// managedServerTLSConfig serves the managed generation's server keypair and
// demands a client certificate signed by the same dev CA, so a client without
// that material cannot even reach a handler.
func managedServerTLSConfig(t *testing.T, certRoot string) *tls.Config {
	t.Helper()
	version, err := devcerts.ResolveManagedVersion(certRoot)
	if err != nil {
		t.Fatalf("ResolveManagedVersion: %v", err)
	}
	paths := version.Paths()
	serverPair, err := tls.LoadX509KeyPair(paths.ServerCertificate, paths.ServerKey)
	if err != nil {
		t.Fatalf("load managed server keypair: %v", err)
	}
	caPEM, err := os.ReadFile(paths.CA)
	if err != nil {
		t.Fatalf("read managed CA: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatalf("parse managed CA %s", paths.CA)
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{serverPair},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
	}
}

// TestRestartLocalDaemonStopsLegacyMTLSDaemonWithManagedMaterial: given a stale
// legacy mTLS daemon on the loopback port and a bare plain-HTTP client, when a
// restart is asked for against the http address, then the shutdown is delivered
// over the managed dev-certificate candidate, the replacement plain-HTTP daemon
// answers with a current identity, and no certificate generation is published.
func TestRestartLocalDaemonStopsLegacyMTLSDaemonWithManagedMaterial(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	certRoot := filepath.Join(t.TempDir(), "certs")
	if err := EnsureDevCerts(certRoot); err != nil {
		t.Fatalf("EnsureDevCerts(%q): %v", certRoot, err)
	}
	t.Setenv(envDevCertDir, certRoot)
	t.Setenv(envClientCert, "")
	t.Setenv(envClientKey, "")
	t.Setenv(envClientCA, "")
	daemon := newLegacyMTLSDaemon(t, certRoot)
	publishedBefore := managedVersionNames(t, certRoot)

	starterCalled := false
	// A bare client: it trusts the system roots only and carries no client
	// certificate, so it can neither verify nor satisfy the legacy daemon.
	base := &http.Client{Timeout: 2 * time.Second}
	err := restartLocalDaemon(base, daemon.httpAddr,
		DaemonStartConfig{Addr: daemon.httpAddr, Local: true}, daemon.freshExpectation,
		func(DaemonStartConfig) error {
			starterCalled = true
			startFreshDaemon(t, daemon.httpAddr, daemon.freshExpectation)
			return nil
		})
	if err != nil {
		t.Fatalf("restartLocalDaemon over a legacy mTLS daemon: %v", err)
	}
	if !starterCalled {
		t.Fatal("starter was never invoked")
	}

	callers := daemon.callers()
	if len(callers) != 1 || callers[0] != devClientCommonName {
		t.Fatalf("POST /shutdown callers = %q, want exactly one managed caller %q", callers, devClientCommonName)
	}
	identity, err := probeDaemonIdentity(base, daemon.httpAddr)
	if err != nil || len(daemonStaleness(identity, daemon.freshExpectation)) != 0 {
		t.Fatalf("post-restart identity = %+v err = %v, want a current plain-HTTP daemon", identity, err)
	}
	if publishedAfter := managedVersionNames(t, certRoot); !equalStringSlices(publishedBefore, publishedAfter) {
		t.Errorf("managed generations = %v, want %v unchanged: a restart must load managed material with publish=false", publishedAfter, publishedBefore)
	}
}
