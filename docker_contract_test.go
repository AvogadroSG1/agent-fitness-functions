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
	mustContain(t, dockerfile, "ENTRYPOINT [\"/app/stack-fitness-functions\", \"server\", \"start\"]")
	mustContain(t, dockerfile, "HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3")
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
		"stack-fitness-functions",
		"client onboard --certificates-only",
		"--force-dev-cert-rotation",
	} {
		mustContain(t, script, needle)
	}
	for _, forbidden := range []string{"openssl", "x509", "keyout", "install -m"} {
		mustNotContain(t, script, forbidden)
	}

	certIgnoreContent, err := os.ReadFile("certs/.gitignore")
	if err != nil {
		t.Fatalf("read certs/.gitignore: %v", err)
	}
	certIgnores := linesSet(string(certIgnoreContent))
	for _, pattern := range []string{"*", "!.gitignore"} {
		if !certIgnores[pattern] {
			t.Fatalf("certs/.gitignore missing %q", pattern)
		}
	}

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
	stub := filepath.Join(dir, "stack-fitness-functions")
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
		"stack-fitness-functions:",
		"image: stack-fitness-functions:${GIT_SHA:-local}",
		"context: .",
		"GIT_SHA: ${GIT_SHA:-dev}",
		"BUILD_DATE: ${BUILD_DATE:-unknown}",
		`"7890:7890"`,
		"--tls-cert",
		"/app/certs/current/server.crt",
		"--tls-key",
		"/app/certs/current/server.key",
		"--tls-ca",
		"/app/certs/current/ca.crt",
		"./configs:/app/configs:ro",
		"./certs:/app/certs:ro",
		"./caller-repos.json:/app/caller-repos.json:ro",
		"STACK_FITNESS_FUNCTIONS_CONFIGS_DIR: /app/configs",
		"STACK_FITNESS_FUNCTIONS_TLS_CERT: /app/certs/current/server.crt",
		"STACK_FITNESS_FUNCTIONS_TLS_KEY: /app/certs/current/server.key",
		"STACK_FITNESS_FUNCTIONS_TLS_CA: /app/certs/current/ca.crt",
		`STACK_FITNESS_FUNCTIONS_RATE_LIMIT: "100"`,
		`STACK_FITNESS_FUNCTIONS_ANALYZER_TIMEOUT: "30s"`,
		`test: ["CMD-SHELL", "if [ -n \"$$STACK_FITNESS_FUNCTIONS_TLS_CA\" ]; then curl --fail --silent --cacert \"$$STACK_FITNESS_FUNCTIONS_TLS_CA\" https://127.0.0.1:7890/health; else curl --fail --silent http://127.0.0.1:7890/health; fi || exit 1"]`,
		"interval: 30s",
		"timeout: 5s",
		"start_period: 15s",
		"retries: 3",
		"restart: unless-stopped",
		"no-new-privileges:true",
		"read_only: true",
		"/tmp:size=256m",
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
