package devcerts

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"time"
)

type lifecycleState uint8

const (
	stateEmpty lifecycleState = iota
	statePublishedTarget
	stateLegacyDirectRootTarget
	stateRecognizedPredecessor
	statePartial
	stateUnknownComplete
)

type materialIdentity uint8

const (
	identityUnknown materialIdentity = iota
	identityTarget
	identityPredecessor
)

type classifiedRoot struct {
	state       lifecycleState
	fresh       bool
	current     *string
	directFiles []fileSpec
	unsafe      bool
}

func classifyRoot(root string, now time.Time, lockHeld bool) (classifiedRoot, error) {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		if err := validateParent(root); err != nil {
			return classifiedRoot{}, err
		}
		return classifiedRoot{state: stateEmpty}, nil
	}
	if err != nil {
		return classifiedRoot{}, err
	}
	if !info.IsDir() || info.Mode().Perm() != 0o755 {
		return classifiedRoot{state: statePartial, unsafe: true}, nil
	}
	entries, err := readPinnedDirectory(root, 0o755)
	if err != nil {
		return classifiedRoot{}, err
	}

	managedPresent := 0
	for _, spec := range managedFiles {
		if pathExists(filepath.Join(root, spec.name)) {
			managedPresent++
		}
	}
	currentPresent := pathExists(filepath.Join(root, "current"))
	versionsPresent := pathExists(filepath.Join(root, "versions"))

	for _, entry := range entries {
		name := entry.Name()
		if name == ".gitignore" && safeGitignore(root) || name == "current" || name == "versions" || isManagedName(name) || lockHeld && name == lockName {
			continue
		}
		return classifiedRoot{state: statePartial, unsafe: true}, nil
	}

	if currentPresent {
		if managedPresent != 0 || !versionsPresent {
			return classifiedRoot{state: statePartial}, nil
		}
		return classifyVersionedRoot(root, now)
	}
	if versionsPresent && !validEmptyVersions(root) {
		return classifiedRoot{state: statePartial, unsafe: true}, nil
	}
	if managedPresent == 0 {
		return classifiedRoot{state: stateEmpty}, nil
	}
	if managedPresent != len(managedFiles) {
		files, err := readPresentDirectMaterial(root)
		return classifiedRoot{state: statePartial, directFiles: files, unsafe: err != nil}, nil
	}
	files, identity, fresh, err := readDirectAndClassifyMaterial(root, now)
	if err != nil || identity == identityUnknown {
		return classifiedRoot{state: stateUnknownComplete, directFiles: files, unsafe: err != nil}, nil
	}
	if identity == identityTarget {
		return classifiedRoot{state: stateLegacyDirectRootTarget, fresh: fresh, directFiles: files}, nil
	}
	return classifiedRoot{state: stateRecognizedPredecessor, fresh: fresh, directFiles: files}, nil
}

func classifyVersionedRoot(root string, now time.Time) (classifiedRoot, error) {
	target, err := readSafeCurrent(root)
	if err != nil {
		return classifiedRoot{state: statePartial}, nil
	}
	entries, err := readPinnedDirectory(filepath.Join(root, "versions"), 0o755)
	if err != nil || len(entries) == 0 || len(entries) > 2 {
		return classifiedRoot{state: statePartial}, nil
	}
	found := false
	var currentIdentity materialIdentity
	var currentFresh bool
	for _, entry := range entries {
		if !versionPattern.MatchString(entry.Name()) {
			return classifiedRoot{state: statePartial}, nil
		}
		versionPath := filepath.Join(root, "versions", entry.Name())
		if _, ok := pinnedDirectory(versionPath, 0o755); !ok {
			return classifiedRoot{state: statePartial}, nil
		}
		_, identity, fresh, materialErr := readAndClassifyMaterial(versionPath, now)
		if materialErr != nil {
			return classifiedRoot{state: statePartial}, nil
		}
		if entry.Name() == filepath.Base(target) {
			if identity == identityUnknown {
				return classifiedRoot{state: stateUnknownComplete, current: &target}, nil
			}
			found = true
			currentIdentity = identity
			currentFresh = fresh
		}
	}
	if !found {
		return classifiedRoot{state: statePartial}, nil
	}
	if currentIdentity == identityTarget {
		return classifiedRoot{state: statePublishedTarget, fresh: currentFresh, current: &target}, nil
	}
	return classifiedRoot{state: stateRecognizedPredecessor, fresh: currentFresh, current: &target}, nil
}

func readSafeCurrent(root string) (string, error) {
	path := filepath.Join(root, "current")
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return "", ErrUnsupportedForBootstrap
	}
	target, err := os.Readlink(path)
	if err != nil || filepath.ToSlash(target) != target || filepath.IsAbs(target) || filepath.Dir(target) != "versions" || !versionPattern.MatchString(filepath.Base(target)) {
		return "", ErrUnsupportedForBootstrap
	}
	reobserved, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, reobserved) {
		return "", ErrUnsupportedForBootstrap
	}
	return target, nil
}

func readAndClassifyMaterial(path string, now time.Time) ([]fileSpec, materialIdentity, bool, error) {
	entries, err := readPinnedDirectory(path, 0o755)
	if err != nil || len(entries) != len(managedFiles) {
		return nil, identityUnknown, false, ErrUnsupportedForBootstrap
	}
	files := make([]fileSpec, 0, len(managedFiles))
	for _, spec := range managedFiles {
		content, _, err := readPinnedRegular(filepath.Join(path, spec.name), spec.mode)
		if err != nil {
			return nil, identityUnknown, false, err
		}
		files = append(files, fileSpec{name: spec.name, mode: spec.mode, data: content})
	}
	identity, fresh := classifyMaterial(files, now)
	return files, identity, fresh, nil
}

func readDirectAndClassifyMaterial(path string, now time.Time) ([]fileSpec, materialIdentity, bool, error) {
	files := make([]fileSpec, 0, len(managedFiles))
	for _, spec := range managedFiles {
		content, _, err := readPinnedRegular(filepath.Join(path, spec.name), spec.mode)
		if err != nil {
			return nil, identityUnknown, false, err
		}
		files = append(files, fileSpec{name: spec.name, mode: spec.mode, data: content})
	}
	identity, fresh := classifyMaterial(files, now)
	return files, identity, fresh, nil
}

func readPresentDirectMaterial(path string) ([]fileSpec, error) {
	var files []fileSpec
	for _, spec := range managedFiles {
		if !pathExists(filepath.Join(path, spec.name)) {
			continue
		}
		content, _, err := readPinnedRegular(filepath.Join(path, spec.name), spec.mode)
		if err != nil {
			return nil, err
		}
		files = append(files, fileSpec{name: spec.name, mode: spec.mode, data: content})
	}
	return files, nil
}

func classifyMaterial(files []fileSpec, now time.Time) (materialIdentity, bool) {
	content := make(map[string][]byte, len(files))
	for _, file := range files {
		content[file.name] = file.data
	}
	ca, ok := parseCertificateBytes(content["ca.crt"])
	if !ok {
		return identityUnknown, false
	}
	server, ok := parseCertificateBytes(content["server.crt"])
	if !ok {
		return identityUnknown, false
	}
	client, ok := parseCertificateBytes(content["client.crt"])
	if !ok || !keyBytesMatch(content["server.key"], server) || !keyBytesMatch(content["client.key"], client) {
		return identityUnknown, false
	}
	if ca.CheckSignatureFrom(ca) != nil || server.CheckSignatureFrom(ca) != nil || client.CheckSignatureFrom(ca) != nil || !bytes.Equal(server.RawIssuer, ca.RawSubject) || !bytes.Equal(client.RawIssuer, ca.RawSubject) {
		return identityUnknown, false
	}
	identity := identityUnknown
	switch {
	case exactProfilesForIdentity(ca, server, client, "agent-fitness-functions-dev-ca", "agent-fitness-functions"):
		identity = identityTarget
	case exactProfilesForIdentity(ca, server, client, "calm-poc-dev-ca", "stack-fitness-functions"):
		identity = identityPredecessor
	}
	fresh := certificateFresh(ca, now) && certificateFresh(server, now) && certificateFresh(client, now)
	return identity, fresh
}

func exactProfilesForIdentity(ca, server, client *x509.Certificate, caName, serverName string) bool {
	periods := ca.NotAfter.Sub(ca.NotBefore) == validity && server.NotAfter.Sub(server.NotBefore) == validity && client.NotAfter.Sub(client.NotBefore) == validity
	validCA := exactCommonName(ca.Subject, caName) && ca.IsCA && ca.BasicConstraintsValid && ca.KeyUsage == x509.KeyUsageCertSign|x509.KeyUsageDigitalSignature && len(ca.ExtKeyUsage) == 0 && len(ca.UnknownExtKeyUsage) == 0 && !hasNames(ca)
	validServer := exactCommonName(server.Subject, "localhost") && !server.IsCA && server.KeyUsage == x509.KeyUsageDigitalSignature|x509.KeyUsageKeyEncipherment && equalExtUsage(server.ExtKeyUsage, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}) && len(server.UnknownExtKeyUsage) == 0 && equalStrings(server.DNSNames, []string{"localhost", serverName}) && equalIPs(server.IPAddresses, []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}) && len(server.EmailAddresses) == 0 && len(server.URIs) == 0
	validClient := exactCommonName(client.Subject, "dev-hook-pool") && !client.IsCA && client.KeyUsage == x509.KeyUsageDigitalSignature|x509.KeyUsageKeyEncipherment && equalExtUsage(client.ExtKeyUsage, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}) && len(client.UnknownExtKeyUsage) == 0 && !hasNames(client)
	return periods && validCA && validServer && validClient
}

func certificateFresh(cert *x509.Certificate, now time.Time) bool {
	return !now.Before(cert.NotBefore) && now.Before(cert.NotAfter)
}

func parseCertificateBytes(content []byte) (*x509.Certificate, bool) {
	block, rest := pem.Decode(content)
	if block == nil || block.Type != "CERTIFICATE" || len(rest) != 0 {
		return nil, false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	return cert, err == nil
}

func keyBytesMatch(content []byte, cert *x509.Certificate) bool {
	block, rest := pem.Decode(content)
	if block == nil || block.Type != "RSA PRIVATE KEY" || len(rest) != 0 {
		return false
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	public, ok := cert.PublicKey.(*rsa.PublicKey)
	return err == nil && ok && key.N.Cmp(public.N) == 0 && key.E == public.E
}

func isManagedName(name string) bool {
	for _, spec := range managedFiles {
		if name == spec.name {
			return true
		}
	}
	return false
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func materialBytesEqual(left, right []fileSpec) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].name != right[i].name || !bytes.Equal(left[i].data, right[i].data) {
			return false
		}
	}
	return true
}
