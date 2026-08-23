package devcerts

import (
	"crypto/tls"
	"crypto/x509"
	"testing"
	"time"
)

// calm-poc-phk.3: given a rotated trust chain, when pre-rotation client
// credentials are presented, then verification against the rotated CA
// fails while freshly published credentials authenticate.
func TestRotatedTrustChainRejectsPredecessorClientCertificate(t *testing.T) {
	root := newManagedRoot(t)
	predecessor, err := generateMaterialForIdentity(time.Now(), "calm-poc-dev-ca", "stack-fitness-functions")
	if err != nil {
		t.Fatalf("generate predecessor material: %v", err)
	}
	var oldClientCert, oldClientKey []byte
	for _, file := range predecessor {
		switch file.name {
		case "client.crt":
			oldClientCert = file.data
		case "client.key":
			oldClientKey = file.data
		}
	}
	if oldClientCert == nil || oldClientKey == nil {
		t.Fatal("predecessor material lacks client credentials")
	}
	writeMaterial(t, root, predecessor)

	if err := Publish(root, true); err != nil {
		t.Fatalf("Publish(rotate predecessor): %v", err)
	}
	version, err := ResolveManagedVersion(root)
	if err != nil {
		t.Fatalf("ResolveManagedVersion: %v", err)
	}
	fresh, err := LoadManagedClient(version)
	if err != nil {
		t.Fatalf("LoadManagedClient(fresh): %v", err)
	}
	if _, err := fresh.Certificate.Leaf.Verify(x509.VerifyOptions{
		Roots:     fresh.RootCAs,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Fatalf("fresh credentials must authenticate against the rotated chain: %v", err)
	}

	oldPair, err := tls.X509KeyPair(oldClientCert, oldClientKey)
	if err != nil {
		t.Fatalf("parse pre-rotation client pair: %v", err)
	}
	oldLeaf, err := x509.ParseCertificate(oldPair.Certificate[0])
	if err != nil {
		t.Fatalf("parse pre-rotation leaf: %v", err)
	}
	if _, err := oldLeaf.Verify(x509.VerifyOptions{
		Roots:     fresh.RootCAs,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err == nil {
		t.Fatal("pre-rotation client certificate still verifies against the rotated trust chain")
	}
}
