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
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/govconfig"
)

//go:embed configtemplates/*
var embeddedConfigTemplates embed.FS

const (
	// defaultOnboardAddr is the machine-local governance daemon in the ADR-0010
	// plain-HTTP listen mode. It is the default for every command that talks to a
	// daemon (validate, onboard, doctor, functions --remote); a legacy https
	// loopback daemon is still reached by scheme fallback during migration.
	defaultOnboardAddr         = "http://127.0.0.1:7890"
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

// generalizedFitnessFunctionKeys are the four newer governance functions
// (mirroring the keys declared in internal/server/config.go's defaultConfig).
// Unlike fitnessFunctionKeys, these are opt-in: the scaffolded config always
// lists them explicitly but leaves them false unless a caller selects one via
// --functions. layer-sovereignty additionally cannot be scaffolded
// non-interactively at all (parseFunctionsFlag rejects it) because it
// requires layer definitions under fitness-function-settings that onboard has
// no way to infer. Duplicated here, deliberately, so this package never
// depends on internal/server.
var generalizedFitnessFunctionKeys = []string{
	"layer-sovereignty",
	"temporal-purity",
	"sql-composition-safety",
	"deterministic-ordering",
}

// allFitnessFunctionKeys is the canonical catalog order every user-facing
// listing follows: the five metric-scored functions, then the four
// generalized ones. It returns a fresh slice so no caller can mutate the two
// source slices through it.
func allFitnessFunctionKeys() []string {
	keys := make([]string, 0, len(fitnessFunctionKeys)+len(generalizedFitnessFunctionKeys))
	keys = append(keys, fitnessFunctionKeys...)
	return append(keys, generalizedFitnessFunctionKeys...)
}

// onboarder holds the resolved inputs for a single onboard run. Dependencies are
// injected (like doctor.go) so the individual steps stay unit-testable.
type onboarder struct {
	repoName             string
	repoRoot             string
	enforcement          string
	addr                 string
	configsDir           string // machine governance root the daemon serves (ADR-0007)
	repoConfigsDir       string // tracked <repoRoot>/configs, the production handoff artifact
	certDir              string
	stdout               io.Writer
	stderr               io.Writer
	httpClient           *http.Client
	starter              func(DaemonStartConfig) error
	certificatesOnly     bool
	forceDevCertRotation bool
	tlsMode              clientTLSMode
	tlsMaterial          clientTLSMaterial
	callerCN             string
	selectedFunctions    map[string]bool
	// localHTTP marks the ADR-0010 managed-local path: a loopback http daemon
	// that authenticates by loopback peer. It owns no certificates and consults
	// no caller-repos.json, so onboarding it is config scaffold + hooks + daemon.
	localHTTP bool
	// update permits rewriting an existing repo-local config (S10): set by the
	// wizard's confirmed diff or the --update flag; without it an existing
	// config is never touched.
	update bool
	// layers carries prompted layer-sovereignty definitions to write into the
	// scaffolded config's fitness-function-settings (S10).
	layers []govconfig.LayerRule
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
	parsedFlags, err := parseOnboardFlags(args)
	if err != nil {
		return onboarder{}, err
	}
	selectedFunctions, err := resolveSelectedFunctions(parsedFlags.functions)
	if err != nil {
		return onboarder{}, err
	}
	if _, err := resolveClientTLSMode("", "", "", ""); err != nil {
		return onboarder{}, err
	}
	if parsedFlags.certificatesOnly {
		return resolveCertificatesOnlyOnboarder(parsedFlags.extra, parsedFlags.forceDevCertRotation)
	}
	mode, repoRoot, repoName, err := resolveOnboardRepoIdentity(parsedFlags.enforcement, parsedFlags.extra, parsedFlags.repo)
	if err != nil {
		return onboarder{}, err
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 3 * time.Second}
	}
	tlsResolution, governance, err := resolveOnboardSession(parsedFlags, stdout, repoName, repoRoot,
		onboardGovernance{enforcement: mode, functions: selectedFunctions}, httpClient)
	if err != nil {
		return onboarder{}, err
	}
	return onboarder{
		repoName:             repoName,
		repoRoot:             repoRoot,
		enforcement:          governance.enforcement,
		addr:                 parsedFlags.addr,
		configsDir:           onboardConfigsDir(),
		repoConfigsDir:       filepath.Join(repoRoot, "configs"),
		certDir:              tlsResolution.certDir,
		stdout:               stdout,
		stderr:               stderr,
		httpClient:           tlsResolution.httpClient,
		starter:              starter,
		forceDevCertRotation: parsedFlags.forceDevCertRotation,
		tlsMode:              tlsResolution.tlsMode,
		tlsMaterial:          tlsResolution.tlsMaterial,
		callerCN:             tlsResolution.callerCN,
		selectedFunctions:    governance.functions,
		localHTTP:            tlsResolution.tlsMode.managed && isLocalHTTP(parsedFlags.addr),
	}, nil
}

// onboardGovernance is the governance a run applies: the enforcement mode and
// the fitness-function selection (nil meaning the all-five default). It is
// resolved from flags first and then, on an interactive run, from the wizard.
type onboardGovernance struct {
	enforcement string
	functions   map[string]bool
}

// resolveOnboardSession resolves the daemon transport and then the governance
// this run applies. The two belong together: the wizard describes the daemon
// through the transport's http client, and its confirmed answers override the
// flag-derived enforcement mode and function selection. A non-interactive run
// returns the flag-derived governance untouched.
func resolveOnboardSession(flags onboardFlags, stdout io.Writer, repoName, repoRoot string, governance onboardGovernance, httpClient *http.Client) (onboardTLSResolution, onboardGovernance, error) {
	tlsResolution, err := resolveOnboardTLS(flags.addr, httpClient)
	if err != nil {
		return onboardTLSResolution{}, onboardGovernance{}, err
	}
	if !wizardApplies(flags) {
		return tlsResolution, governance, nil
	}
	chosen, err := resolveWizardOutcome(stdout, repoName, repoRoot, flags.addr, tlsResolution.httpClient)
	return tlsResolution, chosen, err
}

// onboardFlags holds the parsed `client onboard` flag set plus its positional
// arguments, so resolveOnboarder can be a readable sequence of calls over
// plain values instead of threading *string flag pointers through helpers.
type onboardFlags struct {
	repo                 string
	enforcement          string
	addr                 string
	certificatesOnly     bool
	forceDevCertRotation bool
	functions            string
	// update permits rewriting an existing repo-local config (S10).
	update bool
	extra  []string
}

func parseOnboardFlags(args []string) (onboardFlags, error) {
	flags := flag.NewFlagSet("client onboard", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	repo := flags.String("repo", "", "governance repository name (defaults to the git working-tree basename)")
	enforcement := flags.String("enforcement", "advisory", "initial enforcement mode: advisory or block")
	addr := flags.String("addr", defaultOnboardAddr, "governance daemon base URL")
	certificatesOnly := flags.Bool("certificates-only", false, "publish managed development certificates only")
	forceDevCertRotation := flags.Bool("force-dev-cert-rotation", false, "request managed development certificate rotation")
	functionsFlag := flags.String("functions", "", "comma-separated subset of fitness functions to enable (default: all five)")
	if err := flags.Parse(args); err != nil {
		return onboardFlags{}, usageError{err: err}
	}
	return onboardFlags{
		repo:                 *repo,
		enforcement:          *enforcement,
		addr:                 *addr,
		certificatesOnly:     *certificatesOnly,
		forceDevCertRotation: *forceDevCertRotation,
		functions:            *functionsFlag,
		extra:                flags.Args(),
	}, nil
}

// resolveSelectedFunctions resolves the fitness functions the scaffolded
// config enables from --functions. A run without the flag returns nil — the
// all-five default — which a real terminal then replaces with the wizard's
// selection once the repository has been identified (wizardApplies). Every
// non-interactive context (go test, CI, pipes, --certificates-only) keeps
// today's nil selection unchanged.
func resolveSelectedFunctions(functionsFlag string) (map[string]bool, error) {
	if functionsFlag == "" {
		return nil, nil
	}
	return parseFunctionsFlag(functionsFlag)
}

// wizardApplies reports whether this run presents the interactive onboard
// wizard: a real terminal, no --functions selection to honor, and a run that
// scaffolds governance at all (--certificates-only does not).
func wizardApplies(flags onboardFlags) bool {
	return flags.functions == "" && !flags.certificatesOnly && stdinIsTerminal(os.Stdin)
}

// resolveWizardOutcome runs the onboard wizard over the real terminal and maps
// the confirmed choices onto the enforcement mode and fitness-function
// selection this run will scaffold. Declining aborts before any step has
// written a file, naming what was declined.
func resolveWizardOutcome(stdout io.Writer, repoName, repoRoot, addr string, httpClient *http.Client) (onboardGovernance, error) {
	facts := gatherWizardFacts(repoName, repoRoot, addr, httpClient)
	outcome, err := runOnboardWizard(os.Stdin, stdout, facts)
	if err != nil {
		return onboardGovernance{}, err
	}
	if !outcome.Confirmed {
		return onboardGovernance{}, usageError{err: fmt.Errorf("onboard declined at the confirmation step: %s was not applied and nothing was written", describeWizardChoice(outcome))}
	}
	if err := rejectUnsettableSelection(outcome.Functions); err != nil {
		return onboardGovernance{}, err
	}
	if facts.Current != nil {
		return reportExistingConfigUntouched(stdout, facts, outcome, repoName), nil
	}
	return onboardGovernance{enforcement: outcome.Enforcement, functions: outcome.Functions}, nil
}

// reportExistingConfigUntouched keeps an update run honest: this slice
// scaffolds only a repository's first config, so confirmed changes against an
// existing one are named and dropped rather than silently discarded. Applying
// them is `client onboard --update`, ADR-0010 slice S10.
func reportExistingConfigUntouched(stdout io.Writer, facts wizardFacts, outcome wizardOutcome, repoName string) onboardGovernance {
	current := *facts.Current
	inForce := onboardGovernance{enforcement: defaultWizardEnforcement(facts), functions: current.FitnessFunctions}
	changes := wizardOutcomeChanges(current, outcome)
	if len(changes) == 0 {
		return inForce
	}
	_, _ = fmt.Fprintf(stdout, "\nconfigs/%s/config.json already governs this repository and was left unchanged.\n", repoName)
	for _, change := range changes {
		_, _ = fmt.Fprintf(stdout, "  would change: %s\n", change)
	}
	_, _ = fmt.Fprintln(stdout, "  applying these changes needs `client onboard --update`, which arrives in ADR-0010 slice S10.")
	return inForce
}

func resolveCertificatesOnlyOnboarder(extraArgs []string, forceDevCertRotation bool) (onboarder, error) {
	if len(extraArgs) != 0 {
		return onboarder{}, usageError{err: errors.New("client onboard --certificates-only accepts no repository path")}
	}
	certDir := os.Getenv(envDevCertDir)
	if certDir == "" {
		certDir = governanceCertsDir()
	}
	return onboarder{certDir: certDir, certificatesOnly: true, forceDevCertRotation: forceDevCertRotation}, nil
}

// resolveOnboardRepoIdentity validates the enforcement mode and resolves the
// positional repository path into a repo root and repo name, in the same
// order resolveOnboarder previously performed them inline.
func resolveOnboardRepoIdentity(enforcementFlag string, extraArgs []string, repoFlag string) (string, string, string, error) {
	mode, err := validateEnforcement(enforcementFlag)
	if err != nil {
		return "", "", "", err
	}
	pathArg, err := onboardPathArg(extraArgs)
	if err != nil {
		return "", "", "", err
	}
	repoRoot, err := onboardRepoRoot(pathArg)
	if err != nil {
		return "", "", "", err
	}
	repoName, err := resolveOnboardRepoName(repoFlag, repoRoot)
	if err != nil {
		return "", "", "", err
	}
	return mode, repoRoot, repoName, nil
}

// onboardTLSResolution is the outcome of resolveOnboardTLS: the dev cert
// directory, resolved TLS mode/material, caller identity, and (for the
// external-TLS path) the httpClient reconfigured with that material.
type onboardTLSResolution struct {
	certDir     string
	tlsMode     clientTLSMode
	tlsMaterial clientTLSMaterial
	callerCN    string
	httpClient  *http.Client
}

// resolveOnboardTLS resolves the client TLS mode and, for the
// external-certificate path, loads and validates that material and confirms
// an already-running healthy daemon before returning.
func resolveOnboardTLS(addr string, httpClient *http.Client) (onboardTLSResolution, error) {
	certDir := resolveDevCertDir()
	tlsMode, err := resolveClientTLSMode("", "", "", certDir)
	if err != nil {
		return onboardTLSResolution{}, err
	}
	callerCN := devClientCommonName
	var tlsMaterial clientTLSMaterial
	if !tlsMode.managed {
		tlsMaterial, callerCN, err = loadExternalOnboardMaterial(tlsMode)
		if err != nil {
			return onboardTLSResolution{}, err
		}
		configuredClient, configureErr := configureClientTLSMaterial(httpClient, tlsMaterial)
		if configureErr != nil {
			return onboardTLSResolution{}, usageError{err: fmt.Errorf("invalid external client TLS material: %w", configureErr)}
		}
		if !isHealthy(configuredClient, addr) {
			return onboardTLSResolution{}, usageError{err: fmt.Errorf("external client TLS onboarding requires an already-running healthy daemon at %s; automatic daemon startup requires managed development certificates", addr)}
		}
		httpClient = configuredClient
	}
	return onboardTLSResolution{
		certDir:     certDir,
		tlsMode:     tlsMode,
		tlsMaterial: tlsMaterial,
		callerCN:    callerCN,
		httpClient:  httpClient,
	}, nil
}

func loadExternalOnboardMaterial(mode clientTLSMode) (clientTLSMaterial, string, error) {
	if err := validateExternalTLSInputs(mode); err != nil {
		return clientTLSMaterial{}, "", err
	}
	pair, err := loadExternalKeyPair(mode)
	if err != nil {
		return clientTLSMaterial{}, "", err
	}
	leaf, callerCN, err := parseExternalLeafCertificate(pair)
	if err != nil {
		return clientTLSMaterial{}, "", err
	}
	roots, err := loadExternalCAPool(mode)
	if err != nil {
		return clientTLSMaterial{}, "", err
	}
	pair.Leaf = leaf
	return clientTLSMaterial{mode: mode, certificate: pair, roots: roots}, callerCN, nil
}

func validateExternalTLSInputs(mode clientTLSMode) error {
	if mode.cert == "" || mode.key == "" || mode.ca == "" {
		return usageError{err: errors.New("external client TLS onboarding requires certificate, key, and CA inputs")}
	}
	return nil
}

func loadExternalKeyPair(mode clientTLSMode) (tls.Certificate, error) {
	pair, err := tls.LoadX509KeyPair(mode.cert, mode.key)
	if err != nil {
		return tls.Certificate{}, usageError{err: fmt.Errorf("invalid external client certificate/key pair: %w", err)}
	}
	if len(pair.Certificate) == 0 {
		return tls.Certificate{}, usageError{err: errors.New("external client certificate contains no leaf certificate")}
	}
	return pair, nil
}

func parseExternalLeafCertificate(pair tls.Certificate) (*x509.Certificate, string, error) {
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, "", usageError{err: fmt.Errorf("invalid external client leaf certificate: %w", err)}
	}
	callerCN := leaf.Subject.CommonName
	if callerCN == "" {
		return nil, "", usageError{err: errors.New("external client certificate leaf common name must be non-empty")}
	}
	if strings.TrimSpace(callerCN) != callerCN {
		return nil, "", usageError{err: errors.New("external client certificate leaf common name must not have leading or trailing whitespace")}
	}
	return leaf, callerCN, nil
}

func loadExternalCAPool(mode clientTLSMode) (*x509.CertPool, error) {
	caPEM, err := os.ReadFile(mode.ca)
	if err != nil {
		return nil, usageError{err: fmt.Errorf("read external client CA: %w", err)}
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, usageError{err: errors.New("external client CA contains no PEM certificates")}
	}
	return roots, nil
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

// onboardConfigsDir resolves the DAEMON-facing configs directory onboard
// registers this repository under: AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR wins,
// otherwise the machine governance root (ADR-0007) — the same resolution the
// auto-started daemon uses. This is distinct from repoConfigsDir, the tracked
// <repoRoot>/configs production handoff artifact.
func onboardConfigsDir() string {
	return resolveConfigsDir()
}

func (o *onboarder) run() error {
	_, _ = fmt.Fprintf(o.stdout, "Onboarding %q (enforcement=%s, addr=%s)\n", o.repoName, o.enforcement, o.addr)
	steps := []func() error{o.ensureCerts}
	if o.tlsMode.managed {
		steps = append(steps, o.scaffoldConfig, o.authorizeCaller)
	} else {
		steps = append(steps, o.registerRemote)
	}
	steps = append(steps, o.migrateLegacy, o.installHooks, o.startDaemon, o.awaitRegistration, o.runDoctor)
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	o.printManualRemainder()
	return nil
}

// migrateLegacy quarantines pre-ADR-0007 repo-local governance state so stale
// certificate material can never again be presented to the shared daemon.
// The step header is printed lazily so a repo with no legacy state — every repo
// onboarded since ADR-0007 — says nothing at all. See legacymigration.go for
// what is quarantined and what is only reported.
func (o *onboarder) migrateLegacy() error {
	announced := false
	return migrateLegacyState(o.repoRoot, func(line string) {
		if !announced {
			o.step("Legacy pre-ADR-0007 repo-local state")
			announced = true
		}
		o.detail("%s", line)
	})
}

func (o *onboarder) ensureCerts() error {
	if o.localHTTP {
		// ADR-0010: the loopback plain-HTTP daemon has no TLS to configure, so
		// generating a dev CA here would only recreate the certificate-plumbing
		// failure class the mode exists to remove.
		o.step("Dev certificates: not required (local-http mode)")
		return nil
	}
	if !o.tlsMode.managed {
		o.step("External client certificates: unchanged")
		o.detail("client CN %s validated", o.callerCN)
		return nil
	}
	o.step("Dev certificates: %s", o.certDir)
	if err := ensureDevCerts(o.certDir, o.forceDevCertRotation); err != nil {
		return err
	}
	material, err := loadClientTLSMode(o.tlsMode, false)
	if err != nil {
		return err
	}
	o.tlsMaterial = material
	o.detail("client CN %s ready", devClientCommonName)
	return nil
}

// scaffoldConfig scaffolds the tracked repo-local production artifact (if
// absent) and then always re-syncs it, verbatim, into the shared machine
// governance root the daemon actually serves (ADR-0007). The repo-local file
// is the source of truth: a re-run never rewrites it, only the shared copy.
func (o onboarder) scaffoldConfig() error {
	configPath := filepath.Join(o.repoConfigsDir, o.repoName, "config.json")
	o.step("Server-side config: %s", configPath)
	if fileExists(configPath) {
		o.detail("already present — leaving unchanged")
	} else if err := o.writeScaffoldConfig(configPath); err != nil {
		return err
	}
	return o.syncSharedConfig(configPath)
}

// writeScaffoldConfig renders and writes the fresh repo-local config template.
func (o onboarder) writeScaffoldConfig(configPath string) error {
	content, err := renderScaffoldConfig(o.enforcement, o.selectedFunctions)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", configPath, err)
	}
	o.detail("scaffolded %s config with %s", o.enforcement, scaffoldedFunctionsDetail(o.selectedFunctions))
	return nil
}

// syncSharedConfig copies the tracked repo-local config byte-for-byte into
// the shared machine governance configs dir, overwriting any prior copy so a
// re-onboard re-syncs a user-edited repo-local config. It creates the shared
// dir itself rather than relying on ensureCerts having run first.
func (o onboarder) syncSharedConfig(repoLocalConfigPath string) error {
	content, err := os.ReadFile(repoLocalConfigPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", repoLocalConfigPath, err)
	}
	// Refuse to register a config the daemon would reject. Copying it anyway
	// defers the failure to the next commit, where it surfaces as an HTTP 503
	// naming neither the file nor the invariant that caused it.
	if err := govconfig.Validate(content); err != nil {
		return fmt.Errorf("%s is not a valid governance config: %w", repoLocalConfigPath, err)
	}
	sharedPath := filepath.Join(o.configsDir, o.repoName, "config.json")
	if err := os.MkdirAll(filepath.Dir(sharedPath), 0o755); err != nil {
		return fmt.Errorf("creating shared config directory: %w", err)
	}
	if err := os.WriteFile(sharedPath, content, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", sharedPath, err)
	}
	o.detail("registered with local governance daemon (%s)", filepath.Dir(sharedPath))
	return nil
}

// scaffoldedFunctionsDetail describes the enabled subset for the onboard progress
// line: the default (nil selection) keeps the existing "all five fitness functions
// enabled" wording an existing test greps for; a subset names exactly which ones.
func scaffoldedFunctionsDetail(selected map[string]bool) string {
	if selected == nil {
		return "all five fitness functions enabled"
	}
	enabled := make([]string, 0, len(fitnessFunctionKeys)+len(generalizedFitnessFunctionKeys))
	for _, key := range fitnessFunctionKeys {
		if selected[key] {
			enabled = append(enabled, key)
		}
	}
	for _, key := range generalizedFitnessFunctionKeys {
		if selected[key] {
			enabled = append(enabled, key)
		}
	}
	return fmt.Sprintf("fitness functions enabled: %s", strings.Join(enabled, ", "))
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
func renderScaffoldConfig(enforcement string, selected map[string]bool) ([]byte, error) {
	raw, err := embeddedConfigTemplates.ReadFile("configtemplates/" + enforcement + "-template.json")
	if err != nil {
		return nil, fmt.Errorf("reading embedded config template: %w", err)
	}
	var config scaffoldConfigDocument
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("parsing embedded config template: %w", err)
	}
	if selected != nil {
		// Copy the selection so the overlay below never mutates a
		// caller-owned map.
		functions := make(map[string]bool, len(selected))
		for name, enabled := range selected {
			functions[name] = enabled
		}
		config.FitnessFunctions = functions
		// A selection that already names at least one generalized function
		// (via parseFunctionsFlag) widens the scaffold to all nine keys, the
		// missing generalized ones landing false. A purely-classic selection
		// (whether built by parseFunctionsFlag or handed in directly) keeps
		// the historical five-key envelope untouched — pre-generalized
		// callers of renderScaffoldConfig with a plain five-key map must see
		// exactly five keys back.
		if containsAnyKey(selected, generalizedFitnessFunctionKeys) {
			overlayGeneralizedFunctions(config.FitnessFunctions)
		}
	} else {
		functions := enabledFitnessFunctions()
		overlayGeneralizedFunctions(functions)
		config.FitnessFunctions = functions
	}
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

// containsAnyKey reports whether functions has an entry (present, regardless
// of value) for any of keys.
func containsAnyKey(functions map[string]bool, keys []string) bool {
	for _, key := range keys {
		if _, ok := functions[key]; ok {
			return true
		}
	}
	return false
}

// overlayGeneralizedFunctions ensures every generalized fitness function key
// is present in the scaffolded config, defaulting to false (opt-in) unless a
// selection already set it true. This keeps every scaffold listing all nine
// functions explicitly regardless of enforcement mode or --functions selection.
func overlayGeneralizedFunctions(functions map[string]bool) {
	for _, key := range generalizedFitnessFunctionKeys {
		if _, ok := functions[key]; !ok {
			functions[key] = false
		}
	}
}

func (o onboarder) authorizeCaller() error {
	if o.localHTTP {
		// ADR-0010: a loopback peer carries the implicit caller identity and the
		// daemon never consults caller-repos.json, so there is nothing to bind.
		o.step("Caller authorization: not required (implicit local caller)")
		return nil
	}
	bindingsPath := onboardCallerBindingsPath(o.configsDir)
	o.step("Caller authorization: %s", bindingsPath)
	if o.callerCN == "" {
		return errors.New("client certificate identity is empty")
	}
	changed, err := ensureCallerBinding(bindingsPath, o.callerCN, o.repoName)
	if err != nil {
		return err
	}
	if changed {
		o.detail("authorized CN %s for %s", o.callerCN, o.repoName)
	} else {
		o.detail("CN %s already authorized for %s", o.callerCN, o.repoName)
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
// It reports whether the file was changed. The whole read-modify-write cycle
// runs under an on-disk lock: the bindings file is machine-shared (ADR-0007),
// so two `client onboard` runs in different repositories can race here, and an
// unserialized cycle loses updates or tears the JSON.
func ensureCallerBinding(path, callerCN, repoName string) (changed bool, err error) {
	unlock, err := acquireBindingsLock(path)
	if err != nil {
		return false, err
	}
	defer unlock()
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

const (
	// bindingsLockWait bounds how long a writer waits for the lock; the
	// critical section is a single small-file read+write, so contention
	// clears in milliseconds and a full wait means something is wrong.
	bindingsLockWait = 5 * time.Second
	// bindingsLockStaleAge is the age past which a leftover lock directory
	// is treated as a crashed writer's residue and reaped. Generous compared
	// to the milliseconds a live writer holds it.
	bindingsLockStaleAge = 30 * time.Second
)

// acquireBindingsLock serializes shared caller-repos.json writers with the
// same portable primitive ADR-0004 chose for certificate publication: an
// atomic fixed-path mkdir (no flock, works on macOS and Linux). A lock
// directory older than bindingsLockStaleAge is reaped as crash residue.
// When the bindings directory itself is unusable (unwritable, a path
// component is a file), acquisition degrades to unlocked: the read/write
// cycle is about to fail with its own canonical error anyway, and that error
// is the one callers and tests key on. Only a lock genuinely held past the
// wait bound is an acquisition error.
func acquireBindingsLock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return func() {}, nil
	}
	lockDir := path + ".lock"
	deadline := time.Now().Add(bindingsLockWait)
	for {
		err := os.Mkdir(lockDir, 0o755)
		if err == nil {
			return func() { _ = os.Remove(lockDir) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return func() {}, nil
		}
		reapStaleLockDir(lockDir, bindingsLockStaleAge)
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("caller bindings lock %s held for over %s; remove it if no onboard is running", lockDir, bindingsLockWait)
		}
		time.Sleep(25 * time.Millisecond)
	}
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

// registerRequestBody is the JSON body POSTed to /register — a client-side
// mirror of internal/server's RegisterRequest, duplicated so this package
// never imports internal/server.
type registerRequestBody struct {
	Repo             string          `json:"repo"`
	EnforcementMode  string          `json:"enforcement-mode,omitempty"`
	FitnessFunctions map[string]bool `json:"fitness-functions"`
}

// registerResponseBody is the JSON response from /register — a client-side
// mirror of internal/server's RegisterResponse.
type registerResponseBody struct {
	Repo    string `json:"repo"`
	Created bool   `json:"created"`
}

// registerRemote is the external-TLS-mode counterpart to scaffoldConfig +
// authorizeCaller: a remote server never sees local config/caller-binding
// files, so onboard instead self-service-registers the repo via POST
// /register, carrying the selected fitness functions and enforcement mode.
func (o *onboarder) registerRemote() error {
	o.step("Remote registration: %s/register", o.addr)
	body, err := json.Marshal(registerRequestBody{
		Repo:             o.repoName,
		EnforcementMode:  o.enforcement,
		FitnessFunctions: o.registrationFunctions(),
	})
	if err != nil {
		return fmt.Errorf("encoding register request: %w", err)
	}
	resp, err := o.httpClient.Post(strings.TrimRight(o.addr, "/")+"/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("POST %s/register: %w", o.addr, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return o.handleRegisterResponse(resp)
}

// registrationFunctions is the fitness-functions map registerRemote sends:
// the --functions selection when present, else the all-five default (the
// server has no other way to learn the default subset a remote onboard
// intends).
func (o *onboarder) registrationFunctions() map[string]bool {
	if o.selectedFunctions != nil {
		return o.selectedFunctions
	}
	return enabledFitnessFunctions()
}

// handleRegisterResponse maps the /register HTTP outcome to a progress detail
// line (success) or an actionable error (conflict / other failure).
func (o *onboarder) handleRegisterResponse(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		o.reportRegisterSuccess(resp)
		return nil
	case http.StatusConflict:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("repository %q is already registered with a different configuration at %s; an admin CN is required to change it: %s",
			o.repoName, o.addr, strings.TrimSpace(string(body)))
	default:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s/register: unexpected status %s: %s", o.addr, resp.Status, strings.TrimSpace(string(body)))
	}
}

func (o *onboarder) reportRegisterSuccess(resp *http.Response) {
	var decoded registerResponseBody
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	if decoded.Created {
		o.detail("registered %s with %s", o.repoName, o.enforcement)
		return
	}
	o.detail("%s already registered with matching governance", o.repoName)
}

func (o onboarder) installHooks() error {
	o.step("Installing hooks (git + agent Edit/Write validation)")
	return RunInstallHooks([]string{o.repoRoot}, o.stdout, o.stderr)
}

// startDaemon makes the local daemon current (T1) so the closing doctor gate
// reaches a live server of this generation. Onboarding is where a machine
// crosses generations — a new binary, a new listen mode (ADR-0010), a new
// configs directory — so a daemon that merely answers is not enough: a stale
// one is restarted, and only a current one short-circuits to a no-op.
func (o *onboarder) startDaemon() error {
	o.step("Starting local governance daemon at %s", o.addr)
	httpClient, err := configureClientTLSMaterial(o.httpClient, o.tlsMaterial)
	if err != nil {
		return err
	}
	if !o.tlsMode.managed {
		if !isHealthy(httpClient, o.addr) {
			return fmt.Errorf("external TLS daemon at %s became unavailable during onboarding; it will not be auto-started", o.addr)
		}
		o.detail("external daemon healthy; automatic startup disabled")
		return nil
	}
	daemonCfg := daemonStartConfigFromMaterial(o.addr, o.tlsMaterial)
	report := func(reasons []string) {
		o.detail("daemon stale: %s; restarting", strings.Join(reasons, "; "))
	}
	if err := ensureCurrentDaemonReporting(httpClient, o.addr, daemonCfg, o.starter, report); err != nil {
		return err
	}
	o.detail("daemon healthy and current")
	return nil
}

// awaitRegistration waits for the (possibly already-running) daemon to
// acknowledge this repository's registration before the doctor gate probes
// it. The shared governance configs dir is picked up via fsnotify with a
// debounce, so the files onboard just wrote become visible to a live daemon
// a moment later — polling /preflight absorbs that window instead of letting
// doctor race it and fail a freshly onboarded repo. External mode registers
// synchronously over HTTP, so there is nothing to wait for.
func (o *onboarder) awaitRegistration() error {
	if !o.tlsMode.managed {
		return nil
	}
	httpClient, err := configureClientTLSMaterial(o.httpClient, o.tlsMaterial)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		report, ok := o.preflightAcknowledged(httpClient)
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			detail := ""
			if report.RepoConfigError != "" {
				detail = "; config rejected: " + report.RepoConfigError
			}
			return fmt.Errorf("daemon at %s did not acknowledge %q within 5s (configured=%v valid=%v authorized=%v)%s; check the governance configs dir it serves",
				o.addr, o.repoName, report.RepoConfigured, report.RepoConfigValid, report.CallerAuthorized, detail)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// preflightAcknowledged reports whether GET /preflight already shows this
// repository configured and this caller authorized.
func (o *onboarder) preflightAcknowledged(httpClient *http.Client) (preflightReport, bool) {
	// A short per-request timeout keeps the 5s poll budget meaning several
	// attempts rather than one or two slow round trips (loopback daemon).
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	target := strings.TrimRight(o.addr, "/") + "/preflight?repo=" + o.repoName
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return preflightReport{}, false
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return preflightReport{}, false
	}
	defer func() { _ = response.Body.Close() }()
	var report preflightReport
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&report) != nil {
		return preflightReport{}, false
	}
	// RepoConfigValid matters as much as RepoConfigured: the server reports a
	// present-but-invalid config as configured, so without this a config
	// edited after onboard sails through and only fails at the next commit.
	return report, report.RepoConfigured && report.RepoConfigValid && report.CallerAuthorized
}

func (o *onboarder) runDoctor() error {
	o.step("Running doctor (final gate)")
	httpClient, err := configureClientTLSMaterial(o.httpClient, o.tlsMaterial)
	if err != nil {
		return err
	}
	cfg := doctorConfig{
		addr:       o.addr,
		repo:       o.repoName,
		repoRoot:   o.repoRoot,
		tlsLoaded:  true,
		httpClient: httpClient,
	}
	switch {
	case o.localHTTP:
		cfg.localHTTP = true
	case o.tlsMode.managed:
		paths := o.tlsMaterial.version.Paths()
		cfg.clientCert = paths.ClientCertificate
		cfg.clientKey = paths.ClientKey
		cfg.clientCA = paths.CA
		cfg.managed = true
		cfg.clientLeaf = o.tlsMaterial.certificate.Leaf
		cfg.rootCAs = o.tlsMaterial.roots
	default:
		cfg.clientCert = o.tlsMode.cert
		cfg.clientKey = o.tlsMode.key
		cfg.clientCA = o.tlsMode.ca
	}
	return runDoctorWithConfig(cfg, o.stdout)
}

// printManualRemainder prints whatever step still requires operator action.
// In managed/local mode that is the same manual production hand-off it has
// always been: onboard registers the repo with this machine's shared local
// governance daemon (repoConfigsDir synced into the shared configsDir, plus a
// caller-repos.json entry in the governance root), but a production container
// deployment is a separate target that still needs the tracked repo-local
// config and the caller-binding entry copied over. In external mode
// registerRemote already registered the repo against the server named by
// --addr, so there is nothing left to copy.
func (o onboarder) printManualRemainder() {
	if !o.tlsMode.managed {
		_, _ = fmt.Fprintf(o.stdout, "\n%s is registered with %s. No manual config hand-off is needed.\n", o.repoName, o.addr)
		return
	}
	configPath := filepath.Join(o.repoConfigsDir, o.repoName, "config.json")
	_, _ = fmt.Fprintf(o.stdout, "\n%s is governed locally.\n", o.repoName)
	_, _ = fmt.Fprintln(o.stdout, "Remaining manual step for PRODUCTION governance:")
	_, _ = fmt.Fprintf(o.stdout, "  - copy %s to the production deployment, then redeploy the container\n", configPath)
	_, _ = fmt.Fprintf(o.stdout, "  - %s\n", o.productionCallerAuthorizationRemainder())
	_, _ = fmt.Fprintln(o.stdout, "  - see docs/runbooks/onboard-new-repository.md")
}

// productionCallerAuthorizationRemainder names the caller authorization the
// production (mTLS) deployment still needs. The legacy managed-TLS local path
// already wrote that entry into this machine's governance root, so it can point
// at a concrete file to copy; the local-http path (ADR-0010) never writes one, so
// it describes the entry the operator has to add on the production side instead.
func (o onboarder) productionCallerAuthorizationRemainder() string {
	if o.localHTTP {
		return fmt.Sprintf("authorize the deploying client's CN for %s in the production %s", o.repoName, callerRepoBindingsFileName)
	}
	return fmt.Sprintf("copy the %s entry from %s to the production deployment", o.repoName, onboardCallerBindingsPath(o.configsDir))
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
