//go:build integration

package calm_poc_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/devcerts"
)

func TestDockerComposeFirstGenerationReachesHealthyFromIsolatedRoot(t *testing.T) {
	if output, err := exec.Command("docker", "info").CombinedOutput(); err != nil {
		t.Skipf("Docker unavailable: %v\n%s", err, output)
	}
	trackedBefore := trackedCertDigests(t)
	certRoot := filepath.Join(t.TempDir(), "certs")
	if err := devcerts.Publish(certRoot, false); err != nil {
		t.Fatalf("Publish(isolated Compose root): %v", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(filepath.Dir(certRoot))
	if err != nil {
		t.Fatalf("resolve temporary parent: %v", err)
	}
	certRoot = filepath.Join(resolvedRoot, filepath.Base(certRoot))
	override := filepath.Join(t.TempDir(), "compose.override.yml")
	overrideContent := "services:\n  agent-fitness-functions:\n    volumes:\n      - " + strconv.Quote(certRoot+":/app/certs:ro") + "\n    ports:\n      - \"127.0.0.1::7890\"\n"
	if err := os.WriteFile(override, []byte(overrideContent), 0o600); err != nil {
		t.Fatalf("write Compose override: %v", err)
	}
	project := fmt.Sprintf("q8d11-smoke-%d-%d", os.Getpid(), time.Now().UnixNano())
	composeArgs := []string{"compose", "-p", project, "-f", "docker-compose.yml", "-f", override}
	t.Cleanup(func() {
		command := exec.Command("docker", append(composeArgs, "down", "--volumes", "--remove-orphans")...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("docker compose down: %v\n%s", err, output)
		}
	})
	config := exec.Command("docker", append(composeArgs, "config")...)
	configOutput, err := config.CombinedOutput()
	if err != nil {
		t.Fatalf("docker compose config: %v\n%s", err, configOutput)
	}
	if !bytes.Contains(configOutput, []byte(certRoot)) {
		t.Fatalf("Compose config does not mount isolated certificate root %q:\n%s", certRoot, configOutput)
	}
	up := exec.Command("docker", append(composeArgs, "up", "--build", "--detach", "--wait")...)
	if output, err := up.CombinedOutput(); err != nil {
		t.Fatalf("docker compose first-generation smoke: %v\n%s", err, output)
	}
	versionA := runtimeVersion(t, composeArgs)
	assertRuntimeHealthAndPublicFiles(t, composeArgs)
	if err := os.RemoveAll(filepath.Join(certRoot, filepath.FromSlash(versionA))); err != nil {
		t.Fatalf("remove generation A source: %v", err)
	}
	assertRuntimeHealthAndPublicFiles(t, composeArgs)

	secondRoot := filepath.Join(t.TempDir(), "certs")
	if err := devcerts.Publish(secondRoot, false); err != nil {
		t.Fatalf("Publish(second root): %v", err)
	}
	versionB := importManagedVersion(t, secondRoot, certRoot)
	if versionB == versionA {
		t.Fatalf("rotation version B = version A = %q", versionA)
	}
	restart := exec.Command("docker", append(composeArgs, "restart", "agent-fitness-functions")...)
	if output, err := restart.CombinedOutput(); err != nil {
		t.Fatalf("docker compose restart: %v\n%s", err, output)
	}
	wait := exec.Command("docker", append(composeArgs, "up", "--detach", "--wait")...)
	if output, err := wait.CombinedOutput(); err != nil {
		t.Fatalf("docker compose wait after restart: %v\n%s", err, output)
	}
	if got := runtimeVersion(t, composeArgs); got != versionB {
		t.Fatalf("runtime version after restart = %q, want %q", got, versionB)
	}
	assertRuntimeHealthAndPublicFiles(t, composeArgs)
	if err := os.RemoveAll(filepath.Join(certRoot, filepath.FromSlash(versionB))); err != nil {
		t.Fatalf("remove generation B source: %v", err)
	}
	assertRuntimeHealthAndPublicFiles(t, composeArgs)
	if after := trackedCertDigests(t); after != trackedBefore {
		t.Fatalf("Compose smoke modified tracked certificate fixtures\nbefore=%v\nafter=%v", trackedBefore, after)
	}
}

func TestDockerImageHealthSupportsPlainHTTPAndExplicitTLS(t *testing.T) {
	if output, err := exec.Command("docker", "info").CombinedOutput(); err != nil {
		t.Skipf("Docker unavailable: %v\n%s", err, output)
	}
	tag := fmt.Sprintf("agent-fitness-functions:q8d11-health-%d", os.Getpid())
	build := exec.Command("docker", "build", "--build-arg", "TARGETOS=linux", "--build-arg", "TARGETARCH="+runtime.GOARCH, "--tag", tag, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("docker build health-mode image: %v\n%s", err, output)
	}
	t.Cleanup(func() {
		command := exec.Command("docker", "image", "rm", "--force", tag)
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("docker image cleanup: %v\n%s", err, output)
		}
	})

	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("Abs(.): %v", err)
	}
	runHealthContainer(t, tag, "plain", []string{
		"--env", "AGENT_FITNESS_FUNCTIONS_CLIENT_CA=/unrelated/client-ca.crt",
	}, root)

	certRoot := filepath.Join(t.TempDir(), "certs")
	if err := devcerts.Publish(certRoot, false); err != nil {
		t.Fatalf("Publish(explicit health root): %v", err)
	}
	version, err := devcerts.ResolveManagedVersion(certRoot)
	if err != nil {
		t.Fatalf("ResolveManagedVersion(explicit health root): %v", err)
	}
	paths := version.Paths()
	runHealthContainer(t, tag, "explicit", []string{
		"--mount", "type=bind,src=" + filepath.Dir(paths.CA) + ",dst=/external,readonly",
		"--env", "AGENT_FITNESS_FUNCTIONS_TLS_CERT=/external/server.crt",
		"--env", "AGENT_FITNESS_FUNCTIONS_TLS_KEY=/external/server.key",
		"--env", "AGENT_FITNESS_FUNCTIONS_TLS_CA=/external/ca.crt",
	}, root)
}

func runHealthContainer(t *testing.T, image, mode string, extraArgs []string, root string) {
	t.Helper()
	name := fmt.Sprintf("q8d11-health-%s-%d-%d", mode, os.Getpid(), time.Now().UnixNano())
	args := []string{
		"run", "--detach", "--name", name,
		"--read-only", "--tmpfs", "/tmp:size=256m",
		"--health-interval", "1s", "--health-timeout", "2s", "--health-start-period", "1s", "--health-retries", "10",
		"--mount", "type=bind,src=" + filepath.Join(root, "configs") + ",dst=/app/configs,readonly",
		"--mount", "type=bind,src=" + filepath.Join(root, "caller-repos.json") + ",dst=/app/caller-repos.json,readonly",
		"--env", "AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR=/app/configs",
	}
	args = append(args, extraArgs...)
	args = append(args, image, "--addr", "0.0.0.0:7890")
	run := exec.Command("docker", args...)
	if output, err := run.CombinedOutput(); err != nil {
		t.Fatalf("docker run %s health mode: %v\n%s", mode, err, output)
	}
	t.Cleanup(func() {
		command := exec.Command("docker", "rm", "--force", name)
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("docker container cleanup %s: %v\n%s", mode, err, output)
		}
	})
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		inspect := exec.Command("docker", "inspect", "--format", "{{.State.Status}} {{.State.Health.Status}}", name)
		output, err := inspect.CombinedOutput()
		if err == nil && string(output) == "running healthy\n" {
			return
		}
		if err == nil && strings.HasPrefix(string(output), "exited ") {
			logs, _ := exec.Command("docker", "logs", name).CombinedOutput()
			t.Fatalf("%s health container exited: %s\n%s", mode, output, logs)
		}
		time.Sleep(250 * time.Millisecond)
	}
	inspect := exec.Command("docker", "inspect", "--format", "{{json .State}}", name)
	output, _ := inspect.CombinedOutput()
	logs, _ := exec.Command("docker", "logs", name).CombinedOutput()
	t.Fatalf("%s health container did not become healthy:\n%s\n%s", mode, output, logs)
}

func importManagedVersion(t *testing.T, sourceRoot, destinationRoot string) string {
	t.Helper()
	version, err := devcerts.ResolveManagedVersion(sourceRoot)
	if err != nil {
		t.Fatalf("ResolveManagedVersion(second root): %v", err)
	}
	destination := filepath.Join(destinationRoot, filepath.FromSlash(version.RelativePath()))
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatalf("Mkdir(imported version): %v", err)
	}
	paths := version.Paths()
	for name, source := range map[string]string{
		"ca.crt":     paths.CA,
		"client.crt": paths.ClientCertificate,
		"client.key": paths.ClientKey,
		"server.crt": paths.ServerCertificate,
		"server.key": paths.ServerKey,
	} {
		content, err := os.ReadFile(source)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", name, err)
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, ".key") {
			mode = 0o600
		}
		if err := os.WriteFile(filepath.Join(destination, name), content, mode); err != nil {
			t.Fatalf("WriteFile(imported %s): %v", name, err)
		}
	}
	temporaryCurrent := filepath.Join(destinationRoot, ".integration-current")
	if err := os.Symlink(version.RelativePath(), temporaryCurrent); err != nil {
		t.Fatalf("Symlink(rotation current): %v", err)
	}
	if err := os.Rename(temporaryCurrent, filepath.Join(destinationRoot, "current")); err != nil {
		t.Fatalf("Rename(rotation current): %v", err)
	}
	return version.RelativePath()
}

func runtimeVersion(t *testing.T, composeArgs []string) string {
	t.Helper()
	output := composeExec(t, composeArgs, "cat /run/agent-fitness-functions/pinned-dev-cert-version")
	if strings.Count(output, "\n") != 1 || !strings.HasSuffix(output, "\n") {
		t.Fatalf("runtime version bytes = %q, want one version line", output)
	}
	return strings.TrimSuffix(output, "\n")
}

func assertRuntimeHealthAndPublicFiles(t *testing.T, composeArgs []string) {
	t.Helper()
	entries := composeExec(t, composeArgs, "ls -1 /run/agent-fitness-functions")
	if entries != "health-ca.crt\npinned-dev-cert-version\n" {
		t.Fatalf("runtime entries = %q, want exactly two public artifacts", entries)
	}
	for _, name := range []string{"health-ca.crt", "pinned-dev-cert-version"} {
		if mode := composeExec(t, composeArgs, "stat -c %a /run/agent-fitness-functions/"+name); mode != "644\n" {
			t.Fatalf("runtime %s mode = %q, want 644", name, mode)
		}
	}
	content := composeExec(t, composeArgs, "cat /run/agent-fitness-functions/health-ca.crt /run/agent-fitness-functions/pinned-dev-cert-version")
	if strings.Contains(content, "PRIVATE KEY") || strings.Contains(content, "owner") || strings.Contains(content, "token") {
		t.Fatal("runtime artifacts contain private key or ownership evidence")
	}
	output := composeExec(t, composeArgs, "curl --fail --silent --cacert /run/agent-fitness-functions/health-ca.crt https://127.0.0.1:7890/health")
	if output != "" {
		t.Fatalf("runtime health command output = %q, want empty", output)
	}
}

func composeExec(t *testing.T, composeArgs []string, script string) string {
	t.Helper()
	command := exec.Command("docker", append(composeArgs, "exec", "-T", "agent-fitness-functions", "sh", "-c", script)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("docker compose exec: %v\n%s", err, output)
	}
	return string(output)
}

func trackedCertDigests(t *testing.T) [5][32]byte {
	t.Helper()
	var result [5][32]byte
	for i, name := range []string{"ca.crt", "server.crt", "server.key", "client.crt", "client.key"} {
		content, err := os.ReadFile(filepath.Join("certs", name))
		if err != nil {
			t.Fatalf("read tracked cert fixture %s: %v", name, err)
		}
		result[i] = sha256.Sum256(content)
	}
	return result
}
