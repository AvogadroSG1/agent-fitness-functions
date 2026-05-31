package bridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/poconnor/calm-poc/internal/calm"
)

func TestHandlerHealthReturnsOK(t *testing.T) {
	server := httptest.NewServer(NewHandler(nil, nil))
	defer server.Close()

	response, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /health status = %d, want %d", response.StatusCode, http.StatusOK)
	}
}

func TestHandlerCheckAcceptsSchemaAndReturnsPass(t *testing.T) {
	repo := "repo-one"
	store := newTestConfigStore(t)
	writeRepoConfig(t, store, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		ConfigStore: store,
		PatternPath: writeTestPattern(t),
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: true, Output: `{"hasErrors":false}`}, nil
		}),
	}, nil))
	defer server.Close()

	body := []byte(`{"repo":` + jsonString(repo) + `,"file":"internal/parser/parser.go","proposed_content":"package parser\n\nfunc Parse() error {\n\treturn nil\n}\n","language":"go"}`)
	response, err := http.Post(server.URL+"/check", "application/json", bytesReader(body))
	if err != nil {
		t.Fatalf("POST /check failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST /check status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	var checkResponse CheckResponse
	if err := json.NewDecoder(response.Body).Decode(&checkResponse); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if checkResponse.Status != StatusPass {
		t.Fatalf("status = %q, want %q", checkResponse.Status, StatusPass)
	}
}

func TestHandlerConfigsRejectsNonGET(t *testing.T) {
	server := httptest.NewServer(NewHandler(nil, nil))
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/configs", nil)
	if err != nil {
		t.Fatalf("NewRequest(POST /configs) failed: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST /configs failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /configs status = %d, want %d", response.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestHandlerConfigsReturnsDeterministicSnapshot(t *testing.T) {
	dir := t.TempDir()
	writeMountedConfig(t, dir, "repo-b", `{
		"enforcement-mode": "off",
		"fitness-functions": {
			"cyclomatic-complexity": false
		}
	}`)
	writeMountedConfig(t, dir, "repo-a", `{
		"enforcement-mode": "block",
		"fitness-functions": {
			"cyclomatic-complexity": true,
			"interface-width": false
		}
	}`)
	store := newTestStore(t, context.Background(), dir, newFakeWatcher(), configStoreOptions{
		debounceDelay: 10 * time.Millisecond,
		retryBackoff:  time.Millisecond,
	})
	server := httptest.NewServer(NewHandler(store, nil))
	defer server.Close()

	firstBody, first := mustGetConfigsResponse(t, server.URL)
	secondBody, second := mustGetConfigsResponse(t, server.URL)

	if !bytes.Equal(firstBody, secondBody) {
		t.Fatalf("GET /configs body changed between identical requests:\nfirst:  %s\nsecond: %s", firstBody, secondBody)
	}
	if first.Version == "" {
		t.Fatal("GET /configs version = empty, want content hash")
	}
	if first.Version != second.Version {
		t.Fatalf("GET /configs version changed from %q to %q", first.Version, second.Version)
	}
	if first.LoadedAt.IsZero() {
		t.Fatal("GET /configs loaded_at = zero, want timestamp")
	}
	if got := first.Repos["repo-a"]; got.Status != "valid" {
		t.Fatalf("repo-a status = %q, want %q", got.Status, "valid")
	} else {
		if got.EnforcementMode != EnforcementBlock {
			t.Fatalf("repo-a enforcement-mode = %q, want %q", got.EnforcementMode, EnforcementBlock)
		}
		if got.LastValidAt != nil {
			t.Fatalf("repo-a last_valid_at = %v, want nil for valid repo", got.LastValidAt)
		}
		if got.FitnessFunctions["cyclomatic-complexity"] != true {
			t.Fatal("repo-a cyclomatic-complexity = false, want true")
		}
		if got.FitnessFunctions["interface-width"] != false {
			t.Fatal("repo-a interface-width = true, want false")
		}
	}
	if got := first.Repos["repo-b"]; got.Status != "valid" {
		t.Fatalf("repo-b status = %q, want %q", got.Status, "valid")
	}
}

func TestHandlerConfigsReflectsHotReloadedMixedValidity(t *testing.T) {
	dir := t.TempDir()
	writeMountedConfig(t, dir, "repo-one", `{
		"enforcement-mode": "block",
		"fitness-functions": {
			"cyclomatic-complexity": true
		}
	}`)
	repoTwoPath := writeMountedConfig(t, dir, "repo-two", `{
		"enforcement-mode": "advisory",
		"fitness-functions": {
			"interface-width": true
		}
	}`)
	watcher := newFakeWatcher()
	store := newTestStore(t, context.Background(), dir, watcher, configStoreOptions{
		debounceDelay: 10 * time.Millisecond,
		retryBackoff:  time.Millisecond,
	})
	server := httptest.NewServer(NewHandler(store, nil))
	defer server.Close()

	_, initial := mustGetConfigsResponse(t, server.URL)
	if got := initial.Repos["repo-two"]; got.Status != "valid" {
		t.Fatalf("initial repo-two status = %q, want %q", got.Status, "valid")
	}

	writeMountedConfig(t, dir, "repo-two", `{`)
	watcher.send(fsnotify.Event{Name: repoTwoPath, Op: fsnotify.Write})

	var updated ConfigsResponse
	waitForCondition(t, time.Second, func() bool {
		_, current, err := getConfigsResponse(server.URL)
		if err != nil {
			return false
		}
		if current.Repos["repo-two"].Status != "invalid" {
			return false
		}
		updated = current
		return true
	})

	if updated.Version == initial.Version {
		t.Fatalf("version after reload = %q, want change from %q", updated.Version, initial.Version)
	}
	if updated.LoadedAt.Equal(initial.LoadedAt) {
		t.Fatalf("loaded_at after reload = %s, want change from %s", updated.LoadedAt, initial.LoadedAt)
	}
	if got := updated.Repos["repo-one"]; got.Status != "valid" {
		t.Fatalf("repo-one status = %q, want %q", got.Status, "valid")
	} else if got.LastValidAt != nil {
		t.Fatalf("repo-one last_valid_at = %v, want nil for valid repo", got.LastValidAt)
	}
	repoTwo := updated.Repos["repo-two"]
	if repoTwo.Status != "invalid" {
		t.Fatalf("repo-two status = %q, want %q", repoTwo.Status, "invalid")
	}
	if repoTwo.Error == "" {
		t.Fatal("repo-two error = empty, want parse failure")
	}
	if repoTwo.LastValidAt == nil {
		t.Fatal("repo-two last_valid_at = nil, want preserved valid timestamp")
	}
	if repoTwo.EnforcementMode != "" {
		t.Fatalf("repo-two enforcement-mode = %q, want empty for invalid repo", repoTwo.EnforcementMode)
	}
	if repoTwo.FitnessFunctions != nil {
		t.Fatalf("repo-two fitness-functions = %v, want nil for invalid repo", repoTwo.FitnessFunctions)
	}
}

func TestHandlerConfigsReportsStartupInvalidRepo(t *testing.T) {
	dir := t.TempDir()
	writeMountedConfig(t, dir, "repo-one", `{
		"enforcement-mode": "block",
		"fitness-functions": {
			"cyclomatic-complexity": true
		}
	}`)
	writeMountedConfig(t, dir, "repo-two", `{`)
	store := newTestStore(t, context.Background(), dir, newFakeWatcher(), configStoreOptions{
		debounceDelay: 10 * time.Millisecond,
		retryBackoff:  time.Millisecond,
	})
	server := httptest.NewServer(NewHandler(store, nil))
	defer server.Close()

	_, response := mustGetConfigsResponse(t, server.URL)

	if got := response.Repos["repo-one"]; got.Status != "valid" {
		t.Fatalf("repo-one status = %q, want %q", got.Status, "valid")
	}
	repoTwo := response.Repos["repo-two"]
	if repoTwo.Status != "invalid" {
		t.Fatalf("repo-two status = %q, want %q", repoTwo.Status, "invalid")
	}
	if repoTwo.Error == "" {
		t.Fatal("repo-two error = empty, want startup parse failure")
	}
	if repoTwo.LastValidAt != nil {
		t.Fatalf("repo-two last_valid_at = %v, want nil when repo has never loaded successfully", repoTwo.LastValidAt)
	}
}

func TestHandlerCheckRejectsUnauthenticatedRequest(t *testing.T) {
	repo := "repo-one"
	store := newAuthorizedTestConfigStore(t, `{"callers":{"ci-runner-graft":["repo-one"]}}`)
	writeRepoConfig(t, store, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithOptions(Checker{
		ConfigStore: store,
		PatternPath: writeTestPattern(t),
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: true, Output: `{"hasErrors":false}`}, nil
		}),
	}, nil, HandlerOptions{RequireAuthentication: true}))
	defer server.Close()

	body := []byte(`{"repo":` + jsonString(repo) + `,"file":"internal/parser/parser.go","proposed_content":"package parser\n","language":"go"}`)
	response, err := http.Post(server.URL+"/check", "application/json", bytesReader(body))
	if err != nil {
		t.Fatalf("POST /check failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST /check status = %d, want %d", response.StatusCode, http.StatusUnauthorized)
	}
}

func TestHandlerStateRejectsUnauthenticatedRequest(t *testing.T) {
	store := newAuthorizedTestConfigStore(t, `{"callers":{"ci-runner-graft":["repo-one"]}}`)
	server := httptest.NewServer(NewHandlerWithOptions(Checker{ConfigStore: store}, nil, HandlerOptions{RequireAuthentication: true}))
	defer server.Close()

	response, err := http.Get(server.URL + "/state?repo=repo-one")
	if err != nil {
		t.Fatalf("GET /state failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /state status = %d, want %d", response.StatusCode, http.StatusUnauthorized)
	}
}

func TestHandlerConfigsRejectsUnauthenticatedRequest(t *testing.T) {
	store := newAuthorizedTestConfigStore(t, `{"admins":["governance-admin"]}`)
	server := httptest.NewServer(NewHandlerWithOptions(Checker{ConfigStore: store}, nil, HandlerOptions{RequireAuthentication: true}))
	defer server.Close()

	response, err := http.Get(server.URL + "/configs")
	if err != nil {
		t.Fatalf("GET /configs failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /configs status = %d, want %d", response.StatusCode, http.StatusUnauthorized)
	}
}

func TestHandlerCheckAllowsAuthorizedMTLSCaller(t *testing.T) {
	repo := "repo-one"
	store := newAuthorizedTestConfigStore(t, `{"callers":{"ci-runner-graft":["repo-one"]}}`)
	writeRepoConfig(t, store, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	handler := NewHandlerWithOptions(Checker{
		ConfigStore: store,
		PatternPath: writeTestPattern(t),
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: true, Output: `{"hasErrors":false}`}, nil
		}),
	}, nil, HandlerOptions{RequireAuthentication: true})

	request := httptest.NewRequest(http.MethodPost, "/check", bytes.NewBufferString(`{"repo":"repo-one","file":"x.go","proposed_content":"package main\n","language":"go"}`))
	request.Header.Set("Content-Type", "application/json")
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{newTestClientCertificate(t, "ci-runner-graft")}}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /check status = %d, want %d body=%q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
}

func TestHandlerCheckRejectsUnauthorizedMTLSCaller(t *testing.T) {
	store := newAuthorizedTestConfigStore(t, `{"callers":{"ci-runner-graft":["repo-one"]}}`)
	writeRepoConfig(t, store, "repo-one", EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	handler := NewHandlerWithOptions(Checker{ConfigStore: store}, nil, HandlerOptions{RequireAuthentication: true})

	request := httptest.NewRequest(http.MethodPost, "/check", bytes.NewBufferString(`{"repo":"repo-one","file":"x.go","proposed_content":"package main\n","language":"go"}`))
	request.Header.Set("Content-Type", "application/json")
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{newTestClientCertificate(t, "ci-runner-ringstation")}}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("POST /check status = %d, want %d body=%q", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
}

func TestHandlerConfigsRequiresAdminCaller(t *testing.T) {
	store := newAuthorizedTestConfigStore(t, `{"callers":{"dev-hook-pool":["repo-one"]},"admins":["governance-admin"]}`)
	writeRepoConfig(t, store, "repo-one", EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	handler := NewHandlerWithOptions(Checker{ConfigStore: store}, nil, HandlerOptions{RequireAuthentication: true})

	unauthorized := httptest.NewRequest(http.MethodGet, "/configs", nil)
	unauthorized.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{newTestClientCertificate(t, "dev-hook-pool")}}
	unauthorizedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedRecorder, unauthorized)
	if unauthorizedRecorder.Code != http.StatusForbidden {
		t.Fatalf("GET /configs non-admin status = %d, want %d body=%q", unauthorizedRecorder.Code, http.StatusForbidden, unauthorizedRecorder.Body.String())
	}

	authorized := httptest.NewRequest(http.MethodGet, "/configs", nil)
	authorized.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{newTestClientCertificate(t, "governance-admin")}}
	authorizedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(authorizedRecorder, authorized)
	if authorizedRecorder.Code != http.StatusOK {
		t.Fatalf("GET /configs admin status = %d, want %d body=%q", authorizedRecorder.Code, http.StatusOK, authorizedRecorder.Body.String())
	}
}

func TestHandlerShutdownRequiresAdminCaller(t *testing.T) {
	store := newAuthorizedTestConfigStore(t, `{"callers":{"dev-hook-pool":["repo-one"]},"admins":["governance-admin"]}`)
	called := false
	handler := NewHandlerWithOptions(Checker{ConfigStore: store}, func() {
		called = true
	}, HandlerOptions{RequireAuthentication: true})

	unauthorized := httptest.NewRequest(http.MethodPost, "/shutdown", nil)
	unauthorized.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{newTestClientCertificate(t, "dev-hook-pool")}}
	unauthorizedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedRecorder, unauthorized)
	if unauthorizedRecorder.Code != http.StatusForbidden {
		t.Fatalf("POST /shutdown non-admin status = %d, want %d body=%q", unauthorizedRecorder.Code, http.StatusForbidden, unauthorizedRecorder.Body.String())
	}
	if called {
		t.Fatal("shutdown callback invoked for non-admin caller")
	}

	authorized := httptest.NewRequest(http.MethodPost, "/shutdown", nil)
	authorized.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{newTestClientCertificate(t, "governance-admin")}}
	authorizedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(authorizedRecorder, authorized)
	if authorizedRecorder.Code != http.StatusOK {
		t.Fatalf("POST /shutdown admin status = %d, want %d body=%q", authorizedRecorder.Code, http.StatusOK, authorizedRecorder.Body.String())
	}
	if !called {
		t.Fatal("shutdown callback was not invoked for admin caller")
	}
}

func TestHandlerCheckAllowsTrustedProxyCaller(t *testing.T) {
	repo := "repo-one"
	store := newAuthorizedTestConfigStore(t, `{"callers":{"dev-hook-pool":["repo-one"]}}`)
	writeRepoConfig(t, store, repo, EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	handler := NewHandlerWithOptions(Checker{
		ConfigStore: store,
		PatternPath: writeTestPattern(t),
		Validator: validatorFunc(func(context.Context, string, string) (calm.ValidationResult, error) {
			return calm.ValidationResult{Valid: true, Output: `{"hasErrors":false}`}, nil
		}),
	}, nil, HandlerOptions{RequireAuthentication: true, TrustedProxyHeaders: true, TrustedProxyClientCNs: []string{"proxy-gateway"}})

	request := httptest.NewRequest(http.MethodPost, "/check", bytes.NewBufferString(`{"repo":"repo-one","file":"x.go","proposed_content":"package main\n","language":"go"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Client-CN", "dev-hook-pool")
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{newTestClientCertificate(t, "proxy-gateway")}, VerifiedChains: [][]*x509.Certificate{{newTestClientCertificate(t, "proxy-gateway")}}}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /check trusted proxy status = %d, want %d body=%q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
}

func TestServeWithTLSDisablesCleartextHTTP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	configDir := writeMountedServeConfigDir(t)
	serverCert, serverKey, caPath := writeTestServerTLSFiles(t)
	ready := &readySignalWriter{ch: make(chan string, 1)}

	done := make(chan error, 1)
	go func() {
		done <- serveWithOptions(ctx, ServeOptions{
			Addr:      "127.0.0.1:7894",
			ConfigDir: configDir,
			Ready:     ready,
			NewStore:  NewConfigStore,
			TLS: ServerTLSConfig{
				CertPath: serverCert,
				KeyPath:  serverKey,
				CAPath:   caPath,
			},
		})
	}()

	select {
	case <-ready.ch:
	case <-time.After(time.Second):
		t.Fatal("did not receive ready output")
	}

	httpsClient := newTestHTTPSClient(t, caPath)
	waitForHealthHTTPS(t, httpsClient, "https://127.0.0.1:7894")

	httpClient := &http.Client{Timeout: time.Second}
	response, err := httpClient.Get("http://127.0.0.1:7894/health")
	if err == nil {
		defer response.Body.Close()
		if response.StatusCode == http.StatusOK {
			t.Fatal("GET /health over cleartext returned 200, want cleartext disabled")
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("Serve returned %v, want nil or context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop after context cancellation")
	}
}

func TestHandlerShutdownInvokesCallback(t *testing.T) {
	called := false
	server := httptest.NewServer(NewHandler(nil, func() {
		called = true
	}))
	defer server.Close()

	response, err := http.Post(server.URL+"/shutdown", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /shutdown failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST /shutdown status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if !called {
		t.Fatal("shutdown callback was not invoked")
	}
}

func TestServeStartsDaemonAndRespondsToHealth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	configDir := writeMountedServeConfigDir(t)

	done := make(chan error, 1)
	go func() {
		done <- serveWithDependencies(ctx, "127.0.0.1:7891", configDir, io.Discard, NewConfigStore)
	}()

	client := &http.Client{Timeout: time.Second}
	var response *http.Response
	var err error
	for range 40 {
		response, err = client.Get("http://127.0.0.1:7891/health")
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("daemon did not become healthy: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /health status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("Serve returned %v, want nil or context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop after context cancellation")
	}
}

func TestServeStopsAfterShutdownRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	configDir := writeMountedServeConfigDir(t)
	serverCert, serverKey, adminCert, adminKey, caPath := writeTestMutualTLSFiles(t, "governance-admin")
	adminClient := newTestHTTPSClientWithCertificate(t, caPath, adminCert, adminKey)

	done := make(chan error, 1)
	go func() {
		done <- serveWithOptions(ctx, ServeOptions{
			Addr:      "127.0.0.1:7892",
			ConfigDir: configDir,
			Ready:     io.Discard,
			NewStore:  NewConfigStore,
			TLS: ServerTLSConfig{
				CertPath: serverCert,
				KeyPath:  serverKey,
				CAPath:   caPath,
			},
		})
	}()

	waitForHealthHTTPS(t, adminClient, "https://127.0.0.1:7892")

	request, err := http.NewRequest(http.MethodPost, "https://127.0.0.1:7892/shutdown", nil)
	if err != nil {
		t.Fatalf("NewRequest(POST /shutdown) failed: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := adminClient.Do(request)
	if err != nil {
		t.Fatalf("POST /shutdown failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST /shutdown status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop after /shutdown")
	}
}

func TestServeReturnsErrorForMissingConfigDir(t *testing.T) {
	err := serveWithDependencies(context.Background(), "127.0.0.1:0", filepath.Join(t.TempDir(), "missing"), io.Discard, NewConfigStore)
	if err == nil {
		t.Fatal("Serve error = nil, want missing config dir error")
	}
}

func TestServeReturnsErrorForEmptyConfigDir(t *testing.T) {
	dir := t.TempDir()
	err := serveWithDependencies(context.Background(), "127.0.0.1:0", dir, io.Discard, NewConfigStore)
	if err == nil {
		t.Fatal("Serve error = nil, want empty config dir error")
	}
}

func TestServeWritesReadyAfterConfigLoad(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	configDir := writeMountedServeConfigDir(t)
	ready := &readySignalWriter{ch: make(chan string, 1)}

	done := make(chan error, 1)
	go func() {
		done <- serveWithDependencies(ctx, "127.0.0.1:7893", configDir, ready, NewConfigStore)
	}()

	client := &http.Client{Timeout: time.Second}
	waitForHealth(t, client, "http://127.0.0.1:7893")
	select {
	case got := <-ready.ch:
		if got != "ready\n" {
			t.Fatalf("ready output = %q, want %q", got, "ready\\n")
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive ready output")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("Serve returned %v, want nil or context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop after context cancellation")
	}
}

func waitForHealth(t *testing.T, client *http.Client, addr string) *http.Response {
	t.Helper()
	var response *http.Response
	var err error
	for range 40 {
		response, err = client.Get(addr + "/health")
		if err == nil {
			return response
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("daemon did not become healthy: %v", err)
	return nil
}

func writeMountedServeConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo-one")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo dir: %v", err)
	}
	content := []byte(`{"enforcement-mode":"block","fitness-functions":{"cyclomatic-complexity":true,"interface-width":true,"implementation-depth":true,"logic-density":true,"dependency-discipline":true}}`)
	if err := os.WriteFile(filepath.Join(repoDir, "config.json"), content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	policy := []byte(`{"callers":{"ci-runner-graft":["repo-one"]},"admins":["governance-admin"]}`)
	if err := os.WriteFile(filepath.Join(dir, callerRepoBindingsFileName), policy, 0o644); err != nil {
		t.Fatalf("write caller repo policy: %v", err)
	}
	return dir
}

type readySignalWriter struct {
	ch chan string
}

func (w *readySignalWriter) Write(p []byte) (int, error) {
	w.ch <- string(p)
	return len(p), nil
}

func mustGetConfigsResponse(t *testing.T, serverURL string) ([]byte, ConfigsResponse) {
	t.Helper()
	body, response, err := getConfigsResponse(serverURL)
	if err != nil {
		t.Fatalf("GET /configs failed: %v", err)
	}
	return body, response
}

func getConfigsResponse(serverURL string) ([]byte, ConfigsResponse, error) {
	response, err := http.Get(serverURL + "/configs")
	if err != nil {
		return nil, ConfigsResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, ConfigsResponse{}, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, ConfigsResponse{}, io.ErrUnexpectedEOF
	}
	var configsResponse ConfigsResponse
	if err := json.Unmarshal(body, &configsResponse); err != nil {
		return nil, ConfigsResponse{}, err
	}
	return body, configsResponse, nil
}

func newAuthorizedTestConfigStore(t *testing.T, policyJSON string) *ConfigStore {
	t.Helper()
	store := newTestConfigStore(t)
	policy, err := parseCallerRepoPolicy([]byte(policyJSON))
	if err != nil {
		t.Fatalf("parse caller policy: %v", err)
	}
	store.callerPolicy = policy
	return store
}

func newTestClientCertificate(t *testing.T, commonName string) *x509.Certificate {
	t.Helper()
	return &x509.Certificate{Subject: pkix.Name{CommonName: commonName}}
}

func writeTestServerTLSFiles(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate ca key: %v", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create ca cert: %v", err)
	}
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create server cert: %v", err)
	}
	caPath := filepath.Join(dir, "ca.pem")
	certPath := filepath.Join(dir, "server.pem")
	keyPath := filepath.Join(dir, "server-key.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600); err != nil {
		t.Fatalf("write ca cert: %v", err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}), 0o600); err != nil {
		t.Fatalf("write server cert: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)}), 0o600); err != nil {
		t.Fatalf("write server key: %v", err)
	}
	return certPath, keyPath, caPath
}

func writeTestMutualTLSFiles(t *testing.T, clientCommonName string) (string, string, string, string, string) {
	t.Helper()
	dir := t.TempDir()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate ca key: %v", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(11),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create ca cert: %v", err)
	}
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(12),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create server cert: %v", err)
	}
	clientKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}
	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(13),
		Subject:      pkix.Name{CommonName: clientCommonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, caTemplate, &clientKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create client cert: %v", err)
	}
	caPath := filepath.Join(dir, "ca.pem")
	serverCertPath := filepath.Join(dir, "server.pem")
	serverKeyPath := filepath.Join(dir, "server-key.pem")
	clientCertPath := filepath.Join(dir, "client.pem")
	clientKeyPath := filepath.Join(dir, "client-key.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600); err != nil {
		t.Fatalf("write ca cert: %v", err)
	}
	if err := os.WriteFile(serverCertPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}), 0o600); err != nil {
		t.Fatalf("write server cert: %v", err)
	}
	if err := os.WriteFile(serverKeyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)}), 0o600); err != nil {
		t.Fatalf("write server key: %v", err)
	}
	if err := os.WriteFile(clientCertPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientDER}), 0o600); err != nil {
		t.Fatalf("write client cert: %v", err)
	}
	if err := os.WriteFile(clientKeyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(clientKey)}), 0o600); err != nil {
		t.Fatalf("write client key: %v", err)
	}
	return serverCertPath, serverKeyPath, clientCertPath, clientKeyPath, caPath
}

func newTestHTTPSClientWithCertificate(t *testing.T, caPath, certPath, keyPath string) *http.Client {
	t.Helper()
	client := newTestHTTPSClient(t, caPath)
	certificate, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("load client certificate: %v", err)
	}
	client.Transport.(*http.Transport).TLSClientConfig.Certificates = []tls.Certificate{certificate}
	return client
}

func newTestHTTPSClient(t *testing.T, caPath string) *http.Client {
	t.Helper()
	caContent, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatalf("read ca: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caContent) {
		t.Fatal("append ca certs")
	}
	return &http.Client{Timeout: time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
}

func waitForHealthHTTPS(t *testing.T, client *http.Client, addr string) *http.Response {
	t.Helper()
	var response *http.Response
	var err error
	for range 40 {
		response, err = client.Get(addr + "/health")
		if err == nil {
			return response
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("https daemon did not become healthy: %v", err)
	return nil
}

func bytesReader(body []byte) io.Reader {
	return bytes.NewReader(body)
}
