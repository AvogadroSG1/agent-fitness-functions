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

	"github.com/AvogadroSG1/agent-fitness-functions/internal/devcerts"
)

func TestResolveClientTLSModeRejectsManagedSelectorWithEveryExplicitInput(t *testing.T) {
	tests := []struct {
		name            string
		cert, key, ca   string
		envCert, envKey string
		envCA           string
	}{
		{name: "certificate flag", cert: "/flag/cert"},
		{name: "key flag", key: "/flag/key"},
		{name: "CA flag", ca: "/flag/ca"},
		{name: "certificate environment", envCert: "/env/cert"},
		{name: "key environment", envKey: "/env/key"},
		{name: "CA environment", envCA: "/env/ca"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envDevCertDir, filepath.Join(t.TempDir(), "missing"))
			t.Setenv(envClientCert, tt.envCert)
			t.Setenv(envClientKey, tt.envKey)
			t.Setenv(envClientCA, tt.envCA)
			_, err := resolveClientTLSMode(tt.cert, tt.key, tt.ca, "/repo/certs")
			if err == nil || !IsUsageError(err) {
				t.Fatalf("resolveClientTLSMode error = %v, want usage error", err)
			}
			if !strings.Contains(err.Error(), envDevCertDir) || !strings.Contains(err.Error(), "explicit client TLS") {
				t.Fatalf("error = %q, want exact selector/explicit-input conflict", err)
			}
		})
	}
}

func TestResolveClientTLSModeKeepsExternalInputsExternal(t *testing.T) {
	t.Setenv(envDevCertDir, "")
	t.Setenv(envClientCert, "/external/client.crt")
	t.Setenv(envClientKey, "/external/client.key")
	t.Setenv(envClientCA, "/external/ca.crt")

	mode, err := resolveClientTLSMode("", "", "", "/repo/certs")
	if err != nil {
		t.Fatalf("resolveClientTLSMode: %v", err)
	}
	if mode.managed {
		t.Fatal("mode.managed = true, want external mode")
	}
	if mode.cert != "/external/client.crt" || mode.key != "/external/client.key" || mode.ca != "/external/ca.crt" {
		t.Fatalf("external mode = %+v, want exact environment paths", mode)
	}
}

func TestLoadManagedClientModePublishesAndResolvesExactlyOnce(t *testing.T) {
	t.Setenv(envDevCertDir, "")
	t.Setenv(envClientCert, "")
	t.Setenv(envClientKey, "")
	t.Setenv(envClientCA, "")
	root := filepath.Join(t.TempDir(), "certs")
	mode, err := resolveClientTLSMode("", "", "", root)
	if err != nil {
		t.Fatalf("resolveClientTLSMode: %v", err)
	}

	publishes, resolves := 0, 0
	originalPublish := publishManagedCertificates
	originalResolve := resolveManagedVersion
	publishManagedCertificates = func(root string, force bool) error {
		publishes++
		return devcerts.Publish(root, force)
	}
	resolveManagedVersion = func(root string) (devcerts.ManagedVersion, error) {
		resolves++
		return devcerts.ResolveManagedVersion(root)
	}
	t.Cleanup(func() {
		publishManagedCertificates = originalPublish
		resolveManagedVersion = originalResolve
	})

	material, err := loadClientTLSMode(mode, true)
	if err != nil {
		t.Fatalf("loadClientTLSMode: %v", err)
	}
	if publishes != 1 || resolves != 1 {
		t.Fatalf("publication/resolution calls = %d/%d, want 1/1", publishes, resolves)
	}
	if material.version.RelativePath() == "" || material.certificate.Leaf == nil || material.roots == nil {
		t.Fatalf("managed material incomplete: %+v", material)
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
		"server", "start", "--addr", "127.0.0.1:7890", "--block-on-warmup",
		"--configs-dir", "/repo/configs",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("daemonStartArgs = %v, want %v", args, want)
	}
	env := daemonStartEnv(cfg, []string{"PATH=/bin", envDevCertDir + "=/old", "AGENT_FITNESS_FUNCTIONS_RUNTIME_DIR=/run/old"})
	if !slices.Contains(env, envDevCertDir+"=/repo/certs") {
		t.Fatalf("daemon env = %v, want managed selector", env)
	}
	for _, value := range env {
		if strings.HasPrefix(value, "AGENT_FITNESS_FUNCTIONS_RUNTIME_DIR=") {
			t.Fatalf("daemon env retained host runtime directory: %q", value)
		}
	}
}

func TestDaemonStartArgsOmitsTLSWhenAbsent(t *testing.T) {
	args := daemonStartArgs(DaemonStartConfig{Addr: "http://localhost:7890"})
	want := []string{"server", "start", "--addr", "localhost:7890", "--block-on-warmup"}
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
	if filepath.Dir(filepath.Dir(cfg.TLSCert)) != filepath.Join(certDir, "versions") || filepath.Base(cfg.TLSCert) != devServerCertName || filepath.Dir(cfg.TLSCA) != filepath.Dir(cfg.TLSCert) || filepath.Base(cfg.TLSCA) != devCACertName {
		t.Fatalf("cfg tls paths = %+v, want one pinned version", cfg)
	}
	if cfg.ConfigsDir != filepath.Join(repoRoot, "configs") {
		t.Fatalf("cfg.ConfigsDir = %q, want <repo>/configs", cfg.ConfigsDir)
	}
	if _, err := os.Stat(cfg.TLSKey); err != nil {
		t.Fatalf("dev certs not generated: %v", err)
	}
}

func TestDaemonStartConfigKeepsResolvedServerPathsAfterCurrentRotation(t *testing.T) {
	t.Setenv(envDevCertDir, "")
	t.Setenv(envClientCert, "")
	t.Setenv(envClientKey, "")
	t.Setenv(envClientCA, "")
	root := filepath.Join(t.TempDir(), "certs")
	mode, err := resolveClientTLSMode("", "", "", root)
	if err != nil {
		t.Fatalf("resolveClientTLSMode: %v", err)
	}
	material, err := loadClientTLSMode(mode, true)
	if err != nil {
		t.Fatalf("loadClientTLSMode: %v", err)
	}
	want := material.version.Paths()
	if err := os.Remove(filepath.Join(root, "current")); err != nil {
		t.Fatalf("Remove(current): %v", err)
	}
	if err := os.Symlink("versions/v-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", filepath.Join(root, "current")); err != nil {
		t.Fatalf("Symlink(rotated current): %v", err)
	}

	cfg := daemonStartConfigFromMaterial("https://127.0.0.1:7890", t.TempDir(), material)
	if cfg.TLSCert != want.ServerCertificate || cfg.TLSKey != want.ServerKey || cfg.TLSCA != want.CA {
		t.Fatalf("daemon TLS paths after rotation = (%q, %q, %q), want pinned (%q, %q, %q)", cfg.TLSCert, cfg.TLSKey, cfg.TLSCA, want.ServerCertificate, want.ServerKey, want.CA)
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
