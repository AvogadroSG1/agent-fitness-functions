package client

import (
	"bytes"
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

	"github.com/AvogadroSG1/agent-fitness-functions/internal/analyzer"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/installer"
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
	repair     bool
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
	RepoConfigError  string `json:"repo_config_error"`
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
	if cfg.repair {
		root := installer.StateRoot(os.Getenv)
		_ = installer.RepairRoslynAnalyzer(root, "")
	}
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
	repair := flags.Bool("repair", false, "repair missing or corrupt components (e.g. compile Roslyn analyzer if dotnet is available)")
	if err := flags.Parse(args); err != nil {
		return doctorConfig{}, usageError{err: err}
	}
	if _, err := resolveClientTLSMode(*clientCert, *clientKey, *clientCA, ""); err != nil {
		return doctorConfig{}, err
	}
	repoRoot := resolveRepoRoot("", "")
	certDir := resolveDevCertDir()
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
		repair:     *repair,
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
		checkGovernanceRoot(cfg),
		checkRoslynAnalyzer(cfg),
		checkServerReachable(cfg),
	}
	results = append(results, checkPreflight(cfg)...)
	results = append(results, checkValidationPipeline(cfg))
	results = append(results, checkHooksInstalled(cfg)...)
	results = append(results, checkLegacyRepoLocalCerts(cfg)...)
	results = append(results, checkConfigSync(cfg)...)
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

// checkGovernanceRoot reports whether ADR-0007's machine-scoped governance
// root has a resolvable managed certificate generation. It respects the
// AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR selector: when pinned, the selector's
// directory is the "root" being checked and named in the detail, so a
// deliberately pinned selector is never reported as a broken machine
// default. A never-onboarded machine (no managed certs published anywhere)
// is a hard failure — there is nothing for the client to talk to the server
// with — remediated by `client onboard`, which publishes the shared root.
func checkGovernanceRoot(cfg doctorConfig) checkResult {
	const name = "governance root"
	root := governanceRoot()
	if selector := os.Getenv(envDevCertDir); selector != "" {
		root = selector
	}
	switch {
	case cfg.managed:
		// This session already resolved a managed generation once (onboard
		// startup or resolveDoctorConfig's own TLS load, see
		// TestOnboardReusesOneManagedGenerationForStartupAndDoctor) - reuse
		// that result rather than touching the filesystem a second time.
		if cfg.tlsError != nil {
			return checkResult{name: name, detail: cfg.tlsError.Error(), remediation: "agent-fitness-functions client onboard"}
		}
		return checkResult{name: name, detail: fmt.Sprintf("%s (configs: %s)", root, governanceConfigsDir()), passed: true}
	case cfg.clientCert != "" || cfg.clientKey != "" || cfg.clientCA != "":
		// External client TLS material was explicitly supplied for this
		// session (flags or AGENT_FITNESS_FUNCTIONS_CLIENT_*): the machine
		// governance root is not in play, so its local absence is not this
		// session's problem.
		return checkResult{name: name, detail: "external client TLS material configured; machine governance root not used for this session", passed: true}
	default:
		// No TLS resolution has happened yet against this cfg (for example a
		// doctorConfig built without going through resolveDoctorConfig):
		// probe the managed root directly.
		if _, err := resolveManagedVersion(resolveDevCertDir()); err != nil {
			return checkResult{
				name:        name,
				detail:      fmt.Sprintf("no managed certificates published under %s (%v)", root, err),
				remediation: "agent-fitness-functions client onboard",
			}
		}
		return checkResult{name: name, detail: fmt.Sprintf("%s (configs: %s)", root, governanceConfigsDir()), passed: true}
	}
}

func checkRoslynAnalyzer(cfg doctorConfig) checkResult {
	name := "roslyn analyzer"
	roslynPath := analyzer.DefaultRoslynCLI()
	if roslynPath != "" && roslynPath != "calm-roslyn-analyzer" {
		if info, err := os.Stat(roslynPath); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return checkResult{name: name, detail: roslynPath, passed: true}
		}
	}
	if path, err := exec.LookPath(roslynPath); err == nil {
		return checkResult{name: name, detail: path, passed: true}
	}
	if path, err := exec.LookPath("calm-roslyn-analyzer"); err == nil {
		return checkResult{name: name, detail: path, passed: true}
	}
	if path, err := exec.LookPath("CalmRoslynAnalyzer"); err == nil {
		return checkResult{name: name, detail: path, passed: true}
	}
	return checkResult{
		name:        name,
		detail:      "not found or not executable",
		remediation: "run `agent-fitness-functions doctor --repair` (requires dotnet SDK) or set AGENT_FITNESS_FUNCTIONS_ROSLYN_PATH",
		passed:      false,
	}
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
			remediation: "run `agent-fitness-functions client onboard` to start the local daemon, or point --addr at a running server",
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
		// An invalid config is usually valid JSON that breaks a governance
		// invariant, so lead with the server's own reason and never assert the
		// JSON is malformed. Older servers omit the reason; fall back to
		// naming the file the user has to open.
		detail := fmt.Sprintf("repository %q has an invalid config", cfg.repo)
		if report.RepoConfigError != "" {
			detail += ": " + report.RepoConfigError
		}
		return checkResult{
			name:        "repo configured server-side",
			detail:      detail,
			remediation: fmt.Sprintf("correct configs/%s/config.json on the server, then re-run `agent-fitness-functions client onboard`", cfg.repo),
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
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return preflightReport{}, response.StatusCode, nil
	}
	var report preflightReport
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&report); err != nil {
		return preflightReport{}, response.StatusCode, fmt.Errorf("decoding preflight response: %w", err)
	}
	return report, response.StatusCode, nil
}

// checkValidationPipeline executes a synthetic POST /check request to verify that the
// end-to-end governance validation pipeline (AST parsing, CALM validator, fitness
// scoring) is responsive and functioning, measuring the check latency.
func checkValidationPipeline(cfg doctorConfig) checkResult {
	const name = "validation pipeline"
	if cfg.repo == "" {
		return checkResult{
			name:        name,
			detail:      "could not determine repository name",
			remediation: "run doctor from within a governed repository or pass --repo",
		}
	}
	configured, err := doctorHTTPClient(cfg)
	if err != nil {
		return checkResult{
			name:        name,
			detail:      err.Error(),
			remediation: "fix the client TLS material, then re-run doctor",
		}
	}
	start := time.Now()
	req := fitness.ValidationRequest{
		Repo:            cfg.repo,
		File:            "internal/doctor/synthetic_check.go",
		ProposedContent: "package doctor\n",
		Language:        "go",
	}
	body, err := postCheck(context.Background(), configured, cfg.addr, req, 10*time.Second)
	latency := time.Since(start)
	if err != nil {
		var statusErr httpStatusError
		if errors.As(err, &statusErr) {
			detail := fmt.Sprintf("POST /check returned HTTP %d", statusErr.status)
			if statusErr.body != "" {
				detail += ": " + statusErr.body
			}
			return checkResult{
				name:        name,
				detail:      detail,
				remediation: "check server logs; verify CALM CLI, analyzers, and repository configs are functional",
			}
		}
		if isTimeoutError(err) {
			return checkResult{
				name:        name,
				detail:      fmt.Sprintf("POST /check timed out after %v", latency.Round(time.Millisecond)),
				remediation: "increase client timeout with AGENT_FITNESS_FUNCTIONS_CLIENT_TIMEOUT or tune analyzer performance",
			}
		}
		return checkResult{
			name:        name,
			detail:      fmt.Sprintf("POST /check failed (%v): %v", latency.Round(time.Millisecond), err),
			remediation: "check server logs; ensure daemon is running and reachable",
		}
	}
	var res fitness.ValidationResult
	if err := json.Unmarshal(body, &res); err != nil {
		return checkResult{
			name:        name,
			detail:      fmt.Sprintf("POST /check returned invalid JSON (%v): %v", latency.Round(time.Millisecond), err),
			remediation: "check server logs",
		}
	}
	detail := fmt.Sprintf("POST /check OK (%v)", latency.Round(time.Millisecond))
	if res.Status != "" {
		detail = fmt.Sprintf("POST /check OK (%v, status: %s)", latency.Round(time.Millisecond), res.Status)
	}
	return checkResult{
		name:   name,
		detail: detail,
		passed: true,
	}
}

func doctorHTTPClient(cfg doctorConfig) (*http.Client, error) {
	if cfg.tlsError != nil {
		return nil, cfg.tlsError
	}
	client := cfg.httpClient
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	if cfg.tlsLoaded {
		return client, nil
	}
	return configureTLS(client, cfg.clientCert, cfg.clientKey, cfg.clientCA)
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
		codexHookResult(cfg.repoRoot),
		openCodePluginResult(cfg.repoRoot),
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

// forgeClobberExplanation explains why a governed PreToolUse entry is missing from
// both .claude/settings.json and .claude/settings.local.json when settings.json is
// forge-managed (ADR-0006 "forge interop"): forge's own `forge upgrade` command
// overwrites settings.json wholesale on every managed-file sync, so install-hooks (WP2)
// redirects new entries to settings.local.json instead — but a repo onboarded before
// that redirect existed, or one whose settings.local.json was deleted, would otherwise
// see doctor repeat "run install-hooks" without explaining why the entry keeps
// disappearing.
const forgeClobberExplanation = "forge upgrade rewrites .claude/settings.json — re-run agent-fitness-functions client install-hooks (entries are kept in .claude/settings.local.json)"

// claudeHookEntryLocation checks .claude/settings.json, then .claude/settings.local.json,
// for a PreToolUse entry matching markers (as installed by applyClaudeHook). It returns
// the relative path of whichever file the entry was found in (for use in a check's detail
// line) and the entry's raw command string, so the caller can resolve and stat the script
// the command actually points at instead of trusting the marker match alone — a marker
// match only proves an entry with the right hook NAME is present, not that the script it
// names exists on this machine (e.g. a fresh clone that pulled the tracked settings.json
// but never ran install-hooks).
func claudeHookEntryLocation(repoRoot string, markers []string) (file, command string, found bool, err error) {
	for _, name := range []string{"settings.json", "settings.local.json"} {
		relative := filepath.Join(".claude", name)
		settings, loadErr := loadClaudeSettings(filepath.Join(repoRoot, relative))
		if loadErr != nil {
			return "", "", false, loadErr
		}
		entries := preToolUseEntries(ensureHooksSection(settings))
		if _, matchedCommand, ok := findClaudeHookEntry(entries, markers); ok {
			return relative, matchedCommand, true, nil
		}
	}
	return "", "", false, nil
}

// gitPathArgFromCommand extracts the "hooks/<name>" argument from a portable command of
// the form `"$(git rev-parse --git-path hooks/<name>)"`, as written by
// portableHookCommand. ok is false for any other command shape (e.g. a literal path a
// pre-fix install left behind), which the caller falls back to resolving as a literal
// path instead.
func gitPathArgFromCommand(command string) (arg string, ok bool) {
	const marker = "--git-path "
	index := strings.Index(command, marker)
	if index == -1 {
		return "", false
	}
	rest := command[index+len(marker):]
	if end := strings.IndexAny(rest, ")\""); end != -1 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest), true
}

// resolveHookCommandPath turns a PreToolUse command string into the filesystem path
// doctor should stat: the portable `git rev-parse --git-path` form is resolved the same
// way gitHookPath resolves an installed script's write target (including under a
// core.hooksPath redirect), and any other shape is treated as a literal path left by a
// pre-fix install, relative to repoRoot when it is not already absolute. This is what
// makes a fresh clone that carries the tracked settings.json entry but never ran
// install-hooks resolve to a path that does not exist yet, instead of doctor trusting the
// marker substring alone.
func resolveHookCommandPath(repoRoot, command string) (string, error) {
	if gitPathArg, ok := gitPathArgFromCommand(command); ok {
		resolved, err := gitOutput(repoRoot, "rev-parse", "--git-path", gitPathArg)
		if err != nil {
			return "", err
		}
		return joinRelativeToRepo(repoRoot, resolved), nil
	}
	return joinRelativeToRepo(repoRoot, strings.Trim(command, `"`)), nil
}

func joinRelativeToRepo(repoRoot, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(repoRoot, path)
}

// hookCommandScriptMissing reports whether the script a PreToolUse command resolves to
// is absent from this machine. A resolution error (e.g. gitOutput failing) counts as
// missing too, since doctor cannot confirm the script exists.
func hookCommandScriptMissing(repoRoot, command string) bool {
	path, err := resolveHookCommandPath(repoRoot, command)
	if err != nil {
		return true
	}
	_, statErr := os.Stat(path)
	return statErr != nil
}

func gitGuardSettingsResult(repoRoot string) checkResult {
	name := "agent git-guard hook"
	file, command, found, err := claudeHookEntryLocation(repoRoot, gitGuardNameHistory)
	if err != nil {
		return checkResult{name: name, detail: err.Error(), remediation: "agent-fitness-functions client install-hooks"}
	}
	if !found || hookCommandScriptMissing(repoRoot, command) {
		remediation := "agent-fitness-functions client install-hooks"
		if forgeManagedClaudeSettings(filepath.Join(repoRoot, ".claude", "settings.json")) {
			remediation = forgeClobberExplanation
		}
		detail := "no Bash git-guard PreToolUse entry in .claude/settings.json or .claude/settings.local.json"
		if found {
			detail = "PreToolUse entry configured in " + file + " but its script is not installed on this machine"
		}
		return checkResult{
			name:        name,
			detail:      detail,
			remediation: remediation,
		}
	}
	return checkResult{name: name, detail: "configured in " + file, passed: true}
}

// editWriteHookResult reports the Edit|Write content-validation PreToolUse entry as
// informational: present is a ✔, absent is an advisory ⚠ (never a hard failure) so
// this check does not assume the entry is installed on repos that only ran an older
// install-hooks.
func editWriteHookResult(repoRoot string) checkResult {
	name := "agent Edit/Write hook (optional)"
	file, command, found, err := claudeHookEntryLocation(repoRoot, agentHookNameHistory)
	if err != nil {
		return checkResult{name: name, detail: err.Error(), warning: true}
	}
	if found && !hookCommandScriptMissing(repoRoot, command) {
		return checkResult{name: name, detail: "configured in " + file, passed: true}
	}
	detail := "no Edit|Write PreToolUse entry (pre-write architecture validation not wired)"
	switch {
	case found:
		detail = "PreToolUse entry configured in " + file + " but its script is not installed on this machine"
	case forgeManagedClaudeSettings(filepath.Join(repoRoot, ".claude", "settings.json")):
		detail = forgeClobberExplanation
	}
	return checkResult{
		name:    name,
		detail:  detail,
		warning: true,
	}
}

func codexHookResult(repoRoot string) checkResult {
	name := "codex PreToolUse hooks (optional)"
	codexHooksPath := filepath.Join(repoRoot, ".codex", "hooks.json")
	settings, err := loadClaudeSettings(codexHooksPath)
	if err != nil {
		return checkResult{name: name, detail: err.Error(), warning: true}
	}
	entries := preToolUseEntries(ensureHooksSection(settings))
	_, command, found := findClaudeHookEntry(entries, gitGuardNameHistory)
	if !found {
		_, command, found = findClaudeHookEntry(entries, agentHookNameHistory)
	}
	if found && !hookCommandScriptMissing(repoRoot, command) {
		return checkResult{name: name, detail: "configured in .codex/hooks.json", passed: true}
	}
	if found {
		return checkResult{
			name:        name,
			detail:      "PreToolUse configured in .codex/hooks.json but script is not installed on this machine",
			remediation: "agent-fitness-functions client install-hooks",
			warning:     true,
		}
	}
	return checkResult{
		name:    name,
		detail:  "no PreToolUse entries in .codex/hooks.json",
		warning: true,
	}
}

func openCodePluginResult(repoRoot string) checkResult {
	name := "opencode plugin (optional)"
	pluginPath := filepath.Join(repoRoot, ".opencode", "plugins", "agent-fitness-functions.js")
	content, err := os.ReadFile(pluginPath)
	if err != nil {
		return checkResult{
			name:    name,
			detail:  "plugin not installed at .opencode/plugins/agent-fitness-functions.js",
			warning: true,
		}
	}
	if !strings.Contains(string(content), "tool.execute.before") {
		return checkResult{
			name:        name,
			detail:      "plugin file present but missing tool.execute.before",
			remediation: "agent-fitness-functions client install-hooks",
			warning:     true,
		}
	}
	return checkResult{
		name:   name,
		detail: "installed at .opencode/plugins/agent-fitness-functions.js",
		passed: true,
	}
}

// checkLegacyRepoLocalCerts flags the pre-ADR-0007 <repo>/certs/current
// managed-cert symlink. It is advisory-only dead weight now that every repo
// shares one machine governance root: a repo that still has this layout was
// onboarded before the shared root existed (or never re-onboarded since),
// not one that is broken. Repos onboarded under ADR-0007 never create this
// path, so its absence produces no result at all.
func checkLegacyRepoLocalCerts(cfg doctorConfig) []checkResult {
	if cfg.repoRoot == "" {
		return nil
	}
	legacyCurrent := filepath.Join(cfg.repoRoot, "certs", "current")
	if _, err := os.Lstat(legacyCurrent); err != nil {
		return nil
	}
	return []checkResult{{
		name:        "legacy repo-local certs",
		detail:      legacyCurrent + " still exists from a pre-ADR-0007 layout",
		remediation: "re-run `agent-fitness-functions client onboard`, then delete this directory - it is safe to remove once the shared governance root is healthy",
		warning:     true,
	}}
}

// checkConfigSync compares the tracked repo-local
// configs/<repo>/config.json (the production handoff source of truth) against
// its shared-root copy under governanceConfigsDir(). A repo with no
// repo-local config has nothing to sync and produces no result - un-onboarded
// repos should not be spammed with a check they cannot yet satisfy.
func checkConfigSync(cfg doctorConfig) []checkResult {
	if cfg.repoRoot == "" || cfg.repo == "" {
		return nil
	}
	localPath := filepath.Join(cfg.repoRoot, "configs", cfg.repo, "config.json")
	local, err := os.ReadFile(localPath)
	if err != nil {
		return nil
	}
	sharedPath := filepath.Join(governanceConfigsDir(), cfg.repo, "config.json")
	shared, err := os.ReadFile(sharedPath)
	name := "config sync"
	remediation := "re-run `agent-fitness-functions client onboard` to sync the repo-local config into the shared governance root"
	if err != nil {
		return []checkResult{{
			name:        name,
			detail:      fmt.Sprintf("%s is not registered under the shared governance root (%s)", localPath, sharedPath),
			remediation: remediation,
			warning:     true,
		}}
	}
	if !bytes.Equal(local, shared) {
		return []checkResult{{
			name:        name,
			detail:      fmt.Sprintf("%s differs from the shared copy %s", localPath, sharedPath),
			remediation: remediation,
			warning:     true,
		}}
	}
	return []checkResult{{name: name, detail: sharedPath, passed: true}}
}
