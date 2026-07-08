package client

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Dev-cert material mirrors scripts/generate-dev-certs.sh so the Go generator and
// the shell script stay interchangeable. The client CN dev-hook-pool is the caller
// identity authorized by caller-repos.json for local development.
const (
	devCACertName     = "ca.crt"
	devServerCertName = "server.crt"
	devServerKeyName  = "server.key"
	devClientCertName = "client.crt"
	devClientKeyName  = "client.key"

	devCACommonName     = "calm-poc-dev-ca"
	devServerCommonName = "localhost"
	devClientCommonName = "dev-hook-pool"

	devCertValidity = 365 * 24 * time.Hour
)

// devCertFileNames lists every file EnsureDevCerts manages, in generation order.
var devCertFileNames = []string{
	devCACertName,
	devServerCertName,
	devServerKeyName,
	devClientCertName,
	devClientKeyName,
}

// EnsureDevCerts guarantees a full local development certificate set in certDir.
// A complete set is left untouched (idempotent); a partial set is a hard error so
// stale material is never silently mixed with fresh material; an empty directory is
// populated with a freshly generated CA, server, and client certificate. Behavior
// matches scripts/generate-dev-certs.sh.
func EnsureDevCerts(certDir string) error {
	present, missing, err := devCertInventory(certDir)
	if err != nil {
		return err
	}
	if len(missing) == 0 {
		return nil
	}
	if len(present) > 0 {
		return fmt.Errorf("partial dev certificate set in %s (present: %s); remove these files or run scripts/generate-dev-certs.sh --force to regenerate", certDir, strings.Join(present, ", "))
	}
	return generateDevCerts(certDir)
}

func devCertInventory(certDir string) (present, missing []string, err error) {
	for _, name := range devCertFileNames {
		path := filepath.Join(certDir, name)
		_, statErr := os.Stat(path)
		switch {
		case statErr == nil:
			present = append(present, name)
		case errors.Is(statErr, os.ErrNotExist):
			missing = append(missing, name)
		default:
			return nil, nil, fmt.Errorf("checking dev certificate %s: %w", path, statErr)
		}
	}
	return present, missing, nil
}

type devCertFile struct {
	name string
	data []byte
	mode os.FileMode
}

func generateDevCerts(certDir string) error {
	caCert, caKey, caPEM, err := newDevCA()
	if err != nil {
		return err
	}
	serverCertPEM, serverKeyPEM, err := newDevLeaf(devServerTemplate(), caCert, caKey)
	if err != nil {
		return fmt.Errorf("generating dev server certificate: %w", err)
	}
	clientCertPEM, clientKeyPEM, err := newDevLeaf(devClientTemplate(), caCert, caKey)
	if err != nil {
		return fmt.Errorf("generating dev client certificate: %w", err)
	}
	files := []devCertFile{
		{devCACertName, caPEM, 0o644},
		{devServerCertName, serverCertPEM, 0o644},
		{devServerKeyName, serverKeyPEM, 0o600},
		{devClientCertName, clientCertPEM, 0o644},
		{devClientKeyName, clientKeyPEM, 0o600},
	}
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		return fmt.Errorf("creating dev cert directory %s: %w", certDir, err)
	}
	for _, file := range files {
		path := filepath.Join(certDir, file.name)
		if err := os.WriteFile(path, file.data, file.mode); err != nil {
			return fmt.Errorf("writing dev certificate %s: %w", path, err)
		}
	}
	return nil
}

func newDevCA() (*x509.Certificate, *rsa.PrivateKey, []byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generating dev CA key: %w", err)
	}
	serial, err := newDevSerial()
	if err != nil {
		return nil, nil, nil, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: devCACommonName},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(devCertValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("creating dev CA certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parsing dev CA certificate: %w", err)
	}
	return cert, key, pemEncodeCert(der), nil
}

func newDevLeaf(template *x509.Certificate, caCert *x509.Certificate, caKey *rsa.PrivateKey) (certPEM, keyPEM []byte, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generating dev leaf key: %w", err)
	}
	serial, err := newDevSerial()
	if err != nil {
		return nil, nil, err
	}
	template.SerialNumber = serial
	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("creating dev leaf certificate: %w", err)
	}
	return pemEncodeCert(der), pemEncodeKey(key), nil
}

func devServerTemplate() *x509.Certificate {
	now := time.Now()
	return &x509.Certificate{
		Subject:     pkix.Name{CommonName: devServerCommonName},
		NotBefore:   now.Add(-time.Hour),
		NotAfter:    now.Add(devCertValidity),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"localhost", hookProductPrefix},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
}

func devClientTemplate() *x509.Certificate {
	now := time.Now()
	return &x509.Certificate{
		Subject:     pkix.Name{CommonName: devClientCommonName},
		NotBefore:   now.Add(-time.Hour),
		NotAfter:    now.Add(devCertValidity),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
}

func newDevSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("generating dev certificate serial: %w", err)
	}
	return serial, nil
}

func pemEncodeCert(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func pemEncodeKey(key *rsa.PrivateKey) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}
