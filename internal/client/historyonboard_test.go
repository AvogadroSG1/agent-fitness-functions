package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/historyipc"
)

func TestRemoteOnboardAttemptsLocalWriterAndOnlyWarnsOnFailure(t *testing.T) {
	fixture := writeExternalClientTLSFixture(t, "history-client")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body any
		switch r.URL.Path {
		case "/health":
			return
		case "/register":
			w.WriteHeader(http.StatusCreated)
			body = map[string]any{"repo": "sample", "created": true}
		case "/preflight":
			body = preflightReport{AuthenticatedCN: "history-client", RepoConfigured: true, RepoConfigValid: true, CallerAuthorized: true, EnforcementMode: "advisory"}
		case "/check":
			body = map[string]string{"status": "pass"}
		default:
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Error(err)
		}
	})
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{fixture.server}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: fixture.roots}
	server.StartTLS()
	t.Cleanup(server.Close)
	repo := t.TempDir()
	t.Setenv("GIT_TRACE2_EVENT", "0")
	useDeterministicGitClientTest(t, repo)
	t.Setenv(envDevCertDir, "")
	t.Setenv(envClientCert, fixture.clientCert)
	t.Setenv(envClientKey, fixture.clientKey)
	t.Setenv(envClientCA, fixture.ca)
	t.Setenv("AGENT_FITNESS_FUNCTIONS_HISTORY_SOCKET", repo+"/absent.sock")
	starts := 0
	runtime := historyRuntimeFixture()
	runtime.Request = func(context.Context, string, string) (historyipc.Message, error) {
		return historyipc.Message{}, syscall.ENOENT
	}
	runtime.Start = func() error { starts++; return errors.New("cannot start") }
	var stdout, stderr bytes.Buffer
	err := RunOnboard([]string{"--repo", "sample", "--addr", server.URL, repo}, &stdout, &stderr, &http.Client{Timeout: 3 * time.Second}, func(DaemonStartConfig) error { t.Fatal("remote governance started local daemon"); return nil }, &runtime)
	if err != nil {
		t.Fatalf("onboard failed: %v\n%s", err, stdout.String())
	}
	if starts != 1 {
		t.Fatalf("local history starts=%d", starts)
	}
	if !strings.Contains(stderr.String(), "warning: local history writer unavailable") {
		t.Fatalf("warning missing: %s", stderr.String())
	}
}

func TestInvalidOnboardNeverTouchesHistory(t *testing.T) {
	runtime := historyRuntimeFixture()
	runtime.Request = func(context.Context, string, string) (historyipc.Message, error) {
		t.Fatal("history probed before validation")
		return historyipc.Message{}, nil
	}
	err := RunOnboard([]string{"--enforcement", "invalid"}, io.Discard, io.Discard, nil, nil, &runtime)
	if err == nil {
		t.Fatal("invalid onboarding accepted")
	}
}
