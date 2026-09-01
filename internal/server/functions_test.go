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

// functionsCatalogWire mirrors the /functions wire contract locally so this
// test locks the JSON shape without depending on the response types the
// implementation will introduce.
type functionsCatalogWire struct {
	Version   string `json:"version"`
	Functions map[string]struct {
		Description    string  `json:"description"`
		Threshold      float64 `json:"threshold"`
		Operator       string  `json:"operator"`
		Unit           string  `json:"unit"`
		DefaultEnabled bool    `json:"default_enabled"`
	} `json:"functions"`
}

func functionsRequest(t *testing.T, method, caller string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, "/functions", nil)
	if caller != "" {
		request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{newTestClientCertificate(t, caller)}}
	}
	return request
}

// TestFunctionsEndpointExposesEmbeddedCatalogToAnyAuthenticatedCaller locks the
// discovery contract for self-service onboarding: any authenticated caller —
// not just an admin CN — can ask a running server which fitness functions
// exist and what thresholds /check enforces, so onboarding can offer a picker
// of existing functions without the repo being pre-registered anywhere. The
// catalog must surface the nine hyphenated function names from the embedded
// governance pattern (the same source /check validates against), each with its
// description, threshold, operator, unit, and default-enabled state, plus a
// content-derived version so clients can cache.
func TestFunctionsEndpointExposesEmbeddedCatalogToAnyAuthenticatedCaller(t *testing.T) {
	store := newAuthorizedTestConfigStore(t, `{"callers":{"hook-pool":["repo-one"]},"admins":["governance-admin"]}`)
	handler := NewHandlerWithOptions(Checker{ConfigStore: store}, nil, HandlerOptions{RequireAuthentication: true})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, functionsRequest(t, http.MethodGet, "hook-pool"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /functions status = %d, want %d body=%q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response functionsCatalogWire
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode functions response %q: %v", recorder.Body.String(), err)
	}
	if !strings.HasPrefix(response.Version, "sha256:") {
		t.Fatalf("version = %q, want sha256: prefix", response.Version)
	}
	wantFunctions := map[string]bool{
		"cyclomatic-complexity":  true,
		"interface-width":        true,
		"implementation-depth":   true,
		"logic-density":          true,
		"dependency-discipline":  true,
		"layer-sovereignty":      false,
		"temporal-purity":        false,
		"sql-composition-safety": false,
		"deterministic-ordering": false,
	}
	if len(response.Functions) != len(wantFunctions) {
		t.Fatalf("catalog has %d functions %v, want %d", len(response.Functions), functionNames(response), len(wantFunctions))
	}
	for name, wantEnabled := range wantFunctions {
		entry, ok := response.Functions[name]
		if !ok {
			t.Fatalf("catalog missing function %q; got %v", name, functionNames(response))
		}
		if entry.Description == "" || entry.Operator == "" || entry.Unit == "" {
			t.Fatalf("function %q entry incomplete: %+v", name, entry)
		}
		if entry.DefaultEnabled != wantEnabled {
			t.Fatalf("function %q default_enabled = %v, want %v", name, entry.DefaultEnabled, wantEnabled)
		}
	}
	complexity := response.Functions["cyclomatic-complexity"]
	if complexity.Operator != "lte" || complexity.Threshold != 9 {
		t.Fatalf("cyclomatic-complexity = %+v, want operator lte threshold 9 from patterns/governance.json", complexity)
	}
	if complexity.Unit != "function" {
		t.Fatalf("cyclomatic-complexity unit = %q, want function", complexity.Unit)
	}
}

// TestFunctionsEndpointRejectsUnauthenticatedAndNonGETRequests locks the guard
// rails: the catalog is read-only and, like every governed endpoint, requires
// an authenticated caller identity.
func TestFunctionsEndpointRejectsUnauthenticatedAndNonGETRequests(t *testing.T) {
	store := newAuthorizedTestConfigStore(t, `{"callers":{"hook-pool":["repo-one"]}}`)
	handler := NewHandlerWithOptions(Checker{ConfigStore: store}, nil, HandlerOptions{RequireAuthentication: true})

	cases := []struct {
		name       string
		method     string
		caller     string
		wantStatus int
	}{
		{name: "unauthenticated request", method: http.MethodGet, caller: "", wantStatus: http.StatusUnauthorized},
		{name: "non-GET request", method: http.MethodPost, caller: "hook-pool", wantStatus: http.StatusMethodNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, functionsRequest(t, tc.method, tc.caller))
			if recorder.Code != tc.wantStatus {
				t.Fatalf("%s /functions status = %d, want %d body=%q", tc.method, recorder.Code, tc.wantStatus, recorder.Body.String())
			}
		})
	}
}

func functionNames(response functionsCatalogWire) []string {
	names := make([]string, 0, len(response.Functions))
	for name := range response.Functions {
		names = append(names, name)
	}
	return names
}
