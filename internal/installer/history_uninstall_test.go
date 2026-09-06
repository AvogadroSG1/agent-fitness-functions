package installer

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestUninstallValidatesBeforeWriterStop(t *testing.T) {
	for _, args := range [][]string{nil, {"--unknown"}, {"--yes", "extra"}} {
		stopped := false
		err := RunUninstallBeforeRemoval(args, io.Discard, io.Discard, func(string) string { return t.TempDir() }, func() error { stopped = true; return nil })
		if err == nil || stopped {
			t.Fatalf("args %v: err=%v stopped=%v", args, err, stopped)
		}
	}
}

func TestUnconfirmedWriterStopPreservesInstallation(t *testing.T) {
	home := t.TempDir()
	getenv := func(key string) string {
		if key == "HOME" {
			return home
		}
		return ""
	}
	root := StateRoot(getenv)
	executable := filepath.Join(root, "versions", "v1", "bin", "agent-fitness-functions")
	if err := os.MkdirAll(filepath.Dir(executable), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	err := RunUninstallBeforeRemoval([]string{"--yes"}, io.Discard, io.Discard, getenv, func() error { return errors.New("still writing") })
	if err == nil {
		t.Fatal("unconfirmed stop allowed uninstall")
	}
	if _, err := os.Stat(executable); err != nil {
		t.Fatalf("executable removed: %v", err)
	}
}

func TestUninstallPreservesCloneHistoryAfterVerifiedStop(t *testing.T) {
	home := t.TempDir()
	getenv := func(key string) string {
		if key == "HOME" {
			return home
		}
		return ""
	}
	root := StateRoot(getenv)
	executable := filepath.Join(root, "versions", "v1", "binary")
	database := filepath.Join(home, "clone", ".git", "agent-fitness-functions", "history.sqlite3")
	for _, path := range []string{executable, database} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("preserve history"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	stopped := false
	err := RunUninstallBeforeRemoval([]string{"--yes"}, io.Discard, io.Discard, getenv, func() error {
		if _, err := os.Stat(executable); err != nil {
			t.Fatal("executable removed before stop")
		}
		stopped = true
		return nil
	})
	if err != nil || !stopped {
		t.Fatalf("uninstall=%v stopped=%v", err, stopped)
	}
	if _, err := os.Stat(executable); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("executable still present: %v", err)
	}
	body, err := os.ReadFile(database)
	if err != nil || string(body) != "preserve history" {
		t.Fatalf("history changed: %q %v", body, err)
	}
}

func TestUnsafeStateRootDoesNotStopWriter(t *testing.T) {
	home := t.TempDir()
	getenv := func(key string) string {
		if key == "XDG_STATE_HOME" {
			return home
		}
		return ""
	}
	root := StateRoot(getenv)
	if err := os.Symlink(t.TempDir(), root); err != nil {
		t.Fatal(err)
	}
	err := RunUninstallBeforeRemoval([]string{"--yes"}, io.Discard, io.Discard, getenv, func() error { t.Fatal("unsafe root stopped writer"); return nil })
	if err == nil {
		t.Fatal("unsafe state root accepted")
	}
}
