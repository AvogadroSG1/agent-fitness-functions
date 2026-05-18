package configs_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type calmConfig struct {
	Version          string          `json:"version"`
	Repo             string          `json:"repo"`
	Language         string          `json:"language"`
	EnforcementMode  string          `json:"enforcement-mode"`
	FitnessFunctions map[string]bool `json:"fitness-functions"`
}

func TestRepositoryConfigTemplates(t *testing.T) {
	expected := map[string]struct {
		repo     string
		language string
		mode     string
	}{
		"graft":                {repo: "graft", language: "go", mode: "block"},
		"ringstation":          {repo: "ringstation", language: "python", mode: "advisory"},
		"slackstatus":          {repo: "SlackStatus", language: "csharp", mode: "block"},
		"stackoverflow-api-v3": {repo: "StackOverflow.Api.V3", language: "csharp", mode: "block"},
	}

	for dir, want := range expected {
		t.Run(dir, func(t *testing.T) {
			path := filepath.Join(dir, ".calm", "config.json")
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			var config calmConfig
			if err := json.Unmarshal(content, &config); err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			if config.Repo != want.repo || config.Language != want.language || config.EnforcementMode != want.mode {
				t.Fatalf("config = %+v, want repo %s language %s mode %s", config, want.repo, want.language, want.mode)
			}
			for _, name := range []string{"cyclomatic-complexity", "interface-width", "implementation-depth", "logic-density", "dependency-discipline"} {
				if !config.FitnessFunctions[name] {
					t.Fatalf("%s fitness functions = %+v, want %s enabled", path, config.FitnessFunctions, name)
				}
			}
		})
	}
}
