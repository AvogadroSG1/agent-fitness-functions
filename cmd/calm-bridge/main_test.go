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
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/poconnor/calm-poc/internal/bridge"
)

func TestRunServeRequiresTLSForTrustedProxyHeaders(t *testing.T) {
	t.Setenv("CALM_CONFIGS_DIR", writeMountedServeConfigDir(t))

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
	t.Setenv("CALM_CONFIGS_DIR", writeMountedServeConfigDir(t))

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

func TestRunCheckUsesClientTLSFlags(t *testing.T) {
	var received bridge.CheckRequest
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
			_ = json.NewEncoder(w).Encode(bridge.CheckResponse{Status: bridge.StatusPass})
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
		"check",
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

func TestRunCheckRequiresClientCertAndKeyTogether(t *testing.T) {
	var starterCalled bool
	var stderr bytes.Buffer
	code := runWithDependencies(
		[]string{
			"check",
			"--addr", "https://127.0.0.1:1",
			"--file", "x.go",
			"--repo", "/tmp/repo",
			"--content", "package main\n",
			"--client-cert", "client.pem",
		},
		&bytes.Buffer{},
		&stderr,
		&http.Client{Timeout: time.Second},
		func(string) error {
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
	if !strings.Contains(stderr.String(), "check requires --client-cert and --client-key together") {
		t.Fatalf("stderr = %q, want client cert/key validation error", stderr.String())
	}
}

func TestRunCheckPostsToHealthyDaemon(t *testing.T) {
	var received bridge.CheckRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/check":
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(bridge.CheckResponse{Status: bridge.StatusPass})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	code := run([]string{"check", "--addr", server.URL, "--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n", "--language", "go"}, &stdout, &bytes.Buffer{})
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

func TestRunCheckStartsDaemonWhenCold(t *testing.T) {
	var received bridge.CheckRequest
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
			_ = json.NewEncoder(w).Encode(bridge.CheckResponse{Status: bridge.StatusPass})
		default:
			http.NotFound(w, r)
		}
	})}
	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
	})

	var stdout bytes.Buffer
	code := runWithDependencies(
		[]string{"check", "--addr", "http://" + listener.Addr().String(), "--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n"},
		&stdout,
		&bytes.Buffer{},
		&http.Client{Timeout: time.Second},
		func(string) error {
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

func TestRunCheckReturnsUsageExitCodeForMissingFlags(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"check", "--file", "x.go"}, &bytes.Buffer{}, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "check requires --file and --repo") {
		t.Fatalf("stderr = %q, want usage error", stderr.String())
	}
}

func TestRunCheckIncludesDaemonErrorBody(t *testing.T) {
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

	var stderr bytes.Buffer
	code := run([]string{"check", "--addr", server.URL, "--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n"}, &bytes.Buffer{}, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "invalid check request") {
		t.Fatalf("stderr = %q, want daemon response body", stderr.String())
	}
}

func TestResolveContentReadsRelativeToRepo(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "x.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	content, err := resolveContent(repo, "x.go", "", "", false)
	if err != nil {
		t.Fatalf("resolve content: %v", err)
	}
	if content != "package main\n" {
		t.Fatalf("content = %q, want repo-relative file content", content)
	}
}

func TestRunCheckPreservesContentFileBytes(t *testing.T) {
	var received bridge.CheckRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/check":
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(bridge.CheckResponse{Status: bridge.StatusPass})
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

	code := run([]string{"check", "--addr", server.URL, "--file", "x.go", "--repo", "/tmp/repo", "--content-file", contentFile, "--language", "go"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if received.ProposedContent != want {
		t.Fatalf("proposed content = %q, want exact content file bytes", received.ProposedContent)
	}
}

func TestRunCheckAllowsEmptyContentFile(t *testing.T) {
	var received bridge.CheckRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/check":
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(bridge.CheckResponse{Status: bridge.StatusPass})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	contentFile := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(contentFile, nil, 0o600); err != nil {
		t.Fatalf("write content file: %v", err)
	}

	code := run([]string{"check", "--addr", server.URL, "--file", "x.go", "--repo", "/tmp/repo", "--content-file", contentFile, "--language", "go"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if received.ProposedContent != "" {
		t.Fatalf("proposed content = %q, want empty content", received.ProposedContent)
	}
}

func TestRunCheckStagedReadsIndexInsteadOfWorktree(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	if err := os.WriteFile(filepath.Join(repo, "x.go"), []byte("package staged\n"), 0o644); err != nil {
		t.Fatalf("write staged content: %v", err)
	}
	runGit(t, repo, "add", "x.go")
	if err := os.WriteFile(filepath.Join(repo, "x.go"), []byte("package worktree\n"), 0o644); err != nil {
		t.Fatalf("write worktree content: %v", err)
	}
	var received bridge.CheckRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/check":
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(bridge.CheckResponse{Status: bridge.StatusPass})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var stderr bytes.Buffer
	code := run([]string{"check", "--addr", server.URL, "--file", "x.go", "--repo", repo, "--staged", "--language", "go"}, &bytes.Buffer{}, &stderr)
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
	t.Setenv("CALM_CONFIGS_DIR", configDir)

	done := make(chan int, 1)
	go func() {
		done <- runServe([]string{"--addr", addr}, &bytes.Buffer{})
	}()

	client := &http.Client{Timeout: time.Second}
	for range 40 {
		response, err := client.Get("http://" + addr + "/health")
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
