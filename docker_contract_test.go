package calm_poc_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerfileContainerContract(t *testing.T) {
	content, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	dockerfile := string(content)
	mustContain(t, dockerfile, "FROM golang:1.22.4-alpine3.20 AS go-build")
	mustContain(t, dockerfile, "FROM mcr.microsoft.com/dotnet/sdk:8.0.301 AS dotnet-build")
	mustContain(t, dockerfile, "FROM mcr.microsoft.com/dotnet/runtime-deps:8.0.6")
	mustContain(t, dockerfile, "ARG TARGETOS")
	mustContain(t, dockerfile, "ARG TARGETARCH")
	mustContain(t, dockerfile, "${TARGETOS:?TARGETOS is required}")
	mustContain(t, dockerfile, "${TARGETARCH:?TARGETARCH is required}")
	mustContain(t, dockerfile, "GOOS=$TARGETOS GOARCH=$TARGETARCH")
	mustContain(t, dockerfile, "linux-x64")
	mustContain(t, dockerfile, "linux-arm64")
	mustContain(t, dockerfile, "dotnet publish -c Release --self-contained true -r \"$rid\"")
	mustNotContain(t, dockerfile, "ARG TARGETARCH=amd64")
	mustNotContain(t, dockerfile, "GOARCH=amd64")
	mustNotContain(t, dockerfile, " -r linux-x64 ")
	mustContain(t, dockerfile, "ARG GIT_SHA=dev")
	mustContain(t, dockerfile, "ARG BUILD_DATE=unknown")
	mustContain(t, dockerfile, "org.opencontainers.image.revision=$GIT_SHA")
	mustContain(t, dockerfile, "org.opencontainers.image.created=$BUILD_DATE")
	mustContain(t, dockerfile, "WORKDIR /app")
	mustContain(t, dockerfile, "USER appuser")
	mustContain(t, dockerfile, "ENTRYPOINT [\"/app/agent-fitness-functions\", \"server\", \"start\"]")
	mustContain(t, dockerfile, "HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3")
	mustContain(t, dockerfile, `test -f /run/agent-fitness-functions/health-ca.crt`)
	mustContain(t, dockerfile, `--cacert /run/agent-fitness-functions/health-ca.crt https://127.0.0.1:7890/health`)
	mustContain(t, dockerfile, `AGENT_FITNESS_FUNCTIONS_TLS_CA`)
	mustContain(t, dockerfile, `--cacert "$AGENT_FITNESS_FUNCTIONS_TLS_CA" https://127.0.0.1:7890/health`)
	mustContain(t, dockerfile, `curl --fail --silent http://127.0.0.1:7890/health`)
	mustNotContain(t, dockerfile, "/app/certs/current")
	mustContain(t, dockerfile, "nodejs")
	mustContain(t, dockerfile, "npm")
	mustContain(t, dockerfile, "npm install -g @finos/calm-cli@1.40.0")
	mustContain(t, dockerfile, "python3 -m pip install --no-cache-dir --break-system-packages --require-hashes -r /tmp/requirements.lock")

	userIndex := strings.Index(dockerfile, "useradd -u 1001")
	copyChownIndex := strings.Index(dockerfile, "COPY --from=go-build --chown=appuser:appuser")
	if userIndex == -1 || copyChownIndex == -1 || userIndex > copyChownIndex {
		t.Fatalf("Dockerfile must create appuser uid 1001 before COPY --chown directives")
	}
}

func TestDockerIgnoreExcludesBuildArtifactsAndSecrets(t *testing.T) {
	content, err := os.ReadFile(".dockerignore")
	if err != nil {
		t.Fatalf("read .dockerignore: %v", err)
	}
	ignored := linesSet(string(content))
	for _, pattern := range []string{".git", ".calm", "*.md", "*.log", ".env*", "tmp/", ".tmp/", ".cache/", "bin/", "obj/", "**/bin/", "**/obj/", "certs/*.crt", "certs/*.key", "certs/*.csr", "certs/*.srl"} {
		if !ignored[pattern] {
			t.Fatalf(".dockerignore missing %q", pattern)
		}
	}
}

func TestDevCertificateBootstrapContract(t *testing.T) {
	scriptInfo, err := os.Stat("scripts/generate-dev-certs.sh")
	if err != nil {
		t.Fatalf("stat scripts/generate-dev-certs.sh: %v", err)
	}
	if scriptInfo.Mode()&0o111 == 0 {
		t.Fatalf("scripts/generate-dev-certs.sh must be executable")
	}

	scriptContent, err := os.ReadFile("scripts/generate-dev-certs.sh")
	if err != nil {
		t.Fatalf("read scripts/generate-dev-certs.sh: %v", err)
	}
	script := string(scriptContent)
	for _, needle := range []string{
		"exec",
		"agent-fitness-functions",
		"client onboard --certificates-only",
		"--force-dev-cert-rotation",
	} {
		mustContain(t, script, needle)
	}
	for _, forbidden := range []string{"openssl", "x509", "keyout", "install -m"} {
		mustNotContain(t, script, forbidden)
	}

	// ADR-0007 moved the development CA out of <repo>/certs and into the machine
	// governance root, so the repository no longer carries a certs/ directory to
	// self-ignore. The "no certificate material is tracked" invariant is asserted
	// in repo_hygiene_test.go instead.

	readmeContent, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	readme := string(readmeContent)
	mustContain(t, readme, "scripts/generate-dev-certs.sh")
	mustContain(t, readme, "docker compose up --build")
}

func TestDevCertificateBootstrapDelegatesForceAndPreservesProcessContract(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "args")
	stub := filepath.Join(dir, "agent-fitness-functions")
	content := "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >\"$RECORD\"\nprintf 'delegated stdout\\n'\nprintf 'delegated stderr\\n' >&2\nexit 23\n"
	if err := os.WriteFile(stub, []byte(content), 0o755); err != nil {
		t.Fatalf("write delegated binary: %v", err)
	}
	command := exec.Command("bash", "scripts/generate-dev-certs.sh", "--force")
	command.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "RECORD="+record)
	output, err := command.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 23 {
		t.Fatalf("delegating script error = %v, want exit 23; output=%s", err, output)
	}
	if string(output) != "delegated stdout\ndelegated stderr\n" {
		t.Fatalf("combined output = %q, want delegated streams unchanged", output)
	}
	args, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("read delegated args: %v", err)
	}
	if got, want := string(args), "client onboard --certificates-only --force-dev-cert-rotation\n"; got != want {
		t.Fatalf("delegated args = %q, want %q", got, want)
	}
}

func TestRequirementsPinsRadon(t *testing.T) {
	content, err := os.ReadFile("requirements.txt")
	if err != nil {
		t.Fatalf("read requirements.txt: %v", err)
	}
	if strings.TrimSpace(string(content)) != "radon==6.0.1" {
		t.Fatalf("requirements.txt = %q, want radon==6.0.1", string(content))
	}
}

func TestDockerComposeDeploymentContract(t *testing.T) {
	content, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	compose := string(content)

	for _, needle := range []string{
		"agent-fitness-functions:",
		"image: agent-fitness-functions:${GIT_SHA:-local}",
		"context: .",
		"GIT_SHA: ${GIT_SHA:-dev}",
		"BUILD_DATE: ${BUILD_DATE:-unknown}",
		`"7890:7890"`,
		"./configs:/app/configs",
		"./certs:/app/certs:ro",
		"./caller-repos.json:/app/caller-repos.json",
		"AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR: /app/configs",
		"AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR: /app/certs",
		"AGENT_FITNESS_FUNCTIONS_RUNTIME_DIR: /run/agent-fitness-functions",
		`AGENT_FITNESS_FUNCTIONS_RATE_LIMIT: "100"`,
		`AGENT_FITNESS_FUNCTIONS_ANALYZER_TIMEOUT: "30s"`,
		`AGENT_FITNESS_FUNCTIONS_DISABLE_REGISTRATION: "0"`,
		`test: ["CMD-SHELL", "curl --fail --silent --cacert /run/agent-fitness-functions/health-ca.crt https://127.0.0.1:7890/health || exit 1"]`,
		"interval: 30s",
		"timeout: 5s",
		"start_period: 15s",
		"retries: 3",
		"restart: unless-stopped",
		"no-new-privileges:true",
		"read_only: true",
		"/tmp:size=256m",
		"/run/agent-fitness-functions:uid=1001,gid=1001,mode=0755,size=1m,nosuid,nodev,noexec",
		"cap_drop:",
		"- ALL",
		"memory: 1g",
		`cpus: "1.0"`,
		"Compose",
		"advisory",
		"Swarm",
		"enforced",
		"Kubernetes",
		"Helm chart is authoritative",
	} {
		mustContain(t, compose, needle)
	}
	for _, forbidden := range []string{"AGENT_FITNESS_FUNCTIONS_TLS_CERT", "AGENT_FITNESS_FUNCTIONS_TLS_KEY", "AGENT_FITNESS_FUNCTIONS_TLS_CA", "/app/certs/current"} {
		mustNotContain(t, compose, forbidden)
	}
}

// TestComposeMountsAreWritableForSelfServiceRegistration locks the deployment
// side of POST /register: the configs directory and caller-repos.json must be
// writable bind mounts so the server can persist self-service registrations,
// while the certs mount and the read-only rootfs stay locked down. The
// registration kill switch must be declared explicitly so operators see the
// choice in the compose file.
func TestComposeMountsAreWritableForSelfServiceRegistration(t *testing.T) {
	content, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	compose := string(content)

	mustContain(t, compose, "./configs:/app/configs")
	mustNotContain(t, compose, "./configs:/app/configs:ro")
	mustContain(t, compose, "./caller-repos.json:/app/caller-repos.json")
	mustNotContain(t, compose, "./caller-repos.json:/app/caller-repos.json:ro")
	mustContain(t, compose, "./certs:/app/certs:ro")
	mustContain(t, compose, "read_only: true")
	mustContain(t, compose, `AGENT_FITNESS_FUNCTIONS_DISABLE_REGISTRATION: "0"`)
}

func TestRequirementsLockPinsTransitiveDependenciesWithHashes(t *testing.T) {
	content, err := os.ReadFile("requirements.lock")
	if err != nil {
		t.Fatalf("read requirements.lock: %v", err)
	}
	lock := string(content)

	for _, requirement := range []string{"colorama==", "mando==", "radon==6.0.1", "six=="} {
		mustContain(t, lock, requirement)
		if !requirementHasHash(lock, requirement) {
			t.Fatalf("requirements.lock entry %q must include at least one --hash=sha256 value", requirement)
		}
	}
}

func linesSet(content string) map[string]bool {
	result := map[string]bool{}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			result[trimmed] = true
		}
	}
	return result
}

func requirementHasHash(lock, requirement string) bool {
	start := strings.Index(lock, requirement)
	if start == -1 {
		return false
	}
	nextRequirement := len(lock)
	for _, marker := range []string{"\ncolorama==", "\nmando==", "\nradon==", "\nsix=="} {
		if next := strings.Index(lock[start+1:], marker); next != -1 && start+1+next < nextRequirement {
			nextRequirement = start + 1 + next
		}
	}
	return strings.Contains(lock[start:nextRequirement], "--hash=sha256:")
}

func mustContain(t *testing.T, content, needle string) {
	t.Helper()
	if !strings.Contains(content, needle) {
		t.Fatalf("content missing %q", needle)
	}
}

func mustNotContain(t *testing.T, content, needle string) {
	t.Helper()
	if strings.Contains(content, needle) {
		t.Fatalf("content unexpectedly contains %q", needle)
	}
}
