package client

import (
	"bytes"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// calm-poc-q8d.2: given a governed repository without a certs ignore rule,
// when onboarding generates development credentials, then the generated
// keys are git-ignored, never appear in git status, key modes are 0600,
// unrelated gitignore content is unchanged, and repeated onboarding does
// not duplicate ignore entries.
func TestEnsureCertsInstallsDownstreamIgnoreProtection(t *testing.T) {
	t.Setenv(envDevCertDir, "")
	t.Setenv(envClientCert, "")
	t.Setenv(envClientKey, "")
	t.Setenv(envClientCA, "")

	repo := t.TempDir()
	runGitClientTest(t, repo, "init")
	rootIgnore := filepath.Join(repo, ".gitignore")
	if err := os.WriteFile(rootIgnore, []byte("*.log\n"), 0o644); err != nil {
		t.Fatalf("seed root gitignore: %v", err)
	}

	certDir := filepath.Join(repo, "certs")
	var output bytes.Buffer
	o := onboarder{
		repoName:   "sample",
		repoRoot:   repo,
		addr:       "https://127.0.0.1:7890",
		certDir:    certDir,
		tlsMode:    clientTLSMode{managed: true, root: certDir},
		stdout:     &output,
		httpClient: &http.Client{},
		starter:    func(DaemonStartConfig) error { return nil },
	}
	if err := o.ensureCerts(); err != nil {
		t.Fatalf("ensureCerts: %v", err)
	}

	paths := o.tlsMaterial.version.Paths()
	for _, key := range []string{paths.ClientKey, paths.ServerKey} {
		info, err := os.Stat(key)
		if err != nil {
			t.Fatalf("stat generated key %s: %v", key, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("generated key %s mode = %v, want 0600", key, info.Mode().Perm())
		}
		check := exec.Command("git", "-C", repo, "check-ignore", "-q", key)
		if err := check.Run(); err != nil {
			t.Errorf("generated key %s is not git-ignored: %v", key, err)
		}
	}

	statusOutput, err := exec.Command("git", "-C", repo, "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	if strings.Contains(string(statusOutput), ".key") {
		t.Errorf("git status exposes generated key material:\n%s", statusOutput)
	}

	rootAfter, err := os.ReadFile(rootIgnore)
	if err != nil {
		t.Fatalf("read root gitignore: %v", err)
	}
	if string(rootAfter) != "*.log\n" {
		t.Errorf("unrelated root gitignore content changed: %q", rootAfter)
	}

	certsIgnore := filepath.Join(certDir, ".gitignore")
	firstIgnore, err := os.ReadFile(certsIgnore)
	if err != nil {
		t.Fatalf("certs ignore protection missing after onboarding: %v", err)
	}

	if err := o.ensureCerts(); err != nil {
		t.Fatalf("second ensureCerts: %v", err)
	}
	secondIgnore, err := os.ReadFile(certsIgnore)
	if err != nil {
		t.Fatalf("certs ignore protection missing after re-onboarding: %v", err)
	}
	if !bytes.Equal(firstIgnore, secondIgnore) {
		t.Errorf("repeated onboarding changed ignore protection:\nfirst:\n%s\nsecond:\n%s", firstIgnore, secondIgnore)
	}
}
