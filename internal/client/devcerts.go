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

// EnsureDevCerts publishes the managed local development certificate set.
func EnsureDevCerts(certDir string) error {
	return devcerts.Publish(certDir, false)
}

func ensureDevCerts(certDir string, force bool) error {
	return devcerts.Publish(certDir, force)
}

// RunResolveDevCertVersion prints the single immutable relative managed version.
func RunResolveDevCertVersion(args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return usageError{err: errors.New("client resolve-dev-cert-version accepts no arguments")}
	}
	root := os.Getenv(envDevCertDir)
	if root == "" {
		repoRoot := resolveRepoRoot("", "")
		if repoRoot == "" {
			return errors.New("resolve managed development certificate version: could not locate git root")
		}
		root = filepath.Join(repoRoot, "certs")
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
