package client

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/poconnor/calm-poc/internal/fitness"
)

func TestPackageImportsFitnessContractNotServerInternals(t *testing.T) {
	command := exec.Command("go", "list", "-f", "{{join .Imports \"\\n\"}}", "./internal/client")
	command.Dir = projectRoot(t)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list internal/client: %v\n%s", err, output)
	}
	imports := string(output)
	if !strings.Contains(imports, "github.com/poconnor/calm-poc/internal/fitness") {
		t.Fatalf("imports = %s, want internal/fitness", imports)
	}
	if strings.Contains(imports, "github.com/poconnor/calm-poc/internal/server") {
		t.Fatalf("imports = %s, must not include internal/server", imports)
	}
}

func TestRunCheckPostsValidationRequestThroughPublicClientInterface(t *testing.T) {
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
	err := RunCheck([]string{"--addr", server.URL, "--file", "x.go", "--repo", "/tmp/repo", "--content", "package main\n", "--language", "go"}, &stdout, &http.Client{Timeout: time.Second}, func(string) error { return nil })
	if err != nil {
		t.Fatalf("RunCheck returned error: %v", err)
	}

	if received.File != "x.go" || received.Repo != "/tmp/repo" || received.Language != "go" {
		t.Fatalf("received request = %+v", received)
	}
	if !strings.Contains(stdout.String(), `"status":"pass"`) {
		t.Fatalf("stdout = %q, want pass JSON", stdout.String())
	}
}

func TestResolveContentReadsRelativeToRepo(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "x.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	content, err := resolveContent(repo, "x.go", "", "", false)
	if err != nil {
		t.Fatalf("resolveContent returned error: %v", err)
	}
	if content != "package main\n" {
		t.Fatalf("content = %q, want repo-relative file content", content)
	}
}

func projectRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for current := cwd; ; current = filepath.Dir(current) {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			t.Fatalf("could not find go.mod from %s", cwd)
		}
	}
}
