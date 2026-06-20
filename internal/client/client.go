// Package client owns the commit-time validation client for stack-fitness-functions.
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/poconnor/calm-poc/internal/fitness"
	"github.com/poconnor/calm-poc/internal/sarif"
)

//go:embed hookassets/*
var embeddedHooks embed.FS

// Hook artifact naming. Generated git-hook artifacts carry the product name.
// FINOS CALM surfaces (.calm/, configs/, calm-poc, the calm CLI) are unaffected.
const hookProductPrefix = "stack-fitness-functions"

const (
	gitGuardName       = hookProductPrefix + "-git-guard"
	legacyGitGuardName = "calm-git-guard"
)

func managedHookMarker(hook string) string { return hookProductPrefix + " " + hook + " hook" }
func legacyHookMarker(hook string) string  { return "CALM " + hook + " hook" }
func sidecarHookMarker(hook string) string {
	return "# " + hookProductPrefix + " " + hook + " hook (sidecar)"
}
func legacySidecarMarker(hook string) string { return "# CALM " + hook + " hook (sidecar)" }
func sidecarHookName(hook string) string     { return hookProductPrefix + "-" + hook }

// RunCheck validates one file by posting a validation request to the daemon.
func RunCheck(args []string, stdout io.Writer, httpClient *http.Client, starter func(string) error) error {
	flags := flag.NewFlagSet("client validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	addr := flags.String("addr", "https://127.0.0.1:7890", "daemon base URL")
	file := flags.String("file", "", "file path being checked")
	repo := flags.String("repo", "", "logical repository name sent to the server")
	gitDir := flags.String("git-dir", "", "local git working directory for --staged and disk reads (defaults to --repo)")
	content := flags.String("content", "", "proposed file content")
	contentFile := flags.String("content-file", "", "path to proposed file content")
	language := flags.String("language", "", "source language")
	staged := flags.Bool("staged", false, "read content from git staged state")
	format := flags.String("format", "json", "output format: json or sarif")
	clientCert := flags.String("client-cert", "", "mTLS client certificate path")
	clientKey := flags.String("client-key", "", "mTLS client private key path")
	clientCA := flags.String("client-ca", "", "server CA bundle path")
	if err := flags.Parse(args); err != nil {
		return usageError{err: err}
	}
	if *file == "" || *repo == "" {
		return usageError{err: errors.New("client validate requires --file and --repo")}
	}
	validFormats := map[string]bool{"json": true, "sarif": true}
	if !validFormats[*format] {
		return usageError{err: fmt.Errorf("unsupported format %q: use json or sarif", *format)}
	}
	configuredClient, err := configureTLS(httpClient, *clientCert, *clientKey, *clientCA)
	if err != nil {
		return err
	}
	if err := ensureDaemon(configuredClient, *addr, starter); err != nil {
		return err
	}
	resolvedGitDir := resolveGitDir(*gitDir, *repo)
	proposedContent, err := resolveContent(resolvedGitDir, *file, *content, *contentFile, *staged)
	if err != nil {
		return err
	}
	body, err := postCheck(context.Background(), configuredClient, *addr, fitness.ValidationRequest{
		Repo:            *repo,
		File:            *file,
		ProposedContent: proposedContent,
		Language:        *language,
	})
	if err != nil {
		return err
	}
	return writeValidationResult(stdout, *format, resolvedGitDir, body)
}

// RunInstallHooks installs embedded Git and Claude hooks into a repository.
func RunInstallHooks(args []string, stdout, stderr io.Writer) error {
	if len(args) > 1 {
		return usageError{err: errors.New("client install-hooks accepts at most one repository argument")}
	}
	repo := "."
	if len(args) == 1 {
		repo = args[0]
	}
	repoRoot, err := gitOutput(repo, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("resolving repository root: %w", err)
	}
	installer := hookInstaller{
		repoRoot: repoRoot,
		stdout:   stdout,
		stderr:   stderr,
	}
	if err := installer.installGitHook("pre-commit", "hookassets/pre-commit.sh"); err != nil {
		return err
	}
	if err := installer.installGitHook("pre-push", "hookassets/pre-push.sh"); err != nil {
		return err
	}
	return installer.installGitGuard()
}

type hookInstaller struct {
	repoRoot string
	stdout   io.Writer
	stderr   io.Writer
}

func (installer hookInstaller) installGitHook(hookName, embeddedPath string) error {
	targetHook, err := installer.gitHookPath(hookName)
	if err != nil {
		return err
	}
	hooksDir := filepath.Dir(targetHook)
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("creating hooks directory: %w", err)
	}
	handled, err := installer.handleExistingGitHook(targetHook, hooksDir, hookName, embeddedPath)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if err := installer.writeEmbeddedExecutable(embeddedPath, targetHook); err != nil {
		return err
	}
	if err := installer.writeFormatter(hooksDir); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(installer.stdout, "installed %s\n", targetHook)
	return nil
}

func (installer hookInstaller) handleExistingGitHook(targetHook, hooksDir, hookName, embeddedPath string) (bool, error) {
	content, exists, err := readExistingHook(targetHook, hookName)
	if err != nil || !exists {
		return false, err
	}
	if hookHasSidecar(content, hookName) {
		return true, installer.refreshHookSidecar(targetHook, hooksDir, hookName, embeddedPath)
	}
	if hookIsManaged(content, hookName) {
		return false, nil
	}
	return installer.resolveUnmanagedHook(targetHook, hooksDir, hookName, embeddedPath)
}

func readExistingHook(targetHook, hookName string) ([]byte, bool, error) {
	if _, err := os.Stat(targetHook); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("checking existing %s hook: %w", hookName, err)
	}
	content, err := os.ReadFile(targetHook)
	if err != nil {
		return nil, false, fmt.Errorf("reading existing %s hook: %w", hookName, err)
	}
	return content, true, nil
}

func hookHasSidecar(content []byte, hookName string) bool {
	return bytes.Contains(content, []byte(sidecarHookMarker(hookName))) ||
		bytes.Contains(content, []byte(legacySidecarMarker(hookName)))
}

func hookIsManaged(content []byte, hookName string) bool {
	return bytes.Contains(content, []byte(managedHookMarker(hookName))) ||
		bytes.Contains(content, []byte(legacyHookMarker(hookName)))
}

func (installer hookInstaller) refreshHookSidecar(targetHook, hooksDir, hookName, embeddedPath string) error {
	sidecar := filepath.Join(hooksDir, sidecarHookName(hookName))
	legacySidecar := legacySidecarPath(hooksDir, hookName)
	if err := installer.writeEmbeddedExecutable(embeddedPath, sidecar); err != nil {
		return err
	}
	if err := rewriteHookSidecarReference(targetHook, hookName, legacySidecar, sidecar); err != nil {
		return err
	}
	if err := installer.writeFormatter(hooksDir); err != nil {
		return err
	}
	_ = os.Remove(legacySidecar)
	_, _ = fmt.Fprintf(installer.stdout, "updated %s\n", sidecar)
	return nil
}

func legacySidecarPath(hooksDir, hookName string) string {
	return filepath.Join(hooksDir, "calm-"+hookName)
}

func rewriteHookSidecarReference(targetHook, hookName, legacySidecar, sidecar string) error {
	content, err := os.ReadFile(targetHook)
	if err != nil {
		return fmt.Errorf("reading existing %s hook: %w", hookName, err)
	}
	rewritten, changed := rewriteLegacySidecarBlock(content, hookName, legacySidecar, sidecar)
	if !changed {
		return nil
	}
	if err := os.WriteFile(targetHook, rewritten, 0o755); err != nil {
		return fmt.Errorf("rewriting existing %s hook: %w", hookName, err)
	}
	return nil
}

func managedSidecarHookContent(hookName, sidecar string) []byte {
	return []byte(fmt.Sprintf("#!/usr/bin/env bash\n%s\n%q\n", sidecarHookMarker(hookName), sidecar))
}

func rewriteLegacySidecarBlock(content []byte, hookName, legacySidecar, sidecar string) ([]byte, bool) {
	lines := strings.Split(string(content), "\n")
	newPath := strconv.Quote(sidecar)
	for index := 0; index < len(lines)-1; index++ {
		if lines[index] != legacySidecarMarker(hookName) && lines[index] != sidecarHookMarker(hookName) {
			continue
		}
		changed := false
		if lines[index] != sidecarHookMarker(hookName) {
			lines[index] = sidecarHookMarker(hookName)
			changed = true
		}
		if lines[index+1] != newPath {
			lines[index+1] = newPath
			changed = true
		}
		if !changed {
			return content, false
		}
		return []byte(strings.Join(lines, "\n")), true
	}
	return content, false
}

func (installer hookInstaller) resolveUnmanagedHook(targetHook, hooksDir, hookName, embeddedPath string) (bool, error) {
	if os.Getenv("STACK_FITNESS_FUNCTIONS_HOOK_APPEND") == "1" {
		return true, installer.appendHookSidecar(targetHook, hooksDir, hookName, embeddedPath)
	}
	if os.Getenv("STACK_FITNESS_FUNCTIONS_HOOK_OVERWRITE") == "1" {
		return false, nil
	}
	_, _ = fmt.Fprintf(installer.stderr, "refusing to overwrite existing unmanaged %s hook: %s\n", hookName, targetHook)
	_, _ = fmt.Fprintln(installer.stderr, "set STACK_FITNESS_FUNCTIONS_HOOK_OVERWRITE=1 to replace it, or STACK_FITNESS_FUNCTIONS_HOOK_APPEND=1 to append")
	return true, errors.New("refusing to overwrite existing unmanaged hook")
}

func (installer hookInstaller) appendHookSidecar(targetHook, hooksDir, hookName, embeddedPath string) error {
	sidecar := filepath.Join(hooksDir, sidecarHookName(hookName))
	if err := installer.writeEmbeddedExecutable(embeddedPath, sidecar); err != nil {
		return err
	}
	if err := installer.writeFormatter(hooksDir); err != nil {
		return err
	}
	block := fmt.Sprintf("\n%s\n%q\n", sidecarHookMarker(hookName), sidecar)
	if err := appendFile(targetHook, []byte(block)); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(installer.stdout, "appended %s call to %s (sidecar: %s)\n", hookProductPrefix, targetHook, sidecar)
	return nil
}

func (installer hookInstaller) installGitGuard() error {
	guardPath, err := installer.gitHookPath(gitGuardName)
	if err != nil {
		return err
	}
	if err := installer.writeEmbeddedExecutable("hookassets/git-guard.sh", guardPath); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(installer.stdout, "installed %s\n", guardPath)

	settingsPath := filepath.Join(installer.repoRoot, ".claude", "settings.json")
	settings, err := loadClaudeSettings(settingsPath)
	if err != nil {
		return err
	}
	message, err := upsertGitGuard(settings, guardPath)
	if err != nil {
		return err
	}
	if err := writeClaudeSettings(settingsPath, settings); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(installer.stdout, "%s in %s\n", message, settingsPath)
	return nil
}

func loadClaudeSettings(settingsPath string) (map[string]any, error) {
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating .claude directory: %w", err)
	}
	settings := map[string]any{}
	content, err := os.ReadFile(settingsPath)
	if errors.Is(err, os.ErrNotExist) || len(bytes.TrimSpace(content)) == 0 {
		return settings, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", settingsPath, err)
	}
	if err := json.Unmarshal(content, &settings); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", settingsPath, err)
	}
	return settings, nil
}

func writeClaudeSettings(settingsPath string, settings map[string]any) error {
	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", settingsPath, err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(settingsPath, encoded, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", settingsPath, err)
	}
	return nil
}

func upsertGitGuard(settings map[string]any, guardPath string) (string, error) {
	hooks := ensureHooksSection(settings)
	preToolUse := preToolUseEntries(hooks)
	newEntry := gitGuardHookEntry(guardPath)
	if index, command, found := findGitGuardHookEntry(preToolUse); found {
		preToolUse[index] = newEntry
		hooks["PreToolUse"] = preToolUse
		if command == guardPath {
			return gitGuardName + " already configured", nil
		}
		return "updated " + gitGuardName + " path", nil
	}
	hooks["PreToolUse"] = append(preToolUse, newEntry)
	return "added " + gitGuardName + " to PreToolUse hooks", nil
}

func ensureHooksSection(settings map[string]any) map[string]any {
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		hooks = map[string]any{}
		settings["hooks"] = hooks
	}
	return hooks
}

func preToolUseEntries(hooks map[string]any) []any {
	preToolUse, ok := hooks["PreToolUse"].([]any)
	if !ok {
		return []any{}
	}
	return preToolUse
}

func gitGuardHookEntry(guardPath string) map[string]any {
	return map[string]any{
		"hooks": []any{map[string]any{
			"command": guardPath,
			"type":    "command",
		}},
		"matcher": "Bash",
	}
}

func findGitGuardHookEntry(preToolUse []any) (int, string, bool) {
	for index, rawEntry := range preToolUse {
		command, ok := gitGuardCommand(rawEntry)
		if !ok {
			continue
		}
		if strings.Contains(command, gitGuardName) || strings.Contains(command, legacyGitGuardName) {
			return index, command, true
		}
	}
	return 0, "", false
}

func gitGuardCommand(rawEntry any) (string, bool) {
	entry, ok := rawEntry.(map[string]any)
	if !ok {
		return "", false
	}
	rawHooks, ok := entry["hooks"].([]any)
	if !ok {
		return "", false
	}
	for _, rawHook := range rawHooks {
		hook, ok := rawHook.(map[string]any)
		if !ok {
			continue
		}
		command, _ := hook["command"].(string)
		if command != "" {
			return command, true
		}
	}
	return "", false
}

func (installer hookInstaller) writeFormatter(hooksDir string) error {
	return installer.writeEmbeddedFile("hookassets/format-violations.py", filepath.Join(hooksDir, "format-violations.py"), 0o755)
}

func (installer hookInstaller) writeEmbeddedExecutable(embeddedPath, targetPath string) error {
	return installer.writeEmbeddedFile(embeddedPath, targetPath, 0o755)
}

func (installer hookInstaller) writeEmbeddedFile(embeddedPath, targetPath string, mode fs.FileMode) error {
	content, err := embeddedHooks.ReadFile(embeddedPath)
	if err != nil {
		return fmt.Errorf("reading embedded %s: %w", embeddedPath, err)
	}
	if err := os.WriteFile(targetPath, content, mode); err != nil {
		return fmt.Errorf("writing %s: %w", targetPath, err)
	}
	return nil
}

func (installer hookInstaller) gitHookPath(name string) (string, error) {
	path, err := gitOutput(installer.repoRoot, "rev-parse", "--git-path", "hooks/"+name)
	if err != nil {
		return "", fmt.Errorf("resolving %s hook path: %w", name, err)
	}
	if filepath.IsAbs(path) {
		return path, nil
	}
	return filepath.Join(installer.repoRoot, path), nil
}

func gitOutput(repo string, args ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, detail)
	}
	return strings.TrimSpace(string(output)), nil
}

func appendFile(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("opening %s for append: %w", path, err)
	}
	defer file.Close()
	if _, err := file.Write(content); err != nil {
		return fmt.Errorf("appending %s: %w", path, err)
	}
	return nil
}

func ensureDaemon(httpClient *http.Client, addr string, starter func(string) error) error {
	if isHealthy(httpClient, addr) {
		return nil
	}
	if err := starter(addr); err != nil {
		return fmt.Errorf("starting daemon: %w", err)
	}
	return waitHealthy(httpClient, addr, 500*time.Millisecond)
}

func postCheck(ctx context.Context, httpClient *http.Client, addr string, request fitness.ValidationRequest) ([]byte, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encoding check request: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(addr, "/")+"/check", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building check request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("posting check request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		message := strings.TrimSpace(string(errBody))
		if message == "" {
			return nil, fmt.Errorf("check failed with HTTP %d", response.StatusCode)
		}
		return nil, fmt.Errorf("check failed with HTTP %d: %s", response.StatusCode, message)
	}
	return io.ReadAll(io.LimitReader(response.Body, 10<<20))
}

func configureTLS(base *http.Client, certPath, keyPath, caPath string) (*http.Client, error) {
	if certPath == "" && keyPath == "" && caPath == "" {
		return base, nil
	}
	if (certPath == "") != (keyPath == "") {
		return nil, usageError{err: errors.New("client validate requires --client-cert and --client-key together")}
	}
	configured := cloneHTTPClient(base)
	transport := cloneTransport(configured)
	tlsConfig, err := buildTLSConfig(transport.TLSClientConfig, certPath, keyPath, caPath)
	if err != nil {
		return nil, err
	}
	transport.TLSClientConfig = tlsConfig
	configured.Transport = transport
	return configured, nil
}

func cloneTransport(httpClient *http.Client) *http.Transport {
	base := http.DefaultTransport.(*http.Transport)
	if httpClient.Transport != nil {
		if transport, ok := httpClient.Transport.(*http.Transport); ok {
			base = transport
		}
	}
	return base.Clone()
}

func buildTLSConfig(existing *tls.Config, certPath, keyPath, caPath string) (*tls.Config, error) {
	tlsConfig := cloneTLSConfig(existing)
	if certPath != "" {
		certificate, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, fmt.Errorf("loading client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	if caPath != "" {
		caContent, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("reading client CA bundle: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caContent) {
			return nil, errors.New("parsing client CA bundle")
		}
		tlsConfig.RootCAs = pool
	}
	return tlsConfig, nil
}

func cloneTLSConfig(existing *tls.Config) *tls.Config {
	if existing != nil {
		return existing.Clone()
	}
	return &tls.Config{MinVersion: tls.VersionTLS12}
}

func cloneHTTPClient(base *http.Client) *http.Client {
	if base == nil {
		return &http.Client{Timeout: 2 * time.Second}
	}
	clone := *base
	return &clone
}

func writeValidationResult(stdout io.Writer, format, repo string, body []byte) error {
	if format != "sarif" {
		_, err := stdout.Write(body)
		return err
	}
	var result fitness.ValidationResult
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("decoding check response: %w", err)
	}
	return json.NewEncoder(stdout).Encode(sarif.Convert(result, repo))
}

// resolveGitDir picks the local working directory used to read file content.
// --repo is the logical name sent to the server (e.g. "relocate"); --git-dir is
// the on-disk git working tree. For git hooks and worktrees these differ. When
// --git-dir is omitted, fall back to --repo so legacy callers that pass a
// filesystem path as --repo keep resolving content relative to it.
func resolveGitDir(gitDir, repo string) string {
	if gitDir != "" {
		return gitDir
	}
	return repo
}

func resolveContent(gitDir, file, explicitContent, contentFile string, staged bool) (string, error) {
	if contentFile != "" {
		output, err := os.ReadFile(contentFile)
		if err != nil {
			return "", fmt.Errorf("reading content file %s: %w", contentFile, err)
		}
		return string(output), nil
	}
	if explicitContent != "" {
		return explicitContent, nil
	}
	if staged {
		return resolveContentFromGit(gitDir, file)
	}
	return resolveContentFromDisk(gitDir, file)
}

func resolveContentFromGit(gitDir, file string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", "-C", gitDir, "show", ":"+file)
	output, err := command.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			return "", fmt.Errorf("reading staged content for %s: %w", file, err)
		}
		return "", fmt.Errorf("reading staged content for %s: %w: %s", file, err, detail)
	}
	return string(output), nil
}

func resolveContentFromDisk(gitDir, file string) (string, error) {
	path := file
	if !filepath.IsAbs(path) {
		path = filepath.Join(gitDir, file)
	}
	output, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return string(output), nil
}

func isHealthy(httpClient *http.Client, addr string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(addr, "/")+"/health", nil)
	if err != nil {
		return false
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func waitHealthy(httpClient *http.Client, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isHealthy(httpClient, addr) {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("daemon at %s did not become healthy within %s", addr, timeout)
}

// StartDaemon starts a detached daemon process using the current executable.
func StartDaemon(addr string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	listenAddr := strings.TrimPrefix(strings.TrimPrefix(addr, "http://"), "https://")
	command := exec.Command(executable, "server", "start", "--addr", listenAddr)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

type usageError struct {
	err error
}

func (e usageError) Error() string {
	return e.err.Error()
}

func (e usageError) Unwrap() error {
	return e.err
}

// IsUsageError reports whether err is a client usage error.
func IsUsageError(err error) bool {
	var target usageError
	return errors.As(err, &target)
}
