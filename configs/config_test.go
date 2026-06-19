package configs_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type mountedConfig struct {
	EnforcementMode  string          `json:"enforcement-mode"`
	EnforcementError string          `json:"enforcement-on-error"`
	FitnessFunctions map[string]bool `json:"fitness-functions"`
	ExcludePatterns  []string        `json:"exclude-patterns"`
}

func TestRepositoryConfigTemplates(t *testing.T) {
	expected := map[string]struct {
		mode string
	}{
		"graft":       {mode: "block"},
		"ringstation": {mode: "advisory"},
		"slackstatus": {mode: "block"},
	}
	requiredFitnessFunctions := []string{
		"cyclomatic-complexity",
		"interface-width",
		"implementation-depth",
		"logic-density",
		"dependency-discipline",
	}

	for dir, want := range expected {
		t.Run(dir, func(t *testing.T) {
			assertMountedConfigShape(t, filepath.Join(dir, "config.json"), want.mode, requiredFitnessFunctions)
		})
	}
}

func TestConfigTemplatesMatchMountedSchema(t *testing.T) {
	assertMountedConfigShape(t, "block-template.json", "block", nil)
	assertMountedConfigShape(t, "advisory-template.json", "advisory", nil)
}

func TestCallerRepoBindings(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "caller-repos.json"))
	if err != nil {
		t.Fatalf("read caller-repos.json: %v", err)
	}

	var doc struct {
		Callers map[string][]string `json:"callers"`
		Admins  []string            `json:"admins"`
	}
	if err := json.Unmarshal(content, &doc); err != nil {
		t.Fatalf("parse caller-repos.json: %v", err)
	}

	wantCallers := map[string][]string{
		"ci-runner-graft": {"graft"},
		"ci-runner-all":   {"graft", "ringstation", "slackstatus"},
		"dev-hook-pool":   {"calm-poc", "graft", "ringstation", "slackstatus", "relocate"},
	}
	if !reflect.DeepEqual(doc.Callers, wantCallers) {
		t.Fatalf("caller bindings = %#v, want %#v", doc.Callers, wantCallers)
	}
	wantAdmins := []string{"dev-hook-pool"}
	if !reflect.DeepEqual(doc.Admins, wantAdmins) {
		t.Fatalf("admins = %#v, want %#v", doc.Admins, wantAdmins)
	}
}

func TestCalmPocConfigExcludesIntentionalViolationFixtures(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("calm-poc", "config.json"))
	if err != nil {
		t.Fatalf("read calm-poc/config.json: %v", err)
	}

	var config mountedConfig
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatalf("parse calm-poc/config.json: %v", err)
	}

	wantPatterns := []string{
		"fixtures/violations/go/*.go",
		"fixtures/violations/python/*.py",
	}
	for _, want := range wantPatterns {
		if !containsString(config.ExcludePatterns, want) {
			t.Fatalf("calm-poc exclude-patterns = %#v, want %q", config.ExcludePatterns, want)
		}
	}
}

func assertMountedConfigShape(t *testing.T, path, wantMode string, requiredFitnessFunctions []string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(content, &raw); err != nil {
		t.Fatalf("parse raw %s: %v", path, err)
	}
	if len(raw) != 3 {
		t.Fatalf("%s raw key count = %d, want 3", path, len(raw))
	}
	for _, key := range []string{"enforcement-mode", "enforcement-on-error", "fitness-functions"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("%s missing required key %q", path, key)
		}
	}
	for _, key := range []string{"version", "repo", "language", "daemon"} {
		if _, ok := raw[key]; ok {
			t.Fatalf("%s unexpectedly contains stripped key %q", path, key)
		}
	}

	var config mountedConfig
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if config.EnforcementMode != wantMode {
		t.Fatalf("%s enforcement-mode = %q, want %q", path, config.EnforcementMode, wantMode)
	}
	if config.EnforcementError != "block" {
		t.Fatalf("%s enforcement-on-error = %q, want %q", path, config.EnforcementError, "block")
	}
	for _, name := range requiredFitnessFunctions {
		if !config.FitnessFunctions[name] {
			t.Fatalf("%s fitness functions = %+v, want %s enabled", path, config.FitnessFunctions, name)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
