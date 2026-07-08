package server

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func decodePreflight(t *testing.T, recorder *httptest.ResponseRecorder) PreflightResponse {
	t.Helper()
	var response PreflightResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode preflight response %q: %v", recorder.Body.String(), err)
	}
	return response
}

func preflightRequest(t *testing.T, repo, caller string) *http.Request {
	t.Helper()
	target := "/preflight"
	if repo != "" {
		target += "?repo=" + repo
	}
	request := httptest.NewRequest(http.MethodGet, target, nil)
	if caller != "" {
		request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{newTestClientCertificate(t, caller)}}
	}
	return request
}

func TestHandlerPreflightReportsConfiguredAuthorized(t *testing.T) {
	store := newAuthorizedTestConfigStore(t, `{"callers":{"dev-hook-pool":["repo-one"]}}`)
	writeRepoConfig(t, store, "repo-one", EnforcementAdvisory, map[string]bool{"cyclomatic-complexity": true})
	handler := NewHandlerWithOptions(Checker{ConfigStore: store}, nil, HandlerOptions{RequireAuthentication: true})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, preflightRequest(t, "repo-one", "dev-hook-pool"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /preflight status = %d, want %d body=%q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	response := decodePreflight(t, recorder)
	if response.AuthenticatedCN != "dev-hook-pool" {
		t.Fatalf("authenticated_cn = %q, want dev-hook-pool", response.AuthenticatedCN)
	}
	if !response.RepoConfigured || !response.RepoConfigValid || !response.CallerAuthorized {
		t.Fatalf("response = %+v, want configured+valid+authorized", response)
	}
	if response.EnforcementMode != EnforcementAdvisory {
		t.Fatalf("enforcement_mode = %q, want %q", response.EnforcementMode, EnforcementAdvisory)
	}
}

func TestHandlerPreflightReportsUnauthorizedWithout403(t *testing.T) {
	store := newAuthorizedTestConfigStore(t, `{"callers":{"dev-hook-pool":["repo-one"]}}`)
	writeRepoConfig(t, store, "repo-one", EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	handler := NewHandlerWithOptions(Checker{ConfigStore: store}, nil, HandlerOptions{RequireAuthentication: true})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, preflightRequest(t, "repo-one", "ci-runner-ringstation"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /preflight status = %d, want %d body=%q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	response := decodePreflight(t, recorder)
	if !response.RepoConfigured || !response.RepoConfigValid {
		t.Fatalf("response = %+v, want configured+valid", response)
	}
	if response.CallerAuthorized {
		t.Fatalf("caller_authorized = true, want false for unauthorized caller")
	}
	if response.EnforcementMode != EnforcementBlock {
		t.Fatalf("enforcement_mode = %q, want %q", response.EnforcementMode, EnforcementBlock)
	}
}

func TestHandlerPreflightReportsUnconfiguredWithout404(t *testing.T) {
	store := newAuthorizedTestConfigStore(t, `{"callers":{"dev-hook-pool":["repo-one"]}}`)
	handler := NewHandlerWithOptions(Checker{ConfigStore: store}, nil, HandlerOptions{RequireAuthentication: true})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, preflightRequest(t, "repo-two", "dev-hook-pool"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /preflight status = %d, want %d body=%q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	response := decodePreflight(t, recorder)
	if response.RepoConfigured {
		t.Fatalf("repo_configured = true, want false for unconfigured repo")
	}
	if response.EnforcementMode != "" {
		t.Fatalf("enforcement_mode = %q, want empty for unconfigured repo", response.EnforcementMode)
	}
}

func TestHandlerPreflightReportsInvalidConfig(t *testing.T) {
	store := newAuthorizedTestConfigStore(t, `{"callers":{"dev-hook-pool":["repo-one"]}}`)
	writeInvalidRepoConfig(t, store, "repo-one", `{"enforcement-mode":"bogus"}`)
	handler := NewHandlerWithOptions(Checker{ConfigStore: store}, nil, HandlerOptions{RequireAuthentication: true})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, preflightRequest(t, "repo-one", "dev-hook-pool"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /preflight status = %d, want %d body=%q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	response := decodePreflight(t, recorder)
	if !response.RepoConfigured {
		t.Fatalf("repo_configured = false, want true for present-but-invalid config")
	}
	if response.RepoConfigValid {
		t.Fatalf("repo_config_valid = true, want false for invalid config")
	}
	if response.EnforcementMode != "" {
		t.Fatalf("enforcement_mode = %q, want empty for invalid config", response.EnforcementMode)
	}
}

func TestHandlerPreflightRequiresRepoParam(t *testing.T) {
	store := newAuthorizedTestConfigStore(t, `{"callers":{"dev-hook-pool":["repo-one"]}}`)
	handler := NewHandlerWithOptions(Checker{ConfigStore: store}, nil, HandlerOptions{RequireAuthentication: true})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, preflightRequest(t, "", "dev-hook-pool"))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("GET /preflight status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if got := recorder.Body.String(); !strings.Contains(got, "preflight requires repo") {
		t.Fatalf("body = %q, want preflight requires repo", got)
	}
}

func TestHandlerPreflightRejectsInvalidRepoName(t *testing.T) {
	store := newAuthorizedTestConfigStore(t, `{"callers":{"dev-hook-pool":["repo-one"]}}`)
	handler := NewHandlerWithOptions(Checker{ConfigStore: store}, nil, HandlerOptions{RequireAuthentication: true})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, preflightRequest(t, "Repo/../etc", "dev-hook-pool"))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("GET /preflight status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if got := recorder.Body.String(); !strings.Contains(got, "invalid repository name") {
		t.Fatalf("body = %q, want invalid repository name", got)
	}
}

func TestHandlerPreflightRejectsUnauthenticatedRequest(t *testing.T) {
	store := newAuthorizedTestConfigStore(t, `{"callers":{"dev-hook-pool":["repo-one"]}}`)
	handler := NewHandlerWithOptions(Checker{ConfigStore: store}, nil, HandlerOptions{RequireAuthentication: true})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, preflightRequest(t, "repo-one", ""))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("GET /preflight status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}
