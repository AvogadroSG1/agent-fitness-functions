package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func registerRequest(t *testing.T, caller, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body))
	if caller != "" {
		request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{newTestClientCertificate(t, caller)}}
	}
	return request
}

func decodeRegister(t *testing.T, recorder *httptest.ResponseRecorder) (repo string, created bool) {
	t.Helper()
	var response struct {
		Repo    string `json:"repo"`
		Created bool   `json:"created"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode register response %q: %v", recorder.Body.String(), err)
	}
	return response.Repo, response.Created
}

func newRegisterTestHarness(t *testing.T, ctx context.Context) (*ConfigStore, http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	writeMountedConfig(t, dir, "seed-repo", `{"enforcement-mode":"block","fitness-functions":{}}`)
	writeCallerRepoPolicyFile(t, dir, `{"callers":{"team-a":["seed-repo"]},"admins":["governance-admin"]}`)
	store, err := NewConfigStore(ctx, dir)
	if err != nil {
		t.Fatalf("NewConfigStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	handler := NewHandlerWithOptions(Checker{ConfigStore: store}, nil, HandlerOptions{RequireAuthentication: true})
	return store, handler, dir
}

// TestRegisterCreatesConfigBindsCallerAndServesRepoImmediately locks the
// self-service registration contract: an authenticated caller registers a
// not-yet-configured repo over HTTP, the server persists the config and binds
// the caller's CN (preserving existing admins), and the repo is servable in
// the same store before the response returns — no fsnotify debounce wait, no
// hand-edited files, no redeploy.
func TestRegisterCreatesConfigBindsCallerAndServesRepoImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, handler, dir := newRegisterTestHarness(t, ctx)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, registerRequest(t, "team-a", `{"repo":"newrepo"}`))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("POST /register status = %d, want %d body=%q", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	repo, created := decodeRegister(t, recorder)
	if repo != "newrepo" || !created {
		t.Fatalf("register response = (%q, %v), want (newrepo, true)", repo, created)
	}
	if _, err := os.Stat(filepath.Join(dir, "newrepo", "config.json")); err != nil {
		t.Fatalf("registered config not on disk: %v", err)
	}
	policyContent, err := os.ReadFile(filepath.Join(dir, "caller-repos.json"))
	if err != nil {
		t.Fatalf("read caller-repos.json: %v", err)
	}
	var policyDoc struct {
		Callers map[string][]string `json:"callers"`
		Admins  []string            `json:"admins"`
	}
	if err := json.Unmarshal(policyContent, &policyDoc); err != nil {
		t.Fatalf("parse caller-repos.json %q: %v", policyContent, err)
	}
	if !containsString(policyDoc.Callers["team-a"], "newrepo") {
		t.Fatalf("caller-repos.json callers[team-a] = %v, want to contain newrepo", policyDoc.Callers["team-a"])
	}
	if !containsString(policyDoc.Admins, "governance-admin") {
		t.Fatalf("caller-repos.json admins = %v, want governance-admin preserved", policyDoc.Admins)
	}
	entry, ok := store.Lookup("newrepo")
	if !ok || !entry.Valid {
		t.Fatalf("Lookup(newrepo) = (%+v, %v), want live valid entry without debounce wait", entry, ok)
	}
	preflight := httptest.NewRecorder()
	handler.ServeHTTP(preflight, preflightRequest(t, "newrepo", "team-a"))
	response := decodePreflight(t, preflight)
	if !response.RepoConfigured || !response.RepoConfigValid || !response.CallerAuthorized {
		t.Fatalf("preflight after register = %+v, want configured+valid+authorized", response)
	}
}

// TestRegisterIsIdempotentAndGuardsOverwrites locks the replay and overwrite
// semantics: identical re-registration is a harmless 200 created=false;
// changing an existing repo's governance requires an admin CN, otherwise 409.
func TestRegisterIsIdempotentAndGuardsOverwrites(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, handler, _ := newRegisterTestHarness(t, ctx)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, registerRequest(t, "team-a", `{"repo":"newrepo"}`))
	if first.Code != http.StatusCreated {
		t.Fatalf("initial register status = %d body=%q", first.Code, first.Body.String())
	}

	replay := httptest.NewRecorder()
	handler.ServeHTTP(replay, registerRequest(t, "team-a", `{"repo":"newrepo"}`))
	if replay.Code != http.StatusOK {
		t.Fatalf("replay status = %d, want %d body=%q", replay.Code, http.StatusOK, replay.Body.String())
	}
	if _, created := decodeRegister(t, replay); created {
		t.Fatal("replay created = true, want false for idempotent re-registration")
	}

	differing := `{"repo":"newrepo","fitness-functions":{"logic-density":false}}`
	conflict := httptest.NewRecorder()
	handler.ServeHTTP(conflict, registerRequest(t, "team-a", differing))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("non-admin overwrite status = %d, want %d body=%q", conflict.Code, http.StatusConflict, conflict.Body.String())
	}

	adminOverwrite := httptest.NewRecorder()
	handler.ServeHTTP(adminOverwrite, registerRequest(t, "governance-admin", differing))
	if adminOverwrite.Code >= 300 {
		t.Fatalf("admin overwrite status = %d, want success body=%q", adminOverwrite.Code, adminOverwrite.Body.String())
	}
	entry, ok := store.Lookup("newrepo")
	if !ok || !entry.Valid {
		t.Fatalf("Lookup(newrepo) after admin overwrite = (%+v, %v), want valid entry", entry, ok)
	}
	if entry.Config.FitnessFunctions["logic-density"] {
		t.Fatal("admin overwrite did not apply: logic-density still enabled")
	}
}

// TestRegisterRejectsInvalidRequestsWithoutTouchingDisk locks the guard rails:
// repo names outside the ^[a-z][a-z0-9_-]{0,63}$ grammar (path traversal,
// uppercase, empty) are 400s that leave the configs directory untouched, the
// endpoint is POST-only, and unauthenticated callers get 401.
func TestRegisterRejectsInvalidRequestsWithoutTouchingDisk(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, handler, dir := newRegisterTestHarness(t, ctx)
	entriesBefore := listDirNames(t, dir)

	cases := []struct {
		name       string
		method     string
		caller     string
		body       string
		wantStatus int
	}{
		{name: "path traversal repo", method: http.MethodPost, caller: "team-a", body: `{"repo":"../evil"}`, wantStatus: http.StatusBadRequest},
		{name: "uppercase repo", method: http.MethodPost, caller: "team-a", body: `{"repo":"Evil"}`, wantStatus: http.StatusBadRequest},
		{name: "empty repo", method: http.MethodPost, caller: "team-a", body: `{"repo":""}`, wantStatus: http.StatusBadRequest},
		{name: "unknown fitness function", method: http.MethodPost, caller: "team-a", body: `{"repo":"fresh","fitness-functions":{"bogus":true}}`, wantStatus: http.StatusBadRequest},
		{name: "unauthenticated", method: http.MethodPost, caller: "", body: `{"repo":"fresh"}`, wantStatus: http.StatusUnauthorized},
		{name: "non-POST", method: http.MethodGet, caller: "team-a", body: "", wantStatus: http.StatusMethodNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, "/register", strings.NewReader(tc.body))
			if tc.caller != "" {
				request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{newTestClientCertificate(t, tc.caller)}}
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("%s /register (%s) status = %d, want %d body=%q", tc.method, tc.name, recorder.Code, tc.wantStatus, recorder.Body.String())
			}
		})
	}
	if after := listDirNames(t, dir); !equalStringSlices(entriesBefore, after) {
		t.Fatalf("configs dir changed by rejected requests: before=%v after=%v", entriesBefore, after)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func listDirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %q: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
