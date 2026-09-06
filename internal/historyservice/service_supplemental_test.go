//go:build darwin || linux

package historyservice

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/history"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/historyipc"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/osevent"
)

func socketFixture(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hs-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return filepath.Join(dir, "s")
}

func startFixture(t *testing.T, cfg Config) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg); close(done) }()
	t.Cleanup(func() { cancel(); requireExit(t, done) })
	waitIdentity(t, cfg.SocketPath)
	return cancel, done
}

func waitIdentity(t *testing.T, path string) historyipc.Identity {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		message, err := historyipc.RequestControl(ctx, path, historyipc.KindIdentify)
		if err == nil {
			return *message.Identity
		}
		select {
		case <-ctx.Done():
			t.Fatalf("writer never became ready: %v", err)
		case <-time.After(time.Millisecond):
		}
	}
}

func requireExit(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("writer did not exit")
	}
}

func TestIdentifyShutdownAndPrivatePermissions(t *testing.T) {
	path := socketFixture(t)
	_, done := startFixture(t, Config{SocketPath: path})
	identity := waitIdentity(t, path)
	if identity.PID != os.Getpid() || identity.StartedAt.IsZero() {
		t.Fatalf("identity = %+v", identity)
	}
	for target, mode := range map[string]os.FileMode{path: 0600, filepath.Dir(path): 0700} {
		info, err := os.Stat(target)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("permissions: %v %v", info, err)
		}
	}
	message, err := historyipc.RequestControl(context.Background(), path, historyipc.KindShutdown)
	if err != nil || !message.Accepted {
		t.Fatalf("shutdown = %+v, %v", message, err)
	}
	requireExit(t, done)
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket remains: %v", err)
	}
}

func TestLosingStartPreservesLiveWriter(t *testing.T) {
	path := socketFixture(t)
	cancel, done := startFixture(t, Config{SocketPath: path})
	if err := Run(context.Background(), Config{SocketPath: path}); !errors.Is(err, ErrOwned) {
		t.Fatalf("second run = %v", err)
	}
	waitIdentity(t, path)
	cancel()
	requireExit(t, done)
}

func TestForeignListenerAndSymlinkPreserved(t *testing.T) {
	for _, kind := range []string{"listener", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			path := socketFixture(t)
			if kind == "listener" {
				listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := listener.Close(); err != nil {
						t.Error(err)
					}
				}()
			} else if err := os.Symlink("missing", path); err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			err = Run(context.Background(), Config{SocketPath: path})
			if !errors.Is(err, ErrUnsafeEndpoint) {
				t.Fatalf("run = %v", err)
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("endpoint replaced: %v", err)
			}
		})
	}
}

func TestHistoryCrashHelper(t *testing.T) {
	path := os.Getenv("HISTORY_CRASH_FIXTURE")
	if path == "" {
		return
	}
	go func() {
		if err := Run(context.Background(), Config{SocketPath: path}); err != nil {
			os.Exit(2)
		}
	}()
	waitIdentity(t, path)
	os.Exit(0) // Deliberately bypass every defer, simulating abrupt writer termination.
}

func TestOwnedStaleSocketRecoversAfterCrash(t *testing.T) {
	path := socketFixture(t)
	command := exec.Command(os.Args[0], "-test.run=^TestHistoryCrashHelper$")
	command.Env = append(os.Environ(), "HISTORY_CRASH_FIXTURE="+path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("crash fixture: %v %s", err, output)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("no stale socket: %v", err)
	}
	cancel, done := startFixture(t, Config{SocketPath: path})
	cancel()
	requireExit(t, done)
}

func TestControlsBypassBlockedWriteAndRetainOwnership(t *testing.T) {
	path := socketFixture(t)
	writing := make(chan struct{})
	unblock := make(chan struct{})
	cancelled := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(unblock) }) }
	t.Cleanup(release)
	cfg := Config{SocketPath: path, Insert: func(ctx context.Context, _ history.Event) error {
		close(writing)
		<-ctx.Done()
		close(cancelled)
		<-unblock // Simulate a filesystem operation that ignores cancellation.
		return ctx.Err()
	}, Log: func(context.Context, osevent.Diagnostic) error { return nil }}
	_, done := startFixture(t, cfg)
	// Cleanup MUST unblock before startFixture joins.
	t.Cleanup(release)
	if err := historyipc.Publish(path, serviceEvent()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-writing:
	case <-time.After(3 * time.Second):
		t.Fatal("insert did not start")
	}
	waitIdentity(t, path)
	if _, err := historyipc.RequestControl(context.Background(), path, historyipc.KindShutdown); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("insert cancellation not requested")
	}
	released, err := Released(path)
	if err != nil || released {
		t.Fatalf("ownership released before insertion exited: %v %v", released, err)
	}
	if err := Run(context.Background(), Config{SocketPath: path}); !errors.Is(err, ErrOwned) {
		t.Fatalf("replacement = %v", err)
	}
	release()
	requireExit(t, done)
}

func serviceEvent() history.Event {
	return history.Event{EventID: "0123456789abcdef0123456789abcdef", CompletedAt: time.Now(),
		CommonGitDir: "/fixture/.git", Repository: "fixture", Worktree: "/fixture", File: "x.go", Source: "manual", Status: "pass", DryRun: true,
		RequestJSON: json.RawMessage(`{"repo":"fixture","file":"x.go","proposed_content":"package x","language":"go","dry_run":true}`),
		ResultJSON:  json.RawMessage(`{"status":"pass"}`)}
}

func TestSharedParentIsNeverChmodded(t *testing.T) {
	path := socketFixture(t)
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), Config{SocketPath: path}); !errors.Is(err, ErrUnsafeEndpoint) {
		t.Fatalf("run=%v", err)
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatalf("shared directory changed: %v %v", info, err)
	}
}

func TestConcurrentStartsHaveOneOwner(t *testing.T) {
	path := socketFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	results := make(chan error, 2)
	gate := make(chan struct{})
	var runners sync.WaitGroup
	for range 2 {
		runners.Add(1)
		go func() { defer runners.Done(); <-gate; results <- Run(ctx, Config{SocketPath: path}) }()
	}
	t.Cleanup(func() { cancel(); runners.Wait() })
	close(gate)
	waitIdentity(t, path)
	select {
	case err := <-results:
		if !errors.Is(err, ErrOwned) {
			t.Fatalf("loser=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("loser did not exit")
	}
	cancel()
	requireExit(t, results)
}

func TestCleanupPreservesReplacementInode(t *testing.T) {
	path := socketFixture(t)
	cancel, done := startFixture(t, Config{SocketPath: path})
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("foreign replacement"), 0640); err != nil {
		t.Fatal(err)
	}
	cancel()
	requireExit(t, done)
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "foreign replacement" {
		t.Fatalf("replacement changed: %q %v", contents, err)
	}
}

func TestForeignLockFileIsPreserved(t *testing.T) {
	path := socketFixture(t)
	lock := path + ".lock"
	if err := os.WriteFile(lock, []byte("foreign data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), Config{SocketPath: path}); !errors.Is(err, ErrUnsafeEndpoint) {
		t.Fatalf("run=%v", err)
	}
	contents, err := os.ReadFile(lock)
	if err != nil || string(contents) != "foreign data" {
		t.Fatalf("lock replaced: %q %v", contents, err)
	}
}

func TestPersistenceDiagnosticRetainsContextWithoutPayload(t *testing.T) {
	path := socketFixture(t)
	diagnostics := make(chan osevent.Diagnostic, 1)
	event := serviceEvent()
	tool := "codex"
	event.Tool = &tool
	session := "session-1"
	event.SessionID = &session
	event.RequestJSON = json.RawMessage(`{"repo":"fixture","file":"x.go","proposed_content":"PRIVATE_SOURCE","language":"go","dry_run":true}`)
	cfg := Config{SocketPath: path, Insert: func(context.Context, history.Event) error { return errors.New("PRIVATE_ERROR") }, Log: func(_ context.Context, diagnostic osevent.Diagnostic) error { diagnostics <- diagnostic; return nil }}
	cancel, done := startFixture(t, cfg)
	if err := historyipc.Publish(path, event); err != nil {
		t.Fatal(err)
	}
	var diagnostic osevent.Diagnostic
	select {
	case diagnostic = <-diagnostics:
	case <-time.After(3 * time.Second):
		t.Fatal("missing persistence diagnostic")
	}
	if diagnostic.EventID != event.EventID || diagnostic.Repository != event.Repository || diagnostic.Worktree != event.Worktree || diagnostic.Tool != *event.Tool || diagnostic.SessionID != *event.SessionID {
		t.Fatalf("missing context: %+v", diagnostic)
	}
	body, err := json.Marshal(diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "PRIVATE_") || strings.Contains(string(body), "proposed_content") {
		t.Fatalf("payload leaked: %s", body)
	}
	cancel()
	requireExit(t, done)
}
