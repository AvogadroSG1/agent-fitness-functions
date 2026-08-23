package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
)

// doctorConfig holds the resolved inputs for a doctor run. Everything is injectable so
// the individual checks stay unit-testable without a real repo, server, or filesystem.
type doctorConfig struct {
	addr       string
	repo       string
	repoRoot   string
	clientCert string
	clientKey  string
	clientCA   string
	managed    bool
	clientLeaf *x509.Certificate
	rootCAs    *x509.CertPool
	tlsLoaded  bool
	tlsError   error
	httpClient *http.Client
}

// checkResult is one ordered ✔/✘ line. A warning is an advisory result that prints
// with ⚠ but does not fail the overall run.
type checkResult struct {
	name        string
	detail      string
	remediation string
	passed      bool
	warning     bool
}

// preflightReport mirrors server.PreflightResponse. It is duplicated here rather than
// imported so the client does not depend on the server package.
type preflightReport struct {
	AuthenticatedCN  string `json:"authenticated_cn"`
	RepoConfigured   bool   `json:"repo_configured"`
	RepoConfigValid  bool   `json:"repo_config_valid"`
	CallerAuthorized bool   `json:"caller_authorized"`
	EnforcementMode  string `json:"enforcement_mode"`
}

// RunDoctor answers "is my setup ready?" as an ordered set of ✔/✘ checks, each with a
// one-line remediation on failure. It continues through failures and returns a
// non-nil error when any non-advisory check failed, so main maps that to a non-zero
// exit code.
func RunDoctor(args []string, stdout, stderr io.Writer, httpClient *http.Client) error {
	cfg, err := resolveDoctorConfig(args, httpClient)
	if err != nil {
		return err
	}
	return runDoctorWithConfig(cfg, stdout)
}

func runDoctorWithConfig(cfg doctorConfig, stdout io.Writer) error {
	results := runDoctorChecks(cfg)
	failures := 0
	for _, result := range results {
		printCheckResult(stdout, result)
		if !result.passed && !result.warning {
			failures++
		}
	}
	if failures > 0 {
		return fmt.Errorf("doctor found %d problem(s); see remediation lines above", failures)
	}
	_, _ = fmt.Fprintln(stdout, "\nAll checks passed: this repository is ready for governance.")
	return nil
}

func resolveDoctorConfig(args []string, httpClient *http.Client) (doctorConfig, error) {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	addr := flags.String("addr", "https://127.0.0.1:7890", "governance daemon base URL")
	repo := flags.String("repo", "", "repository name (defaults to the git working-tree basename)")
	clientCert := flags.String("client-cert", "", "mTLS client certificate path")
	clientKey := flags.String("client-key", "", "mTLS client private key path")
	clientCA := flags.String("client-ca", "", "server CA bundle path")
	if err := flags.Parse(args); err != nil {
		return doctorConfig{}, usageError{err: err}
	}
	if _, err := resolveClientTLSMode(*clientCert, *clientKey, *clientCA, ""); err != nil {
		return doctorConfig{}, err
	}
	repoRoot := resolveRepoRoot("", "")
	certDir := resolveDevCertDir(repoRoot)
	mode, err := resolveClientTLSMode(*clientCert, *clientKey, *clientCA, certDir)
	if err != nil {
		return doctorConfig{}, err
	}
	material, err := loadClientTLSMode(mode, false)
	tlsErr := err
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 3 * time.Second}
	}
	if tlsErr == nil {
		httpClient, tlsErr = configureClientTLSMaterial(httpClient, material)
	}
	cert, key, ca := mode.cert, mode.key, mode.ca
	var leaf *x509.Certificate
	if mode.managed && tlsErr == nil {
		paths := material.version.Paths()
		cert, key, ca = paths.ClientCertificate, paths.ClientKey, paths.CA
		leaf = material.certificate.Leaf
	}
	return doctorConfig{
		addr:       *addr,
		repo:       resolveDoctorRepo(*repo, repoRoot),
		repoRoot:   repoRoot,
		clientCert: cert,
		clientKey:  key,
		clientCA:   ca,
		managed:    mode.managed,
		clientLeaf: leaf,
		rootCAs:    material.roots,
		tlsLoaded:  tlsErr == nil,
		tlsError:   tlsErr,
		httpClient: httpClient,
	}, nil
}

// resolveDoctorRepo derives the repository name the server keys its config on: the
// basename of the working-tree root, matching how the shell hooks name the repo.
func resolveDoctorRepo(repoFlag, repoRoot string) string {
	if repoFlag != "" {
		return repoFlag
	}
	if repoRoot == "" {
		return ""
	}
	return filepath.Base(repoRoot)
}

// runDoctorChecks executes every check in order. Offline checks run first so a stopped
// server never hides a missing binary dependency or cert problem.
func runDoctorChecks(cfg doctorConfig) []checkResult {
	results := []checkResult{
		checkBinary(),
		checkPython3(),
		checkPyYAML(),
		checkClientCertificate(cfg),
		checkServerCABundle(cfg),
		checkServerReachable(cfg),
	}
	results = append(results, checkPreflight(cfg)...)
	results = append(results, checkHooksInstalled(cfg)...)
	return results
}

func printCheckResult(stdout io.Writer, result checkResult) {
	mark := "✘"
	switch {
	case result.passed:
		mark = "✔"
	case result.warning:
		mark = "⚠"
	}
	line := mark + " " + result.name
	if result.detail != "" {
		line += ": " + result.detail
	}
	_, _ = fmt.Fprintln(stdout, line)
	if !result.passed && result.remediation != "" {
		_, _ = fmt.Fprintln(stdout, "    → "+result.remediation)
	}
}

func checkBinary() checkResult {
	path, err := os.Executable()
	if err != nil {
		path = "agent-fitness-functions"
	}
	detail := path
	if revision := buildRevision(); revision != "" {
		detail = path + " (" + revision + ")"
	}
	return checkResult{name: "binary", detail: detail, passed: true}
}

func buildRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return truncateRevision(setting.Value, 12)
		}
	}
	return ""
}

func truncateRevision(value string, length int) string {
	if len(value) > length {
		return value[:length]
	}
	return value
}

func checkPython3() checkResult {
	path, err := exec.LookPath("python3")
	if err != nil {
		return checkResult{
			name:        "python3",
			detail:      "not found on PATH",
			remediation: "install python3 (the commit hooks and violation formatter require it)",
		}
	}
	return checkResult{name: "python3", detail: path, passed: true}
}

func checkPyYAML() checkResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "python3", "-c", "import yaml").Run(); err != nil {
		return checkResult{
			name:        "pyyaml",
			detail:      "python3 cannot import yaml",
			remediation: "python3 -m pip install -r hooks/requirements.txt",
		}
	}
	return checkResult{name: "pyyaml", detail: "importable", passed: true}
}

func checkClientCertificate(cfg doctorConfig) checkResult {
	if cfg.tlsError != nil {
		return checkResult{name: "client certificate", detail: cfg.tlsError.Error(), remediation: "regenerate certs with scripts/generate-dev-certs.sh --force"}
	}
	if cfg.clientCert == "" || cfg.clientKey == "" {
		return checkResult{
			name:        "client certificate",
			detail:      "no client cert/key resolved",
			remediation: "run scripts/generate-dev-certs.sh, or set AGENT_FITNESS_FUNCTIONS_CLIENT_CERT and AGENT_FITNESS_FUNCTIONS_CLIENT_KEY",
		}
	}
	leaf := cfg.clientLeaf
	var err error
	if leaf == nil {
		leaf, err = loadClientLeaf(cfg.clientCert, cfg.clientKey)
	}
	if err != nil {
		return checkResult{
			name:        "client certificate",
			detail:      err.Error(),
			remediation: "regenerate certs with scripts/generate-dev-certs.sh --force",
		}
	}
	return clientCertificateResult(cfg.clientCert, leaf)
}

func clientCertificateResult(certPath string, leaf *x509.Certificate) checkResult {
	if time.Until(leaf.NotAfter) <= 0 {
		return checkResult{
			name:        "client certificate",
			detail:      fmt.Sprintf("CN=%s expired at %s", leaf.Subject.CommonName, leaf.NotAfter.Format(time.RFC3339)),
			remediation: "regenerate certs with scripts/generate-dev-certs.sh --force",
		}
	}
	return checkResult{
		name:   "client certificate",
		detail: fmt.Sprintf("CN=%s valid until %s (%s)", leaf.Subject.CommonName, leaf.NotAfter.Format(time.RFC3339), certPath),
		passed: true,
	}
}

func loadClientLeaf(certPath, keyPath string) (*x509.Certificate, error) {
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("cannot load client cert/key: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return nil, errors.New("client certificate contains no certificates")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("cannot parse client certificate: %w", err)
	}
	return leaf, nil
}

func checkServerCABundle(cfg doctorConfig) checkResult {
	if cfg.tlsError != nil {
		return checkResult{name: "server CA bundle", detail: cfg.tlsError.Error(), remediation: "regenerate certs with scripts/generate-dev-certs.sh --force"}
	}
	if cfg.clientCA == "" {
		return checkResult{
			name:        "server CA bundle",
			detail:      "no CA bundle resolved",
			remediation: "run scripts/generate-dev-certs.sh, or set AGENT_FITNESS_FUNCTIONS_CLIENT_CA",
		}
	}
	if cfg.managed && cfg.rootCAs != nil {
		return checkResult{name: "server CA bundle", detail: cfg.clientCA, passed: true}
	}
	content, err := os.ReadFile(cfg.clientCA)
	if err != nil {
		return checkResult{
			name:        "server CA bundle",
			detail:      err.Error(),
			remediation: "run scripts/generate-dev-certs.sh, or set AGENT_FITNESS_FUNCTIONS_CLIENT_CA to a readable CA bundle",
		}
	}
	if !x509.NewCertPool().AppendCertsFromPEM(content) {
		return checkResult{
			name:        "server CA bundle",
			detail:      "no PEM certificates found in " + cfg.clientCA,
			remediation: "regenerate certs with scripts/generate-dev-certs.sh --force",
		}
	}
	return checkResult{name: "server CA bundle", detail: cfg.clientCA, passed: true}
}

func checkServerReachable(cfg doctorConfig) checkResult {
	configured, err := doctorHTTPClient(cfg)
	if err != nil {
		return checkResult{name: "server reachable", detail: err.Error(), remediation: "fix the client TLS material, then re-run doctor"}
	}
	if !isHealthy(configured, cfg.addr) {
		return checkResult{
			name:        "server reachable",
			detail:      "GET " + strings.TrimRight(cfg.addr, "/") + "/health did not return 200",
			remediation: "start the governance server (docker compose up --build) or point --addr at a running daemon",
		}
	}
	return checkResult{name: "server reachable", detail: cfg.addr + "/health OK", passed: true}
}

// checkPreflight performs the authenticated GET /preflight?repo= request and expands
// the JSON facts into distinct check lines with distinct remediations.
func checkPreflight(cfg doctorConfig) []checkResult {
	report, status, err := fetchPreflight(cfg)
	if err != nil {
		return []checkResult{{
			name:        "server authentication",
			detail:      err.Error(),
			remediation: "confirm the server is reachable over TLS with your client certificate",
		}}
	}
	if result, useFacts := preflightStatusResult(status); !useFacts {
		return []checkResult{result}
	}
	return preflightCheckResults(cfg, report)
}

// preflightStatusResult maps a non-200 preflight status to a failing check result. It
// returns useFacts=true only when the status is 200 and the JSON facts should be used.
func preflightStatusResult(status int) (checkResult, bool) {
	switch status {
	case http.StatusOK:
		return checkResult{}, true
	case http.StatusUnauthorized:
		return checkResult{
			name:        "server authentication",
			detail:      "server returned 401 (client certificate not accepted)",
			remediation: "regenerate certs with scripts/generate-dev-certs.sh --force, or set AGENT_FITNESS_FUNCTIONS_CLIENT_CERT/KEY/CA to a trusted pair",
		}, false
	default:
		return checkResult{
			name:        "server authentication",
			detail:      fmt.Sprintf("GET /preflight returned HTTP %d", status),
			remediation: "check the server logs; --repo may be malformed",
		}, false
	}
}

func preflightCheckResults(cfg doctorConfig, report preflightReport) []checkResult {
	return []checkResult{
		{name: "server authentication", detail: "authenticated as CN=" + report.AuthenticatedCN, passed: true},
		repoConfiguredResult(cfg, report),
		callerAuthorizedResult(cfg, report),
		enforcementModeResult(report),
	}
}

func repoConfiguredResult(cfg doctorConfig, report preflightReport) checkResult {
	if !report.RepoConfigured {
		return checkResult{
			name:        "repo configured server-side",
			detail:      fmt.Sprintf("repository %q is not configured", cfg.repo),
			remediation: fmt.Sprintf("create configs/%s/config.json on the server - see docs/runbooks/onboard-new-repository.md", cfg.repo),
		}
	}
	if !report.RepoConfigValid {
		return checkResult{
			name:        "repo configured server-side",
			detail:      fmt.Sprintf("repository %q has an invalid config", cfg.repo),
			remediation: fmt.Sprintf("fix the JSON in configs/%s/config.json on the server", cfg.repo),
		}
	}
	return checkResult{name: "repo configured server-side", detail: cfg.repo, passed: true}
}

func callerAuthorizedResult(cfg doctorConfig, report preflightReport) checkResult {
	if !report.CallerAuthorized {
		return checkResult{
			name:        "caller authorized for repo",
			detail:      fmt.Sprintf("CN=%s is not authorized for %q", report.AuthenticatedCN, cfg.repo),
			remediation: fmt.Sprintf("add CN %s to caller-repos.json for %s on the server", report.AuthenticatedCN, cfg.repo),
		}
	}
	return checkResult{name: "caller authorized for repo", detail: "CN=" + report.AuthenticatedCN, passed: true}
}

func enforcementModeResult(report preflightReport) checkResult {
	if report.EnforcementMode == "" {
		return checkResult{
			name:    "enforcement mode",
			detail:  "unknown (repo not configured or invalid)",
			warning: true,
		}
	}
	return checkResult{name: "enforcement mode", detail: report.EnforcementMode, passed: true}
}

func fetchPreflight(cfg doctorConfig) (preflightReport, int, error) {
	if cfg.repo == "" {
		return preflightReport{}, 0, errors.New("could not determine repository name (run inside a git working tree or pass --repo)")
	}
	configured, err := doctorHTTPClient(cfg)
	if err != nil {
		return preflightReport{}, 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	target := strings.TrimRight(cfg.addr, "/") + "/preflight?repo=" + cfg.repo
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return preflightReport{}, 0, err
	}
	response, err := configured.Do(request)
	if err != nil {
		return preflightReport{}, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return preflightReport{}, response.StatusCode, nil
	}
	var report preflightReport
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&report); err != nil {
		return preflightReport{}, response.StatusCode, fmt.Errorf("decoding preflight response: %w", err)
	}
	return report, response.StatusCode, nil
}

func doctorHTTPClient(cfg doctorConfig) (*http.Client, error) {
	if cfg.tlsError != nil {
		return nil, cfg.tlsError
	}
	if cfg.tlsLoaded {
		return cfg.httpClient, nil
	}
	return configureTLS(cfg.httpClient, cfg.clientCert, cfg.clientKey, cfg.clientCA)
}

// checkHooksInstalled reports the managed git hooks and the .claude/settings.json
// PreToolUse entries. The Bash git-guard is a required check; the Edit|Write
// content-validation hook is reported informationally so this check stays correct
// whether or not that entry has been installed yet.
func checkHooksInstalled(cfg doctorConfig) []checkResult {
	if cfg.repoRoot == "" {
		return []checkResult{{
			name:        "hooks installed",
			detail:      "not inside a git working tree",
			remediation: "run doctor from within the governed repository",
		}}
	}
	return []checkResult{
		gitHookResult(cfg.repoRoot, "pre-commit"),
		gitHookResult(cfg.repoRoot, "pre-push"),
		gitGuardSettingsResult(cfg.repoRoot),
		editWriteHookResult(cfg.repoRoot),
	}
}

func gitHookResult(repoRoot, hookName string) checkResult {
	name := "git " + hookName + " hook"
	path, err := gitOutput(repoRoot, "rev-parse", "--git-path", "hooks/"+hookName)
	if err != nil {
		return checkResult{name: name, detail: err.Error(), remediation: "agent-fitness-functions client install-hooks"}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoRoot, path)
	}
	content, err := os.ReadFile(path)
	if err != nil || !hookIsManaged(content, hookName) {
		return checkResult{
			name:        name,
			detail:      "not installed (missing managed marker)",
			remediation: "agent-fitness-functions client install-hooks",
		}
	}
	return checkResult{name: name, detail: path, passed: true}
}

func gitGuardSettingsResult(repoRoot string) checkResult {
	name := "agent git-guard hook"
	settings, err := loadClaudeSettings(filepath.Join(repoRoot, ".claude", "settings.json"))
	if err != nil {
		return checkResult{name: name, detail: err.Error(), remediation: "agent-fitness-functions client install-hooks"}
	}
	entries := preToolUseEntries(ensureHooksSection(settings))
	if _, _, found := findClaudeHookEntry(entries, gitGuardNameHistory); !found {
		return checkResult{
			name:        name,
			detail:      "no Bash git-guard PreToolUse entry in .claude/settings.json",
			remediation: "agent-fitness-functions client install-hooks",
		}
	}
	return checkResult{name: name, detail: "configured in .claude/settings.json", passed: true}
}

// editWriteHookResult reports the Edit|Write content-validation PreToolUse entry as
// informational: present is a ✔, absent is an advisory ⚠ (never a hard failure) so
// this check does not assume the entry is installed on repos that only ran an older
// install-hooks.
func editWriteHookResult(repoRoot string) checkResult {
	name := "agent Edit/Write hook (optional)"
	settings, err := loadClaudeSettings(filepath.Join(repoRoot, ".claude", "settings.json"))
	if err != nil {
		return checkResult{name: name, detail: err.Error(), warning: true}
	}
	entries := preToolUseEntries(ensureHooksSection(settings))
	if _, _, found := findClaudeHookEntry(entries, agentHookNameHistory); found {
		return checkResult{name: name, detail: "configured in .claude/settings.json", passed: true}
	}
	return checkResult{
		name:    name,
		detail:  "no Edit|Write PreToolUse entry (pre-write architecture validation not wired)",
		warning: true,
	}
}
