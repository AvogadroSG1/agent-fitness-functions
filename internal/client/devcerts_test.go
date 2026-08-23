package client

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

type failingResolverWriter struct {
	err error
}

func (w failingResolverWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func TestRunResolveDevCertVersionPrefixesStdoutWriteFailure(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certs")
	if err := EnsureDevCerts(root); err != nil {
		t.Fatalf("EnsureDevCerts(%q): %v", root, err)
	}
	t.Setenv(envDevCertDir, root)
	wantCause := errors.New("stdout unavailable")
	err := RunResolveDevCertVersion(nil, failingResolverWriter{err: wantCause})
	if !errors.Is(err, wantCause) {
		t.Fatalf("RunResolveDevCertVersion error = %v, want wrapped stdout cause", err)
	}
	if got := err.Error(); !strings.HasPrefix(got, "resolve managed development certificate version: ") {
		t.Fatalf("error = %q, want resolver prefix", got)
	}
}
