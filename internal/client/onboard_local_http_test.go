package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/devcerts"
)

// Coverage for the ADR-0010 managed-local path end of onboarding and diagnosis:
// with the default (http) loopback address there is no certificate to publish
// and no caller binding to write, and doctor must report those checks as not
// applicable rather than failing a machine that correctly has neither.

// resolveLocalHTTPOnboarderForTest resolves an onboarder at the DEFAULT address
// — the plain-HTTP loopback daemon — against a server that answers every request
// 200 OK, without running any step.
func resolveLocalHTTPOnboarderForTest(t *testing.T, repoName, repoRoot string, stdout io.Writer) onboarder {
	t.Helper()
	okClient := &http.Client{Transport: clientRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})}
	o, err := resolveOnboarder([]string{"--repo", repoName, repoRoot}, stdout, &bytes.Buffer{}, okClient, func(DaemonStartConfig) error { return nil })
	if err != nil {
		t.Fatalf("resolveOnboarder(%s): %v", repoName, err)
	}
	return o
}

// failIfCalledDevCerts replaces the two devcerts entry points with stubs that
// record and fail, so a test can prove certificate machinery was never reached.
func failIfCalledDevCerts(t *testing.T) *int {
	t.Helper()
	calls := 0
	originalPublish := publishManagedCertificates
	publishManagedCertificates = func(string, bool) error {
		calls++
		return errors.New("devcerts.Publish must not run in local-http mode")
	}
	originalResolve := resolveManagedVersion
	resolveManagedVersion = func(string) (devcerts.ManagedVersion, error) {
		calls++
		return devcerts.ManagedVersion{}, errors.New("devcerts.ResolveManagedVersion must not run in local-http mode")
	}
	t.Cleanup(func() {
		publishManagedCertificates = originalPublish
		resolveManagedVersion = originalResolve
	})
	return &calls
}

// TestLocalHTTPOnboardSkipsCertificatesAndCallerBindings: given the default
// loopback http address, when onboarding runs its managed-local mutation steps,
// then it scaffolds and syncs the repository config as always but creates no
// certificate material and no caller binding, and says so in its progress output.
func TestLocalHTTPOnboardSkipsCertificatesAndCallerBindings(t *testing.T) {
	govRoot := governanceStateHome(t)
	repoRoot := onboardTestRepo(t)
	devCertCalls := failIfCalledDevCerts(t)

	var stdout bytes.Buffer
	o := resolveLocalHTTPOnboarderForTest(t, "repo-http", repoRoot, &stdout)
	if !o.localHTTP {
		t.Fatalf("onboarder addr %q did not resolve as the local-http path", o.addr)
	}
	for _, step := range []struct {
		name string
		run  func() error
	}{
		{"ensureCerts", o.ensureCerts},
		{"scaffoldConfig", o.scaffoldConfig},
		{"authorizeCaller", o.authorizeCaller},
	} {
		if err := step.run(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
	}

	if *devCertCalls != 0 {
		t.Errorf("devcerts entry points called %d times, want 0 in local-http mode", *devCertCalls)
	}
	for _, absent := range []string{
		filepath.Join(govRoot, "certs"),
		filepath.Join(govRoot, callerRepoBindingsFileName),
	} {
		if _, err := os.Lstat(absent); !os.IsNotExist(err) {
			t.Errorf("local-http onboarding created %q (stat err=%v); the loopback daemon needs neither certificates nor caller bindings", absent, err)
		}
	}
	for _, present := range []string{
		filepath.Join(repoRoot, "configs", "repo-http", "config.json"),
		filepath.Join(govRoot, "configs", "repo-http", "config.json"),
	} {
		if _, err := os.Stat(present); err != nil {
			t.Errorf("config %q: %v, want scaffolded and synced as on every other path", present, err)
		}
	}
	for _, want := range []string{"not required (local-http mode)", "implicit local caller"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("onboard output = %q, want it to name the skip reason %q", stdout.String(), want)
		}
	}
}

// TestDoctorOverLocalHTTPReportsTransportChecksNotApplicable: given a loopback
// http address on a machine that has published no development certificates at
// all, when doctor runs, then the client-certificate, CA-bundle and governance
// root checks pass as not applicable instead of failing, and the caller is
// reported as the implicit local one.
func TestDoctorOverLocalHTTPReportsTransportChecksNotApplicable(t *testing.T) {
	governanceStateHome(t)
	report := preflightReport{
		AuthenticatedCN: "local", RepoConfigured: true, RepoConfigValid: true,
		CallerAuthorized: true, EnforcementMode: "advisory",
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal preflight: %v", err)
	}
	httpClient := &http.Client{Transport: clientRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(encoded)), Header: make(http.Header)}, nil
	})}

	cfg, err := resolveDoctorConfig([]string{"--addr", defaultOnboardAddr, "--repo", "sample"}, httpClient)
	if err != nil {
		t.Fatalf("resolveDoctorConfig: %v", err)
	}
	if !cfg.localHTTP {
		t.Fatalf("doctor addr %q did not resolve as the local-http path", cfg.addr)
	}
	if cfg.tlsError != nil {
		t.Fatalf("tlsError = %v, want none: local-http resolves no certificate material to fail on", cfg.tlsError)
	}
	for _, result := range []checkResult{checkClientCertificate(cfg), checkServerCABundle(cfg)} {
		if !result.passed {
			t.Errorf("%s check failed (%q); want it reported as not applicable", result.name, result.detail)
		}
		if !strings.Contains(result.detail, "not applicable in local-http mode") {
			t.Errorf("%s detail = %q, want it to name local-http mode", result.name, result.detail)
		}
	}
	if root := checkGovernanceRoot(cfg); !root.passed || !strings.Contains(root.detail, "certificates not required") {
		t.Errorf("governance root check = %+v, want a pass that does not demand certificates", root)
	}
	assertCheckDetail(t, checkPreflight(cfg), "server authentication", "implicit local caller")
	assertCheckDetail(t, checkPreflight(cfg), "caller authorized for repo", "implicit local caller")
}

// assertCheckDetail asserts that the named check passed and its detail mentions
// want.
func assertCheckDetail(t *testing.T, results []checkResult, name, want string) {
	t.Helper()
	for _, result := range results {
		if result.name != name {
			continue
		}
		if !result.passed {
			t.Errorf("%s check failed: %q", name, result.detail)
		}
		if !strings.Contains(result.detail, want) {
			t.Errorf("%s detail = %q, want it to contain %q", name, result.detail, want)
		}
		return
	}
	t.Errorf("no %q check in doctor results %+v", name, results)
}
