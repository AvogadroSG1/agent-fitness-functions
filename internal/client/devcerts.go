package client

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/devcerts"
)

const (
	devCACertName     = "ca.crt"
	devServerCertName = "server.crt"
	devServerKeyName  = "server.key"
	devClientCertName = "client.crt"
	devClientKeyName  = "client.key"

	devCACommonName     = "agent-fitness-functions-dev-ca"
	devServerCommonName = "localhost"
	devClientCommonName = "dev-hook-pool"
)

var devCertFileNames = []string{devCACertName, devServerCertName, devServerKeyName, devClientCertName, devClientKeyName}

// certsGitignoreContent mirrors this repository's own certs/.gitignore: ignore
// everything in the directory except the ignore file itself, so generated key
// material can never be staged by mistake.
const certsGitignoreContent = "*\n!.gitignore\n"

// ensureCertsIgnoreProtection idempotently installs certDir/.gitignore so
// downstream repositories governed by this client never accidentally stage
// generated development credentials (calm-poc-q8d.2). devcerts stays
// repo-agnostic; git knowledge lives here instead.
//
// It is a no-op when certDir is not inside a git working tree -- the
// containerized server path never touches a repository, and this must not
// error there. It only writes when certDir/.gitignore does not already exist:
// a user may have customized that file (broader patterns, comments, etc.), and
// the simplest honest contract is "never overwrite user content" rather than
// trying to detect whether an existing file still effectively ignores the
// generated keys.
func ensureCertsIgnoreProtection(certDir string) error {
	if certDir == "" {
		return nil
	}
	if _, err := gitOutput(certDir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil
	}
	ignorePath := filepath.Join(certDir, ".gitignore")
	if fileExists(ignorePath) {
		return nil
	}
	if err := os.WriteFile(ignorePath, []byte(certsGitignoreContent), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", ignorePath, err)
	}
	return nil
}

// EnsureDevCerts publishes the managed local development certificate set.
func EnsureDevCerts(certDir string) error {
	return ensureDevCerts(certDir, false)
}

func ensureDevCerts(certDir string, force bool) error {
	if certDir == governanceCertsDir() {
		if err := ensureGovernanceRoot(); err != nil {
			return err
		}
	}
	if err := devcerts.Publish(certDir, force); err != nil {
		return err
	}
	return ensureCertsIgnoreProtection(certDir)
}

// RunResolveDevCertVersion prints the single immutable relative managed version.
func RunResolveDevCertVersion(args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return usageError{err: errors.New("client resolve-dev-cert-version accepts no arguments")}
	}
	root := os.Getenv(envDevCertDir)
	if root == "" {
		root = governanceCertsDir()
	}
	version, err := resolveManagedVersion(root)
	if err != nil {
		return fmt.Errorf("resolve managed development certificate version: %w", err)
	}
	if _, err := fmt.Fprintln(stdout, version.RelativePath()); err != nil {
		return fmt.Errorf("resolve managed development certificate version: %w", err)
	}
	return nil
}
