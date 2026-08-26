package server

import (
	"context"
	"path/filepath"
	"testing"
)

// TestConfigStoreBootsWithZeroValidConfigsAwaitingRegistration locks the
// self-service onboarding contract: an empty configs directory is a legitimate
// "awaiting registration" steady state, not a deployment error. The server
// must boot against it and serve repos that register afterwards — requiring a
// pre-registered config before the daemon can even start was the onboarding
// friction this removes.
func TestConfigStoreBootsWithZeroValidConfigsAwaitingRegistration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()

	store, err := NewConfigStore(ctx, dir)
	if err != nil {
		t.Fatalf("NewConfigStore over empty dir = %v, want nil (empty dir means awaiting registration)", err)
	}
	defer func() { _ = store.Close() }()

	if snapshot := store.Snapshot(); len(snapshot) != 0 {
		t.Fatalf("Snapshot() = %v, want empty for a fresh store", snapshot)
	}

	writeRepoConfigContent(t, store, "newrepo", `{"enforcement-mode":"advisory","fitness-functions":{}}`)
	entry, ok := store.Lookup("newrepo")
	if !ok || !entry.Valid {
		t.Fatalf("Lookup(newrepo) = (%+v, %v), want a valid entry after post-boot registration", entry, ok)
	}
	if entry.Config.EnforcementMode != EnforcementAdvisory {
		t.Fatalf("enforcement mode = %q, want %q", entry.Config.EnforcementMode, EnforcementAdvisory)
	}
}

// TestConfigStoreStillFailsWhenConfigsDirectoryIsMissing keeps the mount
// misconfiguration guard: an absent configs directory is an operator error and
// must fail boot loudly, unlike the now-legitimate empty directory.
func TestConfigStoreStillFailsWhenConfigsDirectoryIsMissing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store, err := NewConfigStore(ctx, filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		_ = store.Close()
		t.Fatal("NewConfigStore over a missing directory = nil error, want boot failure")
	}
}
