package client

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
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
)

func resolveClientTLSMode(certFlag, keyFlag, caFlag, defaultRoot string) (clientTLSMode, error) {
	selector := os.Getenv(envDevCertDir)
	explicit := certFlag != "" || keyFlag != "" || caFlag != "" || os.Getenv(envClientCert) != "" || os.Getenv(envClientKey) != "" || os.Getenv(envClientCA) != ""
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

func loadClientTLSMode(mode clientTLSMode, publish bool) (clientTLSMaterial, error) {
	material := clientTLSMaterial{mode: mode}
	if !mode.managed {
		return material, nil
	}
	if mode.root == "" {
		return clientTLSMaterial{}, errors.New("could not determine managed development certificate root")
	}
	if publish {
		if err := publishManagedCertificates(mode.root, false); err != nil {
			return clientTLSMaterial{}, err
		}
		if err := ensureCertsIgnoreProtection(mode.root); err != nil {
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

// prepareDaemonStart resolves the auto-start configuration and, on the zero-config
// loopback path, materializes dev certificates so the auto-started TLS server and
// this client trust the same CA. Dev material is only provisioned when the caller
// passed no explicit client TLS flags and the addr is an https loopback URL.
func prepareDaemonStart(addr, certDir, repoRoot, certFlag, keyFlag, caFlag string) (DaemonStartConfig, error) {
	mode, err := resolveClientTLSMode(certFlag, keyFlag, caFlag, certDir)
	if err != nil {
		return DaemonStartConfig{}, err
	}
	selector := os.Getenv(envDevCertDir)
	explicitServerTLS := os.Getenv(envServerCert) != "" || os.Getenv(envServerKey) != "" || os.Getenv(envServerCA) != ""
	if selector != "" && explicitServerTLS {
		return DaemonStartConfig{}, usageError{err: errors.New("AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR cannot be combined with explicit server or client TLS inputs")}
	}
	if !mode.managed || explicitServerTLS || certDir == "" || !isLocalHTTPS(addr) {
		return DaemonStartConfig{Addr: addr, CertDir: certDir}, nil
	}
	material, err := loadClientTLSMode(mode, true)
	if err != nil {
		return DaemonStartConfig{}, err
	}
	return daemonStartConfigFromMaterial(addr, repoRoot, material), nil
}

func daemonStartConfigFromMaterial(addr, repoRoot string, material clientTLSMaterial) DaemonStartConfig {
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
	cfg.ConfigsDir = resolveConfigsDir(repoRoot)
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
// wins, otherwise <repo-root>/certs.
func resolveDevCertDir(repoRoot string) string {
	if dir := os.Getenv(envDevCertDir); dir != "" {
		return dir
	}
	if repoRoot == "" {
		return ""
	}
	return filepath.Join(repoRoot, "certs")
}

// resolveConfigsDir locates the repository configs directory the auto-started daemon
// must serve. AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR wins; otherwise <repo-root>/configs
// is used when it exists. An empty result is surfaced to the user at start time.
func resolveConfigsDir(repoRoot string) string {
	if dir := os.Getenv(envConfigsDir); dir != "" {
		return dir
	}
	if repoRoot == "" {
		return ""
	}
	candidate := filepath.Join(repoRoot, "configs")
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}
	return ""
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

// StartDaemon starts a detached daemon process using the current executable.
func StartDaemon(cfg DaemonStartConfig) error {
	if cfg.Local && cfg.ConfigsDir == "" {
		return errors.New("no repository configs directory found: set AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR or add <repo>/configs/<repo>/config.json before auto-starting the local daemon")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := exec.Command(executable, daemonStartArgs(cfg)...)
	command.Env = daemonStartEnv(cfg, cfg.Env)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
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
