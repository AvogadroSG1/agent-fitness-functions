package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// S2 red-test contract (calm-poc-fuz1): GET /health stays an unauthenticated
// 200 (probeDaemon compatibility) and gains a JSON identity body so clients
// can detect a stale daemon without completing mTLS.

type healthBody struct {
	Status        string `json:"status"`
	BuildRevision string `json:"build_revision"`
	BuildModified bool   `json:"build_modified"`
	ListenMode    string `json:"listen_mode"`
	ConfigsDir    string `json:"configs_dir"`
	PID           int    `json:"pid"`
	StartedAt     string `json:"started_at"`
}

func getHealth(t *testing.T, options HandlerOptions) (int, healthBody) {
	t.Helper()
	handler := NewHandlerWithOptions(Checker{}, nil, options)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	var body healthBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("GET /health body %q is not JSON: %v", recorder.Body.String(), err)
	}
	return recorder.Code, body
}

func TestHealthReportsDaemonIdentity(t *testing.T) {
	identity := Identity{
		BuildRevision: "e758e98da399",
		BuildModified: true,
		ListenMode:    "mtls",
		ConfigsDir:    "/govroot/configs",
	}
	code, body := getHealth(t, HandlerOptions{Identity: identity})
	if code != http.StatusOK {
		t.Fatalf("GET /health = %d, want 200", code)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	if body.BuildRevision != identity.BuildRevision {
		t.Errorf("build_revision = %q, want %q", body.BuildRevision, identity.BuildRevision)
	}
	if !body.BuildModified {
		t.Error("build_modified = false, want true")
	}
	if body.ListenMode != identity.ListenMode {
		t.Errorf("listen_mode = %q, want %q", body.ListenMode, identity.ListenMode)
	}
	if body.ConfigsDir != identity.ConfigsDir {
		t.Errorf("configs_dir = %q, want %q", body.ConfigsDir, identity.ConfigsDir)
	}
	if body.PID != os.Getpid() {
		t.Errorf("pid = %d, want the serving process id %d", body.PID, os.Getpid())
	}
	if _, err := time.Parse(time.RFC3339, body.StartedAt); err != nil {
		t.Errorf("started_at %q is not RFC3339: %v", body.StartedAt, err)
	}
}

// A zero Identity still answers 200 with a well-formed ok body so legacy
// probes and load balancers keep working unchanged.
func TestHealthWithZeroIdentityStaysOK(t *testing.T) {
	code, body := getHealth(t, HandlerOptions{})
	if code != http.StatusOK {
		t.Fatalf("GET /health = %d, want 200", code)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	if body.BuildRevision != "" || body.ListenMode != "" || body.ConfigsDir != "" {
		t.Errorf("zero identity must not invent values, got %+v", body)
	}
}
