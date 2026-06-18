package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRevParse(t *testing.T, repo, rev string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "rev-parse", rev).CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse %s: %v\n%s", rev, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestPrePushForwardsDiscoveredMTLSCerts(t *testing.T) {
	repo := initGitRepo(t)
	// git rev-parse --show-toplevel resolves symlinks (macOS /var -> /private/var),
	// so resolve here too to match the cert paths the hook forwards.
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		repo = resolved
	}
	runGit(t, repo, "config", "user.email", "t@example.com")
	runGit(t, repo, "config", "user.name", "t")

	writeFile(t, filepath.Join(repo, "README.md"), "init\n")
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")
	baseSHA := gitRevParse(t, repo, "HEAD")

	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	runGit(t, repo, "add", "sample.go")
	runGit(t, repo, "commit", "-m", "add sample")
	headSHA := gitRevParse(t, repo, "HEAD")

	certDir := filepath.Join(repo, "certs")
	for _, name := range []string{"client.crt", "client.key", "ca.crt"} {
		writeFile(t, filepath.Join(certDir, name), "x")
	}

	logPath := filepath.Join(t.TempDir(), "calls.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STACK_FITNESS_FUNCTIONS_LOG"
echo '{"status":"pass"}'
`)

	command := exec.Command("bash", hookScriptPathFor(t, "pre-push.sh"))
	command.Dir = repo
	command.Stdin = strings.NewReader("refs/heads/main " + headSHA + " refs/heads/main " + baseSHA + "\n")
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STACK_FITNESS_FUNCTIONS_LOG="+logPath,
	)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pre-push failed: %v\n%s", err, out)
	}

	got := readFile(t, logPath)
	for _, want := range []string{
		"--addr https://127.0.0.1:7890",
		"--client-cert " + filepath.Join(certDir, "client.crt"),
		"--client-key " + filepath.Join(certDir, "client.key"),
		"--client-ca " + filepath.Join(certDir, "ca.crt"),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in invocations:\n%s", want, got)
		}
	}
}
