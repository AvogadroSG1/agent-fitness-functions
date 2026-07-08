package client

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveOnboardRepoName(t *testing.T) {
	tests := []struct {
		name     string
		repoFlag string
		repoRoot string
		want     string
		wantErr  string
	}{
		{name: "flag wins", repoFlag: "graft", repoRoot: "/home/user/Weird_Repo", want: "graft"},
		{name: "basename fallback", repoFlag: "", repoRoot: "/home/user/calm-poc", want: "calm-poc"},
		{name: "invalid basename asks for --repo", repoFlag: "", repoRoot: "/home/user/Weird_Repo", wantErr: "pass --repo"},
		{name: "invalid flag rejected", repoFlag: "Bad Name", repoRoot: "/home/user/calm-poc", wantErr: "invalid repository name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveOnboardRepoName(tt.repoFlag, tt.repoRoot)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want to contain %q", err, tt.wantErr)
				}
				if !IsUsageError(err) {
					t.Fatalf("err = %v, want usage error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolveOnboardRepoName(%q, %q) = %q, want %q", tt.repoFlag, tt.repoRoot, got, tt.want)
			}
		})
	}
}

func TestValidateEnforcement(t *testing.T) {
	for _, mode := range []string{"advisory", "block"} {
		if got, err := validateEnforcement(mode); err != nil || got != mode {
			t.Fatalf("validateEnforcement(%q) = %q, %v; want %q, nil", mode, got, err, mode)
		}
	}
	if _, err := validateEnforcement("off"); err == nil || !IsUsageError(err) {
		t.Fatalf("validateEnforcement(off) err = %v, want usage error", err)
	}
}

func TestRenderScaffoldConfigFillsAllFiveFunctions(t *testing.T) {
	for _, enforcement := range []string{"advisory", "block"} {
		content, err := renderScaffoldConfig(enforcement)
		if err != nil {
			t.Fatalf("renderScaffoldConfig(%q): %v", enforcement, err)
		}
		var doc scaffoldConfigDocument
		if err := json.Unmarshal(content, &doc); err != nil {
			t.Fatalf("unmarshal scaffolded config: %v\n%s", err, content)
		}
		if doc.EnforcementMode != enforcement {
			t.Fatalf("enforcement-mode = %q, want %q", doc.EnforcementMode, enforcement)
		}
		if doc.EnforcementOnError != "block" {
			t.Fatalf("enforcement-on-error = %q, want block", doc.EnforcementOnError)
		}
		if len(doc.FitnessFunctions) != len(fitnessFunctionKeys) {
			t.Fatalf("fitness-functions = %v, want %d keys", doc.FitnessFunctions, len(fitnessFunctionKeys))
		}
		for _, key := range fitnessFunctionKeys {
			if !doc.FitnessFunctions[key] {
				t.Fatalf("fitness function %q not enabled in %s scaffold", key, enforcement)
			}
		}
		if !bytes.HasSuffix(content, []byte("\n")) {
			t.Fatalf("scaffolded config must end with a newline")
		}
	}
}

func TestScaffoldConfigFreshAndAlreadyExists(t *testing.T) {
	configsDir := t.TempDir()
	var stdout bytes.Buffer
	o := onboarder{repoName: "sample", enforcement: "advisory", configsDir: configsDir, stdout: &stdout, stderr: &bytes.Buffer{}}

	if err := o.scaffoldConfig(); err != nil {
		t.Fatalf("fresh scaffoldConfig: %v", err)
	}
	configPath := filepath.Join(configsDir, "sample", "config.json")
	first, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read scaffolded config: %v", err)
	}
	if !strings.Contains(stdout.String(), "scaffolded advisory config") {
		t.Fatalf("stdout = %q, want scaffolded message", stdout.String())
	}

	stdout.Reset()
	if err := o.scaffoldConfig(); err != nil {
		t.Fatalf("second scaffoldConfig: %v", err)
	}
	if !strings.Contains(stdout.String(), "already present") {
		t.Fatalf("stdout = %q, want already-present message", stdout.String())
	}
	second, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("re-read config: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("idempotent scaffold rewrote config:\nfirst=%s\nsecond=%s", first, second)
	}
}

func TestEnsureCallerBindingFreshFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "caller-repos.json")
	changed, err := ensureCallerBinding(path, "dev-hook-pool", "sample")
	if err != nil {
		t.Fatalf("ensureCallerBinding: %v", err)
	}
	if !changed {
		t.Fatalf("changed = false, want true for a fresh file")
	}
	repos := readCallerRepos(t, path, "dev-hook-pool")
	if len(repos) != 1 || repos[0] != "sample" {
		t.Fatalf("callers[dev-hook-pool] = %v, want [sample]", repos)
	}
}

func TestEnsureCallerBindingAppendsPreservingExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "caller-repos.json")
	seed := `{
  "callers": {
    "ci-runner-graft": ["graft"],
    "dev-hook-pool": ["calm-poc", "graft"]
  },
  "admins": ["dev-hook-pool"]
}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed caller-repos.json: %v", err)
	}

	changed, err := ensureCallerBinding(path, "dev-hook-pool", "sample")
	if err != nil {
		t.Fatalf("ensureCallerBinding: %v", err)
	}
	if !changed {
		t.Fatalf("changed = false, want true when appending a new repo")
	}

	document := readCallerDocument(t, path)
	dev := stringList(t, document, "dev-hook-pool")
	for _, want := range []string{"calm-poc", "graft", "sample"} {
		if !containsString(dev, want) {
			t.Fatalf("dev-hook-pool = %v, want to contain %q", dev, want)
		}
	}
	other := stringListFromCallers(t, document, "ci-runner-graft")
	if len(other) != 1 || other[0] != "graft" {
		t.Fatalf("ci-runner-graft = %v, want [graft] preserved", other)
	}
	admins, ok := document["admins"].([]any)
	if !ok || len(admins) != 1 || admins[0] != "dev-hook-pool" {
		t.Fatalf("admins = %v, want [dev-hook-pool] preserved", document["admins"])
	}
}

func TestEnsureCallerBindingIdempotentWhenPresent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "caller-repos.json")
	if _, err := ensureCallerBinding(path, "dev-hook-pool", "sample"); err != nil {
		t.Fatalf("first ensureCallerBinding: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after first: %v", err)
	}
	changed, err := ensureCallerBinding(path, "dev-hook-pool", "sample")
	if err != nil {
		t.Fatalf("second ensureCallerBinding: %v", err)
	}
	if changed {
		t.Fatalf("changed = true, want false when repo already authorized")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after second: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("idempotent binding rewrote the file:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestOnboardCallerBindingsPath(t *testing.T) {
	tests := []struct {
		name       string
		configsDir string
		want       string
	}{
		{name: "sibling of configs dir", configsDir: "/srv/app/configs", want: filepath.FromSlash("/srv/app/caller-repos.json")},
		{name: "inside non-configs dir", configsDir: "/srv/custom", want: filepath.FromSlash("/srv/custom/caller-repos.json")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := onboardCallerBindingsPath(tt.configsDir); got != tt.want {
				t.Fatalf("onboardCallerBindingsPath(%q) = %q, want %q", tt.configsDir, got, tt.want)
			}
		})
	}
}

func TestEmbeddedConfigTemplatesMatchAuthoritative(t *testing.T) {
	configsDir := filepath.Join(projectRoot(t), "configs")
	for _, name := range []string{"advisory-template.json", "block-template.json"} {
		authoritative, err := os.ReadFile(filepath.Join(configsDir, name))
		if err != nil {
			t.Fatalf("read configs/%s: %v", name, err)
		}
		embedded, err := embeddedConfigTemplates.ReadFile("configtemplates/" + name)
		if err != nil {
			t.Fatalf("read embedded configtemplates/%s: %v", name, err)
		}
		if !bytes.Equal(authoritative, embedded) {
			t.Fatalf("configtemplates/%s has drifted from configs/%s; re-copy configs/%s into internal/client/configtemplates/", name, name, name)
		}
	}
}

func readCallerDocument(t *testing.T, path string) map[string]any {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var document map[string]any
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return document
}

func readCallerRepos(t *testing.T, path, callerCN string) []string {
	t.Helper()
	return stringList(t, readCallerDocument(t, path), callerCN)
}

func stringList(t *testing.T, document map[string]any, callerCN string) []string {
	t.Helper()
	return stringListFromCallers(t, document, callerCN)
}

func stringListFromCallers(t *testing.T, document map[string]any, callerCN string) []string {
	t.Helper()
	callers, ok := document["callers"].(map[string]any)
	if !ok {
		t.Fatalf("document has no callers object: %v", document)
	}
	raw, ok := callers[callerCN].([]any)
	if !ok {
		return nil
	}
	repos := make([]string, 0, len(raw))
	for _, item := range raw {
		repos = append(repos, item.(string))
	}
	return repos
}
