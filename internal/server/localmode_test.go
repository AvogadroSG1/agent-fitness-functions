package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// S3 red-test contract (calm-poc-rzss, ADR-0010): the loopback plain-HTTP
// local listen mode serves every authenticated route to loopback peers with an
// implicit caller — no client certificates and no caller-repos.json — while
// refusing non-loopback peers and non-loopback binds. mTLS mode behavior is
// pinned by the existing test suite and must not change.

func newLocalHTTPHandler(t *testing.T, shutdown func()) http.Handler {
	t.Helper()
	store := newAuthorizedTestConfigStore(t, `{"callers":{}}`)
	writeRepoConfig(t, store, "repo-one", EnforcementAdvisory, map[string]bool{"cyclomatic-complexity": true})
	return NewHandlerWithOptions(Checker{ConfigStore: store}, shutdown, HandlerOptions{
		RequireAuthentication: true,
		LocalHTTP:             true,
	})
}

func localRequest(method, target, remoteAddr string) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	request.RemoteAddr = remoteAddr
	request.TLS = nil
	return request
}

func TestLocalHTTPServesPreflightFromLoopbackWithoutTLS(t *testing.T) {
	handler := newLocalHTTPHandler(t, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, localRequest(http.MethodGet, "/preflight?repo=repo-one", "127.0.0.1:54321"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /preflight = %d, want 200 body=%q", recorder.Code, recorder.Body.String())
	}
	response := decodePreflight(t, recorder)
	if response.AuthenticatedCN != "local" {
		t.Errorf("authenticated_cn = %q, want the implicit local caller %q", response.AuthenticatedCN, "local")
	}
	if !response.RepoConfigured || !response.RepoConfigValid {
		t.Errorf("response = %+v, want configured+valid", response)
	}
	if !response.CallerAuthorized {
		t.Error("caller_authorized = false, want true: loopback peers are implicitly authorized with no caller-repos.json")
	}
}

func TestLocalHTTPAllowsShutdownFromLoopback(t *testing.T) {
	handler := newLocalHTTPHandler(t, func() {})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, localRequest(http.MethodPost, "/shutdown", "127.0.0.1:40000"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /shutdown = %d, want 200 body=%q", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "shutting_down") {
		t.Errorf("body = %q, want shutting_down", recorder.Body.String())
	}
}

func TestLocalHTTPRejectsNonLoopbackPeers(t *testing.T) {
	handler := newLocalHTTPHandler(t, func() {})
	for _, target := range []string{"/preflight?repo=repo-one", "/shutdown"} {
		method := http.MethodGet
		if target == "/shutdown" {
			method = http.MethodPost
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, localRequest(method, target, "10.0.0.5:44444"))
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s %s from non-loopback peer = %d, want 401 (fail closed)", method, target, recorder.Code)
		}
	}
}

func TestLocalHTTPModeValidatesBindAndTLSInputs(t *testing.T) {
	cases := []struct {
		name    string
		options ServeOptions
		wantErr string
	}{
		{
			name:    "non-loopback bind refused",
			options: ServeOptions{ListenMode: ListenModeLocalHTTP, Addr: "0.0.0.0:7890"},
			wantErr: "loopback",
		},
		{
			name: "explicit TLS material refused",
			options: ServeOptions{
				ListenMode: ListenModeLocalHTTP,
				Addr:       "127.0.0.1:7890",
				TLS:        ServerTLSConfig{CertPath: "server.crt", KeyPath: "server.key"},
			},
			wantErr: "TLS",
		},
		{
			name:    "managed certificates refused",
			options: ServeOptions{ListenMode: ListenModeLocalHTTP, Addr: "127.0.0.1:7890", ManagedRoot: "/govroot"},
			wantErr: "managed",
		},
		{
			name:    "ipv4 loopback accepted",
			options: ServeOptions{ListenMode: ListenModeLocalHTTP, Addr: "127.0.0.1:7890"},
		},
		{
			name:    "localhost accepted",
			options: ServeOptions{ListenMode: ListenModeLocalHTTP, Addr: "localhost:7890"},
		},
		{
			name:    "ipv6 loopback accepted",
			options: ServeOptions{ListenMode: ListenModeLocalHTTP, Addr: "[::1]:7890"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateServeOptions(testCase.options)
			if testCase.wantErr == "" {
				if err != nil {
					t.Fatalf("validateServeOptions = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("validateServeOptions = %v, want error mentioning %q", err, testCase.wantErr)
			}
		})
	}
}
