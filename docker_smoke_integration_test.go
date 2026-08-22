//go:build integration

package calm_poc_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	overrideContent := "services:\n  stack-fitness-functions:\n    volumes:\n      - " + strconv.Quote(certRoot+":/app/certs:ro") + "\n    ports:\n      - \"127.0.0.1::7890\"\n"
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
	if after := trackedCertDigests(t); after != trackedBefore {
		t.Fatalf("Compose smoke modified tracked certificate fixtures\nbefore=%v\nafter=%v", trackedBefore, after)
	}
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
