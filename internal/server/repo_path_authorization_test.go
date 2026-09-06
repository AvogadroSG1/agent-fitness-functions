package server

import (
	"context"
	"path/filepath"
	"testing"
)

// Installed Git and agent hooks send --repo as the repository's toplevel
// PATH (git rev-parse --show-toplevel); authorization must canonicalize
// exactly like config lookup does, or every locally onboarded repo fails
// with "invalid repository name" at commit time.
func TestAuthorizeRepoAccessAcceptsRepositoryPathForms(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	writeMountedConfig(t, configDir, "matrix-go", `{"enforcement-mode":"block"}`)
	writeCallerRepoPolicyFile(t, root, `{"callers":{"dev-hook-pool":["matrix-go"]}}`)

	store, err := NewConfigStore(context.Background(), configDir)
	if err != nil {
		t.Fatalf("NewConfigStore() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("Close() error = %v", closeErr)
		}
	})

	for _, repo := range []string{
		"matrix-go",
		"/private/tmp/matrix-run.ABC123/matrix-go",
		"/Users/someone/code/matrix-go/",
	} {
		name, err := authorizeRepoAccess(HandlerOptions{}, store, "dev-hook-pool", repo)
		if err != nil {
			t.Errorf("authorizeRepoAccess(%q) = %v, want authorized as matrix-go", repo, err)
			continue
		}
		if name != "matrix-go" {
			t.Errorf("authorizeRepoAccess(%q) name = %q, want matrix-go", repo, name)
		}
	}

	if _, err := authorizeRepoAccess(HandlerOptions{}, store, "dev-hook-pool", "/private/tmp/other-repo"); err == nil {
		t.Error("authorizeRepoAccess authorized a path whose canonical name is not in the caller policy")
	}
}
