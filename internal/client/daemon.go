package client

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/devcerts"
)

const (
	envClientCert = "AGENT_FITNESS_FUNCTIONS_CLIENT_CERT"
	envClientKey  = "AGENT_FITNESS_FUNCTIONS_CLIENT_KEY"
	envClientCA   = "AGENT_FITNESS_FUNCTIONS_CLIENT_CA"
	envDevCertDir = "AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR"
	envConfigsDir = "AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR"
	envServerCert = "AGENT_FITNESS_FUNCTIONS_TLS_CERT"
	envServerKey  = "AGENT_FITNESS_FUNCTIONS_TLS_KEY"
	envServerCA   = "AGENT_FITNESS_FUNCTIONS_TLS_CA"
)

type clientTLSMode struct {
	managed bool
	root    string
	cert    string
	key     string
	ca      string
}

type clientTLSMaterial struct {
	mode        clientTLSMode
	version     devcerts.ManagedVersion
	certificate tls.Certificate
	roots       *x509.CertPool
}

var (
	publishManagedCertificates = devcerts.Publish
	resolveManagedVersion      = devcerts.ResolveManagedVersion
	loadManagedClient          = devcerts.LoadManagedClient
	daemonExecutable           = os.Executable
)

// hasExplicitClientTLS reports whether the caller supplied client TLS material
// directly (flags or env), as opposed to selecting managed dev certificates.
func hasExplicitClientTLS(certFlag, keyFlag, caFlag string) bool {
	return certFlag != "" || keyFlag != "" || caFlag != "" ||
		os.Getenv(envClientCert) != "" || os.Getenv(envClientKey) != "" || os.Getenv(envClientCA) != ""
}

func resolveClientTLSMode(certFlag, keyFlag, caFlag, defaultRoot string) (clientTLSMode, error) {
	selector := os.Getenv(envDevCertDir)
	explicit := hasExplicitClientTLS(certFlag, keyFlag, caFlag)
	if selector != "" && explicit {
		return clientTLSMode{}, usageError{err: errors.New("AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR cannot be combined with explicit client TLS inputs")}
	}
	if explicit {
		return clientTLSMode{
			cert: firstNonEmpty(certFlag, os.Getenv(envClientCert)),
			key:  firstNonEmpty(keyFlag, os.Getenv(envClientKey)),
			ca:   firstNonEmpty(caFlag, os.Getenv(envClientCA)),
		}, nil
	}
	root := selector
	if root == "" {
		root = defaultRoot
	}
	return clientTLSMode{managed: true, root: root}, nil
}

// publishManagedRoot publishes the managed dev certificate set at root and
// protects it from accidental git staging. When root is the machine
// governance default (ADR-0007), it first creates the governance root so the
// publisher's parent-directory check finds a real, pinned directory rather
// than failing on a machine that has never published there before.
func publishManagedRoot(root string) error {
	if root == governanceCertsDir() {
		if err := ensureGovernanceRoot(); err != nil {
			return err
		}
	}
	if err := publishManagedCertificates(root, false); err != nil {
		return err
	}
	return ensureCertsIgnoreProtection(root)
}

func loadClientTLSMode(mode clientTLSMode, publish bool) (clientTLSMaterial, error) {
	material := clientTLSMaterial{mode: mode}
	if !mode.managed {
		return material, nil
	}
	if mode.root == "" {
		return clientTLSMaterial{}, errors.New("could not determine managed development certificate root")
	}
	if publish {
		if err := publishManagedRoot(mode.root); err != nil {
			return clientTLSMaterial{}, err
		}
	}
	version, err := resolveManagedVersion(mode.root)
	if err != nil {
		return clientTLSMaterial{}, err
	}
	clientMaterial, err := loadManagedClient(version)
	if err != nil {
		return clientTLSMaterial{}, err
	}
	material.version = version
	material.certificate = clientMaterial.Certificate
	material.roots = clientMaterial.RootCAs
	return material, nil
}

// DaemonStartConfig describes how the auto-started local daemon must be launched so
// the default https client can reach it. Local is true only for the zero-config
// loopback path, where dev TLS material and a repo configs directory are provisioned.
type DaemonStartConfig struct {
	Addr        string
	Local       bool
	ManagedRoot string
	TLSCert     string
	TLSKey      string
	TLSCA       string
	ConfigsDir  string
	CertDir     string
	Env         []string
}

// hasExplicitServerTLS reports whether the caller supplied server TLS material
// directly via env, bypassing managed dev-certificate provisioning.
func hasExplicitServerTLS() bool {
	return os.Getenv(envServerCert) != "" || os.Getenv(envServerKey) != "" || os.Getenv(envServerCA) != ""
}

// skipsManagedProvisioning reports whether dev-certificate provisioning must be
// skipped: unmanaged client TLS, caller-supplied server TLS, no cert dir, or an
// addr that is not a local https loopback URL.
func skipsManagedProvisioning(mode clientTLSMode, explicitServerTLS bool, certDir, addr string) bool {
	return !mode.managed || explicitServerTLS || certDir == "" || !isLocalHTTPS(addr)
}

// prepareDaemonStart resolves the auto-start configuration and, on the zero-config
// loopback path, materializes dev certificates so the auto-started TLS server and
// this client trust the same CA. Dev material is only provisioned when the caller
// passed no explicit client TLS flags and the addr is an https loopback URL.
func prepareDaemonStart(addr, certDir, certFlag, keyFlag, caFlag string) (DaemonStartConfig, error) {
	mode, err := resolveClientTLSMode(certFlag, keyFlag, caFlag, certDir)
	if err != nil {
		return DaemonStartConfig{}, err
	}
	selector := os.Getenv(envDevCertDir)
	explicitServerTLS := hasExplicitServerTLS()
	if selector != "" && explicitServerTLS {
		return DaemonStartConfig{}, usageError{err: errors.New("AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR cannot be combined with explicit server or client TLS inputs")}
	}
	if skipsManagedProvisioning(mode, explicitServerTLS, certDir, addr) {
		return DaemonStartConfig{Addr: addr, CertDir: certDir}, nil
	}
	material, err := loadClientTLSMode(mode, true)
	if err != nil {
		return DaemonStartConfig{}, err
	}
	return daemonStartConfigFromMaterial(addr, material), nil
}

func daemonStartConfigFromMaterial(addr string, material clientTLSMaterial) DaemonStartConfig {
	cfg := DaemonStartConfig{Addr: addr, CertDir: material.mode.root}
	if !material.mode.managed {
		return cfg
	}
	paths := material.version.Paths()
	cfg.Local = true
	cfg.ManagedRoot = material.mode.root
	cfg.TLSCert = paths.ServerCertificate
	cfg.TLSKey = paths.ServerKey
	cfg.TLSCA = paths.CA
	cfg.ConfigsDir = resolveConfigsDir()
	return cfg
}

// resolveRepoRoot anchors dev-cert and configs discovery on the governed repository,
// mirroring the shell hooks' `git rev-parse --show-toplevel`. It prefers the --repo
// argument, then the --file directory, then the current working directory.
func resolveRepoRoot(repo, file string) string {
	if repo != "" {
		if root, err := gitOutput(repo, "rev-parse", "--show-toplevel"); err == nil {
			return root
		}
		if info, err := os.Stat(repo); err == nil && info.IsDir() {
			return repo
		}
	}
	if file != "" && filepath.IsAbs(file) {
		if root, err := gitOutput(filepath.Dir(file), "rev-parse", "--show-toplevel"); err == nil {
			return root
		}
	}
	if root, err := gitOutput(".", "rev-parse", "--show-toplevel"); err == nil {
		return root
	}
	return ""
}

// resolveDevCertDir mirrors the shell hooks: AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR
// wins, otherwise the machine governance root (ADR-0007).
func resolveDevCertDir() string {
	if dir := os.Getenv(envDevCertDir); dir != "" {
		return dir
	}
	return governanceCertsDir()
}

// resolveConfigsDir locates the configs directory the auto-started daemon must
// serve. AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR wins; otherwise the machine
// governance root (ADR-0007) — the daemon's configs dir, not the tracked
// per-repository <repo>/configs production handoff artifact.
func resolveConfigsDir() string {
	if dir := os.Getenv(envConfigsDir); dir != "" {
		return dir
	}
	return governanceConfigsDir()
}

func isLocalHTTPS(addr string) bool {
	parsed, err := url.Parse(addr)
	if err != nil || parsed.Scheme != "https" {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func daemonStartArgs(cfg DaemonStartConfig) []string {
	listenAddr := strings.TrimPrefix(strings.TrimPrefix(cfg.Addr, "http://"), "https://")
	args := []string{"server", "start", "--addr", listenAddr, "--block-on-warmup"}
	if cfg.ConfigsDir != "" {
		args = append(args, "--configs-dir", cfg.ConfigsDir)
	}
	return args
}

// daemonLogPath is where the detached daemon's stdout/stderr are captured: beside
// the gitignored dev certs, so it is repo-scoped and never committed. Empty when
// no cert dir was resolved — output capture is then simply unavailable.
func daemonLogPath(cfg DaemonStartConfig) string {
	if cfg.CertDir == "" {
		return ""
	}
	return filepath.Join(cfg.CertDir, "daemon.log")
}

// openDaemonLog opens the daemon log for append, creating it if needed, and writes
// a parent-side start header naming the listen address — so even an exec that never
// produces output leaves a trace. Falls back to io.Discard (never an error) when
// there is no log destination or the file cannot be opened: output capture is a
// diagnostic aid, never a new failure mode for daemon auto-start.
func openDaemonLog(cfg DaemonStartConfig, listenAddr string) io.WriteCloser {
	path := daemonLogPath(cfg)
	if path == "" {
		return nopWriteCloser{io.Discard}
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nopWriteCloser{io.Discard}
	}
	_, _ = fmt.Fprintf(file, "starting daemon addr=%s\n", listenAddr)
	return file
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// StartDaemon starts a detached daemon process using the current executable.
func StartDaemon(cfg DaemonStartConfig) error {
	// resolveConfigsDir always yields the governance default on the real CLI
	// path, so this guard is defense in depth for hand-built configs (tests,
	// future callers) rather than a reachable production failure.
	if cfg.Local && cfg.ConfigsDir == "" {
		return errors.New("no governance configs directory found: run `agent-fitness-functions client onboard` in the repository (or set AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR) before auto-starting the local daemon")
	}
	executable, err := daemonExecutable()
	if err != nil {
		return err
	}
	args := daemonStartArgs(cfg)
	log := openDaemonLog(cfg, listenAddrFromArgs(args))
	command := exec.Command(executable, args...)
	command.Env = daemonStartEnv(cfg, cfg.Env)
	command.Stdout = log
	command.Stderr = log
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		_ = log.Close()
		return err
	}
	_ = log.Close()
	return command.Process.Release()
}

// listenAddrFromArgs recovers the listen address daemonStartArgs computed, so the
// start-header log line names the same address the child was told to bind.
func listenAddrFromArgs(args []string) string {
	for i, arg := range args {
		if arg == "--addr" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func daemonStartEnv(cfg DaemonStartConfig, base []string) []string {
	if base == nil {
		base = os.Environ()
	}
	filtered := make([]string, 0, len(base)+1)
	for _, value := range base {
		name, _, _ := strings.Cut(value, "=")
		if name == envDevCertDir || name == "AGENT_FITNESS_FUNCTIONS_RUNTIME_DIR" {
			continue
		}
		filtered = append(filtered, value)
	}
	if cfg.ManagedRoot != "" {
		filtered = append(filtered, envDevCertDir+"="+cfg.ManagedRoot)
	}
	return filtered
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
