package client

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

//go:embed configtemplates/*
var embeddedConfigTemplates embed.FS

const (
	defaultOnboardAddr         = "https://127.0.0.1:7890"
	callerRepoBindingsFileName = "caller-repos.json"
	// repoNameRule is the server's repository-name grammar (internal/server/config.go
	// repoNamePattern), duplicated for actionable client-side error messages.
	repoNameRule = "^[a-z][a-z0-9_-]{0,63}$"
)

var onboardRepoNamePattern = regexp.MustCompile(repoNameRule)

// fitnessFunctionKeys are the five governance functions the scaffolded config
// enables explicitly. They mirror the keys accepted by the server's
// normalizeFitnessFunctions (internal/server/config.go), duplicated here so the
// client package never depends on internal/server.
var fitnessFunctionKeys = []string{
	"cyclomatic-complexity",
	"interface-width",
	"implementation-depth",
	"logic-density",
	"dependency-discipline",
}

// onboarder holds the resolved inputs for a single onboard run. Dependencies are
// injected (like doctor.go) so the individual steps stay unit-testable.
type onboarder struct {
	repoName             string
	repoRoot             string
	enforcement          string
	addr                 string
	configsDir           string
	certDir              string
	stdout               io.Writer
	stderr               io.Writer
	httpClient           *http.Client
	starter              func(DaemonStartConfig) error
	certificatesOnly     bool
	forceDevCertRotation bool
}

// RunOnboard performs the whole local 0-to-governed sequence in one command:
// repo-name detection, dev certs, server-side config scaffold, caller
// authorization, hook installation, local daemon auto-start, and a final doctor
// gate. It returns a non-nil error (mapped to a non-zero exit) when any step or
// the closing doctor run fails; advisory doctor warnings are not failures.
func RunOnboard(args []string, stdout, stderr io.Writer, httpClient *http.Client, starter func(DaemonStartConfig) error) error {
	o, err := resolveOnboarder(args, stdout, stderr, httpClient, starter)
	if err != nil {
		return err
	}
	if o.certificatesOnly {
		return ensureDevCerts(o.certDir, o.forceDevCertRotation)
	}
	return o.run()
}

func resolveOnboarder(args []string, stdout, stderr io.Writer, httpClient *http.Client, starter func(DaemonStartConfig) error) (onboarder, error) {
	flags := flag.NewFlagSet("client onboard", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	repo := flags.String("repo", "", "governance repository name (defaults to the git working-tree basename)")
	enforcement := flags.String("enforcement", "advisory", "initial enforcement mode: advisory or block")
	addr := flags.String("addr", defaultOnboardAddr, "governance daemon base URL")
	certificatesOnly := flags.Bool("certificates-only", false, "publish managed development certificates only")
	forceDevCertRotation := flags.Bool("force-dev-cert-rotation", false, "request managed development certificate rotation")
	if err := flags.Parse(args); err != nil {
		return onboarder{}, usageError{err: err}
	}
	if *certificatesOnly {
		if len(flags.Args()) != 0 {
			return onboarder{}, usageError{err: errors.New("client onboard --certificates-only accepts no repository path")}
		}
		certDir := os.Getenv(envDevCertDir)
		if certDir == "" {
			workingDir, err := os.Getwd()
			if err != nil {
				return onboarder{}, fmt.Errorf("resolve development certificate directory: %w", err)
			}
			if root := resolveRepoRoot("", ""); root != "" {
				workingDir = root
			}
			certDir = filepath.Join(workingDir, "certs")
		}
		return onboarder{certDir: certDir, certificatesOnly: true, forceDevCertRotation: *forceDevCertRotation}, nil
	}
	mode, err := validateEnforcement(*enforcement)
	if err != nil {
		return onboarder{}, err
	}
	pathArg, err := onboardPathArg(flags.Args())
	if err != nil {
		return onboarder{}, err
	}
	repoRoot, err := onboardRepoRoot(pathArg)
	if err != nil {
		return onboarder{}, err
	}
	repoName, err := resolveOnboardRepoName(*repo, repoRoot)
	if err != nil {
		return onboarder{}, err
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 3 * time.Second}
	}
	return onboarder{
		repoName:             repoName,
		repoRoot:             repoRoot,
		enforcement:          mode,
		addr:                 *addr,
		configsDir:           onboardConfigsDir(repoRoot),
		certDir:              resolveDevCertDir(repoRoot),
		stdout:               stdout,
		stderr:               stderr,
		httpClient:           httpClient,
		starter:              starter,
		forceDevCertRotation: *forceDevCertRotation,
	}, nil
}

// onboardRepoRoot requires a real git working tree: onboard scaffolds certs,
// configs, and caller bindings, so a plain directory must be rejected before
// any files are created rather than failing midway through install-hooks.
// resolveRepoRoot alone is too lenient here — it accepts any existing
// directory to support cert discovery for validate.
func onboardRepoRoot(pathArg string) (string, error) {
	if root := resolveRepoRoot(pathArg, ""); root != "" {
		if top, err := gitOutput(root, "rev-parse", "--show-toplevel"); err == nil {
			return top, nil
		}
	}
	return "", errors.New("could not locate a git working tree: run onboard inside the repository or pass its path")
}

func validateEnforcement(mode string) (string, error) {
	switch mode {
	case "advisory", "block":
		return mode, nil
	default:
		return "", usageError{err: fmt.Errorf("unsupported enforcement mode %q: use advisory or block", mode)}
	}
}

func onboardPathArg(args []string) (string, error) {
	if len(args) > 1 {
		return "", usageError{err: errors.New("client onboard accepts at most one repository path argument")}
	}
	if len(args) == 1 {
		return args[0], nil
	}
	return ".", nil
}

// resolveOnboardRepoName prefers --repo, else the working-tree basename, and
// validates the result against the server's repo-name grammar so a bad basename
// fails with an actionable "pass --repo" message rather than a later 400.
func resolveOnboardRepoName(repoFlag, repoRoot string) (string, error) {
	name := repoFlag
	fromFlag := true
	if name == "" {
		name = filepath.Base(repoRoot)
		fromFlag = false
	}
	if onboardRepoNamePattern.MatchString(name) {
		return name, nil
	}
	if fromFlag {
		return "", usageError{err: fmt.Errorf("invalid repository name %q: must match %s", name, repoNameRule)}
	}
	return "", usageError{err: fmt.Errorf("working-tree basename %q is not a valid repository name (must match %s); pass --repo <name>", name, repoNameRule)}
}

// onboardConfigsDir resolves the configs directory the same way the daemon does,
// but returns the <repo>/configs default even when it does not yet exist so the
// scaffold step can create it. STACK_FITNESS_FUNCTIONS_CONFIGS_DIR still wins.
func onboardConfigsDir(repoRoot string) string {
	if dir := os.Getenv(envConfigsDir); dir != "" {
		return dir
	}
	return filepath.Join(repoRoot, "configs")
}

func (o onboarder) run() error {
	_, _ = fmt.Fprintf(o.stdout, "Onboarding %q (enforcement=%s, addr=%s)\n", o.repoName, o.enforcement, o.addr)
	steps := []func() error{
		o.ensureCerts,
		o.scaffoldConfig,
		o.authorizeCaller,
		o.installHooks,
		o.startDaemon,
		o.runDoctor,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	o.printManualRemainder()
	return nil
}

func (o onboarder) ensureCerts() error {
	o.step("Dev certificates: %s", o.certDir)
	if err := ensureDevCerts(o.certDir, o.forceDevCertRotation); err != nil {
		return err
	}
	o.detail("client CN %s ready", devClientCommonName)
	return nil
}

func (o onboarder) scaffoldConfig() error {
	configPath := filepath.Join(o.configsDir, o.repoName, "config.json")
	o.step("Server-side config: %s", configPath)
	if fileExists(configPath) {
		o.detail("already present — leaving unchanged")
		return nil
	}
	content, err := renderScaffoldConfig(o.enforcement)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", configPath, err)
	}
	o.detail("scaffolded %s config with all five fitness functions enabled", o.enforcement)
	return nil
}

// scaffoldConfigDocument is the config shape written by onboard. It carries the
// template's enforcement fields and the explicitly populated fitness functions.
type scaffoldConfigDocument struct {
	EnforcementMode    string          `json:"enforcement-mode"`
	EnforcementOnError string          `json:"enforcement-on-error,omitempty"`
	FitnessFunctions   map[string]bool `json:"fitness-functions"`
}

// renderScaffoldConfig loads the embedded advisory/block template (whose
// fitness-functions map is empty) and fills every one of the five functions so
// the written config is explicit, matching the onboarding runbook's example.
func renderScaffoldConfig(enforcement string) ([]byte, error) {
	raw, err := embeddedConfigTemplates.ReadFile("configtemplates/" + enforcement + "-template.json")
	if err != nil {
		return nil, fmt.Errorf("reading embedded config template: %w", err)
	}
	var config scaffoldConfigDocument
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("parsing embedded config template: %w", err)
	}
	config.FitnessFunctions = enabledFitnessFunctions()
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding scaffolded config: %w", err)
	}
	return append(encoded, '\n'), nil
}

func enabledFitnessFunctions() map[string]bool {
	functions := make(map[string]bool, len(fitnessFunctionKeys))
	for _, key := range fitnessFunctionKeys {
		functions[key] = true
	}
	return functions
}

func (o onboarder) authorizeCaller() error {
	bindingsPath := onboardCallerBindingsPath(o.configsDir)
	o.step("Caller authorization: %s", bindingsPath)
	changed, err := ensureCallerBinding(bindingsPath, devClientCommonName, o.repoName)
	if err != nil {
		return err
	}
	if changed {
		o.detail("authorized CN %s for %s", devClientCommonName, o.repoName)
	} else {
		o.detail("CN %s already authorized for %s", devClientCommonName, o.repoName)
	}
	return nil
}

// onboardCallerBindingsPath mirrors the server's callerRepoBindingsPath: the file
// is a sibling of a directory literally named "configs", else it lives inside the
// resolved configs directory.
func onboardCallerBindingsPath(configsDir string) string {
	clean := filepath.Clean(configsDir)
	if filepath.Base(clean) == "configs" {
		return filepath.Join(filepath.Dir(clean), callerRepoBindingsFileName)
	}
	return filepath.Join(clean, callerRepoBindingsFileName)
}

// ensureCallerBinding adds repoName to callerCN's repository list, creating the
// file when absent and preserving all existing content (other callers, admins).
// It reports whether the file was changed.
func ensureCallerBinding(path, callerCN, repoName string) (bool, error) {
	document, err := loadCallerBindings(path)
	if err != nil {
		return false, err
	}
	callers := callerBindingsSection(document)
	repos := callerRepoList(callers, callerCN)
	if containsString(repos, repoName) {
		return false, nil
	}
	callers[callerCN] = append(repos, repoName)
	if err := writeCallerBindings(path, document); err != nil {
		return false, err
	}
	return true, nil
}

func loadCallerBindings(path string) (map[string]any, error) {
	document := map[string]any{}
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return document, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return document, nil
	}
	if err := json.Unmarshal(content, &document); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return document, nil
}

func callerBindingsSection(document map[string]any) map[string]any {
	callers, ok := document["callers"].(map[string]any)
	if !ok {
		callers = map[string]any{}
		document["callers"] = callers
	}
	return callers
}

func callerRepoList(callers map[string]any, callerCN string) []string {
	raw, ok := callers[callerCN].([]any)
	if !ok {
		return nil
	}
	repos := make([]string, 0, len(raw))
	for _, item := range raw {
		if repo, ok := item.(string); ok {
			repos = append(repos, repo)
		}
	}
	return repos
}

func writeCallerBindings(path string, document map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating caller bindings directory: %w", err)
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func (o onboarder) installHooks() error {
	o.step("Installing hooks (git + agent Edit/Write validation)")
	return RunInstallHooks([]string{o.repoRoot}, o.stdout, o.stderr)
}

// startDaemon auto-starts the local TLS daemon (T1) so the closing doctor gate
// can reach a live server; it reuses prepareDaemonStart/ensureDaemon exactly as
// the validate path does. A healthy daemon short-circuits to a no-op.
func (o onboarder) startDaemon() error {
	o.step("Starting local governance daemon at %s", o.addr)
	daemonCfg, err := prepareDaemonStart(o.addr, o.certDir, o.repoRoot, "", "", "")
	if err != nil {
		return err
	}
	cert, key, ca := resolveClientTLSPaths("", "", "", o.certDir)
	httpClient, err := configureTLS(o.httpClient, cert, key, ca)
	if err != nil {
		return err
	}
	if err := ensureDaemon(httpClient, o.addr, daemonCfg, o.starter); err != nil {
		return err
	}
	o.detail("daemon healthy")
	return nil
}

func (o onboarder) runDoctor() error {
	o.step("Running doctor (final gate)")
	cert, key, ca := resolveClientTLSPaths("", "", "", o.certDir)
	args := appendClientTLSArgs([]string{"--addr", o.addr, "--repo", o.repoName}, cert, key, ca)
	return RunDoctor(args, o.stdout, o.stderr, o.httpClient)
}

func appendClientTLSArgs(args []string, cert, key, ca string) []string {
	if cert != "" && key != "" {
		args = append(args, "--client-cert", cert, "--client-key", key)
	}
	if ca != "" {
		args = append(args, "--client-ca", ca)
	}
	return args
}

func (o onboarder) printManualRemainder() {
	configPath := filepath.Join(o.configsDir, o.repoName, "config.json")
	bindingsPath := onboardCallerBindingsPath(o.configsDir)
	_, _ = fmt.Fprintf(o.stdout, "\n%s is governed locally.\n", o.repoName)
	_, _ = fmt.Fprintln(o.stdout, "Remaining manual step for PRODUCTION governance:")
	_, _ = fmt.Fprintf(o.stdout, "  - copy %s and the %s entry to the production deployment, then redeploy the container\n", configPath, bindingsPath)
	_, _ = fmt.Fprintln(o.stdout, "  - see docs/runbooks/onboard-new-repository.md")
}

func (o onboarder) step(format string, args ...any) {
	_, _ = fmt.Fprintf(o.stdout, "\n> "+format+"\n", args...)
}

func (o onboarder) detail(format string, args ...any) {
	_, _ = fmt.Fprintf(o.stdout, "  "+format+"\n", args...)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
