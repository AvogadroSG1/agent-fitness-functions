package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/poconnor/calm-poc/internal/bridge"
)

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

	content, err := resolveContent(repo, "x.go", "", false)
	if err != nil {
		t.Fatalf("resolve content: %v", err)
	}
	if content != "package main\n" {
		t.Fatalf("content = %q, want repo-relative file content", content)
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
