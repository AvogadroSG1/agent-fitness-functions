package client

import (
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	envClientCert = "STACK_FITNESS_FUNCTIONS_CLIENT_CERT"
	envClientKey  = "STACK_FITNESS_FUNCTIONS_CLIENT_KEY"
	envClientCA   = "STACK_FITNESS_FUNCTIONS_CLIENT_CA"
	envDevCertDir = "STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR"
	envConfigsDir = "STACK_FITNESS_FUNCTIONS_CONFIGS_DIR"
)

// DaemonStartConfig describes how the auto-started local daemon must be launched so
// the default https client can reach it. Local is true only for the zero-config
// loopback path, where dev TLS material and a repo configs directory are provisioned.
type DaemonStartConfig struct {
	Addr       string
	Local      bool
	TLSCert    string
	TLSKey     string
	TLSCA      string
	ConfigsDir string
	CertDir    string
}

// prepareDaemonStart resolves the auto-start configuration and, on the zero-config
// loopback path, materializes dev certificates so the auto-started TLS server and
// this client trust the same CA. Dev material is only provisioned when the caller
// passed no explicit client TLS flags and the addr is an https loopback URL.
func prepareDaemonStart(addr, certDir, repoRoot, certFlag, keyFlag, caFlag string) (DaemonStartConfig, error) {
	cfg := DaemonStartConfig{Addr: addr, CertDir: certDir}
	explicitTLS := certFlag != "" || keyFlag != "" || caFlag != ""
	if explicitTLS || certDir == "" || !isLocalHTTPS(addr) {
		return cfg, nil
	}
	if err := EnsureDevCerts(certDir); err != nil {
		return DaemonStartConfig{}, err
	}
	cfg.Local = true
	cfg.TLSCert = filepath.Join(certDir, devServerCertName)
	cfg.TLSKey = filepath.Join(certDir, devServerKeyName)
	cfg.TLSCA = filepath.Join(certDir, devCACertName)
	cfg.ConfigsDir = resolveConfigsDir(repoRoot)
	return cfg, nil
}

// resolveClientTLSPaths applies flag > env > dev-cert-dir default precedence to the
// mTLS client material. Default-dir paths are only used when the files exist, so a
// plain-HTTP local server still works when no dev certs are present. cert and key are
// resolved together at the default tier to avoid an unpaired half.
func resolveClientTLSPaths(certFlag, keyFlag, caFlag, certDir string) (cert, key, ca string) {
	cert = firstNonEmpty(certFlag, os.Getenv(envClientCert))
	key = firstNonEmpty(keyFlag, os.Getenv(envClientKey))
	ca = firstNonEmpty(caFlag, os.Getenv(envClientCA))
	if cert == "" && key == "" && certDir != "" {
		defaultCert := filepath.Join(certDir, devClientCertName)
		defaultKey := filepath.Join(certDir, devClientKeyName)
		if fileExists(defaultCert) && fileExists(defaultKey) {
			cert, key = defaultCert, defaultKey
		}
	}
	if ca == "" && certDir != "" {
		if defaultCA := filepath.Join(certDir, devCACertName); fileExists(defaultCA) {
			ca = defaultCA
		}
	}
	return cert, key, ca
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

// resolveDevCertDir mirrors the shell hooks: STACK_FITNESS_FUNCTIONS_DEV_CERT_DIR
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
// must serve. STACK_FITNESS_FUNCTIONS_CONFIGS_DIR wins; otherwise <repo-root>/configs
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
	args := []string{"server", "start", "--addr", listenAddr}
	if cfg.TLSCert != "" {
		args = append(args, "--tls-cert", cfg.TLSCert, "--tls-key", cfg.TLSKey, "--tls-ca", cfg.TLSCA)
	}
	if cfg.ConfigsDir != "" {
		args = append(args, "--configs-dir", cfg.ConfigsDir)
	}
	return args
}

// StartDaemon starts a detached daemon process using the current executable.
func StartDaemon(cfg DaemonStartConfig) error {
	if cfg.Local && cfg.ConfigsDir == "" {
		return errors.New("no repository configs directory found: set STACK_FITNESS_FUNCTIONS_CONFIGS_DIR or add <repo>/configs/<repo>/config.json before auto-starting the local daemon")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := exec.Command(executable, daemonStartArgs(cfg)...)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
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
