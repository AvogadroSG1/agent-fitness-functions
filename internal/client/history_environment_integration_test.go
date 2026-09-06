//go:build integration && (darwin || linux)

package client

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistoryCrossProcessIsolatesCallerGitState(t *testing.T) {
	// Construct a harmless caller repository independently of the environment
	// sanitizer under test. No inherited selector may reach even its setup.
	caller := t.TempDir()
	var safeEnvironment []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			safeEnvironment = append(safeEnvironment, entry)
		}
	}
	safeEnvironment = append(safeEnvironment, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", caller}, args...)...)
		cmd.Env = safeEnvironment
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("initialize owned caller: %v\n%s", err, out)
		}
	}
	git("init", "-b", "main")
	git("config", "user.name", "Caller Fixture")
	git("config", "user.email", "caller@example.invalid")
	if err := os.WriteFile(filepath.Join(caller, "example.go"), []byte("package caller\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "example.go")
	git("-c", "core.hooksPath=/dev/null", "commit", "-m", "owned caller")
	snapshot := make(map[string][]byte)
	for _, name := range []string{".git/config", ".git/index", ".git/HEAD", ".git/refs/heads/main", "example.go"} {
		contents, err := os.ReadFile(filepath.Join(caller, name))
		if err != nil {
			t.Fatal(err)
		}
		snapshot[name] = contents
	}
	for key, value := range map[string]string{
		"GIT_DIR": filepath.Join(caller, ".git"), "GIT_WORK_TREE": caller,
		"GIT_COMMON_DIR": filepath.Join(caller, ".git"), "GIT_INDEX_FILE": filepath.Join(caller, ".git/index"),
		"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "user.name", "GIT_CONFIG_VALUE_0": "Injected Caller",
	} {
		t.Setenv(key, value)
	}
	t.Run("fixture writes", func(t *testing.T) {
		root, binary := buildHistoryProcessBinary(t)
		f := newHistoryProcessFixture(t, root, binary)
		f.start()
		server, requests := historyFixtureServer(t, `{"status":"pass","violations":[]}`)
		out, stderr, code := f.validate(f.repo, server.URL, "package isolated\n")
		if code != 0 || stderr != "" || !strings.Contains(out, `"status":"pass"`) {
			t.Fatalf("isolated validation: exit=%d stdout=%q stderr=%q", code, out, stderr)
		}
		f.request(requests)
		f.eventually(func() bool { return len(f.page(f.repo).Records) == 1 })
	})
	for name, before := range snapshot {
		after, err := os.ReadFile(filepath.Join(caller, name))
		if err != nil || !bytes.Equal(after, before) {
			t.Errorf("fixture modified owned caller %s: error=%v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(caller, ".git/agent-fitness-functions")); !os.IsNotExist(err) {
		t.Errorf("fixture history reached caller: %v", err)
	}
}

func TestHistoryRequestObservationHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := receiveHistoryRequest(ctx, make(chan []byte)); !errors.Is(err, context.Canceled) {
		t.Fatalf("missing HTTP request ignored cancellation: %v", err)
	}
}
