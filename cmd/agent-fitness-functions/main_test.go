package main

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
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/client"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/devcerts"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/server"
)

func TestRunResolveDevCertVersionProtocol(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certs")
	if err := devcerts.Publish(root, false); err != nil {
		t.Fatalf("Publish(%q): %v", root, err)
	}
	t.Setenv("AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR", root)

	var stdout, stderr bytes.Buffer
	code := run([]string{"client", "resolve-dev-cert-version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr = %q", code, stderr.String())
	}
	if !regexp.MustCompile(`^versions/v-[0-9a-f]{32}\n$`).MatchString(stdout.String()) {
		t.Fatalf("stdout = %q, want one validated relative version line", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunResolveDevCertVersionUsageAndFailureProtocol(t *testing.T) {
	t.Run("argument is usage failure", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"client", "resolve-dev-cert-version", "extra"}, &stdout, &stderr)
		if code != 2 || stdout.Len() != 0 || stderr.String() != "client resolve-dev-cert-version accepts no arguments\n" {
			t.Fatalf("exit/stdout/stderr = %d/%q/%q, want 2/empty/exact usage error", code, stdout.String(), stderr.String())
		}
	})

	t.Run("missing publication is runtime failure", func(t *testing.T) {
		t.Setenv("AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR", filepath.Join(t.TempDir(), "missing"))
		var stdout, stderr bytes.Buffer
		code := run([]string{"client", "resolve-dev-cert-version"}, &stdout, &stderr)
		if code != 1 || stdout.Len() != 0 {
			t.Fatalf("exit/stdout = %d/%q, want 1/empty", code, stdout.String())
		}
		if got := stderr.String(); !strings.HasPrefix(got, "resolve managed development certificate version: ") || strings.Contains(got, "PRIVATE KEY") {
			t.Fatalf("stderr = %q, want safe managed resolution failure", got)
		}
	})
}

func TestRunClientValidateRejectsSelectorConflictBeforeFilesystem(t *testing.T) {
	t.Setenv("AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR", filepath.Join(t.TempDir(), "missing"))
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"client", "validate",
		"--file", "missing.go",
		"--repo", filepath.Join(t.TempDir(), "missing-repo"),
		"--client-cert", "/external/client.crt",
	}, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 {
		t.Fatalf("exit/stdout = %d/%q, want 2/empty", code, stdout.String())
	}
	want := "AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR cannot be combined with explicit client TLS inputs\n"
	if stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunServeDefaultsToWorkingDirectoryManagedCertificates(t *testing.T) {
	t.Setenv("AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR", writeMountedServeConfigDir(t))
	t.Setenv("AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR", "")
	workingDirectory := t.TempDir()
	originalWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatalf("Chdir(%q): %v", workingDirectory, err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWorkingDirectory) })

	var stderr bytes.Buffer
	code := runServe([]string{
		"--addr", "127.0.0.1:0",
	}, &stderr)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "resolve managed server certificate version") || !strings.Contains(stderr.String(), filepath.Join(workingDirectory, "certs")) && !strings.Contains(stderr.String(), "invalid managed certificate root") {
		t.Fatalf("stderr = %q, want working-directory managed certificate resolution error", stderr.String())
	}
}

func TestRunServeRequiresTLSForTrustedProxyHeaders(t *testing.T) {
	t.Setenv("AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR", writeMountedServeConfigDir(t))
	// A client-cert-only env var selects the plain-HTTP server mode (see
	// TestResolveServerStartTLSModeProvenance's "client cert alone preserves HTTP"
	// case) so this test exercises the trusted-proxy/TLS validation itself rather
	// than the unrelated default managed-certificate resolution.
	t.Setenv("AGENT_FITNESS_FUNCTIONS_CLIENT_CERT", "/external/client.crt")

	var stderr bytes.Buffer
	code := runServe([]string{
		"--addr", "127.0.0.1:0",
		"--trusted-proxy-headers",
		"--trusted-proxy-client-cns", "proxy-gateway",
	}, &stderr)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "trusted proxy mode requires TLS") {
		t.Fatalf("stderr = %q, want trusted proxy TLS validation error", stderr.String())
	}
}

func TestRunServeRequiresTrustedProxyClientCNs(t *testing.T) {
	t.Setenv("AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR", writeMountedServeConfigDir(t))

	var stderr bytes.Buffer
	code := runServe([]string{
		"--addr", "127.0.0.1:0",
		"--trusted-proxy-headers",
		"--tls-cert", "server.pem",
		"--tls-key", "server-key.pem",
		"--tls-ca", "ca.pem",
	}, &stderr)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "trusted proxy mode requires at least one trusted proxy client CN") {
		t.Fatalf("stderr = %q, want trusted proxy client CN validation error", stderr.String())
	}
}

func TestResolveTLSPathPrefersFlagThenEnv(t *testing.T) {
	const envName = "AGENT_FITNESS_FUNCTIONS_TLS_CERT"
	t.Setenv(envName, "/env/server.crt")
	if got := resolveTLSPath("/flag/server.crt", envName); got != "/flag/server.crt" {
		t.Fatalf("resolveTLSPath with flag set = %q, want flag value to win", got)
	}
	if got := resolveTLSPath("", envName); got != "/env/server.crt" {
		t.Fatalf("resolveTLSPath with only env set = %q, want env fallback", got)
	}
	t.Setenv(envName, "")
	if got := resolveTLSPath("", envName); got != "" {
		t.Fatalf("resolveTLSPath with neither set = %q, want empty", got)
	}
}

func TestResolveServerStartTLSModeProvenance(t *testing.T) {
	tests := []struct {
		name        string
		selector    string
		certFlag    string
		keyFlag     string
		caFlag      string
		serverCert  string
		serverKey   string
		serverCA    string
		clientCert  string
		clientKey   string
		clientCA    string
		wantManaged string
		wantTLS     server.ServerTLSConfig
		wantHTTP    bool
		wantErr     bool
		wantGetwd   int
	}{
		{name: "direct default", wantManaged: "/work/certs", wantGetwd: 1},
		{name: "managed selector", selector: "/managed", wantManaged: "/managed"},
		{name: "server cert flag selects external", certFlag: "/flag/server.crt", wantTLS: server.ServerTLSConfig{CertPath: "/flag/server.crt"}},
		{name: "server key flag selects external", keyFlag: "/flag/server.key", wantTLS: server.ServerTLSConfig{KeyPath: "/flag/server.key"}},
		{name: "server CA flag selects external", caFlag: "/flag/ca.crt", wantTLS: server.ServerTLSConfig{CAPath: "/flag/ca.crt"}},
		{name: "server cert env selects external", serverCert: "/env/server.crt", wantTLS: server.ServerTLSConfig{CertPath: "/env/server.crt"}},
		{name: "server key env selects external", serverKey: "/env/server.key", wantTLS: server.ServerTLSConfig{KeyPath: "/env/server.key"}},
		{name: "server CA env selects external", serverCA: "/env/ca.crt", wantTLS: server.ServerTLSConfig{CAPath: "/env/ca.crt"}},
		{name: "flags override server env", certFlag: "/flag/server.crt", keyFlag: "/flag/server.key", caFlag: "/flag/ca.crt", serverCert: "/env/server.crt", serverKey: "/env/server.key", serverCA: "/env/ca.crt", wantTLS: server.ServerTLSConfig{CertPath: "/flag/server.crt", KeyPath: "/flag/server.key", CAPath: "/flag/ca.crt"}},
		{name: "client cert alone preserves HTTP", clientCert: "/client/client.crt", wantHTTP: true},
		{name: "client key alone preserves HTTP", clientKey: "/client/client.key", wantHTTP: true},
		{name: "client CA alone preserves HTTP", clientCA: "/client/ca.crt", wantHTTP: true},
		{name: "managed conflicts server flag", selector: "/managed", certFlag: "/flag/server.crt", wantErr: true},
		{name: "managed conflicts server key flag", selector: "/managed", keyFlag: "/flag/server.key", wantErr: true},
		{name: "managed conflicts server CA flag", selector: "/managed", caFlag: "/flag/ca.crt", wantErr: true},
		{name: "managed conflicts server cert env", selector: "/managed", serverCert: "/env/server.crt", wantErr: true},
		{name: "managed conflicts server key env", selector: "/managed", serverKey: "/env/server.key", wantErr: true},
		{name: "managed conflicts server env", selector: "/managed", serverCA: "/env/ca.crt", wantErr: true},
		{name: "managed conflicts client cert", selector: "/managed", clientCert: "/client/client.crt", wantErr: true},
		{name: "managed conflicts client key", selector: "/managed", clientKey: "/client/client.key", wantErr: true},
		{name: "managed conflicts client CA", selector: "/managed", clientCA: "/client/ca.crt", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR", tt.selector)
			t.Setenv("AGENT_FITNESS_FUNCTIONS_TLS_CERT", tt.serverCert)
			t.Setenv("AGENT_FITNESS_FUNCTIONS_TLS_KEY", tt.serverKey)
			t.Setenv("AGENT_FITNESS_FUNCTIONS_TLS_CA", tt.serverCA)
			t.Setenv("AGENT_FITNESS_FUNCTIONS_CLIENT_CERT", tt.clientCert)
			t.Setenv("AGENT_FITNESS_FUNCTIONS_CLIENT_KEY", tt.clientKey)
			t.Setenv("AGENT_FITNESS_FUNCTIONS_CLIENT_CA", tt.clientCA)
			getwdCalls := 0
			mode, err := resolveServerStartTLSMode(tt.certFlag, tt.keyFlag, tt.caFlag, func() (string, error) {
				getwdCalls++
				return "/work", nil
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveServerStartTLSMode error = %v, wantErr %v", err, tt.wantErr)
			}
			if getwdCalls != tt.wantGetwd {
				t.Fatalf("Getwd calls = %d, want %d", getwdCalls, tt.wantGetwd)
			}
			if mode.ManagedRoot != tt.wantManaged {
				t.Fatalf("ManagedRoot = %q, want %q", mode.ManagedRoot, tt.wantManaged)
			}
			if mode.TLS != tt.wantTLS {
				t.Fatalf("TLS = %+v, want %+v", mode.TLS, tt.wantTLS)
			}
			if tt.wantHTTP && (mode.ManagedRoot != "" || mode.TLS.Enabled()) {
				t.Fatalf("client-only mode = %+v, want plain HTTP server mode", mode)
			}
		})
	}
}

func TestResolveServerStartTLSModeDoesNotGetWorkingDirectoryForExternalTLS(t *testing.T) {
	for _, source := range []string{"flags", "environment"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv("AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR", "")
			t.Setenv("AGENT_FITNESS_FUNCTIONS_TLS_CERT", "")
			t.Setenv("AGENT_FITNESS_FUNCTIONS_TLS_KEY", "")
			t.Setenv("AGENT_FITNESS_FUNCTIONS_TLS_CA", "")
			t.Setenv("AGENT_FITNESS_FUNCTIONS_CLIENT_CERT", "")
			t.Setenv("AGENT_FITNESS_FUNCTIONS_CLIENT_KEY", "")
			t.Setenv("AGENT_FITNESS_FUNCTIONS_CLIENT_CA", "")
			cert, key, ca := "server.crt", "server.key", "ca.crt"
			if source == "environment" {
				t.Setenv("AGENT_FITNESS_FUNCTIONS_TLS_CERT", cert)
				t.Setenv("AGENT_FITNESS_FUNCTIONS_TLS_KEY", key)
				t.Setenv("AGENT_FITNESS_FUNCTIONS_TLS_CA", ca)
				cert, key, ca = "", "", ""
			}
			mode, err := resolveServerStartTLSMode(cert, key, ca, func() (string, error) {
				return "", errors.New("injected Getwd failure")
			})
			if err != nil {
				t.Fatalf("explicit external TLS consulted working directory: %v", err)
			}
			if mode.TLS.CertPath != "server.crt" || mode.ManagedRoot != "" {
				t.Fatalf("mode = %+v, want explicit external TLS", mode)
			}
		})
	}
}

func TestResolveServerStartTLSModeReportsWorkingDirectoryFailureOnlyForDefaultManagedMode(t *testing.T) {
	for _, name := range []string{"AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR", "AGENT_FITNESS_FUNCTIONS_TLS_CERT", "AGENT_FITNESS_FUNCTIONS_TLS_KEY", "AGENT_FITNESS_FUNCTIONS_TLS_CA", "AGENT_FITNESS_FUNCTIONS_CLIENT_CERT", "AGENT_FITNESS_FUNCTIONS_CLIENT_KEY", "AGENT_FITNESS_FUNCTIONS_CLIENT_CA"} {
		t.Setenv(name, "")
	}
	_, err := resolveServerStartTLSMode("", "", "", func() (string, error) {
		return "", errors.New("injected Getwd failure")
	})
	if err == nil || !strings.Contains(err.Error(), "resolve server working directory") {
		t.Fatalf("default managed mode error = %v, want working-directory failure", err)
	}
}

func TestRunServeRejectsManagedConflictBeforeFilesystemSideEffects(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing-certs")
	t.Setenv("AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR", root)
	t.Setenv("AGENT_FITNESS_FUNCTIONS_CLIENT_CA", "/external/ca.crt")
	var stderr bytes.Buffer
	code := runServe([]string{"--addr", "127.0.0.1:0"}, &stderr)
	if code != 2 {
		t.Fatalf("runServe conflict exit = %d, want 2; stderr=%q", code, stderr.String())
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("managed root side effect before conflict rejection: %v", err)
	}
}

// TestRunServeReadsTLSEnvVarsAsFallback proves the three AGENT_FITNESS_FUNCTIONS_TLS_*
// env vars are honored: with all three set (and no flags), the all-or-none validation
// passes and failure comes from loading the bogus cert files — not from a missing-flag
// error. Without the fallback the server would have silently started plain HTTP.
func TestRunServeReadsTLSEnvVarsAsFallback(t *testing.T) {
	t.Setenv("AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR", writeMountedServeConfigDir(t))
	t.Setenv("AGENT_FITNESS_FUNCTIONS_TLS_CERT", filepath.Join(t.TempDir(), "server.crt"))
	t.Setenv("AGENT_FITNESS_FUNCTIONS_TLS_KEY", filepath.Join(t.TempDir(), "server.key"))
	t.Setenv("AGENT_FITNESS_FUNCTIONS_TLS_CA", filepath.Join(t.TempDir(), "ca.crt"))

	var stderr bytes.Buffer
	code := runServe([]string{"--addr", "127.0.0.1:0"}, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr = %q", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "tls requires") {
		t.Fatalf("stderr = %q, want a cert-load failure, not the all-or-none validation error", stderr.String())
	}
	if !strings.Contains(stderr.String(), "tls certificate") {
		t.Fatalf("stderr = %q, want tls certificate load failure proving env vars were used", stderr.String())
	}
}

// TestRunServePartialTLSEnvVarsFailValidation proves setting only some of the TLS env
// vars fails the all-or-none check with a message naming both the flags and env vars.
func TestRunServePartialTLSEnvVarsFailValidation(t *testing.T) {
	t.Setenv("AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR", writeMountedServeConfigDir(t))
	t.Setenv("AGENT_FITNESS_FUNCTIONS_TLS_CERT", filepath.Join(t.TempDir(), "server.crt"))
	t.Setenv("AGENT_FITNESS_FUNCTIONS_TLS_KEY", "")
	t.Setenv("AGENT_FITNESS_FUNCTIONS_TLS_CA", "")

	var stderr bytes.Buffer
	code := runServe([]string{"--addr", "127.0.0.1:0"}, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "tls requires") {
		t.Fatalf("stderr = %q, want all-or-none TLS validation error", stderr.String())
	}
	if !strings.Contains(stderr.String(), "AGENT_FITNESS_FUNCTIONS_TLS_CERT") {
		t.Fatalf("stderr = %q, want validation error to mention the env vars", stderr.String())
	}
}

// TestRunServeTLSFlagsOverrideEnvVars proves flags win over the env fallback: a flag
// cert+key pair with only one env var still trips the all-or-none check (no CA anywhere).
func TestRunServeTLSFlagsOverrideEnvVars(t *testing.T) {
	t.Setenv("AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR", writeMountedServeConfigDir(t))
	t.Setenv("AGENT_FITNESS_FUNCTIONS_TLS_CERT", filepath.Join(t.TempDir(), "env-server.crt"))
	t.Setenv("AGENT_FITNESS_FUNCTIONS_TLS_KEY", "")
	t.Setenv("AGENT_FITNESS_FUNCTIONS_TLS_CA", "")

	var stderr bytes.Buffer
	code := runServe([]string{
		"--addr", "127.0.0.1:0",
		"--tls-cert", filepath.Join(t.TempDir(), "flag-server.crt"),
		"--tls-key", filepath.Join(t.TempDir(), "flag-server.key"),
	}, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "tls requires") {
		t.Fatalf("stderr = %q, want all-or-none error (flag cert+key, no ca)", stderr.String())
	}
}

func TestRunDispatchesServerStart(t *testing.T) {
	t.Setenv("AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR", writeMountedServeConfigDir(t))

	var stderr bytes.Buffer
	code := run([]string{
		"server",
		"start",
		"--addr", "127.0.0.1:0",
		"--trusted-proxy-headers",
	}, &bytes.Buffer{}, &stderr)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "trusted proxy mode requires at least one trusted proxy client CN") {
		t.Fatalf("stderr = %q, want managed server start dispatch validation", stderr.String())
	}
}

func TestRunRejectsOldTopLevelCommands(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{name: "check", command: "check"},
		{name: "serve", command: "serve"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			code := run([]string{tt.command}, &bytes.Buffer{}, &stderr)
			if code != 2 {
				t.Fatalf("exit code = %d, want 2", code)
			}
			if !strings.Contains(stderr.String(), "unknown command "+strconv.Quote(tt.command)) {
				t.Fatalf("stderr = %q, want unknown command", stderr.String())
			}
		})
	}
}

func TestRunClientValidateUsesClientTLSFlags(t *testing.T) {
	var received fitness.ValidationRequest
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
				http.Error(w, "client certificate required", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/check":
			if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
				http.Error(w, "client certificate required", http.StatusUnauthorized)
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(fitness.ValidationResult{Status: fitness.StatusPass})
		default:
			http.NotFound(w, r)
		}
	}))
	server.TLS = &tls.Config{
		ClientAuth: tls.RequireAnyClientCert,
		MinVersion: tls.VersionTLS12,
	}
	server.StartTLS()
	defer server.Close()

	dir := t.TempDir()
	caPath := filepath.Join(dir, "server-ca.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600); err != nil {
		t.Fatalf("write server ca: %v", err)
	}
	clientCertPath, clientKeyPath := writeTestClientCertificate(t, dir, "ci-runner-graft")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"client",
		"validate",
		"--addr", server.URL,
		"--file", "x.go",
		"--repo", "/tmp/repo",
		"--content", "package main\n",
		"--client-ca", caPath,
		"--client-cert", clientCertPath,
		"--client-key", clientKeyPath,
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if received.File != "x.go" || received.Repo != "/tmp/repo" {
		t.Fatalf("received request = %+v", received)
	}
	if !strings.Contains(stdout.String(), "\"status\":\"pass\"") {
		t.Fatalf("stdout = %q, want pass JSON", stdout.String())
	}
}

func TestRunClientValidateRequiresClientCertAndKeyTogether(t *testing.T) {
	var starterCalled bool
	var stderr bytes.Buffer
	code := runWithDependencies(
		[]string{
			"client",
			"validate",
			"--addr", "https://127.0.0.1:1",
			"--file", "x.go",
			"--repo", "/tmp/repo",
			"--content", "package main\n",
			"--client-cert", "client.pem",
		},
		&bytes.Buffer{},
		&stderr,
		&http.Client{Timeout: time.Second},
		func(client.DaemonStartConfig) error {
			starterCalled = true
			return nil
		},
	)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if starterCalled {
		t.Fatal("daemon starter was called before client TLS flag validation")
	}
	if !strings.Contains(stderr.String(), "client validate requires --client-cert and --client-key together") {
		t.Fatalf("stderr = %q, want client cert/key validation error", stderr.String())
	}
}

func TestRunClientValidateAllowsBareLogicalRepoWithContentFile(t *testing.T) {
	var received fitness.ValidationRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/check":
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(fitness.ValidationResult{Status: fitness.StatusPass})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	contentPath := filepath.Join(t.TempDir(), "content.go")
	if err := os.WriteFile(contentPath, []byte("package logical\n"), 0o600); err != nil {
		t.Fatalf("write content file: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"client", "validate", "--addr", server.URL, "--file", "remote.go", "--repo", "graft", "--content-file", contentPath, "--language", "go"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if received.Repo != "graft" || received.ProposedContent != "package logical\n" {
		t.Fatalf("received request = %+v", received)
	}
}

func TestRunClientValidatePostsToHealthyDaemon(t *testing.T) {
	var received fitness.ValidationRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/check":
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(fitness.ValidationResult{Status: fitness.StatusPass})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	code := run([]string{"client", "validate", "--addr", server.URL, "--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n", "--language", "go"}, &stdout, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	if received.File != "x.go" || received.Repo != "/tmp/repo" || received.Language != "go" {
		t.Fatalf("received request = %+v", received)
	}
	if !strings.Contains(stdout.String(), `"status":"pass"`) {
		t.Fatalf("stdout = %q, want pass JSON", stdout.String())
	}
}

func TestRunClientValidateStartsDaemonWhenCold(t *testing.T) {
	var received fitness.ValidationRequest
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/check":
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(fitness.ValidationResult{Status: fitness.StatusPass})
		default:
			http.NotFound(w, r)
		}
	})}
	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
	})

	var stdout bytes.Buffer
	code := runWithDependencies(
		[]string{"client", "validate", "--addr", "http://" + listener.Addr().String(), "--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n"},
		&stdout,
		&bytes.Buffer{},
		&http.Client{Timeout: time.Second},
		func(client.DaemonStartConfig) error {
			go func() {
				_ = server.Serve(listener)
			}()
			return nil
		},
	)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if received.File != "x.go" {
		t.Fatalf("received file = %q, want x.go", received.File)
	}
	if !strings.Contains(stdout.String(), `"status":"pass"`) {
		t.Fatalf("stdout = %q, want pass JSON", stdout.String())
	}
}

func TestRunDispatchesDoctorAndReportsFailures(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--addr", "https://127.0.0.1:1", "--repo", "calm-poc"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("doctor exit code = %d, want 1 (unreachable server); stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "server reachable") {
		t.Fatalf("stdout = %q, want doctor check output", stdout.String())
	}
	if !strings.Contains(stderr.String(), "doctor found") {
		t.Fatalf("stderr = %q, want problem summary", stderr.String())
	}
}

func TestRunDispatchesClientOnboardUsageError(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"client", "onboard", "--enforcement", "bogus"}, &bytes.Buffer{}, &stderr)
	if code != 2 {
		t.Fatalf("onboard bad-enforcement exit code = %d, want 2; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unsupported enforcement mode") {
		t.Fatalf("stderr = %q, want enforcement usage error", stderr.String())
	}
}

func TestRunClientOnboardCertificatesOnlyPublishesManagedRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certs")
	t.Setenv("AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR", root)
	var stdout, stderr bytes.Buffer
	code := run([]string{"client", "onboard", "--certificates-only"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("certificates-only exit code = %d, want 0; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	target, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil {
		t.Fatalf("Readlink(current): %v", err)
	}
	if matched, _ := regexp.MatchString(`^versions/v-[0-9a-f]{32}$`, target); !matched {
		t.Fatalf("current target = %q, want version publication", target)
	}
	if strings.Contains(stdout.String()+stderr.String(), "PRIVATE KEY") {
		t.Fatal("CLI output exposed private key material")
	}
}

func TestRunClientOnboardCertificatesOnlyAcceptsForceWithoutBroadeningState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR", root)
	seed := filepath.Join(root, "unknown")
	if err := os.WriteFile(seed, []byte("preserve me\n"), 0o640); err != nil {
		t.Fatalf("seed unsupported state: %v", err)
	}
	before, err := os.ReadFile(seed)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	var stderr bytes.Buffer
	code := run([]string{"client", "onboard", "--certificates-only", "--force-dev-cert-rotation"}, &bytes.Buffer{}, &stderr)
	if code != 1 {
		t.Fatalf("forced unsupported exit code = %d, want 1; stderr=%q", code, stderr.String())
	}
	after, err := os.ReadFile(seed)
	if err != nil {
		t.Fatalf("read preserved seed: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("forced unsupported state changed: before=%q after=%q", before, after)
	}
}

func TestRunDoctorRejectsUnknownFlagWithUsageCode(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"doctor", "--nope"}, &bytes.Buffer{}, &stderr)
	if code != 2 {
		t.Fatalf("doctor unknown-flag exit code = %d, want 2", code)
	}
}

func TestRunClientValidateReturnsUsageExitCodeForMissingFlags(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"client", "validate", "--file", "x.go"}, &bytes.Buffer{}, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "client validate requires --file and --repo") {
		t.Fatalf("stderr = %q, want usage error", stderr.String())
	}
}

func TestRunClientValidateReportsInfraErrorForDaemonFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/check":
			http.Error(w, "invalid check request", http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// A daemon-side failure is an infrastructure error, not a fitness-function block:
	// it exits with the reserved infra code (3) and emits a machine-readable error
	// object — carrying kind and the daemon's response body — on stdout.
	var stdout, stderr bytes.Buffer
	code := run([]string{"client", "validate", "--addr", server.URL, "--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n"}, &stdout, &stderr)
	if code != client.InfraErrorExitCode {
		t.Fatalf("exit code = %d, want %d (infra error)", code, client.InfraErrorExitCode)
	}
	for _, want := range []string{`"status":"error"`, `"error_kind":"invalid_request"`, "invalid check request"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want to contain %q", stdout.String(), want)
		}
	}
}

func TestResolveContentReadsRelativeToRepo(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "x.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	var received fitness.ValidationRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/check":
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(fitness.ValidationResult{Status: fitness.StatusPass})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	code := run([]string{"client", "validate", "--addr", server.URL, "--file", "x.go", "--repo", repo}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if received.ProposedContent != "package main\n" {
		t.Fatalf("content = %q, want repo-relative file content", received.ProposedContent)
	}
}

func TestRunClientValidatePreservesContentFileBytes(t *testing.T) {
	var received fitness.ValidationRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/check":
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(fitness.ValidationResult{Status: fitness.StatusPass})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	contentFile := filepath.Join(t.TempDir(), "content")
	want := "package sample\n\n"
	if err := os.WriteFile(contentFile, []byte(want), 0o600); err != nil {
		t.Fatalf("write content file: %v", err)
	}

	code := run([]string{"client", "validate", "--addr", server.URL, "--file", "x.go", "--repo", "/tmp/repo", "--content-file", contentFile, "--language", "go"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if received.ProposedContent != want {
		t.Fatalf("proposed content = %q, want exact content file bytes", received.ProposedContent)
	}
}

func TestRunClientValidateAllowsEmptyContentFile(t *testing.T) {
	var received fitness.ValidationRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/check":
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(fitness.ValidationResult{Status: fitness.StatusPass})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	contentFile := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(contentFile, nil, 0o600); err != nil {
		t.Fatalf("write content file: %v", err)
	}

	code := run([]string{"client", "validate", "--addr", server.URL, "--file", "x.go", "--repo", "/tmp/repo", "--content-file", contentFile, "--language", "go"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if received.ProposedContent != "" {
		t.Fatalf("proposed content = %q, want empty content", received.ProposedContent)
	}
}

func TestRunClientValidateStagedReadsIndexInsteadOfWorktree(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	if err := os.WriteFile(filepath.Join(repo, "x.go"), []byte("package staged\n"), 0o644); err != nil {
		t.Fatalf("write staged content: %v", err)
	}
	runGit(t, repo, "add", "x.go")
	if err := os.WriteFile(filepath.Join(repo, "x.go"), []byte("package worktree\n"), 0o644); err != nil {
		t.Fatalf("write worktree content: %v", err)
	}
	var received fitness.ValidationRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/check":
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(fitness.ValidationResult{Status: fitness.StatusPass})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var stderr bytes.Buffer
	code := run([]string{"client", "validate", "--addr", server.URL, "--file", "x.go", "--repo", repo, "--staged", "--language", "go"}, &bytes.Buffer{}, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if received.ProposedContent != "package staged\n" {
		t.Fatalf("proposed content = %q, want staged index content", received.ProposedContent)
	}
}

func TestRunBaselineWritesReport(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "x.go"), []byte("package sample\n\nfunc Run() {}\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	output := filepath.Join(t.TempDir(), "baseline.json")

	var stdout bytes.Buffer
	code := run([]string{"baseline", "--repo", repo, "--language", "go", "--output", output, "--name", "sample"}, &stdout, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	if !strings.Contains(string(content), `"repository": "sample"`) {
		t.Fatalf("baseline report = %s, want repository name", content)
	}
}

func TestRunBaselineEmitConfigWritesConfigAndReport(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "x.go"), []byte("package sample\n\nfunc Run() string { return \"ok\" }\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	output := filepath.Join(t.TempDir(), "baseline.json")
	configPath := filepath.Join(t.TempDir(), "config.json")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"baseline", "--repo", repo, "--language", "go",
		"--output", output, "--emit-config", configPath, "--name", "selftest",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "Onboarding recommendation for \"selftest\"") {
		t.Fatalf("stdout missing recommendation header:\n%s", out)
	}
	if !strings.Contains(out, "Threshold delta") || !strings.Contains(out, "GLOBAL and compiled into the binary") {
		t.Fatalf("stdout missing threshold-delta report or limitation note:\n%s", out)
	}
	if !strings.Contains(out, "enforcement-mode:") {
		t.Fatalf("stdout missing enforcement recommendation:\n%s", out)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read emitted config: %v", err)
	}
	for _, needle := range []string{
		`"enforcement-mode"`,
		`"enforcement-on-error": "block"`,
		`"cyclomatic-complexity": true`,
		`"dependency-discipline": true`,
	} {
		if !strings.Contains(string(content), needle) {
			t.Fatalf("emitted config missing %q:\n%s", needle, content)
		}
	}
}

func TestRunBaselineWithoutEmitConfigSkipsConfig(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "x.go"), []byte("package sample\n\nfunc Run() {}\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	output := filepath.Join(t.TempDir(), "baseline.json")

	var stdout bytes.Buffer
	code := run([]string{"baseline", "--repo", repo, "--language", "go", "--output", output, "--name", "sample"}, &stdout, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if strings.Contains(stdout.String(), "Onboarding recommendation") {
		t.Fatalf("stdout should not include onboarding report without --emit-config:\n%s", stdout.String())
	}
}

func runGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func TestRunBaselineWritesCSharpReport(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "Example.cs"), []byte("public class Example {}"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	roslyn := filepath.Join(t.TempDir(), "roslyn")
	script := `#!/usr/bin/env bash
set -euo pipefail
cat <<JSON
{
  "calm_node": "Example",
  "language": "csharp",
  "file": "$1",
  "functions": [{"name":"Run","cyclomatic_complexity":1,"is_public":true,"loc":1}],
  "file_metrics": {"total_loc": 1, "logic_loc": 1, "public_methods": 1, "ldr": 1},
  "import_metrics": {"total": 0, "used": 0, "ddc": 1}
}
JSON
`
	if err := os.WriteFile(roslyn, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake roslyn: %v", err)
	}
	output := filepath.Join(t.TempDir(), "baseline.json")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"baseline", "--repo", repo, "--language", "csharp", "--output", output, "--name", "sample-csharp", "--roslyn", roslyn}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	if !strings.Contains(string(content), `"language": "csharp"`) || !strings.Contains(string(content), `"distributions": {`) {
		t.Fatalf("baseline report = %s, want csharp report with distributions", content)
	}
}

func TestRunBaselineBuildsLocalRoslynWhenPathOmitted(t *testing.T) {
	if _, err := exec.LookPath("dotnet"); err != nil {
		t.Skip("dotnet not installed")
	}
	roslynExecutable := filepath.Clean(filepath.Join("..", "..", "tools", "roslyn-analyzer", "bin", "Debug", "net8.0", "CalmRoslynAnalyzer"))
	if runtime.GOOS == "windows" {
		roslynExecutable += ".exe"
	}
	restoreRoslyn := temporarilyMoveFile(t, roslynExecutable)
	defer restoreRoslyn()

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "Example.cs"), []byte("public class Example { public void Run() {} }"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	output := filepath.Join(t.TempDir(), "baseline.json")

	var stderr bytes.Buffer
	code := run([]string{"baseline", "--repo", repo, "--language", "csharp", "--output", output, "--name", "sample-csharp"}, &bytes.Buffer{}, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	if !strings.Contains(string(content), `"language": "csharp"`) {
		t.Fatalf("baseline report = %s, want csharp report", content)
	}
}

func temporarilyMoveFile(t *testing.T, path string) func() {
	t.Helper()
	backup := path + ".testbak"
	if err := os.Rename(path, backup); err != nil {
		if os.IsNotExist(err) {
			return func() {}
		}
		t.Fatalf("move %s aside: %v", path, err)
	}
	return func() {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove rebuilt %s: %v", path, err)
		}
		if err := os.Rename(backup, path); err != nil {
			t.Fatalf("restore %s: %v", path, err)
		}
	}
}

func TestRunServeStopsOnSIGTERM(t *testing.T) {
	testRunServeStopsOnSignal(t, syscall.SIGTERM)
}

func TestRunServeStopsOnSIGINT(t *testing.T) {
	testRunServeStopsOnSignal(t, os.Interrupt)
}

func testRunServeStopsOnSignal(t *testing.T, signal os.Signal) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	configDir := writeMountedServeConfigDir(t)
	t.Setenv("AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR", configDir)
	certRoot := filepath.Join(t.TempDir(), "certs")
	if err := devcerts.Publish(certRoot, false); err != nil {
		t.Fatalf("Publish(%q): %v", certRoot, err)
	}
	version, err := devcerts.ResolveManagedVersion(certRoot)
	if err != nil {
		t.Fatalf("ResolveManagedVersion: %v", err)
	}
	caPEM, err := os.ReadFile(version.Paths().CA)
	if err != nil {
		t.Fatalf("ReadFile(CA): %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("AppendCertsFromPEM(CA) failed")
	}
	t.Setenv("AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR", certRoot)

	done := make(chan int, 1)
	go func() {
		done <- runServe([]string{"--addr", addr}, &bytes.Buffer{})
	}()

	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
	for range 40 {
		response, err := client.Get("https://" + addr + "/health")
		if err == nil {
			_ = response.Body.Close()
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("find process: %v", err)
	}
	if err := process.Signal(signal); err != nil {
		t.Fatalf("send signal: %v", err)
	}

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("runServe exit code = %d, want 0", code)
		}
	case <-time.After(time.Second):
		t.Fatal("runServe did not stop after SIGTERM")
	}
}

func writeTestClientCertificate(t *testing.T, dir, commonName string) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(101),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create client certificate: %v", err)
	}
	certPath := filepath.Join(dir, "client.pem")
	keyPath := filepath.Join(dir, "client-key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write client certificate: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatalf("write client key: %v", err)
	}
	return certPath, keyPath
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
	return dir
}
