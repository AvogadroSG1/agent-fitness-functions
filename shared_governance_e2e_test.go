package calm_poc_test

// Acceptance test for calm-poc-mx5 (ADR-0007) and the calm-poc-wgi autostart
// scenario, end to end with the REAL binary and a REAL daemon:
//   1. onboard repo A → daemon starts on the shared port serving the machine
//      governance root
//   2. onboard repo B while A's daemon is running → succeeds with NO
//      port_conflict, same daemon (hot-reloaded config registration)
//   3. both repos validate against the one daemon
//   4. kill the daemon; a hook-context commit in repo A auto-restarts it with
//      its managed root (the calm-poc-wgi failure mode), and repo B then
//      validates against the restarted daemon.
//
// Requires the FINOS `calm` CLI on PATH (like the internal/server tests); the
// test skips when it is missing.

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// buildProductBinary compiles the real binary once into dir and returns its path.
func buildProductBinary(t *testing.T, dir string) string {
	t.Helper()
	binary := filepath.Join(dir, "agent-fitness-functions")
	build := exec.Command("go", "build", "-o", binary, "./cmd/agent-fitness-functions")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}
	return binary
}

// freeLoopbackPort reserves and releases an ephemeral port for the test daemon,
// so the e2e never fights the developer's own governance daemon on 7890.
func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func killDaemonOnPort(t *testing.T, port int) {
	t.Helper()
	out, err := exec.Command("lsof", "-ti", "tcp:"+strconv.Itoa(port)).Output()
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		return
	}
	for _, pid := range strings.Fields(string(out)) {
		_ = exec.Command("kill", pid).Run()
	}
	waitForPortState(t, port, false, 5*time.Second)
}

func daemonPIDOnPort(t *testing.T, port int) string {
	t.Helper()
	out, _ := exec.Command("lsof", "-ti", "tcp:"+strconv.Itoa(port)).Output()
	return strings.TrimSpace(string(out))
}

func waitForPortState(t *testing.T, port int, listening bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			if listening {
				return
			}
		} else if !listening {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("port %d did not reach listening=%v in %v", port, listening, timeout)
}

// e2eGitRepo creates a governed-repo candidate with one committed Go file.
// The directory is NAMED name because the working-tree basename is the
// governance repo key both onboard and the hooks derive by default.
func e2eGitRepo(t *testing.T, env []string, name string) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	} {
		runE2EGit(t, repo, env, args...)
	}
	if err := os.WriteFile(filepath.Join(repo, "sample.go"), []byte("package sample\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runE2EGit(t, repo, env, "add", "sample.go")
	runE2EGit(t, repo, env, "commit", "--no-verify", "-m", "seed")
	return repo
}

func runE2EGit(t *testing.T, repo string, env []string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	command.Env = env
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func runProduct(t *testing.T, binary, dir string, env []string, args ...string) string {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Dir = dir
	command.Env = env
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", filepath.Base(binary), strings.Join(args, " "), err, output)
	}
	return string(output)
}

func TestSharedGovernanceTwoReposOneDaemonSurvivesDaemonDeath(t *testing.T) {
	if _, err := exec.LookPath("calm"); err != nil {
		t.Skip("FINOS calm CLI not on PATH; e2e daemon validation needs it")
	}
	stateHome := t.TempDir()
	binDir := t.TempDir()
	binary := buildProductBinary(t, binDir)
	port := freeLoopbackPort(t)
	addr := fmt.Sprintf("https://127.0.0.1:%d", port)
	t.Cleanup(func() { killDaemonOnPort(t, port) })

	env := append(os.Environ(),
		"XDG_STATE_HOME="+stateHome,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AGENT_FITNESS_FUNCTIONS_ADDR="+addr,
		"AGENT_FITNESS_FUNCTIONS_BIN="+binary,
	)
	// The governance selectors must not leak in from the developer machine.
	for _, name := range []string{"AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR", "AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR",
		"AGENT_FITNESS_FUNCTIONS_CLIENT_CERT", "AGENT_FITNESS_FUNCTIONS_CLIENT_KEY", "AGENT_FITNESS_FUNCTIONS_CLIENT_CA"} {
		env = append(env, name+"=")
	}

	repoA := e2eGitRepo(t, env, "repo-a")
	repoB := e2eGitRepo(t, env, "repo-b")

	// 1. Onboard repo A: auto-starts the shared daemon.
	runProduct(t, binary, repoA, env, "client", "onboard", "--addr", addr, repoA)
	waitForPortState(t, port, true, 10*time.Second)
	firstPID := daemonPIDOnPort(t, port)
	if firstPID == "" {
		t.Fatal("no daemon listening after onboarding repo A")
	}

	// 2. Onboard repo B while A's daemon runs: no port_conflict, same daemon.
	output := runProduct(t, binary, repoB, env, "client", "onboard", "--addr", addr, repoB)
	if strings.Contains(output, "does not trust") {
		t.Fatalf("onboarding repo B hit the port conflict the shared root removes:\n%s", output)
	}
	if pid := daemonPIDOnPort(t, port); pid != firstPID {
		t.Fatalf("daemon pid changed %s -> %s; repo B must hot-register, not restart", firstPID, pid)
	}

	// 3. Both repos validate against the one daemon. --repo carries the repo
	// PATH exactly as the installed hooks pass it; the server keys governance
	// on its basename.
	for repoName, repo := range map[string]string{"repo-a": repoA, "repo-b": repoB} {
		out := runProduct(t, binary, repo, env, "client", "validate", "--file", "sample.go", "--language", "go", "--repo", repo, "--addr", addr)
		if !strings.Contains(out, `"status"`) {
			t.Fatalf("validate in %s = %q, want a verdict", repoName, out)
		}
	}

	// 4. Kill the daemon; a hook-context commit in repo A must restart it
	// (calm-poc-wgi), and repo B then validates against the restarted daemon.
	killDaemonOnPort(t, port)
	if err := os.WriteFile(filepath.Join(repoA, "sample.go"), []byte("package sample\n\nfunc Run() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runE2EGit(t, repoA, env, "add", "sample.go")
	runE2EGit(t, repoA, env, "commit", "-m", "governed change") // hooks active: pre-commit validates and auto-starts
	waitForPortState(t, port, true, 10*time.Second)
	out := runProduct(t, binary, repoB, env, "client", "validate", "--file", "sample.go", "--language", "go", "--repo", repoB, "--addr", addr)
	if !strings.Contains(out, `"status"`) {
		t.Fatalf("validate in repo-b after restart = %q, want a verdict", out)
	}
}
