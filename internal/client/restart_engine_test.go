package client

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// S5 (calm-poc-5lku) review follow-up. Three properties the red tests do not
// reach: a lock held across a long critical section must not be reaped by a
// competitor, the lock must admit exactly one racing restart, and a shutdown
// that was only believed (dropped connection) must never turn into a false
// success when the old daemon is in fact still listening.

// fastRestartTiming shortens every wait so a test can reach the drain and
// fresh-probe timeouts in milliseconds. The production budget lives in
// defaultRestartTiming and is unchanged by this.
func fastRestartTiming() restartTiming {
	return restartTiming{
		lock:      lockTiming{wait: 400 * time.Millisecond, staleAge: 30 * time.Second, heartbeat: 50 * time.Millisecond, poll: 10 * time.Millisecond},
		drainWait: 300 * time.Millisecond,
		freshWait: 300 * time.Millisecond,
		poll:      10 * time.Millisecond,
	}
}

// freshDaemonPool binds replacement plain-HTTP daemons serving a current
// identity. Unlike startFreshDaemon it reports failure instead of calling
// t.Fatalf, so it is safe to call from a starter running on another goroutine.
type freshDaemonPool struct {
	mutex   sync.Mutex
	servers []*http.Server
}

func (p *freshDaemonPool) bind(addr string, expect daemonExpectations) error {
	host := strings.TrimPrefix(addr, "http://")
	deadline := time.Now().Add(2 * time.Second)
	for {
		listener, err := net.Listen("tcp", host)
		if err == nil {
			server := &http.Server{Handler: identityHandler(expect)}
			p.mutex.Lock()
			p.servers = append(p.servers, server)
			p.mutex.Unlock()
			go func() { _ = server.Serve(listener) }()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("replacement daemon could not bind %s: %w", host, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (p *freshDaemonPool) closeAll() {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	for _, server := range p.servers {
		_ = server.Close()
	}
}

// identityHandler answers every path with one daemon identity body.
func identityHandler(expect daemonExpectations) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "build_revision": expect.BuildRevision,
			"listen_mode": expect.ListenMode, "configs_dir": expect.ConfigsDir, "pid": os.Getpid(),
		})
	})
}

// TestRestartLockHeartbeatOutlivesTheStaleAge: given a lock held far longer than
// its stale age — the ordinary case for a restart, whose critical section spans
// a drain, a starter and a health wait — when a competitor waits past that stale
// age, then the heartbeat keeps the lock from being reaped, while a lock nobody
// is refreshing is still reaped as crash residue.
func TestRestartLockHeartbeatOutlivesTheStaleAge(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	lockDir := restartLockPath()
	// The competitor waits 250ms against a 60ms stale age, so it reaches the
	// reap decision several times over: only a live heartbeat can stop it.
	timing := lockTiming{wait: 250 * time.Millisecond, staleAge: 60 * time.Millisecond, heartbeat: 15 * time.Millisecond, poll: 10 * time.Millisecond}

	release, err := acquireLockDir(lockDir, timing)
	if err != nil {
		t.Fatalf("acquireLockDir: %v", err)
	}
	if _, err := acquireLockDir(lockDir, timing); err == nil {
		t.Fatal("a competitor took a lock whose holder is still heartbeating")
	} else if !strings.Contains(err.Error(), restartLockName) {
		t.Errorf("contention error = %q, want it to name %s", err.Error(), restartLockName)
	}

	release()
	regained, err := acquireLockDir(lockDir, timing)
	if err != nil {
		t.Fatalf("lock stayed held after release: %v", err)
	}
	regained()

	// A crashed holder leaves a directory nothing refreshes; that one is residue.
	if err := os.Mkdir(lockDir, 0o755); err != nil {
		t.Fatalf("planting abandoned lock: %v", err)
	}
	abandoned := time.Now().Add(-time.Second)
	if err := os.Chtimes(lockDir, abandoned, abandoned); err != nil {
		t.Fatalf("ageing abandoned lock: %v", err)
	}
	reaped, err := acquireLockDir(lockDir, timing)
	if err != nil {
		t.Fatalf("an abandoned lock was not reaped: %v", err)
	}
	reaped()
}

// TestRestartLocalDaemonAdmitsExactlyOneRacer: given two agents restarting the
// same daemon at once, when both call restartLocalDaemon, then the mkdir lock
// admits exactly one — the other gets the named contention failure and never
// runs a starter, so the two never thrash the port.
func TestRestartLocalDaemonAdmitsExactlyOneRacer(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	harness := newRestartHarness(t, daemonIdentity{BuildRevision: "0523ea04afd8", ListenMode: "mtls", ConfigsDir: "/gov/configs"})
	pool := &freshDaemonPool{}
	defer pool.closeAll()

	var starterCount int64
	loserReturned := make(chan struct{})
	var loserOnce sync.Once
	// The winner holds the lock until the loser has returned, so the loser
	// must fail on contention rather than simply arriving after the winner.
	starter := func(DaemonStartConfig) error {
		atomic.AddInt64(&starterCount, 1)
		select {
		case <-loserReturned:
		case <-time.After(3 * time.Second):
			return fmt.Errorf("no restart was refused: the lock admitted both")
		}
		return pool.bind(harness.addr, harness.expect)
	}

	results := make(chan error, 2)
	var group sync.WaitGroup
	for racer := 0; racer < 2; racer++ {
		group.Add(1)
		go func() {
			defer group.Done()
			err := restartLocalDaemonWithTiming(&http.Client{Timeout: time.Second}, harness.addr,
				DaemonStartConfig{Addr: harness.addr, Local: true}, harness.expect, starter, fastRestartTiming())
			if err != nil {
				loserOnce.Do(func() { close(loserReturned) })
			}
			results <- err
		}()
	}
	group.Wait()
	close(results)

	succeeded, refused := 0, 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case strings.Contains(err.Error(), restartLockName):
			refused++
		default:
			t.Errorf("unexpected restart failure: %v", err)
		}
	}
	if succeeded != 1 || refused != 1 {
		t.Fatalf("succeeded = %d refused = %d, want exactly one of each", succeeded, refused)
	}
	if count := atomic.LoadInt64(&starterCount); count != 1 {
		t.Fatalf("starter ran %d times, want 1: the lock must gate auto-start too", count)
	}
}

// hungShutdownDaemon serves a stale identity and drops the connection on POST
// /shutdown without answering — and keeps listening. That is the shape of a
// daemon that ignores the request: the shutdown looks accepted, but the port
// never frees.
func hungShutdownDaemon(t *testing.T, identity daemonIdentity) *httptest.Server {
	t.Helper()
	handler := http.NewServeMux()
	handler.HandleFunc("/shutdown", func(w http.ResponseWriter, _ *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("test server does not support hijacking")
			return
		}
		connection, _, err := hijacker.Hijack()
		if err == nil {
			_ = connection.Close()
		}
	})
	handler.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "build_revision": identity.BuildRevision,
			"listen_mode": identity.ListenMode, "configs_dir": identity.ConfigsDir, "pid": os.Getpid(),
		})
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

// TestRestartLocalDaemonReportsStaleWhenShutdownWasOnlyBelieved: given a daemon
// that drops the shutdown connection but keeps listening, when the restart
// proceeds on that ambiguous acceptance, then the drain times out, the starter
// cannot bind, and the verdict is the staleness the probe still observes —
// never a false success — with the dropped connection named in the error.
func TestRestartLocalDaemonReportsStaleWhenShutdownWasOnlyBelieved(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	stale := daemonIdentity{BuildRevision: "0523ea04afd8", ListenMode: "mtls", ConfigsDir: "/gov/configs"}
	server := hungShutdownDaemon(t, stale)
	expect := daemonExpectations{BuildRevision: "fresh0000000", ListenMode: "local-http", ConfigsDir: "/gov/configs"}

	pool := &freshDaemonPool{}
	defer pool.closeAll()
	// A real auto-start against a port that never freed: the bind fails.
	starter := func(DaemonStartConfig) error {
		listener, err := net.Listen("tcp", strings.TrimPrefix(server.URL, "http://"))
		if err != nil {
			return err
		}
		_ = listener.Close()
		return nil
	}

	err := restartLocalDaemonWithTiming(&http.Client{Timeout: time.Second}, server.URL,
		DaemonStartConfig{Addr: server.URL, Local: true}, expect, starter, fastRestartTiming())
	if err == nil {
		t.Fatal("restartLocalDaemon reported success although the stale daemon never left")
	}
	for _, want := range []string{"still stale", "connection dropped", "address already in use"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q must mention %q", err.Error(), want)
		}
	}
}
