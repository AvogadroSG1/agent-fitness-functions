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
	envClientCert    = "AGENT_FITNESS_FUNCTIONS_CLIENT_CERT"
	envClientKey     = "AGENT_FITNESS_FUNCTIONS_CLIENT_KEY"
	envClientCA      = "AGENT_FITNESS_FUNCTIONS_CLIENT_CA"
	envDevCertDir    = "AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR"
	envConfigsDir    = "AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR"
	envServerCert    = "AGENT_FITNESS_FUNCTIONS_TLS_CERT"
	envServerKey     = "AGENT_FITNESS_FUNCTIONS_TLS_KEY"
	envServerCA      = "AGENT_FITNESS_FUNCTIONS_TLS_CA"
	envClientTimeout = "AGENT_FITNESS_FUNCTIONS_CLIENT_TIMEOUT"
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

// explicit reports whether this mode carries caller-supplied client TLS material.
// Unmanaged is not the same as explicit: a remote --addr turns managed provisioning
// off and leaves a zero mode with no certificate paths at all.
func (mode clientTLSMode) explicit() bool {
	return !mode.managed && (mode.cert != "" || mode.key != "" || mode.ca != "")
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
	// ExplicitClientTLS records that the caller supplied client TLS material, so a
	// failure message can distinguish it from unmanaged-with-no-certificates.
	ExplicitClientTLS bool
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

// prepareDaemonStart resolves the auto-start configuration. A loopback http addr
// is the ADR-0010 managed-local path and needs no certificate material at all; a
// loopback https addr is the legacy managed-TLS local path, where dev certificates
// are materialized so the auto-started TLS server and this client trust the same
// CA. Dev material is only provisioned when the caller passed no explicit client
// TLS flags and the addr is an https loopback URL.
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
	if isLocalHTTP(addr) && !explicitServerTLS {
		return localHTTPDaemonStartConfig(addr, mode), nil
	}
	if skipsManagedProvisioning(mode, explicitServerTLS, certDir, addr) {
		return DaemonStartConfig{Addr: addr, CertDir: certDir, ExplicitClientTLS: mode.explicit()}, nil
	}
	material, err := loadClientTLSMode(mode, true)
	if err != nil {
		return DaemonStartConfig{}, err
	}
	return daemonStartConfigFromMaterial(addr, material), nil
}

// localHTTPDaemonStartConfig is the ADR-0010 managed-local start configuration: a
// loopback plain-HTTP daemon authenticates callers by loopback peer, so the start
// owns a governance configs directory and no certificate material whatsoever — no
// managed root to publish into, no server certificate, no CA.
func localHTTPDaemonStartConfig(addr string, mode clientTLSMode) DaemonStartConfig {
	return DaemonStartConfig{
		Addr:              addr,
		Local:             true,
		ConfigsDir:        resolveConfigsDir(),
		ExplicitClientTLS: mode.explicit(),
	}
}

func daemonStartConfigFromMaterial(addr string, material clientTLSMaterial) DaemonStartConfig {
	if isLocalHTTP(addr) {
		return localHTTPDaemonStartConfig(addr, material.mode)
	}
	cfg := DaemonStartConfig{Addr: addr, CertDir: material.mode.root, ExplicitClientTLS: material.mode.explicit()}
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

// localDaemonScheme is the single classification rule behind every local-vs-remote
// decision in this package. It returns the URL scheme of addr when addr names a
// loopback governance daemon, and "" for anything else:
//
//   - loopback http  → managed-local governance in the ADR-0010 plain-HTTP listen
//     mode: the daemon authenticates by loopback peer, so the client carries no
//     certificate material at all;
//   - loopback https → the legacy managed-TLS local daemon, still supported while
//     machines migrate off mTLS;
//   - anything else  → remote/unmanaged: the caller owns its own TLS material and
//     nothing is auto-provisioned or auto-started on its behalf.
func localDaemonScheme(addr string) string {
	parsed, err := url.Parse(addr)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return parsed.Scheme
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return parsed.Scheme
	}
	return ""
}

func isLocalHTTPS(addr string) bool { return localDaemonScheme(addr) == "https" }

func isLocalHTTP(addr string) bool { return localDaemonScheme(addr) == "http" }

// alternateSchemeAddr is addr with its scheme flipped between http and https,
// defined only for loopback daemon addresses: scheme fallback is a migration aid
// for the machine-local daemon (ADR-0010), never applied to a remote endpoint
// where retrying https as http would be a transport-security downgrade.
func alternateSchemeAddr(addr string) string {
	switch localDaemonScheme(addr) {
	case "http":
		return "https://" + strings.TrimPrefix(addr, "http://")
	case "https":
		return "http://" + strings.TrimPrefix(addr, "https://")
	default:
		return ""
	}
}

// localHTTPStart reports whether this auto-start must launch the daemon in the
// ADR-0010 loopback plain-HTTP listen mode: a managed-local start whose address is
// a loopback http URL. A managed-local start against a loopback https URL is the
// legacy mTLS daemon and keeps the server's default listen mode.
func localHTTPStart(cfg DaemonStartConfig) bool {
	return cfg.Local && !isLocalHTTPS(cfg.Addr)
}

func daemonStartArgs(cfg DaemonStartConfig) []string {
	listenAddr := strings.TrimPrefix(strings.TrimPrefix(cfg.Addr, "http://"), "https://")
	args := []string{"server", "start", "--addr", listenAddr, "--block-on-warmup"}
	if localHTTPStart(cfg) {
		args = append(args, "--listen-mode", "local-http")
	}
	if cfg.ConfigsDir != "" {
		args = append(args, "--configs-dir", cfg.ConfigsDir)
	}
	return args
}

// daemonLogPath is where the detached daemon's stdout/stderr are captured: beside
// the gitignored dev certs when a managed-TLS start resolved a cert dir, otherwise
// (the ADR-0010 local-http start, which owns no cert dir) directly in the machine
// governance root. Empty for an unmanaged start, which owns neither location —
// output capture is then simply unavailable.
func daemonLogPath(cfg DaemonStartConfig) string {
	if cfg.CertDir != "" {
		return filepath.Join(cfg.CertDir, "daemon.log")
	}
	if cfg.Local {
		return filepath.Join(governanceRoot(), "daemon.log")
	}
	return ""
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
	// The local-http start owns no cert dir, so the governance root may not exist
	// yet on a machine that has never published dev certificates there.
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
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
