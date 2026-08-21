//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
)

func TestCheckerWithRealCALMBlocksGraftCyclomaticComplexityFixture(t *testing.T) {
	if _, err := exec.LookPath("calm"); err != nil {
		t.Skip("calm CLI not installed")
	}
	source, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "violations", "go", "graft-migrate-chain.go"))
	if err != nil {
		t.Fatalf("read graft fixture: %v", err)
	}
	store := newTestConfigStore(t)
	writeRepoConfig(t, store, "graft", EnforcementBlock, map[string]bool{"cyclomatic-complexity": true})
	server := httptest.NewServer(NewHandlerWithChecker(Checker{
		ConfigStore: store,
		PatternPath: filepath.Join("..", "..", "patterns", "governance.json"),
	}, nil))
	defer server.Close()

	start := time.Now()
	response, err := http.Post(server.URL+"/check", "application/json", strings.NewReader(`{
		"repo": "/Users/poconnor/peter_code/graft",
		"file": "internal/migrate/migrate.go",
		"language": "go",
		"proposed_content": `+jsonString(string(source))+`
	}`))
	if err != nil {
		t.Fatalf("POST /check: %v", err)
	}
	defer response.Body.Close()
	t.Logf("real synchronous Go /check latency: %s", time.Since(start))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	var body fitness.ValidationResult
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != fitness.StatusBlock {
		t.Fatalf("response = %+v, want block", body)
	}
	foundChain := false
	for _, violation := range body.Violations {
		if violation.Function == "Chain" && violation.Limit == 9 {
			foundChain = true
		}
	}
	if !foundChain {
		t.Fatalf("violations = %+v, want Chain cyclomatic complexity violation", body.Violations)
	}
}
