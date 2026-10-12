//go:build integration

package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	governanceServer "github.com/AvogadroSG1/agent-fitness-functions/internal/server"
)

type nativeHost struct {
	name        string
	binary      string
	entryRel    string
	toolArgs    func(tool, value string) string
	baseArgs    func(prompt string) []string
	providerEnv []string
}

func TestPiOMPCheckerNativeRuntime(t *testing.T) {
	for _, prerequisite := range []string{"node", "git", "calm"} {
		if _, err := exec.LookPath(prerequisite); err != nil {
			t.Skipf("%s is unavailable; native Pi/OMP runtime smoke skipped", prerequisite)
		}
	}
	piPath, piErr := exec.LookPath("pi")
	ompPath, ompErr := exec.LookPath("omp")
	if piErr != nil {
		t.Skipf("Pi runtime unavailable: %v", piErr)
	}
	if ompErr != nil {
		t.Skipf("OMP runtime unavailable: %v", ompErr)
	}

	cli := buildNativeSmokeCLI(t)
	providerFixture, err := filepath.Abs("testdata/native-smoke-provider.ts")
	if err != nil {
		t.Fatalf("resolve provider fixture: %v", err)
	}
	for _, host := range []nativeHost{
		{
			name:     "pi",
			binary:   piPath,
			entryRel: filepath.Join(".pi", "extensions", "agent-fitness-functions", "index.ts"),
			toolArgs: piToolArguments,
			baseArgs: func(prompt string) []string {
				return []string{"--approve", "-p", "--mode", "json", "--no-session", "--provider", "fitness-smoke", "--model", "fixture", "--tools", "edit,write,bash", prompt}
			},
		},
		{
			name:     "omp",
			binary:   ompPath,
			entryRel: filepath.Join(".omp", "extensions", "agent-fitness-functions", "index.ts"),
			toolArgs: ompToolArguments,
			baseArgs: func(prompt string) []string {
				return []string{"-p", "--mode", "json", "--no-session", "--no-title", "--no-lsp", "--no-skills", "--no-rules", "--auto-approve", "--model", "fixture", "--tools", "edit,write,bash", prompt}
			},
			providerEnv: []string{"PI_EDIT_VARIANT=replace", "PI_EDIT_FUZZY=0"},
		},
	} {
		host := host
		t.Run(host.name, func(t *testing.T) {
			runNativeHostSmoke(t, host, cli, providerFixture)
		})
	}
}

func buildNativeSmokeCLI(t *testing.T) string {
	t.Helper()
	root := projectRootForNativeSmoke(t)
	binary := filepath.Join(t.TempDir(), "agent-fitness-functions")
	command := exec.Command("go", "build", "-o", binary, "./cmd/agent-fitness-functions")
	command.Dir = root
	command.Env = append(os.Environ(), "GOCACHE="+filepath.Join(root, ".tmp", "go-build"), "GOMODCACHE="+filepath.Join(root, ".tmp", "go-mod"), "CGO_ENABLED=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build smoke CLI: %v\n%s", err, output)
	}
	return binary
}

func runNativeHostSmoke(t *testing.T, host nativeHost, cli, providerFixture string) {
	t.Helper()
	checkerURL, closeChecker := startNativeSmokeChecker(t)
	defer closeChecker()

	runNativeScenario(t, host, cli, providerFixture, checkerURL, "good edit", "edit", host.toolArgs("edit", "smoke.go"), "return 2", `"role":"tool"`)
	runNativeScenario(t, host, cli, providerFixture, checkerURL, "good write", "write", host.toolArgs("write", "allowed.go"), "return 3", `"role":"tool"`)
	runNativeScenario(t, host, cli, providerFixture, checkerURL, "blocked write", "write", host.toolArgs("write", "blocked.go"), "FORBIDDEN_DEPENDENCY", "FORBIDDEN_DEPENDENCY")
	runNativeScenario(t, host, cli, providerFixture, checkerURL, "blocked git bypass", "bash", host.toolArgs("bash", "git commit --no-verify --allow-empty -m bypass"), "git commit --no-verify --allow-empty -m bypass", "git-guard")
	runNativeScenario(t, host, cli, providerFixture, checkerURL, "allowed git status", "bash", host.toolArgs("bash", "git status --short"), "git status --short", `"role":"tool"`)

	repo := newNativeSmokeRepo(t, cli)
	entry := filepath.Join(repo, host.entryRel)
	provider := newNativeSmokeProvider(t, host.toolArgs("write", "explicit.go"), "write")
	defer provider.Close()
	args := append([]string{"--no-extensions", "-e", entry}, host.baseArgs("run explicit fixture")...)
	output := runNativeProcess(t, host, repo, args, cli, checkerURL, provider, providerFixture)
	if strings.Contains(strings.ToLower(output), "failed to load extension") || strings.Contains(strings.ToLower(output), "cannot find module") {
		t.Fatalf("explicit %s extension load failed:\n%s", host.name, output)
	}
	assertNativeSmokeState(t, checkerURL)
}

func runNativeScenario(t *testing.T, host nativeHost, cli, providerFixture, checkerURL, name, tool, toolArgs, expectedFile, expectedProvider string) {
	t.Helper()
	repo := newNativeSmokeRepo(t, cli)
	provider := newNativeSmokeProvider(t, toolArgs, tool)

	defer provider.Close()
	output := runNativeProcess(t, host, repo, host.baseArgs("run "+name), cli, checkerURL, provider, providerFixture)
	if strings.Contains(strings.ToLower(output), "failed to load extension") || strings.Contains(strings.ToLower(output), "cannot find module") {
		t.Fatalf("%s extension load failed:\n%s", host.name, output)
	}
	if provider.RequestCount() < 2 {
		t.Fatalf("%s %s made %d provider requests, want tool call and follow-up\noutput=%s", host.name, name, provider.RequestCount(), output)
	}
	second := provider.Request(1)
	if !strings.Contains(string(second), expectedProvider) {
		t.Fatalf("%s %s follow-up request lacks %q:\n%s", host.name, name, expectedProvider, second)
	}
	if tool == "edit" {
		content := readNativeSmokeFile(t, filepath.Join(repo, "smoke.go"))
		if !strings.Contains(content, expectedFile) {
			t.Fatalf("%s good edit did not land: %q", host.name, content)
		}
	}
	if tool == "write" {
		if strings.Contains(toolArgs, "FORBIDDEN_DEPENDENCY") {
			if _, err := os.Stat(filepath.Join(repo, "blocked.go")); !os.IsNotExist(err) {
				t.Fatalf("%s blocked write created blocked.go: stat error=%v", host.name, err)
			}
		} else {
			content := readNativeSmokeFile(t, filepath.Join(repo, "allowed.go"))
			if !strings.Contains(content, expectedFile) {
				t.Fatalf("%s good write did not land: %q", host.name, content)
			}
		}
	}
	if tool == "bash" && strings.Contains(toolArgs, "--no-verify") {
		if output := runGitNativeSmoke(repo, "rev-parse", "--verify", "HEAD"); output == "" {
			t.Fatalf("%s bypass scenario lost the initial commit", host.name)
		}
	}
}

func assertNativeSmokeState(t *testing.T, checkerURL string) {
	t.Helper()
	response, err := http.Get(checkerURL + "/state?repo=pi-omp-smoke")
	if err != nil {
		t.Fatalf("GET smoke state: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET smoke state status = %d", response.StatusCode)
	}
	var state governanceServer.StateResponse
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatalf("decode smoke state: %v", err)
	}
	if len(state.Violations) != 0 {
		t.Fatalf("dry-run native proposals persisted violations: %+v", state.Violations)
	}
}

func startNativeSmokeChecker(t *testing.T) (string, func()) {
	t.Helper()
	configs := t.TempDir()
	configDir := filepath.Join(configs, "pi-omp-smoke")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("create smoke config directory: %v", err)
	}
	config := `{"enforcement-mode":"block","enforcement-on-error":"block","fitness-functions":{"cyclomatic-complexity":false,"dependency-discipline":false,"implementation-depth":false,"interface-width":false,"logic-density":false,"layer-sovereignty":true,"deterministic-ordering":false,"temporal-purity":false,"sql-composition-safety":false},"fitness-function-settings":{"layer-sovereignty":{"layers":[{"name":"source","paths":["*.go"],"forbidden-patterns":["FORBIDDEN_DEPENDENCY"]}]}}}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(config), 0o644); err != nil {
		t.Fatalf("write smoke config: %v", err)
	}
	store, err := governanceServer.NewConfigStore(context.Background(), configs)
	if err != nil {
		t.Fatalf("create smoke config store: %v", err)
	}
	handler := governanceServer.NewHandlerWithOptions(governanceServer.Checker{ConfigStore: store}, nil, governanceServer.HandlerOptions{RequireAuthentication: true, LocalHTTP: true})
	httpServer := httptest.NewServer(handler)
	return httpServer.URL, func() {
		httpServer.Close()
		_ = store.Close()
	}
}

func newNativeSmokeRepo(t *testing.T, cli string) string {
	t.Helper()
	repo := t.TempDir()
	runGitNativeSmoke(repo, "init")
	runGitNativeSmoke(repo, "config", "user.name", "Checker Fixture")
	runGitNativeSmoke(repo, "config", "user.email", "checker-fixture@example.invalid")
	writeNativeSmokeFile(t, filepath.Join(repo, "smoke.go"), "package smoke\n\nfunc Good() int { return 1 }\n")
	runGitNativeSmoke(repo, "add", "smoke.go")
	runGitNativeSmoke(repo, "commit", "-m", "initial")
	command := exec.Command(cli, "client", "install-hooks", repo)
	command.Dir = repo
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("install hooks: %v\n%s", err, output)
	}
	return repo
}

func runNativeProcess(t *testing.T, host nativeHost, repo string, args []string, cli, checkerURL string, provider *nativeSmokeProvider, providerFixture ...string) string {
	t.Helper()
	if len(providerFixture) == 0 {
		providerFixture = []string{filepath.Join(projectRootForNativeSmoke(t), "hooks", "testdata", "native-smoke-provider.ts")}
	}
	args = append([]string{"-e", providerFixture[0]}, args...)
	commandContext, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(commandContext, host.binary, args...)
	command.Dir = repo
	command.Env = nativeSmokeEnvironment(repo, cli, checkerURL, provider.URL(), host.providerEnv)
	output, err := command.CombinedOutput()
	if commandContext.Err() != nil {
		t.Fatalf("%s timed out: %v\n%s", host.name, commandContext.Err(), output)
	}
	if err != nil {
		t.Fatalf("%s process failed: %v\n%s", host.name, err, output)
	}
	return string(output)
}

func nativeSmokeEnvironment(repo, cli, checkerURL, providerURL string, hostEnv []string) []string {
	blockedPrefixes := []string{
		"AGENT_FITNESS_FUNCTIONS_CLIENT_",
		"AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR=",
		"AGENT_FITNESS_FUNCTIONS_TLS_",
	}
	env := make([]string, 0, len(os.Environ())+8)
	for _, value := range os.Environ() {
		skip := false
		for _, prefix := range blockedPrefixes {
			if strings.HasPrefix(value, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			env = append(env, value)
		}
	}
	env = append(env,
		"AGENT_FITNESS_FUNCTIONS_BIN="+cli,
		"AGENT_FITNESS_FUNCTIONS_ADDR="+checkerURL,
		"AGENT_FITNESS_FUNCTIONS_REPO_NAME=pi-omp-smoke",
		"AGENT_FITNESS_FUNCTIONS_ON_ERROR=block",
		"PI_CODING_AGENT_DIR="+filepath.Join(repo, ".agent-state"),
		"NATIVE_SMOKE_PROVIDER_URL="+providerURL,
	)
	return append(env, hostEnv...)
}

type nativeSmokeProvider struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests [][]byte
	toolArgs string
	toolName string
}

func newNativeSmokeProvider(t *testing.T, toolArgs, toolName string) *nativeSmokeProvider {
	t.Helper()
	provider := &nativeSmokeProvider{toolArgs: toolArgs, toolName: toolName}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.handle))
	return provider
}

func (provider *nativeSmokeProvider) URL() string { return provider.server.URL }

func (provider *nativeSmokeProvider) Close() { provider.server.Close() }

func (provider *nativeSmokeProvider) RequestCount() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return len(provider.requests)
}

func (provider *nativeSmokeProvider) Request(index int) []byte {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]byte(nil), provider.requests[index]...)
}

func (provider *nativeSmokeProvider) handle(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	provider.mu.Lock()
	requestNumber := len(provider.requests)
	provider.requests = append(provider.requests, body)
	provider.mu.Unlock()
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache")
	response.WriteHeader(http.StatusOK)
	if requestNumber == 0 {
		call := fmt.Sprintf(`{"id":"fixture","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"fixture-call","type":"function","function":{"name":%q,"arguments":%q}}]},"finish_reason":null}]}`, provider.toolName, provider.toolArgs)
		writeSmokeSSE(response, call)
		finish := `{"id":"fixture","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`
		writeSmokeSSE(response, finish)
	} else {
		text := `{"id":"fixture","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":null}]}`
		writeSmokeSSE(response, text)
		finish := `{"id":"fixture","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`
		writeSmokeSSE(response, finish)
	}
	_, _ = io.WriteString(response, "data: [DONE]\n\n")
	if flusher, ok := response.(http.Flusher); ok {
		flusher.Flush()
	}
}

func writeSmokeSSE(response http.ResponseWriter, payload string) {
	_, _ = io.WriteString(response, "data: "+payload+"\n\n")
	if flusher, ok := response.(http.Flusher); ok {
		flusher.Flush()
	}
}

func piToolArguments(tool, value string) string {
	switch tool {
	case "edit":
		return `{"path":"smoke.go","edits":[{"oldText":"return 1","newText":"return 2"}]}`
	case "write":
		if value == "allowed.go" {
			return `{"path":"allowed.go","content":"package smoke\nfunc Allowed() int { return 3 }\n"}`
		}
		return `{"path":"blocked.go","content":"package smoke\n// FORBIDDEN_DEPENDENCY\n"}`
	default:
		return fmt.Sprintf(`{"command":%q}`, value)
	}
}

func ompToolArguments(tool, value string) string {
	switch tool {
	case "edit":
		return `{"path":"smoke.go","old_string":"return 1","new_string":"return 2"}`
	case "write":
		if value == "allowed.go" {
			return `{"path":"allowed.go","content":"package smoke\nfunc Allowed() int { return 3 }\n"}`
		}
		return `{"path":"blocked.go","content":"package smoke\n// FORBIDDEN_DEPENDENCY\n"}`
	default:
		return fmt.Sprintf(`{"command":%q}`, value)
	}
}

func projectRootForNativeSmoke(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get hooks working directory: %v", err)
	}
	return filepath.Dir(workingDirectory)
}

func writeNativeSmokeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write smoke file %s: %v", path, err)
	}
}

func readNativeSmokeFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read smoke file %s: %v", path, err)
	}
	return string(content)
}

func runGitNativeSmoke(repo string, args ...string) string {
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		panic(fmt.Sprintf("git %v failed: %v\n%s", args, err, output))
	}
	return strings.TrimSpace(string(output))
}
