package calm_poc_test

import (
	"os"
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
	mustContain(t, dockerfile, "RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build")
	mustContain(t, dockerfile, "RUN dotnet publish -c Release --self-contained true -r linux-x64")
	mustContain(t, dockerfile, "ARG GIT_SHA=dev")
	mustContain(t, dockerfile, "ARG BUILD_DATE=unknown")
	mustContain(t, dockerfile, "org.opencontainers.image.revision=$GIT_SHA")
	mustContain(t, dockerfile, "org.opencontainers.image.created=$BUILD_DATE")
	mustContain(t, dockerfile, "WORKDIR /app")
	mustContain(t, dockerfile, "USER appuser")
	mustContain(t, dockerfile, "ENTRYPOINT [\"/app/calm-bridge\", \"serve\"]")
	mustContain(t, dockerfile, "HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3")
	mustContain(t, dockerfile, "python3 -m pip install --no-cache-dir --break-system-packages -r /tmp/requirements.txt")

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
	for _, pattern := range []string{".git", ".calm", "*.md", "*.log", ".env*", "tmp/", ".tmp/", ".cache/", "bin/", "obj/", "**/bin/", "**/obj/"} {
		if !ignored[pattern] {
			t.Fatalf(".dockerignore missing %q", pattern)
		}
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

func mustContain(t *testing.T, content, needle string) {
	t.Helper()
	if !strings.Contains(content, needle) {
		t.Fatalf("Dockerfile missing %q", needle)
	}
}
