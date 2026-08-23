package client

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestResolveClientTLSPathsPrecedence(t *testing.T) {
	certDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(certDir, "current"), 0o755); err != nil {
		t.Fatalf("mkdir current: %v", err)
	}
	writeFileTest(t, filepath.Join(certDir, "current", devClientCertName), "cert")
	writeFileTest(t, filepath.Join(certDir, "current", devClientKeyName), "key")
	writeFileTest(t, filepath.Join(certDir, "current", devCACertName), "ca")

	cases := []struct {
		name                      string
		certFlag, keyFlag, caFlag string
		envCert, envKey, envCA    string
		useCertDir                bool
		wantCert, wantKey, wantCA string
	}{
		{
			name:     "flag wins over env and default",
			certFlag: "/flag/cert", keyFlag: "/flag/key", caFlag: "/flag/ca",
			envCert: "/env/cert", envKey: "/env/key", envCA: "/env/ca",
			useCertDir: true,
			wantCert:   "/flag/cert", wantKey: "/flag/key", wantCA: "/flag/ca",
		},
		{
			name:    "env wins over default",
			envCert: "/env/cert", envKey: "/env/key", envCA: "/env/ca",
			useCertDir: true,
			wantCert:   "/env/cert", wantKey: "/env/key", wantCA: "/env/ca",
		},
		{
			name:       "default dir used when files exist",
			useCertDir: true,
			wantCert:   filepath.Join(certDir, "current", devClientCertName),
			wantKey:    filepath.Join(certDir, "current", devClientKeyName),
			wantCA:     filepath.Join(certDir, "current", devCACertName),
		},
		{
			name:       "none when no flags, env, or files",
			useCertDir: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envClientCert, tc.envCert)
			t.Setenv(envClientKey, tc.envKey)
			t.Setenv(envClientCA, tc.envCA)
			dir := ""
			if tc.useCertDir {
				dir = certDir
			}
			cert, key, ca := resolveClientTLSPaths(tc.certFlag, tc.keyFlag, tc.caFlag, dir)
			if cert != tc.wantCert || key != tc.wantKey || ca != tc.wantCA {
				t.Fatalf("resolveClientTLSPaths = (%q, %q, %q), want (%q, %q, %q)", cert, key, ca, tc.wantCert, tc.wantKey, tc.wantCA)
			}
		})
	}
}

func TestResolveClientTLSPathsDefaultCertKeyPairedTogether(t *testing.T) {
	t.Setenv(envClientCert, "")
	t.Setenv(envClientKey, "")
	t.Setenv(envClientCA, "")
	certDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(certDir, "current"), 0o755); err != nil {
		t.Fatalf("mkdir current: %v", err)
	}
	// Only the certificate exists; the key is missing, so neither is used.
	writeFileTest(t, filepath.Join(certDir, "current", devClientCertName), "cert")
	writeFileTest(t, filepath.Join(certDir, "current", devCACertName), "ca")

	cert, key, ca := resolveClientTLSPaths("", "", "", certDir)
	if cert != "" || key != "" {
		t.Fatalf("cert/key = (%q, %q), want both empty when key file is absent", cert, key)
	}
	if ca != filepath.Join(certDir, "current", devCACertName) {
		t.Fatalf("ca = %q, want discovered default", ca)
	}
}

func TestEnsureDevCertsGeneratesFullSet(t *testing.T) {
	certDir := filepath.Join(t.TempDir(), "certs")
	if err := EnsureDevCerts(certDir); err != nil {
		t.Fatalf("EnsureDevCerts returned error: %v", err)
	}

	certDir = filepath.Join(certDir, "current")
	assertFileMode(t, filepath.Join(certDir, devCACertName), 0o644)
	assertFileMode(t, filepath.Join(certDir, devServerCertName), 0o644)
	assertFileMode(t, filepath.Join(certDir, devServerKeyName), 0o600)
	assertFileMode(t, filepath.Join(certDir, devClientCertName), 0o644)
	assertFileMode(t, filepath.Join(certDir, devClientKeyName), 0o600)

	caCert := loadCertTest(t, filepath.Join(certDir, devCACertName))
	if caCert.Subject.CommonName != devCACommonName || !caCert.IsCA {
		t.Fatalf("ca subject = %q isCA=%v, want %q isCA=true", caCert.Subject.CommonName, caCert.IsCA, devCACommonName)
	}

	serverCert := loadCertTest(t, filepath.Join(certDir, devServerCertName))
	if serverCert.Subject.CommonName != devServerCommonName {
		t.Fatalf("server CN = %q, want %q", serverCert.Subject.CommonName, devServerCommonName)
	}
	if !slices.Contains(serverCert.DNSNames, "localhost") || !slices.Contains(serverCert.DNSNames, "agent-fitness-functions") {
		t.Fatalf("server DNS names = %v, want localhost and agent-fitness-functions", serverCert.DNSNames)
	}
	if !containsIP(serverCert.IPAddresses, net.IPv4(127, 0, 0, 1)) || !containsIP(serverCert.IPAddresses, net.IPv6loopback) {
		t.Fatalf("server IPs = %v, want loopback IPv4 and IPv6", serverCert.IPAddresses)
	}
	if !slices.Contains(serverCert.ExtKeyUsage, x509.ExtKeyUsageServerAuth) {
		t.Fatalf("server ext key usage = %v, want serverAuth", serverCert.ExtKeyUsage)
	}

	clientCert := loadCertTest(t, filepath.Join(certDir, devClientCertName))
	if clientCert.Subject.CommonName != devClientCommonName {
		t.Fatalf("client CN = %q, want %q", clientCert.Subject.CommonName, devClientCommonName)
	}
	if !slices.Contains(clientCert.ExtKeyUsage, x509.ExtKeyUsageClientAuth) {
		t.Fatalf("client ext key usage = %v, want clientAuth", clientCert.ExtKeyUsage)
	}

	assertServerCertChainsToCA(t, certDir)
	assertKeyPairsLoad(t, certDir)
}

func TestEnsureDevCertsIsIdempotent(t *testing.T) {
	certDir := filepath.Join(t.TempDir(), "certs")
	if err := EnsureDevCerts(certDir); err != nil {
		t.Fatalf("first EnsureDevCerts returned error: %v", err)
	}
	before := readDevCertSet(t, filepath.Join(certDir, "current"))
	if err := EnsureDevCerts(certDir); err != nil {
		t.Fatalf("second EnsureDevCerts returned error: %v", err)
	}
	after := readDevCertSet(t, filepath.Join(certDir, "current"))
	for name, data := range before {
		if !bytes.Equal(data, after[name]) {
			t.Fatalf("%s changed on second EnsureDevCerts, want idempotent no-op", name)
		}
	}
}

func TestEnsureDevCertsRejectsPartialSet(t *testing.T) {
	certDir := t.TempDir()
	writeFileTest(t, filepath.Join(certDir, devCACertName), "stale")

	err := EnsureDevCerts(certDir)
	if err == nil {
		t.Fatal("EnsureDevCerts succeeded on a partial set, want error")
	}
	if !strings.Contains(err.Error(), "unsupported for bootstrap") {
		t.Fatalf("error = %v, want bootstrap refusal", err)
	}
	// The generator must not have overwritten or completed the partial set.
	if _, statErr := os.Stat(filepath.Join(certDir, devServerCertName)); !os.IsNotExist(statErr) {
		t.Fatalf("server cert exists after partial-set rejection, want untouched")
	}
}

func TestDaemonStartArgsUsesManagedSelectorWithoutTLSArguments(t *testing.T) {
	cfg := DaemonStartConfig{
		Addr:        "https://127.0.0.1:7890",
		Local:       true,
		ManagedRoot: "/repo/certs",
		ConfigsDir:  "/repo/configs",
	}
	args := daemonStartArgs(cfg)
	want := []string{
		"server", "start", "--addr", "127.0.0.1:7890",
		"--configs-dir", "/repo/configs",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("daemonStartArgs = %v, want %v", args, want)
	}
	env := daemonStartEnv(cfg, []string{"PATH=/bin", envDevCertDir + "=/old", "STACK_FITNESS_FUNCTIONS_RUNTIME_DIR=/run/old"})
	if !slices.Contains(env, envDevCertDir+"=/repo/certs") {
		t.Fatalf("daemon env = %v, want managed selector", env)
	}
	for _, value := range env {
		if strings.HasPrefix(value, "STACK_FITNESS_FUNCTIONS_RUNTIME_DIR=") {
			t.Fatalf("daemon env retained host runtime directory: %q", value)
		}
	}
}

func TestDaemonStartArgsOmitsTLSWhenAbsent(t *testing.T) {
	args := daemonStartArgs(DaemonStartConfig{Addr: "http://localhost:7890"})
	want := []string{"server", "start", "--addr", "localhost:7890"}
	if !slices.Equal(args, want) {
		t.Fatalf("daemonStartArgs = %v, want %v", args, want)
	}
}

func TestPrepareDaemonStartProvisionsLocalDevMode(t *testing.T) {
	t.Setenv(envClientCert, "")
	t.Setenv(envClientKey, "")
	t.Setenv(envClientCA, "")
	repoRoot := t.TempDir()
	certDir := filepath.Join(repoRoot, "certs")
	if err := os.MkdirAll(filepath.Join(repoRoot, "configs"), 0o755); err != nil {
		t.Fatalf("mkdir configs: %v", err)
	}
	t.Setenv(envConfigsDir, "")

	cfg, err := prepareDaemonStart("https://127.0.0.1:7890", certDir, repoRoot, "", "", "")
	if err != nil {
		t.Fatalf("prepareDaemonStart returned error: %v", err)
	}
	if !cfg.Local {
		t.Fatal("cfg.Local = false, want true for https loopback zero-config path")
	}
	if cfg.ManagedRoot != certDir {
		t.Fatalf("cfg.ManagedRoot = %q, want %q", cfg.ManagedRoot, certDir)
	}
	if cfg.ConfigsDir != filepath.Join(repoRoot, "configs") {
		t.Fatalf("cfg.ConfigsDir = %q, want <repo>/configs", cfg.ConfigsDir)
	}
	if _, err := os.Stat(filepath.Join(certDir, "current", devServerKeyName)); err != nil {
		t.Fatalf("dev certs not generated: %v", err)
	}
}

func TestPrepareDaemonStartSkipsWhenExplicitTLSFlags(t *testing.T) {
	t.Setenv(envDevCertDir, "")
	certDir := filepath.Join(t.TempDir(), "certs")
	cfg, err := prepareDaemonStart("https://127.0.0.1:7890", certDir, t.TempDir(), "/my/cert", "/my/key", "/my/ca")
	if err != nil {
		t.Fatalf("prepareDaemonStart returned error: %v", err)
	}
	if cfg.Local {
		t.Fatal("cfg.Local = true, want false when explicit TLS flags are supplied")
	}
	if _, err := os.Stat(certDir); !os.IsNotExist(err) {
		t.Fatalf("dev certs generated despite explicit flags: stat err = %v", err)
	}
}

func TestPrepareDaemonStartRejectsManagedSelectorWithExplicitClientTLSBeforePublication(t *testing.T) {
	certDir := filepath.Join(t.TempDir(), "certs")
	t.Setenv(envDevCertDir, certDir)
	_, err := prepareDaemonStart("https://127.0.0.1:7890", certDir, t.TempDir(), "client.crt", "client.key", "ca.crt")
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("prepareDaemonStart conflict error = %v", err)
	}
	if _, statErr := os.Lstat(certDir); !os.IsNotExist(statErr) {
		t.Fatalf("managed root changed before conflict rejection: %v", statErr)
	}
}

func TestPrepareDaemonStartPreservesServerTLSEnvAsExternalWithoutManagedSelector(t *testing.T) {
	certDir := filepath.Join(t.TempDir(), "certs")
	t.Setenv(envDevCertDir, "")
	t.Setenv(envServerCA, "/external/ca.crt")
	cfg, err := prepareDaemonStart("https://127.0.0.1:7890", certDir, t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("prepareDaemonStart external server TLS: %v", err)
	}
	if cfg.ManagedRoot != "" || cfg.Local {
		t.Fatalf("cfg = %+v, want external daemon mode", cfg)
	}
	if _, statErr := os.Lstat(certDir); !os.IsNotExist(statErr) {
		t.Fatalf("managed root changed in external mode: %v", statErr)
	}
}

func TestPrepareDaemonStartRejectsManagedSelectorWithServerTLSEnv(t *testing.T) {
	certDir := filepath.Join(t.TempDir(), "certs")
	t.Setenv(envDevCertDir, certDir)
	t.Setenv(envServerCA, "/external/ca.crt")
	_, err := prepareDaemonStart("https://127.0.0.1:7890", certDir, t.TempDir(), "", "", "")
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("prepareDaemonStart managed/server conflict error = %v", err)
	}
	if _, statErr := os.Lstat(certDir); !os.IsNotExist(statErr) {
		t.Fatalf("managed root changed before conflict rejection: %v", statErr)
	}
}

func TestPrepareDaemonStartSkipsForNonLoopbackAddr(t *testing.T) {
	certDir := filepath.Join(t.TempDir(), "certs")
	cfg, err := prepareDaemonStart("https://example.com:7890", certDir, t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("prepareDaemonStart returned error: %v", err)
	}
	if cfg.Local {
		t.Fatal("cfg.Local = true, want false for a non-loopback addr")
	}
	if _, err := os.Stat(certDir); !os.IsNotExist(err) {
		t.Fatalf("dev certs generated for non-loopback addr: stat err = %v", err)
	}
}

func TestStartDaemonRequiresConfigsDirInLocalMode(t *testing.T) {
	err := StartDaemon(DaemonStartConfig{Addr: "https://127.0.0.1:7890", Local: true})
	if err == nil {
		t.Fatal("StartDaemon succeeded without a configs dir, want error")
	}
	if !strings.Contains(err.Error(), "configs directory") {
		t.Fatalf("error = %v, want configs directory guidance", err)
	}
}

func TestIsLocalHTTPS(t *testing.T) {
	cases := map[string]bool{
		"https://127.0.0.1:7890": true,
		"https://localhost:7890": true,
		"https://[::1]:7890":     true,
		"http://127.0.0.1:7890":  false,
		"https://example.com":    false,
		"not a url":              false,
	}
	for addr, want := range cases {
		if got := isLocalHTTPS(addr); got != want {
			t.Fatalf("isLocalHTTPS(%q) = %v, want %v", addr, got, want)
		}
	}
}

func writeFileTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("%s mode = %v, want %v", path, info.Mode().Perm(), want)
	}
}

func loadCertTest(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatalf("no PEM certificate in %s", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return cert
}

func assertServerCertChainsToCA(t *testing.T, certDir string) {
	t.Helper()
	caPEM, err := os.ReadFile(filepath.Join(certDir, devCACertName))
	if err != nil {
		t.Fatalf("read ca: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("appending dev CA failed")
	}
	serverCert := loadCertTest(t, filepath.Join(certDir, devServerCertName))
	if _, err := serverCert.Verify(x509.VerifyOptions{Roots: roots, DNSName: "localhost"}); err != nil {
		t.Fatalf("server cert does not verify against dev CA: %v", err)
	}
}

func assertKeyPairsLoad(t *testing.T, certDir string) {
	t.Helper()
	if _, err := tls.LoadX509KeyPair(filepath.Join(certDir, devServerCertName), filepath.Join(certDir, devServerKeyName)); err != nil {
		t.Fatalf("server key pair does not load: %v", err)
	}
	if _, err := tls.LoadX509KeyPair(filepath.Join(certDir, devClientCertName), filepath.Join(certDir, devClientKeyName)); err != nil {
		t.Fatalf("client key pair does not load: %v", err)
	}
}

func readDevCertSet(t *testing.T, certDir string) map[string][]byte {
	t.Helper()
	set := make(map[string][]byte, len(devCertFileNames))
	for _, name := range devCertFileNames {
		data, err := os.ReadFile(filepath.Join(certDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		set[name] = data
	}
	return set
}

func containsIP(ips []net.IP, want net.IP) bool {
	for _, ip := range ips {
		if ip.Equal(want) {
			return true
		}
	}
	return false
}
