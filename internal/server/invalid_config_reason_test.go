package server

// Red contract for calm-poc-7r68: when a repository config is present but
// semantically invalid, the reason MUST reach the caller. loadConfig already
// wraps ConfigEntry.Error, but writeCheckError serializes only
// CheckError.Message — so a client saw `repository "observatory" has an
// invalid config` and nothing about which invariant it broke.
//
// The observatory outage this locks: layer-sovereignty enabled with no
// fitness-function-settings.layer-sovereignty.layers.

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const layerSovereigntyWithoutLayers = `{
  "enforcement-mode": "block",
  "fitness-functions": {"cyclomatic-complexity": true, "layer-sovereignty": true}
}`

func TestHandlerCheckReportsWhyConfigIsInvalid(t *testing.T) {
	store := newAuthorizedTestConfigStore(t, `{"callers":{"dev-hook-pool":["repo-one"]}}`)
	writeInvalidRepoConfig(t, store, "repo-one", layerSovereigntyWithoutLayers)
	handler := NewHandlerWithOptions(Checker{ConfigStore: store}, nil, HandlerOptions{RequireAuthentication: true})

	request := httptest.NewRequest(http.MethodPost, "/check", bytes.NewBufferString(
		`{"repo":"repo-one","file":"x.go","proposed_content":"package main\n","language":"go"}`))
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{newTestClientCertificate(t, "dev-hook-pool")}}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /check status = %d, want %d body=%q", recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "layer-sovereignty") {
		t.Fatalf("POST /check body = %q, want it to name the invariant that failed (layer-sovereignty)", body)
	}
}

func TestHandlerPreflightReportsWhyConfigIsInvalid(t *testing.T) {
	store := newAuthorizedTestConfigStore(t, `{"callers":{"dev-hook-pool":["repo-one"]}}`)
	writeInvalidRepoConfig(t, store, "repo-one", layerSovereigntyWithoutLayers)
	handler := NewHandlerWithOptions(Checker{ConfigStore: store}, nil, HandlerOptions{RequireAuthentication: true})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, preflightRequest(t, "repo-one", "dev-hook-pool"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /preflight status = %d, want %d body=%q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	response := decodePreflight(t, recorder)
	if response.RepoConfigValid {
		t.Fatal("repo_config_valid = true, want false")
	}
	if !strings.Contains(response.RepoConfigError, "layer-sovereignty") {
		t.Fatalf("repo_config_error = %q, want it to name the invariant that failed (layer-sovereignty)", response.RepoConfigError)
	}
}
