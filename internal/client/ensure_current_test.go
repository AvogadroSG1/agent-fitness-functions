package client

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// S6 red-test contract (calm-poc-l9tb, calm-poc-cpvk): onboarding keeps the
// machine daemon current — probe identity, restart when stale, leave a fresh
// daemon alone — and speculative validations are dry-run end to end so they
// never poison the daemon's violation ledger.

// The test binary is unstamped (no vcs revision), so the revision dimension is
// skipped and currency rides on listen mode + configs dir, which the fakes
// control.

func TestEnsureCurrentDaemonRestartsStaleDaemon(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR", "/gov/configs")
	stale := daemonIdentity{BuildRevision: "0523ea04afd8", ListenMode: "mtls", ConfigsDir: "/gov/configs"}
	harness := newRestartHarness(t, stale)

	starterCalled := false
	starter := func(DaemonStartConfig) error {
		starterCalled = true
		startFreshDaemon(t, harness.addr, daemonExpectations{ListenMode: "local-http", ConfigsDir: "/gov/configs"})
		return nil
	}
	err := ensureCurrentDaemon(&http.Client{Timeout: time.Second}, harness.addr, DaemonStartConfig{Addr: harness.addr, Local: true}, starter)
	if err != nil {
		t.Fatalf("ensureCurrentDaemon: %v", err)
	}
	if !starterCalled {
		t.Fatal("stale daemon was not replaced")
	}
}

func TestEnsureCurrentDaemonLeavesFreshDaemonAlone(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR", "/gov/configs")
	var shutdownHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/shutdown" {
			shutdownHits.Add(1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "listen_mode": "local-http", "configs_dir": "/gov/configs"})
	}))
	defer server.Close()

	starterCalled := false
	err := ensureCurrentDaemon(server.Client(), server.URL, DaemonStartConfig{Addr: server.URL, Local: true},
		func(DaemonStartConfig) error { starterCalled = true; return nil })
	if err != nil {
		t.Fatalf("ensureCurrentDaemon against a fresh daemon: %v", err)
	}
	if starterCalled || shutdownHits.Load() != 0 {
		t.Fatalf("fresh daemon disturbed: starter=%v shutdownHits=%d", starterCalled, shutdownHits.Load())
	}
}

func TestEnsureCurrentDaemonStartsWhenNothingListens(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR", "/gov/configs")
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := server.URL
	server.Close()

	starterCalled := false
	starter := func(DaemonStartConfig) error {
		starterCalled = true
		startFreshDaemon(t, addr, daemonExpectations{ListenMode: "local-http", ConfigsDir: "/gov/configs"})
		return nil
	}
	if err := ensureCurrentDaemon(&http.Client{Timeout: time.Second}, addr, DaemonStartConfig{Addr: addr, Local: true}, starter); err != nil {
		t.Fatalf("ensureCurrentDaemon with no listener: %v", err)
	}
	if !starterCalled {
		t.Fatal("absent daemon was not started")
	}
}

// A foreign process answering /health with a non-200 is a named conflict, not
// a restart target and not a 5s generic timeout.
func TestEnsureCurrentDaemonNamesForeignSquatter(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR", "/gov/configs")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	starterCalled := false
	err := ensureCurrentDaemon(server.Client(), server.URL, DaemonStartConfig{Addr: server.URL, Local: true},
		func(DaemonStartConfig) error { starterCalled = true; return nil })
	if err == nil {
		t.Fatal("ensureCurrentDaemon succeeded against a foreign squatter, want a named error")
	}
	if starterCalled {
		t.Fatal("auto-start raced a foreign process owning the port")
	}
	if !strings.Contains(err.Error(), server.URL) {
		t.Errorf("error %q must name the occupied address", err.Error())
	}
}

// RunCheck --dry-run must forward dry_run on the wire; without the flag the
// request stays a persisting validation.
func TestRunCheckForwardsDryRun(t *testing.T) {
	var received struct {
		DryRun bool `json:"dry_run"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/check" {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &received)
			_, _ = w.Write([]byte(`{"status":"pass"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := RunCheck([]string{"--addr", server.URL, "--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n", "--language", "go", "--dry-run"},
		&stdout, &http.Client{Timeout: time.Second}, func(DaemonStartConfig) error { return nil })
	if err != nil {
		t.Fatalf("RunCheck --dry-run: %v", err)
	}
	if !received.DryRun {
		t.Fatal("dry_run was not forwarded on the wire")
	}

	err = RunCheck([]string{"--addr", server.URL, "--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n", "--language", "go"},
		&stdout, &http.Client{Timeout: time.Second}, func(DaemonStartConfig) error { return nil })
	if err != nil {
		t.Fatalf("RunCheck without --dry-run: %v", err)
	}
	if received.DryRun {
		t.Fatal("dry_run leaked into a persisting validation")
	}
}

// The agent pre-write hook validates proposals that may never land, so it must
// ask for a dry run; the commit-path hooks validate content that will land and
// must not.
func TestHookAssetsDryRunSplit(t *testing.T) {
	preToolUse, err := embeddedHooks.ReadFile("hookassets/pre-tool-use.sh")
	if err != nil {
		t.Fatalf("read pre-tool-use asset: %v", err)
	}
	if !strings.Contains(string(preToolUse), "--dry-run") {
		t.Error("pre-tool-use.sh must request a dry-run validation")
	}
	for _, name := range []string{"hookassets/pre-commit.sh", "hookassets/pre-push.sh"} {
		content, err := embeddedHooks.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(content), "--dry-run") {
			t.Errorf("%s validates content that will land and must not dry-run", name)
		}
	}
}

// The doctor's validation-pipeline probe is speculative by definition.
func TestDoctorPipelineProbeIsDryRun(t *testing.T) {
	var received struct {
		DryRun bool `json:"dry_run"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		_, _ = w.Write([]byte(`{"status":"pass"}`))
	}))
	defer server.Close()

	result := checkValidationPipeline(doctorConfig{
		repo:       "sample",
		addr:       server.URL,
		tlsLoaded:  true,
		httpClient: server.Client(),
	})
	if !result.passed {
		t.Fatalf("pipeline probe failed: %+v", result)
	}
	if !received.DryRun {
		t.Fatal("doctor probe must be dry-run so it never poisons repo state")
	}
}

// Doctor surfaces a stale daemon as a warning naming the reasons.
func TestCheckDaemonCurrencyWarnsOnStaleDaemon(t *testing.T) {
	t.Setenv("AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR", "/gov/configs")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "listen_mode": "mtls", "configs_dir": "/gov/configs"})
	}))
	defer server.Close()

	result := checkDaemonCurrency(doctorConfig{addr: server.URL, tlsLoaded: true, httpClient: server.Client()})
	if result.passed {
		t.Fatalf("result = %+v, want a stale warning", result)
	}
	if !strings.Contains(result.detail, "listen mode") {
		t.Errorf("detail %q must carry the staleness reason", result.detail)
	}
	if !strings.Contains(result.remediation, "client onboard") {
		t.Errorf("remediation %q must point at client onboard", result.remediation)
	}

	fresh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "listen_mode": "local-http", "configs_dir": "/gov/configs"})
	}))
	defer fresh.Close()
	result = checkDaemonCurrency(doctorConfig{addr: fresh.URL, tlsLoaded: true, httpClient: fresh.Client()})
	if !result.passed {
		t.Fatalf("result = %+v, want pass for a current daemon", result)
	}
}
