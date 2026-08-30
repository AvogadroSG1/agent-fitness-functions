package client

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/devcerts"
)

func TestRunOnboardInvalidExternalTLSMakesZeroMutations(t *testing.T) {
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)
	external := t.TempDir()
	cert := filepath.Join(external, "client.crt")
	key := filepath.Join(external, "client.key")
	ca := filepath.Join(external, "ca.crt")
	for _, path := range []string{cert, key, ca} {
		if err := os.WriteFile(path, []byte("invalid external material"), 0o600); err != nil {
			t.Fatalf("WriteFile(%q): %v", path, err)
		}
	}
	t.Setenv(envDevCertDir, "")
	t.Setenv(envClientCert, cert)
	t.Setenv(envClientKey, key)
	t.Setenv(envClientCA, ca)
	starterCalls := 0
	err := RunOnboard([]string{repo}, io.Discard, io.Discard, &http.Client{}, func(DaemonStartConfig) error {
		starterCalls++
		return nil
	})
	if err == nil || !IsUsageError(err) {
		t.Fatalf("RunOnboard invalid external TLS error = %v, want usage error", err)
	}
	if starterCalls != 0 {
		t.Fatalf("starter calls = %d, want 0", starterCalls)
	}
	for _, path := range []string{
		filepath.Join(repo, "configs"),
		filepath.Join(repo, callerRepoBindingsFileName),
		filepath.Join(repo, ".claude"),
		filepath.Join(repo, ".git", "hooks", "pre-commit"),
		filepath.Join(repo, ".git", "hooks", "pre-push"),
	} {
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			t.Fatalf("invalid external onboarding mutated %q: %v", path, statErr)
		}
	}
}

func TestRunOnboardExternalTLSRequiresHealthyDaemonBeforeMutation(t *testing.T) {
	fixture := writeExternalClientTLSFixture(t, "external-offline-client")
	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)
	t.Setenv(envDevCertDir, "")
	t.Setenv(envClientCert, fixture.clientCert)
	t.Setenv(envClientKey, fixture.clientKey)
	t.Setenv(envClientCA, fixture.ca)
	err := RunOnboard([]string{"--repo", "sample", "--addr", "https://127.0.0.1:1", repo}, io.Discard, io.Discard, &http.Client{Timeout: 100 * time.Millisecond}, func(DaemonStartConfig) error {
		t.Fatal("external onboarding must not call starter")
		return nil
	})
	if err == nil || !IsUsageError(err) || !strings.Contains(err.Error(), "already-running healthy daemon") {
		t.Fatalf("RunOnboard offline external daemon error = %v, want actionable usage error", err)
	}
	for _, path := range []string{filepath.Join(repo, "configs"), filepath.Join(repo, callerRepoBindingsFileName), filepath.Join(repo, ".claude")} {
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			t.Fatalf("offline external onboarding mutated %q: %v", path, statErr)
		}
	}
}

func TestExternalOnboardMaterialAuthorizesLeafCommonName(t *testing.T) {
	const wantCN = "external-governance-client"
	fixture := writeExternalClientTLSFixture(t, wantCN)
	mode := clientTLSMode{cert: fixture.clientCert, key: fixture.clientKey, ca: fixture.ca}
	material, callerCN, err := loadExternalOnboardMaterial(mode)
	if err != nil {
		t.Fatalf("loadExternalOnboardMaterial: %v", err)
	}
	if callerCN != wantCN || material.certificate.Leaf == nil || material.certificate.Leaf.Subject.CommonName != wantCN {
		t.Fatalf("external caller identity = %q/%v, want %q", callerCN, material.certificate.Leaf, wantCN)
	}

	configsDir := filepath.Join(t.TempDir(), "configs")
	o := onboarder{repoName: "sample", configsDir: configsDir, callerCN: callerCN, stdout: io.Discard}
	if err := o.authorizeCaller(); err != nil {
		t.Fatalf("authorizeCaller: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(filepath.Dir(configsDir), callerRepoBindingsFileName))
	if err != nil {
		t.Fatalf("ReadFile(caller bindings): %v", err)
	}
	if !bytes.Contains(content, []byte(`"`+wantCN+`"`)) || bytes.Contains(content, []byte(`"dev-hook-pool"`)) {
		t.Fatalf("caller bindings = %s, want actual external CN only", content)
	}
}

func TestLoadExternalOnboardMaterialRejectsUnsafeIdentityProfiles(t *testing.T) {
	valid := writeExternalClientTLSFixture(t, "valid-client")
	other := writeExternalClientTLSFixture(t, "other-client")
	emptyCN := writeExternalClientTLSFixture(t, "")
	leadingSpaceCN := writeExternalClientTLSFixture(t, " leading-space-client")
	trailingSpaceCN := writeExternalClientTLSFixture(t, "trailing-space-client ")
	tests := []struct {
		name string
		mode clientTLSMode
	}{
		{name: "mismatched key", mode: clientTLSMode{cert: valid.clientCert, key: other.clientKey, ca: valid.ca}},
		{name: "empty common name", mode: clientTLSMode{cert: emptyCN.clientCert, key: emptyCN.clientKey, ca: emptyCN.ca}},
		{name: "leading common name whitespace", mode: clientTLSMode{cert: leadingSpaceCN.clientCert, key: leadingSpaceCN.clientKey, ca: leadingSpaceCN.ca}},
		{name: "trailing common name whitespace", mode: clientTLSMode{cert: trailingSpaceCN.clientCert, key: trailingSpaceCN.clientKey, ca: trailingSpaceCN.ca}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := loadExternalOnboardMaterial(tt.mode); err == nil || !IsUsageError(err) {
				t.Fatalf("loadExternalOnboardMaterial error = %v, want usage rejection", err)
			}
		})
	}
}

func TestRunOnboardNoncanonicalExternalCNMakesZeroMutations(t *testing.T) {
	for _, commonName := range []string{" leading-space-client", "trailing-space-client "} {
		t.Run(commonName, func(t *testing.T) {
			fixture := writeExternalClientTLSFixture(t, commonName)
			repo := t.TempDir()
			useDeterministicGitClientTest(t, repo)
			t.Setenv(envDevCertDir, "")
			t.Setenv(envClientCert, fixture.clientCert)
			t.Setenv(envClientKey, fixture.clientKey)
			t.Setenv(envClientCA, fixture.ca)
			err := RunOnboard([]string{"--repo", "sample", repo}, io.Discard, io.Discard, &http.Client{}, func(DaemonStartConfig) error {
				t.Fatal("noncanonical external identity must not call starter")
				return nil
			})
			if err == nil || !IsUsageError(err) || !strings.Contains(err.Error(), "leading or trailing whitespace") {
				t.Fatalf("RunOnboard noncanonical external CN error = %v, want whitespace usage error", err)
			}
			for _, path := range []string{filepath.Join(repo, "configs"), filepath.Join(repo, callerRepoBindingsFileName), filepath.Join(repo, ".claude")} {
				if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
					t.Fatalf("noncanonical external identity mutated %q: %v", path, statErr)
				}
			}
		})
	}
}

func TestRunOnboardValidExternalTLSRegistersActualCNWithoutAutostart(t *testing.T) {
	const wantCN = "external-run-client"
	clientFixture := writeExternalClientTLSFixture(t, wantCN)
	serverFixture := writeExternalClientTLSFixture(t, "unrelated-server-hierarchy-client")
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/register":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"repo": "sample", "created": true})
		case "/preflight":
			_ = json.NewEncoder(w).Encode(preflightReport{
				AuthenticatedCN: wantCN, RepoConfigured: true, RepoConfigValid: true,
				CallerAuthorized: true, EnforcementMode: "advisory",
			})
		default:
			http.NotFound(w, request)
		}
	}))
	server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{serverFixture.server},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientFixture.roots,
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)
	t.Setenv(envDevCertDir, "")
	t.Setenv(envClientCert, clientFixture.clientCert)
	t.Setenv(envClientKey, clientFixture.clientKey)
	t.Setenv(envClientCA, serverFixture.ca)
	starterCalls := 0
	var stdout bytes.Buffer
	err := RunOnboard([]string{"--repo", "sample", "--addr", server.URL, repo}, &stdout, io.Discard, &http.Client{Timeout: 3 * time.Second}, func(DaemonStartConfig) error {
		starterCalls++
		return nil
	})
	if err != nil {
		t.Fatalf("RunOnboard valid external TLS: %v\n%s", err, stdout.String())
	}
	if starterCalls != 0 {
		t.Fatalf("starter calls = %d, want 0 for external TLS", starterCalls)
	}
	if _, statErr := os.Stat(filepath.Join(repo, callerRepoBindingsFileName)); !os.IsNotExist(statErr) {
		t.Fatalf("local caller bindings written in external mode (stat err=%v), want none — a remote server never sees local files", statErr)
	}
	if !strings.Contains(stdout.String(), "registered sample") {
		t.Fatalf("stdout = %q, want a registration confirmation", stdout.String())
	}
}

type externalClientTLSFixture struct {
	clientCert string
	clientKey  string
	ca         string
	server     tls.Certificate
	roots      *x509.CertPool
}

func writeExternalClientTLSFixture(t *testing.T, commonName string) externalClientTLSFixture {
	t.Helper()
	return writeExternalClientTLSFixtureWithUsage(t, commonName, x509.ExtKeyUsageClientAuth)
}

func writeExternalClientTLSFixtureWithUsage(t *testing.T, commonName string, usage x509.ExtKeyUsage) externalClientTLSFixture {
	t.Helper()
	dir := t.TempDir()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey(CA): %v", err)
	}
	now := time.Now().UTC()
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "external-test-ca"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, IsCA: true, BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("CreateCertificate(CA): %v", err)
	}
	intermediateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey(intermediate): %v", err)
	}
	intermediateTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "external-test-client-intermediate"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, IsCA: true, BasicConstraintsValid: true,
	}
	intermediateDER, err := x509.CreateCertificate(rand.Reader, intermediateTemplate, caTemplate, &intermediateKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("CreateCertificate(intermediate): %v", err)
	}
	clientKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey(client): %v", err)
	}
	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: commonName},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{usage},
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, intermediateTemplate, &clientKey.PublicKey, intermediateKey)
	if err != nil {
		t.Fatalf("CreateCertificate(client): %v", err)
	}
	caPath := filepath.Join(dir, "ca.crt")
	certPath := filepath.Join(dir, "client.crt")
	keyPath := filepath.Join(dir, "client.key")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o644); err != nil {
		t.Fatalf("WriteFile(CA): %v", err)
	}
	clientChain := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientDER}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: intermediateDER})...)
	if err := os.WriteFile(certPath, clientChain, 0o644); err != nil {
		t.Fatalf("WriteFile(client cert): %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(clientKey)}), 0o600); err != nil {
		t.Fatalf("WriteFile(client key): %v", err)
	}
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey(server): %v", err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(4), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"localhost"}, IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("CreateCertificate(server): %v", err)
	}
	serverPair, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}),
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)}),
	)
	if err != nil {
		t.Fatalf("X509KeyPair(server): %v", err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("ParseCertificate(CA): %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	return externalClientTLSFixture{clientCert: certPath, clientKey: keyPath, ca: caPath, server: serverPair, roots: roots}
}

func TestOnboardReusesOneManagedGenerationForStartupAndDoctor(t *testing.T) {
	t.Setenv(envDevCertDir, "")
	t.Setenv(envClientCert, "")
	t.Setenv(envClientKey, "")
	t.Setenv(envClientCA, "")
	resolves := 0
	originalResolve := resolveManagedVersion
	resolveManagedVersion = func(root string) (devcerts.ManagedVersion, error) {
		resolves++
		return devcerts.ResolveManagedVersion(root)
	}
	t.Cleanup(func() { resolveManagedVersion = originalResolve })

	var output bytes.Buffer
	certDir := filepath.Join(t.TempDir(), "certs")
	o := onboarder{
		repoName: "sample",
		repoRoot: t.TempDir(),
		addr:     "https://127.0.0.1:7890",
		certDir:  certDir,
		tlsMode:  clientTLSMode{managed: true, root: certDir},
		stdout:   &output,
		httpClient: &http.Client{Transport: clientRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			body := `{}`
			if strings.Contains(request.URL.Path, "preflight") {
				body = `{"authenticated_cn":"dev-hook-pool","repo_configured":true,"repo_config_valid":true,"caller_authorized":true,"enforcement_mode":"advisory"}`
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})},
		starter: func(DaemonStartConfig) error { return nil },
	}
	if err := o.ensureCerts(); err != nil {
		t.Fatalf("ensureCerts: %v", err)
	}
	daemonCfg := daemonStartConfigFromMaterial(o.addr, o.tlsMaterial)
	if !daemonCfg.Local || daemonCfg.TLSCert == "" {
		t.Fatalf("daemon config did not reuse managed material: %+v", daemonCfg)
	}
	paths := o.tlsMaterial.version.Paths()
	_ = runDoctorWithConfig(doctorConfig{
		addr: o.addr, repo: o.repoName, repoRoot: o.repoRoot,
		clientCert: paths.ClientCertificate, clientKey: paths.ClientKey, clientCA: paths.CA,
		managed: true, clientLeaf: o.tlsMaterial.certificate.Leaf, rootCAs: o.tlsMaterial.roots,
		tlsLoaded: true, httpClient: o.httpClient,
	}, &output)
	if resolves != 1 {
		t.Fatalf("managed resolver calls = %d, want 1 across onboard startup and doctor", resolves)
	}
}

func TestResolveOnboardRepoName(t *testing.T) {
	tests := []struct {
		name     string
		repoFlag string
		repoRoot string
		want     string
		wantErr  string
	}{
		{name: "flag wins", repoFlag: "graft", repoRoot: "/home/user/Weird_Repo", want: "graft"},
		{name: "basename fallback", repoFlag: "", repoRoot: "/home/user/calm-poc", want: "calm-poc"},
		{name: "invalid basename asks for --repo", repoFlag: "", repoRoot: "/home/user/Weird_Repo", wantErr: "pass --repo"},
		{name: "invalid flag rejected", repoFlag: "Bad Name", repoRoot: "/home/user/calm-poc", wantErr: "invalid repository name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveOnboardRepoName(tt.repoFlag, tt.repoRoot)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want to contain %q", err, tt.wantErr)
				}
				if !IsUsageError(err) {
					t.Fatalf("err = %v, want usage error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolveOnboardRepoName(%q, %q) = %q, want %q", tt.repoFlag, tt.repoRoot, got, tt.want)
			}
		})
	}
}

func TestValidateEnforcement(t *testing.T) {
	for _, mode := range []string{"advisory", "block"} {
		if got, err := validateEnforcement(mode); err != nil || got != mode {
			t.Fatalf("validateEnforcement(%q) = %q, %v; want %q, nil", mode, got, err, mode)
		}
	}
	if _, err := validateEnforcement("off"); err == nil || !IsUsageError(err) {
		t.Fatalf("validateEnforcement(off) err = %v, want usage error", err)
	}
}

func TestRenderScaffoldConfigFillsAllFiveFunctions(t *testing.T) {
	for _, enforcement := range []string{"advisory", "block"} {
		content, err := renderScaffoldConfig(enforcement, nil)
		if err != nil {
			t.Fatalf("renderScaffoldConfig(%q): %v", enforcement, err)
		}
		var doc scaffoldConfigDocument
		if err := json.Unmarshal(content, &doc); err != nil {
			t.Fatalf("unmarshal scaffolded config: %v\n%s", err, content)
		}
		if doc.EnforcementMode != enforcement {
			t.Fatalf("enforcement-mode = %q, want %q", doc.EnforcementMode, enforcement)
		}
		if doc.EnforcementOnError != "block" {
			t.Fatalf("enforcement-on-error = %q, want block", doc.EnforcementOnError)
		}
		if len(doc.FitnessFunctions) != len(fitnessFunctionKeys) {
			t.Fatalf("fitness-functions = %v, want %d keys", doc.FitnessFunctions, len(fitnessFunctionKeys))
		}
		for _, key := range fitnessFunctionKeys {
			if !doc.FitnessFunctions[key] {
				t.Fatalf("fitness function %q not enabled in %s scaffold", key, enforcement)
			}
		}
		if !bytes.HasSuffix(content, []byte("\n")) {
			t.Fatalf("scaffolded config must end with a newline")
		}
	}
}

func TestScaffoldConfigFreshAndAlreadyExists(t *testing.T) {
	configsDir := t.TempDir()
	repoConfigsDir := t.TempDir()
	var stdout bytes.Buffer
	o := onboarder{repoName: "sample", enforcement: "advisory", configsDir: configsDir, repoConfigsDir: repoConfigsDir, stdout: &stdout, stderr: &bytes.Buffer{}}

	if err := o.scaffoldConfig(); err != nil {
		t.Fatalf("fresh scaffoldConfig: %v", err)
	}
	configPath := filepath.Join(repoConfigsDir, "sample", "config.json")
	first, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read scaffolded config: %v", err)
	}
	if !strings.Contains(stdout.String(), "scaffolded advisory config") {
		t.Fatalf("stdout = %q, want scaffolded message", stdout.String())
	}

	stdout.Reset()
	if err := o.scaffoldConfig(); err != nil {
		t.Fatalf("second scaffoldConfig: %v", err)
	}
	if !strings.Contains(stdout.String(), "already present") {
		t.Fatalf("stdout = %q, want already-present message", stdout.String())
	}
	second, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("re-read config: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("idempotent scaffold rewrote config:\nfirst=%s\nsecond=%s", first, second)
	}
}

func TestEnsureCallerBindingFreshFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "caller-repos.json")
	changed, err := ensureCallerBinding(path, "dev-hook-pool", "sample")
	if err != nil {
		t.Fatalf("ensureCallerBinding: %v", err)
	}
	if !changed {
		t.Fatalf("changed = false, want true for a fresh file")
	}
	repos := readCallerRepos(t, path, "dev-hook-pool")
	if len(repos) != 1 || repos[0] != "sample" {
		t.Fatalf("callers[dev-hook-pool] = %v, want [sample]", repos)
	}
}

func TestEnsureCallerBindingAppendsPreservingExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "caller-repos.json")
	seed := `{
  "callers": {
    "ci-runner-graft": ["graft"],
    "dev-hook-pool": ["calm-poc", "graft"]
  },
  "admins": ["dev-hook-pool"]
}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed caller-repos.json: %v", err)
	}

	changed, err := ensureCallerBinding(path, "dev-hook-pool", "sample")
	if err != nil {
		t.Fatalf("ensureCallerBinding: %v", err)
	}
	if !changed {
		t.Fatalf("changed = false, want true when appending a new repo")
	}

	document := readCallerDocument(t, path)
	dev := stringList(t, document, "dev-hook-pool")
	for _, want := range []string{"calm-poc", "graft", "sample"} {
		if !containsString(dev, want) {
			t.Fatalf("dev-hook-pool = %v, want to contain %q", dev, want)
		}
	}
	other := stringListFromCallers(t, document, "ci-runner-graft")
	if len(other) != 1 || other[0] != "graft" {
		t.Fatalf("ci-runner-graft = %v, want [graft] preserved", other)
	}
	admins, ok := document["admins"].([]any)
	if !ok || len(admins) != 1 || admins[0] != "dev-hook-pool" {
		t.Fatalf("admins = %v, want [dev-hook-pool] preserved", document["admins"])
	}
}

func TestEnsureCallerBindingIdempotentWhenPresent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "caller-repos.json")
	if _, err := ensureCallerBinding(path, "dev-hook-pool", "sample"); err != nil {
		t.Fatalf("first ensureCallerBinding: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after first: %v", err)
	}
	changed, err := ensureCallerBinding(path, "dev-hook-pool", "sample")
	if err != nil {
		t.Fatalf("second ensureCallerBinding: %v", err)
	}
	if changed {
		t.Fatalf("changed = true, want false when repo already authorized")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after second: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("idempotent binding rewrote the file:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestEnsureCallerBindingSurfacesReadErrors(t *testing.T) {
	dir := t.TempDir()
	// A regular file where a directory is expected makes ReadFile fail with
	// ENOTDIR — a non-ENOENT read error that must not be mistaken for an
	// empty bindings file (and must never trigger a rewrite).
	blocker := filepath.Join(dir, "notadir")
	original := []byte(`{"callers":{"ci":["graft"]}}`)
	if err := os.WriteFile(blocker, original, 0o644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}
	path := filepath.Join(blocker, "caller-repos.json")
	changed, err := ensureCallerBinding(path, "dev-hook-pool", "sample")
	if err == nil || !strings.Contains(err.Error(), "reading") {
		t.Fatalf("err = %v, want reading error for unreadable bindings", err)
	}
	if changed {
		t.Fatalf("changed = true, want false when the bindings file cannot be read")
	}
	after, err := os.ReadFile(blocker)
	if err != nil {
		t.Fatalf("re-read blocker file: %v", err)
	}
	if !bytes.Equal(original, after) {
		t.Fatalf("unreadable-bindings failure modified existing content:\nbefore=%s\nafter=%s", original, after)
	}
}

func TestOnboardRepoRootRejectsNonGitDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := onboardRepoRoot(dir); err == nil || !strings.Contains(err.Error(), "git working tree") {
		t.Fatalf("onboardRepoRoot(%q) err = %v, want git-working-tree error", dir, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("onboardRepoRoot created files in a rejected directory: %v", entries)
	}
}

func TestOnboardRepoRootAnchorsSubdirectoryToTopLevel(t *testing.T) {
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")
	sub := filepath.Join(repo, "pkg", "inner")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	got, err := onboardRepoRoot(sub)
	if err != nil {
		t.Fatalf("onboardRepoRoot(%q): %v", sub, err)
	}
	want, wantErr := gitOutput(repo, "rev-parse", "--show-toplevel")
	if wantErr != nil {
		t.Fatalf("git rev-parse in fixture repo: %v", wantErr)
	}
	if got != want {
		t.Fatalf("onboardRepoRoot(%q) = %q, want toplevel %q", sub, got, want)
	}
}

func TestOnboardCallerBindingsPath(t *testing.T) {
	tests := []struct {
		name       string
		configsDir string
		want       string
	}{
		{name: "sibling of configs dir", configsDir: "/srv/app/configs", want: filepath.FromSlash("/srv/app/caller-repos.json")},
		{name: "inside non-configs dir", configsDir: "/srv/custom", want: filepath.FromSlash("/srv/custom/caller-repos.json")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := onboardCallerBindingsPath(tt.configsDir); got != tt.want {
				t.Fatalf("onboardCallerBindingsPath(%q) = %q, want %q", tt.configsDir, got, tt.want)
			}
		})
	}
}

func TestEmbeddedConfigTemplatesMatchAuthoritative(t *testing.T) {
	configsDir := filepath.Join(projectRoot(t), "configs")
	for _, name := range []string{"advisory-template.json", "block-template.json"} {
		authoritative, err := os.ReadFile(filepath.Join(configsDir, name))
		if err != nil {
			t.Fatalf("read configs/%s: %v", name, err)
		}
		embedded, err := embeddedConfigTemplates.ReadFile("configtemplates/" + name)
		if err != nil {
			t.Fatalf("read embedded configtemplates/%s: %v", name, err)
		}
		if !bytes.Equal(authoritative, embedded) {
			t.Fatalf("configtemplates/%s has drifted from configs/%s; re-copy configs/%s into internal/client/configtemplates/", name, name, name)
		}
	}
}

func readCallerDocument(t *testing.T, path string) map[string]any {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var document map[string]any
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return document
}

func readCallerRepos(t *testing.T, path, callerCN string) []string {
	t.Helper()
	return stringList(t, readCallerDocument(t, path), callerCN)
}

func stringList(t *testing.T, document map[string]any, callerCN string) []string {
	t.Helper()
	return stringListFromCallers(t, document, callerCN)
}

func stringListFromCallers(t *testing.T, document map[string]any, callerCN string) []string {
	t.Helper()
	callers, ok := document["callers"].(map[string]any)
	if !ok {
		t.Fatalf("document has no callers object: %v", document)
	}
	raw, ok := callers[callerCN].([]any)
	if !ok {
		return nil
	}
	repos := make([]string, 0, len(raw))
	for _, item := range raw {
		repos = append(repos, item.(string))
	}
	return repos
}

// TestOnboardExternalModeRegistersRepoAgainstRemoteServerInsteadOfWritingLocalFiles
// locks the WP7 self-service contract: with external TLS material pointing at
// a remote governance server, onboard registers the repo via POST /register —
// carrying the selected fitness functions — instead of scaffolding
// configs/<repo>/config.json and caller-repos.json locally, which a remote
// server would never see. Local file writes remain the managed/local-daemon
// path only.
func TestOnboardExternalModeRegistersRepoAgainstRemoteServerInsteadOfWritingLocalFiles(t *testing.T) {
	const wantCN = "external-run-client"
	clientFixture := writeExternalClientTLSFixture(t, wantCN)
	serverFixture := writeExternalClientTLSFixture(t, "unrelated-server-hierarchy-client")
	var mu sync.Mutex
	var registerBodies []map[string]any
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/register":
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			registerBodies = append(registerBodies, body)
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"repo": body["repo"], "created": true})
		case "/preflight":
			_ = json.NewEncoder(w).Encode(preflightReport{
				AuthenticatedCN: wantCN, RepoConfigured: true, RepoConfigValid: true,
				CallerAuthorized: true, EnforcementMode: "advisory",
			})
		default:
			http.NotFound(w, request)
		}
	}))
	server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{serverFixture.server},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientFixture.roots,
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	repo := t.TempDir()
	useDeterministicGitClientTest(t, repo)
	t.Setenv(envDevCertDir, "")
	t.Setenv(envClientCert, clientFixture.clientCert)
	t.Setenv(envClientKey, clientFixture.clientKey)
	t.Setenv(envClientCA, serverFixture.ca)
	var stdout bytes.Buffer
	err := RunOnboard([]string{"--repo", "sample", "--addr", server.URL, "--functions", "cyclomatic-complexity,logic-density", repo}, &stdout, io.Discard, &http.Client{Timeout: 3 * time.Second}, func(DaemonStartConfig) error {
		t.Fatal("external onboarding must not call starter")
		return nil
	})
	if err != nil {
		t.Fatalf("RunOnboard external register: %v\n%s", err, stdout.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(registerBodies) != 1 {
		t.Fatalf("POST /register calls = %d, want 1 (external mode must register remotely)", len(registerBodies))
	}
	body := registerBodies[0]
	if body["repo"] != "sample" {
		t.Fatalf("register body repo = %v, want sample", body["repo"])
	}
	functions, ok := body["fitness-functions"].(map[string]any)
	if !ok {
		t.Fatalf("register body fitness-functions = %v, want map", body["fitness-functions"])
	}
	if functions["cyclomatic-complexity"] != true || functions["logic-density"] != true || functions["interface-width"] != false {
		t.Fatalf("register body fitness-functions = %v, want selected subset explicit", functions)
	}
	if _, err := os.Stat(filepath.Join(repo, "configs", "sample", "config.json")); !os.IsNotExist(err) {
		t.Fatalf("local config scaffolded in external mode (stat err=%v), want none", err)
	}
	if _, err := os.Stat(filepath.Join(repo, callerRepoBindingsFileName)); !os.IsNotExist(err) {
		t.Fatalf("local caller bindings written in external mode (stat err=%v), want none", err)
	}
}
