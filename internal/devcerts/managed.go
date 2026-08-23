package devcerts

import (
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"
)

var managedTargetPattern = regexp.MustCompile(`^versions/v-[0-9a-f]{32}$`)

// ManagedPaths is the concrete five-file path set for one immutable managed version.
type ManagedPaths struct {
	CA                string
	ClientCertificate string
	ClientKey         string
	ServerCertificate string
	ServerKey         string
}

// ServerMaterial is process-owned parsed managed server TLS material.
type ServerMaterial struct {
	Certificate tls.Certificate
	ClientCAs   *x509.CertPool
	CA          []byte
}

type managedOperations struct {
	lstat    func(string) (os.FileInfo, error)
	readlink func(string) (string, error)
	open     func(string) (*os.File, error)
}

var defaultManagedOperations = managedOperations{
	lstat:    os.Lstat,
	readlink: os.Readlink,
	open:     os.Open,
}

type managedVersionState struct {
	relativePath     string
	paths            ManagedPaths
	versionDirectory os.FileInfo
	files            map[string]os.FileInfo
	operations       managedOperations
}

// ManagedVersion identifies one immutable managed certificate generation.
type ManagedVersion struct {
	state *managedVersionState
}

// RelativePath returns the validated versions/<opaque-id> publication path.
func (v ManagedVersion) RelativePath() string {
	if v.state == nil {
		return ""
	}
	return v.state.relativePath
}

// Paths returns a by-value snapshot of the version's concrete file paths.
func (v ManagedVersion) Paths() ManagedPaths {
	if v.state == nil {
		return ManagedPaths{}
	}
	return v.state.paths
}

// ResolveManagedVersion resolves, validates, and fingerprints current exactly once.
func ResolveManagedVersion(root string) (ManagedVersion, error) {
	return resolveManagedVersionWithOperations(root, defaultManagedOperations)
}

func resolveManagedVersionWithOperations(root string, operations managedOperations) (ManagedVersion, error) {
	if operations.lstat == nil || operations.readlink == nil || operations.open == nil {
		return ManagedVersion{}, errors.New("invalid managed certificate filesystem operations")
	}
	if _, err := pinManagedDirectory(root, 0o755, operations); err != nil {
		return ManagedVersion{}, errors.New("invalid managed certificate root")
	}
	versions := filepath.Join(root, "versions")
	if _, err := pinManagedDirectory(versions, 0o755, operations); err != nil {
		return ManagedVersion{}, errors.New("invalid managed certificate versions directory")
	}

	current := filepath.Join(root, "current")
	currentInfo, err := operations.lstat(current)
	if err != nil || currentInfo.Mode()&os.ModeSymlink == 0 {
		return ManagedVersion{}, errors.New("invalid managed certificate current link")
	}
	target, err := operations.readlink(current)
	if err != nil || !managedTargetPattern.MatchString(target) {
		return ManagedVersion{}, errors.New("invalid managed certificate current target")
	}

	versionRoot := filepath.Join(root, filepath.FromSlash(target))
	versionDirectory, err := pinManagedDirectory(versionRoot, 0o755, operations)
	if err != nil {
		return ManagedVersion{}, errors.New("invalid managed certificate version directory")
	}
	paths := ManagedPaths{
		CA:                filepath.Join(versionRoot, "ca.crt"),
		ClientCertificate: filepath.Join(versionRoot, "client.crt"),
		ClientKey:         filepath.Join(versionRoot, "client.key"),
		ServerCertificate: filepath.Join(versionRoot, "server.crt"),
		ServerKey:         filepath.Join(versionRoot, "server.key"),
	}
	if err := validateManagedEntries(versionRoot, operations); err != nil {
		return ManagedVersion{}, err
	}
	files := make(map[string]os.FileInfo, len(managedFiles))
	for _, spec := range managedFiles {
		path := filepath.Join(versionRoot, spec.name)
		info, err := fingerprintManagedFile(path, spec.mode, operations)
		if err != nil {
			return ManagedVersion{}, fmt.Errorf("fingerprint managed certificate file %s: %w", spec.name, err)
		}
		files[path] = info
	}
	revalidatedDirectory, err := pinManagedDirectory(versionRoot, 0o755, operations)
	if err != nil || !os.SameFile(versionDirectory, revalidatedDirectory) {
		return ManagedVersion{}, errors.New("managed certificate version changed during resolution")
	}
	return ManagedVersion{state: &managedVersionState{
		relativePath:     target,
		paths:            paths,
		versionDirectory: versionDirectory,
		files:            files,
		operations:       operations,
	}}, nil
}

func pinManagedDirectory(path string, mode os.FileMode, operations managedOperations) (os.FileInfo, error) {
	observed, err := operations.lstat(path)
	if err != nil || !observed.IsDir() || observed.Mode().Perm() != mode || observed.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUnsupportedForBootstrap
	}
	file, err := operations.open(path)
	if err != nil {
		return nil, err
	}
	pinned, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil || closeErr != nil || !pinned.IsDir() || pinned.Mode().Perm() != mode || !os.SameFile(observed, pinned) {
		return nil, ErrUnsupportedForBootstrap
	}
	return observed, nil
}

func validateManagedEntries(versionRoot string, operations managedOperations) error {
	directory, err := operations.open(versionRoot)
	if err != nil {
		return errors.New("open managed certificate version directory")
	}
	entries, readErr := directory.ReadDir(-1)
	closeErr := directory.Close()
	if readErr != nil || closeErr != nil {
		return errors.New("read managed certificate version directory")
	}
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"ca.crt", "client.crt", "client.key", "server.crt", "server.key"}) {
		return errors.New("managed certificate version must contain exactly five material files")
	}
	return nil
}

func fingerprintManagedFile(path string, mode os.FileMode, operations managedOperations) (os.FileInfo, error) {
	observed, err := operations.lstat(path)
	if err != nil || !observed.Mode().IsRegular() || observed.Mode().Perm() != mode || observed.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUnsupportedForBootstrap
	}
	file, err := operations.open(path)
	if err != nil {
		return nil, err
	}
	pinned, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil || closeErr != nil || !pinned.Mode().IsRegular() || pinned.Mode().Perm() != mode || !os.SameFile(observed, pinned) {
		return nil, ErrUnsupportedForBootstrap
	}
	return observed, nil
}

// LoadManagedServer loads server certificate, private key, and client CA from one resolved generation.
func LoadManagedServer(version ManagedVersion) (ServerMaterial, error) {
	if version.state == nil {
		return ServerMaterial{}, errors.New("invalid managed certificate version")
	}
	state := version.state
	versionRoot := filepath.Dir(state.paths.CA)
	if err := revalidateManagedDirectory(versionRoot, state); err != nil {
		return ServerMaterial{}, err
	}
	caPEM, err := readFingerprintedManagedFile(state.paths.CA, 0o644, state)
	if err != nil {
		return ServerMaterial{}, fmt.Errorf("load managed CA certificate: %w", err)
	}
	certPEM, err := readFingerprintedManagedFile(state.paths.ServerCertificate, 0o644, state)
	if err != nil {
		return ServerMaterial{}, fmt.Errorf("load managed server certificate: %w", err)
	}
	keyPEM, err := readFingerprintedManagedFile(state.paths.ServerKey, 0o600, state)
	if err != nil {
		return ServerMaterial{}, fmt.Errorf("load managed server key: %w", err)
	}
	if err := revalidateManagedDirectory(versionRoot, state); err != nil {
		return ServerMaterial{}, err
	}

	ca, err := parseOneManagedCertificate(caPEM)
	if err != nil {
		return ServerMaterial{}, errors.New("parse managed CA certificate")
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil || len(pair.Certificate) != 1 {
		return ServerMaterial{}, errors.New("parse managed server certificate and key")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || !validManagedServerIdentity(ca, leaf, time.Now()) {
		return ServerMaterial{}, errors.New("managed server certificate identity mismatch")
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: "localhost", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		return ServerMaterial{}, errors.New("verify managed server certificate")
	}
	pair.Leaf = leaf
	return ServerMaterial{Certificate: pair, ClientCAs: pool, CA: slices.Clone(caPEM)}, nil
}

func revalidateManagedDirectory(versionRoot string, state *managedVersionState) error {
	directory, err := pinManagedDirectory(versionRoot, 0o755, state.operations)
	if err != nil || !os.SameFile(state.versionDirectory, directory) {
		return errors.New("managed certificate version directory changed")
	}
	return nil
}

func readFingerprintedManagedFile(path string, mode os.FileMode, state *managedVersionState) ([]byte, error) {
	observed, err := state.operations.lstat(path)
	want := state.files[path]
	if err != nil || want == nil || !observed.Mode().IsRegular() || observed.Mode().Perm() != mode || observed.Mode()&os.ModeSymlink != 0 || !os.SameFile(want, observed) {
		return nil, ErrUnsupportedForBootstrap
	}
	file, err := state.operations.open(path)
	if err != nil {
		return nil, err
	}
	pinned, err := file.Stat()
	if err != nil || !pinned.Mode().IsRegular() || pinned.Mode().Perm() != mode || !os.SameFile(want, pinned) {
		return nil, errors.Join(ErrUnsupportedForBootstrap, file.Close())
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	reobserved, err := state.operations.lstat(path)
	if err != nil || !os.SameFile(want, reobserved) {
		return nil, errors.Join(ErrUnsupportedForBootstrap, file.Close())
	}
	repinned, err := file.Stat()
	if err != nil || !os.SameFile(want, repinned) {
		return nil, errors.Join(ErrUnsupportedForBootstrap, file.Close())
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return content, nil
}

func parseOneManagedCertificate(content []byte) (*x509.Certificate, error) {
	block, rest := pem.Decode(content)
	if block == nil || block.Type != "CERTIFICATE" || len(rest) != 0 {
		return nil, errors.New("invalid certificate PEM")
	}
	return x509.ParseCertificate(block.Bytes)
}

func validManagedServerIdentity(ca, server *x509.Certificate, now time.Time) bool {
	if !exactCertificateLifetime(ca, now) || !exactCommonName(ca.Subject, "agent-fitness-functions-dev-ca") || !ca.IsCA || !ca.BasicConstraintsValid || ca.KeyUsage != x509.KeyUsageCertSign|x509.KeyUsageDigitalSignature || len(ca.ExtKeyUsage) != 0 || len(ca.UnknownExtKeyUsage) != 0 || hasNames(ca) {
		return false
	}
	if err := ca.CheckSignatureFrom(ca); err != nil {
		return false
	}
	if !exactCertificateLifetime(server, now) || !exactCommonName(server.Subject, "localhost") || server.IsCA || server.KeyUsage != x509.KeyUsageDigitalSignature|x509.KeyUsageKeyEncipherment || !equalExtUsage(server.ExtKeyUsage, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}) || len(server.UnknownExtKeyUsage) != 0 || !equalStrings(server.DNSNames, []string{"localhost", "agent-fitness-functions"}) || !equalIPs(server.IPAddresses, []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}) || len(server.EmailAddresses) != 0 || len(server.URIs) != 0 {
		return false
	}
	public, ok := server.PublicKey.(*rsa.PublicKey)
	return ok && public.N.Sign() > 0
}
