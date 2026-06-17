package hooks

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPreToolUseBlocksWriteViolation(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STACK_FITNESS_FUNCTIONS_LOG"
printf '{"status":"block","violations":[{"message":"too complex"}]}\n'
`)
	payload := `{"tool_name":"Write","tool_input":{"file_path":"sample.go","content":"package sample\nfunc Run() {}\n"}}`

	output, err := runPreToolUse(t, repo, payload, fakeBin, logPath, "")
	if exitCode(err) != 2 {
		t.Fatalf("pre-tool-use succeeded, want block; output=%s", output)
	}
	if !strings.Contains(string(output), "calm_check:") || !strings.Contains(string(output), "blocking") {
		t.Fatalf("output = %s, want YAML calm_check block with blocking mode", output)
	}
	logContent := readFile(t, logPath)
	for _, want := range []string{"client validate", "--file sample.go", "--repo ", "--content-file ", "--language go"} {
		if !strings.Contains(logContent, want) {
			t.Fatalf("calm log = %s, want %s", logContent, want)
		}
	}
}

func TestPreToolUseAllowsEditAdvisory(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "sample.py"), "print('old')\n")
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STACK_FITNESS_FUNCTIONS_LOG"
printf '{"status":"advisory","violations":[{"message":"warning only"}]}\n'
`)
	payload := `{"tool_name":"Edit","tool_input":{"file_path":"sample.py","old_string":"print('old')","new_string":"print('new')"}}`

	output, err := runPreToolUse(t, repo, payload, fakeBin, logPath, "")
	if err != nil {
		t.Fatalf("pre-tool-use failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "calm_check:") || !strings.Contains(string(output), "advisory") {
		t.Fatalf("output = %s, want YAML calm_check block with advisory mode", output)
	}
	if !strings.Contains(readFile(t, logPath), "--language python") {
		t.Fatalf("calm log = %s, want python language", readFile(t, logPath))
	}
}

func TestPreToolUseSkipsUnsupportedEditBeforeReconstruction(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "# old title\n")
	payload := `{"tool_name":"Edit","tool_input":{"file_path":"README.md","old_string":"missing old title","new_string":"new title"}}`

	output, err := runPreToolUse(t, repo, payload, t.TempDir(), "", "")
	if err != nil {
		t.Fatalf("pre-tool-use failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "unsupported file type") {
		t.Fatalf("output = %s, want unsupported file skip", output)
	}
}

func TestPreToolUseSendsReconstructedEditContent(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n\nfunc Message() string {\n\treturn \"old\"\n}\n")
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
while [[ $# -gt 0 ]]; do
  case "$1" in
    --content-file)
      shift
      cat "$1" > "$STACK_FITNESS_FUNCTIONS_LOG"
      ;;
  esac
  shift
done
printf '{"status":"pass"}\n'
`)
	payload := `{"tool_name":"Edit","tool_input":{"file_path":"sample.go","old_string":"return \"old\"\n","new_string":"return \"new\"\n"}}`

	output, err := runPreToolUse(t, repo, payload, fakeBin, logPath, "")
	if err != nil {
		t.Fatalf("pre-tool-use failed: %v\n%s", err, output)
	}
	want := "package sample\n\nfunc Message() string {\n\treturn \"new\"\n}\n"
	if got := readFile(t, logPath); got != want {
		t.Fatalf("content file = %q, want reconstructed full file %q", got, want)
	}
}

func TestPreToolUseAllowsPassWithAbsolutePath(t *testing.T) {
	repo := initGitRepo(t)
	path := filepath.Join(repo, "src", "Widget.cs")
	writeFile(t, path, "namespace Demo;\n")
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STACK_FITNESS_FUNCTIONS_LOG"
printf '{"status":"pass"}\n'
`)
	payload := `{"tool_name":"Write","tool_input":{"file_path":"` + path + `","content":"namespace Demo;\n"}}`

	output, err := runPreToolUse(t, repo, payload, fakeBin, logPath, "http://127.0.0.1:9999")
	if err != nil {
		t.Fatalf("pre-tool-use failed: %v\n%s", err, output)
	}
	logContent := readFile(t, logPath)
	for _, want := range []string{"--file src/Widget.cs", "--addr http://127.0.0.1:9999", "--language csharp"} {
		if !strings.Contains(logContent, want) {
			t.Fatalf("calm log = %s, want %s", logContent, want)
		}
	}
}

func TestPreToolUseChecksRunningDaemonKnownBadAndGood(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, ".calm", "config.json"), `{
  "enforcement-mode": "block",
  "fitness-functions": {
    "cyclomatic-complexity": true,
    "interface-width": false,
    "implementation-depth": false,
    "logic-density": false,
    "dependency-discipline": false
  }
}`)
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")
	fitnessBin := buildFitnessBin(t)
	daemon := startFitnessDaemon(t, fitnessBin)
	t.Setenv("STACK_FITNESS_FUNCTIONS_REPO_NAME", "repo-one")
	t.Setenv("STACK_FITNESS_FUNCTIONS_CLIENT_CERT", daemon.clientCertPath)
	t.Setenv("STACK_FITNESS_FUNCTIONS_CLIENT_KEY", daemon.clientKeyPath)
	t.Setenv("STACK_FITNESS_FUNCTIONS_CLIENT_CA", daemon.serverCAPath)

	badPayload := `{"tool_name":"Write","tool_input":{"file_path":"sample.go","content":"package sample\nfunc Score(kind string, retries int, urgent bool) int {\nscore := 0\nif kind == \"create\" { score++ }\nif kind == \"update\" { score++ }\nif kind == \"delete\" { score++ }\nif kind == \"manual\" { score++ }\nif kind == \"batch\" { score++ }\nif kind == \"sync\" { score++ }\nif retries > 0 { score++ }\nif retries > 1 { score++ }\nif retries > 2 { score++ }\nif urgent { score++ }\nreturn score\n}\n"}}`
	output, err := runPreToolUseWithBin(t, repo, badPayload, fitnessBin, "", "", daemon.url)
	if exitCode(err) != 2 {
		t.Fatalf("pre-tool-use succeeded, want running daemon block; output=%s", output)
	}
	if !strings.Contains(string(output), "cyclomatic-complexity") {
		t.Fatalf("output = %s, want YAML cyclomatic-complexity violation", output)
	}

	goodPayload := `{"tool_name":"Write","tool_input":{"file_path":"sample.go","content":"package sample\nfunc Score(kind string, retries int, urgent bool) int {\nscore := map[string]int{\"create\": 1, \"update\": 1, \"delete\": 1, \"manual\": 1, \"batch\": 1, \"sync\": 1}[kind]\nif urgent { score++ }\nif retries > 0 { score += min(retries, 3) }\nreturn score\n}\n"}}`
	output, err = runPreToolUseWithBin(t, repo, goodPayload, fitnessBin, "", "", daemon.url)
	if err != nil {
		t.Fatalf("pre-tool-use failed for known-good content: %v\n%s", err, output)
	}
}

func TestPreToolUsePreservesEmptyAndTrailingNewlineContent(t *testing.T) {
	repo := initGitRepo(t)
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
while [[ $# -gt 0 ]]; do
  case "$1" in
    --content-file)
      shift
      python3 - "$1" "$STACK_FITNESS_FUNCTIONS_LOG" <<'PY'
import pathlib
import sys

content = pathlib.Path(sys.argv[1]).read_text()
with open(sys.argv[2], "a", encoding="utf-8") as handle:
    handle.write(repr(content) + "\n")
PY
      ;;
  esac
  shift
done
printf '{"status":"pass"}\n'
`)
	firstPayload := `{"tool_name":"Write","tool_input":{"file_path":"empty.go","content":""}}`
	if output, err := runPreToolUse(t, repo, firstPayload, fakeBin, logPath, ""); err != nil {
		t.Fatalf("empty pre-tool-use failed: %v\n%s", err, output)
	}
	secondPayload := `{"tool_name":"Write","tool_input":{"file_path":"sample.go","content":"package sample\n\n"}}`
	if output, err := runPreToolUse(t, repo, secondPayload, fakeBin, logPath, ""); err != nil {
		t.Fatalf("newline pre-tool-use failed: %v\n%s", err, output)
	}
	logContent := readFile(t, logPath)
	if !strings.Contains(logContent, "''") || !strings.Contains(logContent, "'package sample\\n\\n'") {
		t.Fatalf("content log = %s, want empty and trailing newline content preserved", logContent)
	}
}

func TestPreToolUseBlocksBinarySourceContent(t *testing.T) {
	repo := initGitRepo(t)
	payload := "{\"tool_name\":\"Write\",\"tool_input\":{\"file_path\":\"sample.go\",\"content\":\"abc\\u0000def\"}}"
	output, err := runPreToolUse(t, repo, payload, t.TempDir(), "", "")
	if exitCode(err) != 2 {
		t.Fatalf("pre-tool-use exit = %v, want block; output=%s", err, output)
	}
	if !strings.Contains(string(output), "blocked binary content") {
		t.Fatalf("output = %s, want binary block", output)
	}
}

func TestPreToolUseSkipsFilesOutsideRepo(t *testing.T) {
	repo := initGitRepo(t)
	outside := filepath.Join(t.TempDir(), "sample.go")
	payload := `{"tool_name":"Write","tool_input":{"file_path":"` + outside + `","content":"package outside\n"}}`
	output, err := runPreToolUse(t, repo, payload, t.TempDir(), "", "")
	if err != nil {
		t.Fatalf("pre-tool-use failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "outside repository") {
		t.Fatalf("output = %s, want outside repo skip", output)
	}
}

func TestPreToolUseRejectsRemoteBridgeWithoutOptIn(t *testing.T) {
	repo := initGitRepo(t)
	payload := `{"tool_name":"Write","tool_input":{"file_path":"sample.go","content":"package sample\n"}}`
	output, err := runPreToolUse(t, repo, payload, t.TempDir(), "", "http://127.0.0.1:80@evil.example")
	if exitCode(err) != 2 {
		t.Fatalf("pre-tool-use succeeded, want remote bridge rejection; output=%s", output)
	}
	if !strings.Contains(string(output), "must be loopback") {
		t.Fatalf("output = %s, want loopback diagnostic", output)
	}
}

func TestPreToolUseReportsMalformedJSONWithoutTraceback(t *testing.T) {
	repo := initGitRepo(t)
	output, err := runPreToolUse(t, repo, `{"tool_input":`, t.TempDir(), "", "")
	if exitCode(err) != 2 {
		t.Fatalf("pre-tool-use exit = %v, want block; output=%s", err, output)
	}
	if !strings.Contains(string(output), "Invalid PreToolUse payload") || strings.Contains(string(output), "Traceback") {
		t.Fatalf("output = %s, want clean invalid payload diagnostic", output)
	}
}

func TestPreToolUseBlocksInvalidBridgeJSONWithoutTraceback(t *testing.T) {
	repo := initGitRepo(t)
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf 'not json\n'
`)
	payload := `{"tool_name":"Write","tool_input":{"file_path":"sample.go","content":"package sample\n"}}`
	output, err := runPreToolUse(t, repo, payload, fakeBin, "", "")
	if exitCode(err) != 2 {
		t.Fatalf("pre-tool-use exit = %v, want block; output=%s", err, output)
	}
	if !strings.Contains(string(output), "invalid JSON") || strings.Contains(string(output), "Traceback") {
		t.Fatalf("output = %s, want clean invalid bridge JSON diagnostic", output)
	}
}

func TestPreToolUseHandlesLargeContentThroughContentFile(t *testing.T) {
	repo := initGitRepo(t)
	logPath := filepath.Join(t.TempDir(), "calm.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
while [[ $# -gt 0 ]]; do
  case "$1" in
    --content-file)
      shift
      wc -c < "$1" >> "$STACK_FITNESS_FUNCTIONS_LOG"
      ;;
  esac
  shift
done
printf '{"status":"pass"}\n'
`)
	large := strings.Repeat("x", 512*1024)
	payload := `{"tool_name":"Write","tool_input":{"file_path":"large.go","content":"` + large + `"}}`
	if output, err := runPreToolUse(t, repo, payload, fakeBin, logPath, ""); err != nil {
		t.Fatalf("pre-tool-use failed: %v\n%s", err, output)
	}
	if !strings.Contains(readFile(t, logPath), "524288") {
		t.Fatalf("log = %s, want content file byte count", readFile(t, logPath))
	}
}

func TestPreToolUseForwardsDiscoveredMTLSCerts(t *testing.T) {
	repo := initGitRepo(t)
	// git rev-parse --show-toplevel resolves symlinks (macOS /var -> /private/var),
	// so resolve here too to match the cert paths the hook forwards.
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		repo = resolved
	}
	runGit(t, repo, "config", "user.email", "t@example.com")
	runGit(t, repo, "config", "user.name", "t")
	writeFile(t, filepath.Join(repo, "sample.go"), "package sample\n")

	certDir := filepath.Join(repo, "certs")
	for _, name := range []string{"client.crt", "client.key", "ca.crt"} {
		writeFile(t, filepath.Join(certDir, name), "x")
	}

	logPath := filepath.Join(t.TempDir(), "calls.log")
	fakeBin := fakeFitnessBin(t, `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STACK_FITNESS_FUNCTIONS_LOG"
echo '{"status":"pass"}'
`)

	payload := `{"tool_input":{"file_path":"sample.go","content":"package sample\n"}}`
	command := exec.Command("bash", hookScriptPathFor(t, "pre-tool-use.sh"))
	command.Dir = repo
	command.Stdin = strings.NewReader(payload)
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STACK_FITNESS_FUNCTIONS_LOG="+logPath,
	)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pre-tool-use failed: %v\n%s", err, out)
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

func runPreToolUse(t *testing.T, repo, payload, fakeBin, logPath, addr string) ([]byte, error) {
	t.Helper()
	return runPreToolUseWithBin(t, repo, payload, "", fakeBin, logPath, addr)
}

func runPreToolUseWithBin(t *testing.T, repo, payload, fitnessBin, pathDir, logPath, addr string) ([]byte, error) {
	t.Helper()
	command := exec.Command("bash", hookScriptPathFor(t, "pre-tool-use.sh"))
	command.Dir = repo
	command.Stdin = strings.NewReader(payload)
	env := os.Environ()
	if pathDir != "" {
		env = append(env, "PATH="+pathDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	if fitnessBin != "" {
		env = append(env, "STACK_FITNESS_FUNCTIONS_BIN="+fitnessBin)
	}
	if logPath != "" {
		env = append(env, "STACK_FITNESS_FUNCTIONS_LOG="+logPath)
	}
	if addr != "" {
		env = append(env, "STACK_FITNESS_FUNCTIONS_ADDR="+addr)
	}
	command.Env = env
	return command.CombinedOutput()
}

func hookScriptPathFor(t *testing.T, name string) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	return filepath.Join(cwd, name)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

type fitnessDaemon struct {
	url            string
	clientCertPath string
	clientKeyPath  string
	serverCAPath   string
}

func startFitnessDaemon(t *testing.T, fitnessBin string) fitnessDaemon {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	configsDir := t.TempDir()
	repoDir := filepath.Join(configsDir, "repo-one")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	configJSON := `{"enforcement-mode":"block","fitness-functions":{"cyclomatic-complexity":true}}`
	if err := os.WriteFile(filepath.Join(repoDir, "config.json"), []byte(configJSON), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configsDir, "caller-repos.json"), []byte(`{"callers":{"pre-tool-use-test":["repo-one"]}}`), 0o644); err != nil {
		t.Fatalf("write caller policy: %v", err)
	}
	certDir := t.TempDir()
	serverCertPath, serverKeyPath, caPath, clientCertPath, clientKeyPath := writeMTLSFixture(t, certDir, "pre-tool-use-test")

	command := exec.Command(fitnessBin, "server", "start", "--addr", addr, "--tls-cert", serverCertPath, "--tls-key", serverKeyPath, "--tls-ca", caPath)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	command.Dir = filepath.Dir(cwd)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Env = append(os.Environ(), "STACK_FITNESS_FUNCTIONS_CONFIGS_DIR="+configsDir)
	if err := command.Start(); err != nil {
		t.Fatalf("start bridge daemon: %v", err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	rootCAs := x509.NewCertPool()
	caContent, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatalf("read test ca: %v", err)
	}
	if !rootCAs.AppendCertsFromPEM(caContent) {
		t.Fatal("append test ca")
	}
	client := &http.Client{
		Timeout:   time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: rootCAs, MinVersion: tls.VersionTLS12}},
	}
	for range 50 {
		response, err := client.Get("https://" + addr + "/health")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return fitnessDaemon{
					url:            "https://" + addr,
					clientCertPath: clientCertPath,
					clientKeyPath:  clientKeyPath,
					serverCAPath:   caPath,
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("bridge daemon did not become healthy")
	return fitnessDaemon{}
}

func writeMTLSFixture(t *testing.T, dir, clientCN string) (string, string, string, string, string) {
	t.Helper()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate ca key: %v", err)
	}
	ca := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "stack-fitness-functions-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create ca: %v", err)
	}
	caPath := filepath.Join(dir, "ca.pem")
	writeCertificatePEM(t, caPath, caDER)

	serverCertPath, serverKeyPath := writeSignedCertificate(t, dir, "server", ca, caKey, pkix.Name{CommonName: "localhost"}, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	clientCertPath, clientKeyPath := writeSignedCertificate(t, dir, "client", ca, caKey, pkix.Name{CommonName: clientCN}, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})
	return serverCertPath, serverKeyPath, caPath, clientCertPath, clientKeyPath
}

func writeSignedCertificate(t *testing.T, dir, name string, ca *x509.Certificate, caKey *rsa.PrivateKey, subject pkix.Name, usages []x509.ExtKeyUsage) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate %s key: %v", name, err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      subject,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  usages,
	}
	if len(usages) == 1 && usages[0] == x509.ExtKeyUsageServerAuth {
		template.DNSNames = []string{"localhost"}
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create %s certificate: %v", name, err)
	}
	certPath := filepath.Join(dir, name+".pem")
	keyPath := filepath.Join(dir, name+"-key.pem")
	writeCertificatePEM(t, certPath, der)
	writePrivateKeyPEM(t, keyPath, key)
	return certPath, keyPath
}

func writeCertificatePEM(t *testing.T, path string, der []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write certificate %s: %v", path, err)
	}
}

func writePrivateKeyPEM(t *testing.T, path string, key *rsa.PrivateKey) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatalf("write private key %s: %v", path, err)
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}
