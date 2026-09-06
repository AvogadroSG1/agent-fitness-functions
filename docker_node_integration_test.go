//go:build integration

package calm_poc_test

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// The caller supplies the freshly built product image. CALM 1.40.0's
// commander 14 dependency declares Node >=20; the image MUST satisfy it.
func TestDockerRuntimeSupportsCALM(t *testing.T) {
	image := os.Getenv("AGENT_FITNESS_FUNCTIONS_TEST_IMAGE")
	if image == "" {
		t.Skip("set AGENT_FITNESS_FUNCTIONS_TEST_IMAGE to the built product image")
	}
	output, err := exec.Command("docker", "run", "--rm", "--entrypoint", "node", image, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("container Node runtime: %v\n%s", err, output)
	}
	version := strings.TrimPrefix(strings.TrimSpace(string(output)), "v")
	major, err := strconv.Atoi(strings.SplitN(version, ".", 2)[0])
	if err != nil || major < 20 {
		t.Fatalf("container Node %q does not meet CALM dependency requirement >=20: %v", version, err)
	}
	output, err = exec.Command("docker", "run", "--rm", "--entrypoint", "calm", image, "--version").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "1.40.0" {
		t.Fatalf("container CALM version = %q, error=%v; want 1.40.0", output, err)
	}
}
