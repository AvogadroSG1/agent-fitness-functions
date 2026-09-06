package client

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/govconfig"
)

// S10 red-test contract (calm-poc-z6m8): the wizard prompts for layer
// definitions when layer-sovereignty is enabled without settings, a confirmed
// diff (or the --update flag) rewrites the existing repo-local config, and
// plain non-interactive onboard still never rewrites.

func TestWizardPromptsForLayersWhenEnablingLayerSovereignty(t *testing.T) {
	var out strings.Builder
	outcome, err := runOnboardWizard(
		wizardScript(
			"",                   // enforcement default
			"6", "",              // toggle layer-sovereignty, confirm picker
			"domain",             // layer name
			"internal/domain/.*", // path patterns
			"database/sql",       // forbidden patterns
			"",                   // empty name finishes layer entry
			"y",                  // accept diff
		),
		&out,
		wizardFacts{RepoName: "fresh-repo"},
	)
	if err != nil {
		t.Fatalf("runOnboardWizard: %v", err)
	}
	if !outcome.Confirmed || !outcome.Functions["layer-sovereignty"] {
		t.Fatalf("outcome = %+v, want confirmed with layer-sovereignty enabled", outcome)
	}
	if len(outcome.Layers) != 1 {
		t.Fatalf("layers = %+v, want exactly one prompted layer", outcome.Layers)
	}
	layer := outcome.Layers[0]
	if layer.Name != "domain" {
		t.Errorf("layer name = %q, want domain", layer.Name)
	}
	if len(layer.Paths) != 1 || layer.Paths[0] != "internal/domain/.*" {
		t.Errorf("layer paths = %q, want the prompted pattern", layer.Paths)
	}
	if len(layer.ForbiddenPatterns) != 1 || layer.ForbiddenPatterns[0] != "database/sql" {
		t.Errorf("layer forbidden = %q, want the prompted pattern", layer.ForbiddenPatterns)
	}
}

func TestWizardRepromptsOnInvalidLayerPattern(t *testing.T) {
	var out strings.Builder
	outcome, err := runOnboardWizard(
		wizardScript(
			"",
			"6", "",
			"domain",
			"[",                  // invalid regex: must re-prompt, not fail
			"internal/domain/.*", // valid retry
			"database/sql",
			"",
			"y",
		),
		&out,
		wizardFacts{RepoName: "fresh-repo"},
	)
	if err != nil {
		t.Fatalf("runOnboardWizard: %v", err)
	}
	if len(outcome.Layers) != 1 || len(outcome.Layers[0].Paths) != 1 || outcome.Layers[0].Paths[0] != "internal/domain/.*" {
		t.Fatalf("layers = %+v, want the valid retry only", outcome.Layers)
	}
	if !strings.Contains(out.String(), "invalid") {
		t.Errorf("output must explain the rejected pattern:\n%s", out.String())
	}
}

func writeExistingScaffoldConfig(t *testing.T, repoRoot, repoName string) string {
	t.Helper()
	dir := filepath.Join(repoRoot, "configs", repoName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "config.json")
	content := `{"enforcement-mode":"advisory","fitness-functions":{"cyclomatic-complexity":true,"interface-width":true,"implementation-depth":true,"logic-density":true,"dependency-discipline":true}}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func updateTestOnboarder(t *testing.T, repoRoot string, update bool) *onboarder {
	t.Helper()
	return &onboarder{
		repoName:       "repo-x",
		repoRoot:       repoRoot,
		repoConfigsDir: filepath.Join(repoRoot, "configs"),
		configsDir:     t.TempDir(),
		enforcement:    "block",
		selectedFunctions: map[string]bool{
			"cyclomatic-complexity": true,
			"interface-width":       false,
			"implementation-depth":  false,
			"logic-density":         false,
			"dependency-discipline": false,
		},
		update:    update,
		localHTTP: true,
		stdout:    &bytes.Buffer{},
		stderr:    &bytes.Buffer{},
	}
}

func TestScaffoldConfigUpdateRewritesExistingConfig(t *testing.T) {
	repoRoot := onboardTestRepo(t)
	path := writeExistingScaffoldConfig(t, repoRoot, "repo-x")

	if err := updateTestOnboarder(t, repoRoot, true).scaffoldConfig(); err != nil {
		t.Fatalf("scaffoldConfig with update: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	parsed, err := govconfig.Parse(content)
	if err != nil {
		t.Fatalf("rewritten config does not parse: %v\n%s", err, content)
	}
	if parsed.EnforcementMode != govconfig.EnforcementBlock {
		t.Errorf("enforcement = %q, want block after update", parsed.EnforcementMode)
	}
	if parsed.FitnessFunctions["logic-density"] {
		t.Error("logic-density must be off after the update selection")
	}
}

func TestScaffoldConfigWithoutUpdateNeverRewrites(t *testing.T) {
	repoRoot := onboardTestRepo(t)
	path := writeExistingScaffoldConfig(t, repoRoot, "repo-x")
	before, _ := os.ReadFile(path)

	if err := updateTestOnboarder(t, repoRoot, false).scaffoldConfig(); err != nil {
		t.Fatalf("scaffoldConfig without update: %v", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("existing config was rewritten without update authority")
	}
}

func TestScaffoldConfigWritesPromptedLayers(t *testing.T) {
	repoRoot := onboardTestRepo(t)
	o := updateTestOnboarder(t, repoRoot, true)
	o.selectedFunctions["layer-sovereignty"] = true
	o.layers = []govconfig.LayerRule{{Name: "domain", Paths: []string{"internal/domain/.*"}, ForbiddenPatterns: []string{"database/sql"}}}
	writeExistingScaffoldConfig(t, repoRoot, "repo-x")

	if err := o.scaffoldConfig(); err != nil {
		t.Fatalf("scaffoldConfig with layers: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(repoRoot, "configs", "repo-x", "config.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	parsed, err := govconfig.Parse(content)
	if err != nil {
		t.Fatalf("config with layers does not parse: %v\n%s", err, content)
	}
	if !parsed.Enabled("layer-sovereignty") {
		t.Error("layer-sovereignty must be enabled")
	}
	if layers := parsed.LayerRules(); len(layers) != 1 || layers[0].Name != "domain" {
		t.Errorf("layers = %+v, want the prompted domain layer", layers)
	}
}

func TestParseOnboardFlagsAcceptsUpdate(t *testing.T) {
	flags, err := parseOnboardFlags([]string{"--update", "--enforcement", "block"})
	if err != nil {
		t.Fatalf("parseOnboardFlags(--update): %v", err)
	}
	if !flags.update {
		t.Fatal("--update must set the update flag")
	}
	flags, err = parseOnboardFlags([]string{})
	if err != nil || flags.update {
		t.Fatalf("update must default false (err=%v flags=%+v)", err, flags)
	}
}
