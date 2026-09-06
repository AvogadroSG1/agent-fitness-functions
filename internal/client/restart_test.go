package client

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// S5 red-test contract (calm-poc-5lku, ADR-0010): the client can decide a
// local daemon is stale and gracefully replace it — restart lock, POST
// /shutdown, drain, auto-start — judging success solely by observing a fresh
// identity, never by signaling processes.

func TestDaemonStalenessVerdicts(t *testing.T) {
	current := daemonExpectations{
		BuildRevision: "e758e98da399",
		ListenMode:    "local-http",
		ConfigsDir:    "/gov/configs",
	}
	matching := daemonIdentity{BuildRevision: "e758e98da399", ListenMode: "local-http", ConfigsDir: "/gov/configs"}

	cases := []struct {
		name       string
		identity   daemonIdentity
		expect     daemonExpectations
		wantReason string
	}{
		{name: "current daemon is fresh", identity: matching, expect: current},
		{name: "legacy binary", identity: daemonIdentity{Legacy: true}, expect: current, wantReason: "legacy"},
		{
			name:       "revision mismatch",
			identity:   daemonIdentity{BuildRevision: "0523ea04afd8", ListenMode: "local-http", ConfigsDir: "/gov/configs"},
			expect:     current,
			wantReason: "revision",
		},
		{
			name:     "unstamped builds do not compare revisions",
			identity: daemonIdentity{BuildRevision: "", ListenMode: "local-http", ConfigsDir: "/gov/configs"},
			expect:   daemonExpectations{BuildRevision: "", ListenMode: "local-http", ConfigsDir: "/gov/configs"},
		},
		{
			name:       "dirty build with matching revision is stale",
			identity:   daemonIdentity{BuildRevision: "e758e98da399", BuildModified: true, ListenMode: "local-http", ConfigsDir: "/gov/configs"},
			expect:     current,
			wantReason: "modified",
		},
		{
			name:       "configs dir mismatch",
			identity:   daemonIdentity{BuildRevision: "e758e98da399", ListenMode: "local-http", ConfigsDir: "/elsewhere"},
			expect:     current,
			wantReason: "configs",
		},
		{
			name:       "listen mode mismatch drives the mTLS to local-http migration",
			identity:   daemonIdentity{BuildRevision: "e758e98da399", ListenMode: "mtls", ConfigsDir: "/gov/configs"},
			expect:     current,
			wantReason: "listen mode",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			reasons := daemonStaleness(testCase.identity, testCase.expect)
			if testCase.wantReason == "" {
				if len(reasons) != 0 {
					t.Fatalf("reasons = %q, want fresh", reasons)
				}
				return
			}
			if !strings.Contains(strings.Join(reasons, "; "), testCase.wantReason) {
				t.Fatalf("reasons = %q, want one mentioning %q", reasons, testCase.wantReason)
			}
		})
	}
}

func TestProbeDaemonIdentityParsesAndFlagsLegacy(t *testing.T) {
	identityServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "build_revision": "abc123def456", "listen_mode": "local-http", "configs_dir": "/g/configs", "pid": 42,
		})
	}))
	defer identityServer.Close()
	identity, err := probeDaemonIdentity(identityServer.Client(), identityServer.URL)
	if err != nil || identity.Legacy || identity.BuildRevision != "abc123def456" || identity.ListenMode != "local-http" {
		t.Fatalf("identity = %+v err = %v, want parsed identity", identity, err)
	}

	legacyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer legacyServer.Close()
	identity, err = probeDaemonIdentity(legacyServer.Client(), legacyServer.URL)
	if err != nil || !identity.Legacy {
		t.Fatalf("identity = %+v err = %v, want legacy flag for empty body", identity, err)
	}
}

// restartHarness runs a fake daemon on a fixed loopback port whose /shutdown
// closes it, so a starter (or a simulated racer) can bind a replacement.
type restartHarness struct {
	addr     string
	expect   daemonExpectations
	shutdown chan struct{}
}

func newRestartHarness(t *testing.T, identity daemonIdentity) *restartHarness {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	harness := &restartHarness{
		addr:     "http://" + listener.Addr().String(),
		expect:   daemonExpectations{BuildRevision: "fresh0000000", ListenMode: "local-http", ConfigsDir: "/gov/configs"},
		shutdown: make(chan struct{}),
	}
	server := &http.Server{Handler: harness.handler(identity)}
	go func() { _ = server.Serve(listener) }()
	go func() {
		<-harness.shutdown
		_ = server.Close()
	}()
	t.Cleanup(func() { _ = server.Close() })
	return harness
}

func (h *restartHarness) handler(identity daemonIdentity) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "build_revision": identity.BuildRevision,
			"listen_mode": identity.ListenMode, "configs_dir": identity.ConfigsDir, "pid": os.Getpid(),
		})
	})
	mux.HandleFunc("/shutdown", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"shutting_down"}`))
		close(h.shutdown)
	})
	return mux
}

// startFreshDaemon binds a replacement fake daemon serving the fresh identity
// on addr, retrying briefly while the old listener drains.
func startFreshDaemon(t *testing.T, addr string, expect daemonExpectations) {
	t.Helper()
	host := strings.TrimPrefix(addr, "http://")
	deadline := time.Now().Add(3 * time.Second)
	for {
		listener, err := net.Listen("tcp", host)
		if err == nil {
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status": "ok", "build_revision": expect.BuildRevision,
					"listen_mode": expect.ListenMode, "configs_dir": expect.ConfigsDir, "pid": os.Getpid(),
				})
			})}
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(func() { _ = server.Close() })
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("replacement daemon could not bind %s: %v", host, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRestartLocalDaemonReplacesStaleDaemon(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	stale := daemonIdentity{BuildRevision: "0523ea04afd8", ListenMode: "mtls", ConfigsDir: "/gov/configs"}
	harness := newRestartHarness(t, stale)

	starterCalled := false
	starter := func(DaemonStartConfig) error {
		starterCalled = true
		startFreshDaemon(t, harness.addr, harness.expect)
		return nil
	}
	err := restartLocalDaemon(&http.Client{Timeout: time.Second}, harness.addr, DaemonStartConfig{Addr: harness.addr, Local: true}, harness.expect, starter)
	if err != nil {
		t.Fatalf("restartLocalDaemon: %v", err)
	}
	if !starterCalled {
		t.Fatal("starter was never invoked")
	}
	identity, err := probeDaemonIdentity(&http.Client{Timeout: time.Second}, harness.addr)
	if err != nil || identity.BuildRevision != harness.expect.BuildRevision {
		t.Fatalf("post-restart identity = %+v err = %v, want fresh", identity, err)
	}
}

// A concurrent validate may auto-start the replacement first; losing the bind
// race is success as long as a fresh daemon answers.
func TestRestartLocalDaemonAcceptsRacerWinningBind(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	stale := daemonIdentity{BuildRevision: "0523ea04afd8", ListenMode: "mtls", ConfigsDir: "/gov/configs"}
	harness := newRestartHarness(t, stale)

	starter := func(DaemonStartConfig) error {
		startFreshDaemon(t, harness.addr, harness.expect)
		return errors.New("listen tcp 127.0.0.1: bind: address already in use")
	}
	err := restartLocalDaemon(&http.Client{Timeout: time.Second}, harness.addr, DaemonStartConfig{Addr: harness.addr, Local: true}, harness.expect, starter)
	if err != nil {
		t.Fatalf("restartLocalDaemon must judge by observed identity, got: %v", err)
	}
}

// When no shutdown route works, the human gets the escape hatch and no
// auto-start races the live listener.
func TestRestartLocalDaemonEscapeHatchWhenShutdownRefused(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/shutdown" {
			http.Error(w, "shutdown refused", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "build_revision": "0523ea04afd8", "listen_mode": "mtls"})
	}))
	defer server.Close()

	starterCalled := false
	err := restartLocalDaemon(server.Client(), server.URL, DaemonStartConfig{Addr: server.URL, Local: true},
		daemonExpectations{BuildRevision: "fresh0000000", ListenMode: "local-http"},
		func(DaemonStartConfig) error { starterCalled = true; return nil })
	if err == nil {
		t.Fatal("restartLocalDaemon succeeded with shutdown refused, want escape-hatch error")
	}
	if starterCalled {
		t.Fatal("auto-start attempted while the old daemon still owns the port")
	}
	if !strings.Contains(err.Error(), "lsof") {
		t.Errorf("error %q must hand the human the lsof escape hatch", err.Error())
	}
}

// Concurrent onboards must not thrash the daemon: a held restart lock is a
// named, actionable failure.
func TestRestartLocalDaemonRespectsRestartLock(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	lockPath := filepath.Join(stateHome, "agent-fitness-functions", "governance", "restart.lock")
	if err := os.MkdirAll(lockPath, 0o755); err != nil {
		t.Fatalf("pre-holding restart lock: %v", err)
	}
	stale := daemonIdentity{BuildRevision: "0523ea04afd8", ListenMode: "mtls", ConfigsDir: "/gov/configs"}
	harness := newRestartHarness(t, stale)

	err := restartLocalDaemon(&http.Client{Timeout: time.Second}, harness.addr, DaemonStartConfig{Addr: harness.addr, Local: true}, harness.expect,
		func(DaemonStartConfig) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "restart.lock") {
		t.Fatalf("error = %v, want a named restart.lock contention failure", err)
	}
}
