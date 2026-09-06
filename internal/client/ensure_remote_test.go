package client

// S6 review follow-up (calm-poc-l9tb): making a daemon current means stopping
// and replacing a process, which is only ever legitimate for the machine's own
// loopback daemon. DaemonStartConfig.Local marks managed certificate
// provisioning rather than locality, so it cannot be the guard — the address is.

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestEnsureCurrentDaemonNeverRestartsRemoteDaemon: given a remote governance
// daemon whose identity would count as stale on a loopback address, when the
// client is asked to make it current, then it is left alone — no POST /shutdown
// and no auto-start — because a shared remote daemon belongs to nobody's laptop.
func TestEnsureCurrentDaemonNeverRestartsRemoteDaemon(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR", "/gov/configs")
	var shutdownHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/shutdown" {
			shutdownHits.Add(1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "build_revision": "0523ea04afd8", "listen_mode": "mtls", "configs_dir": "/elsewhere/configs",
		})
	}))
	defer server.Close()

	const remoteAddr = "http://governance.example.internal:7890"
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(server.URL, "http://"))
		},
	}}
	starterCalled := false
	err := ensureCurrentDaemon(client, remoteAddr, DaemonStartConfig{Addr: remoteAddr, Local: true},
		func(DaemonStartConfig) error { starterCalled = true; return nil })
	if err != nil {
		t.Fatalf("ensureCurrentDaemon against a healthy remote daemon: %v", err)
	}
	if shutdownHits.Load() != 0 {
		t.Errorf("POST /shutdown reached a remote daemon %d time(s)", shutdownHits.Load())
	}
	if starterCalled {
		t.Error("auto-start ran for a remote address")
	}
}
