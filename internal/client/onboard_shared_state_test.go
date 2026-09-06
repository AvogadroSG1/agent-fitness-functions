package client

// Red contract for calm-poc-mx5.3 (ADR-0007): onboard registers each repo with
// the ONE machine governance root — shared configs dir + sibling
// caller-repos.json — while the tracked repo-local configs/<repo>/config.json
// remains the production handoff source of truth, copied (never symlinked)
// into the shared dir on every onboard run.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// onboardTestRepo creates a git-initialized repo root for onboarding.
func onboardTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cmd := exec.Command("git", "init", "--quiet", root)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	return root
}

// resolveOnboarderForTest resolves the onboarder for repoName at repoRoot
// against a daemon that answers every request 200 OK, without running any
// step. Callers drive the steps they care about. The addr is pinned to the
// legacy managed-TLS loopback daemon: these scenarios are about the dev-cert
// and caller-binding artifacts of the mTLS local path, which the ADR-0010
// local-http default (now `http://`) deliberately no longer produces.
func resolveOnboarderForTest(t *testing.T, repoName, repoRoot string) onboarder {
	t.Helper()
	okClient := &http.Client{Transport: clientRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})}
	o, err := resolveOnboarder([]string{"--repo", repoName, "--addr", "https://127.0.0.1:7890", repoRoot}, &bytes.Buffer{}, &bytes.Buffer{}, okClient, func(DaemonStartConfig) error { return nil })
	if err != nil {
		t.Fatalf("resolveOnboarder(%s): %v", repoName, err)
	}
	return o
}

// runManagedOnboardCore resolves the onboarder for repoName at repoRoot and
// runs the managed local mutation steps (certs, config scaffold+sync, caller
// authorization) without the daemon/doctor tail, mirroring run()'s order.
func runManagedOnboardCore(t *testing.T, repoName, repoRoot string) *onboarder {
	t.Helper()
	o := resolveOnboarderForTest(t, repoName, repoRoot)
	for name, step := range map[string]func() error{"ensureCerts": o.ensureCerts, "scaffoldConfig": o.scaffoldConfig, "authorizeCaller": o.authorizeCaller} {
		if err := step(); err != nil {
			t.Fatalf("%s(%s): %v (onboard owns creating the machine governance root on a fresh machine)", name, repoName, err)
		}
	}
	return &o
}

func sharedCallerRepos(t *testing.T, govRoot, callerCN string) []string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(govRoot, "caller-repos.json"))
	if err != nil {
		t.Fatalf("read shared caller-repos.json: %v", err)
	}
	var document struct {
		Callers map[string][]string `json:"callers"`
	}
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatalf("parse shared caller-repos.json: %v", err)
	}
	return document.Callers[callerCN]
}

func TestOnboardTwoReposShareOneGovernanceRoot(t *testing.T) {
	govRoot := governanceStateHome(t)

	repoA := onboardTestRepo(t)
	repoB := onboardTestRepo(t)
	runManagedOnboardCore(t, "repo-a", repoA)
	runManagedOnboardCore(t, "repo-b", repoB)

	for _, tt := range []struct{ name, root string }{{"repo-a", repoA}, {"repo-b", repoB}} {
		shared := filepath.Join(govRoot, "configs", tt.name, "config.json")
		if _, err := os.Stat(shared); err != nil {
			t.Errorf("shared config for %s: %v, want registered under the machine governance root", tt.name, err)
		}
		local := filepath.Join(tt.root, "configs", tt.name, "config.json")
		if _, err := os.Stat(local); err != nil {
			t.Errorf("tracked repo-local config for %s: %v, want production handoff artifact scaffolded", tt.name, err)
		}
		if _, err := os.Lstat(filepath.Join(tt.root, "caller-repos.json")); err == nil {
			t.Errorf("repo-local caller-repos.json written for %s; caller authorization must live only in the governance root", tt.name)
		}
	}

	repos := sharedCallerRepos(t, govRoot, devClientCommonName)
	if !containsString(repos, "repo-a") || !containsString(repos, "repo-b") {
		t.Fatalf("shared callers[%s] = %v, want both repo-a and repo-b accumulated", devClientCommonName, repos)
	}
}

func TestOnboardResyncsRepoLocalConfigIntoSharedCopy(t *testing.T) {
	govRoot := governanceStateHome(t)
	repoRoot := onboardTestRepo(t)

	// A user-calibrated repo-local config is the source of truth: onboard must
	// leave it untouched and copy it verbatim into the shared dir.
	custom := []byte("{\n  \"enforcement-mode\": \"block\",\n  \"fitness-functions\": {\n    \"cyclomatic-complexity\": true\n  }\n}\n")
	localPath := filepath.Join(repoRoot, "configs", "repo-c", "config.json")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localPath, custom, 0o644); err != nil {
		t.Fatal(err)
	}

	runManagedOnboardCore(t, "repo-c", repoRoot)

	localAfter, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(localAfter, custom) {
		t.Fatalf("repo-local config rewritten:\n%s", localAfter)
	}
	shared, err := os.ReadFile(filepath.Join(govRoot, "configs", "repo-c", "config.json"))
	if err != nil {
		t.Fatalf("shared copy: %v, want verbatim sync of the repo-local config", err)
	}
	if !bytes.Equal(shared, custom) {
		t.Fatalf("shared copy = %s, want byte-identical repo-local content", shared)
	}
	info, err := os.Lstat(filepath.Join(govRoot, "configs", "repo-c", "config.json"))
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("shared copy is a symlink; ADR-0007 requires a copy so fsnotify hot-reload keeps working")
	}
}

func TestOnboardManualRemainderNamesRepoLocalConfigAsProductionArtifact(t *testing.T) {
	governanceStateHome(t)
	repoRoot := onboardTestRepo(t)
	o := runManagedOnboardCore(t, "repo-d", repoRoot)

	var stdout bytes.Buffer
	o.stdout = &stdout
	o.printManualRemainder()

	localPath := filepath.Join(repoRoot, "configs", "repo-d", "config.json")
	if !strings.Contains(stdout.String(), localPath) {
		t.Fatalf("manual remainder = %q, want the tracked repo-local config %q named as the production handoff artifact", stdout.String(), localPath)
	}
}

func TestDaemonConflictErrorRemediationNamesOnboardForLegacyDaemons(t *testing.T) {
	err := daemonConflictError{addr: "https://127.0.0.1:7890", cause: io.EOF}
	message := err.Error()
	if !strings.Contains(message, "client onboard") {
		t.Fatalf("conflict message = %q, want remediation naming `client onboard` (a legacy pre-shared-root daemon is the expected cause once repos share one governance root)", message)
	}
}

// The governance root's caller-repos.json is shared by every repository on
// the machine, so two `client onboard` runs may mutate it concurrently
// (ADR-0007 promises concurrent onboarding works). ensureCallerBinding must
// serialize its read-modify-write cycle: without a lock, a lost update
// silently drops another repo's just-added authorization.
func TestEnsureCallerBindingSerializesConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "caller-repos.json")
	const writers = 32
	start := make(chan struct{})
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		go func(n int) {
			<-start
			_, err := ensureCallerBinding(path, devClientCommonName, fmt.Sprintf("repo-%02d", n))
			errs <- err
		}(i)
	}
	close(start)
	for i := 0; i < writers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("ensureCallerBinding: %v", err)
		}
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Callers map[string][]string `json:"callers"`
	}
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatalf("parse %s: %v\n%s", path, err, content)
	}
	repos := document.Callers[devClientCommonName]
	for i := 0; i < writers; i++ {
		want := fmt.Sprintf("repo-%02d", i)
		if !containsString(repos, want) {
			t.Fatalf("lost update: %s missing from %v (%d of %d survived)", want, repos, len(repos), writers)
		}
	}
}
