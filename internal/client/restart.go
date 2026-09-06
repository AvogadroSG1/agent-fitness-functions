package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// restartLockWait bounds how long a restart waits for the machine-wide
	// lock. The critical section is one shutdown plus one start, so a full
	// wait means another restart is wedged, not merely slow.
	restartLockWait = 5 * time.Second
	// restartLockStaleAge is the age past which a leftover lock directory is
	// treated as a crashed restart's residue and reaped.
	restartLockStaleAge = 30 * time.Second
	// restartDrainWait bounds the wait for the old listener to disappear.
	restartDrainWait = 5 * time.Second
	// restartFreshWait bounds the wait for a current daemon to answer after
	// the start, mirroring waitHealthy's budget.
	restartFreshWait       = 5 * time.Second
	restartPollInterval    = 25 * time.Millisecond
	restartDialTimeout     = 250 * time.Millisecond
	shutdownRequestTimeout = 2 * time.Second
	restartLockName        = "restart.lock"
)

// restartLocalDaemon gracefully replaces the local daemon — restart lock, POST
// /shutdown, drain, starter — judging success solely by re-probing a fresh
// identity (a concurrent validate may win the bind, ADR-0010). It never signals
// processes directly.
func restartLocalDaemon(client *http.Client, addr string, cfg DaemonStartConfig, expect daemonExpectations, starter func(DaemonStartConfig) error) error {
	release, err := acquireRestartLock()
	if err != nil {
		return err
	}
	defer release()
	if err := requestDaemonShutdown(client, addr); err != nil {
		return err
	}
	drainDaemonAddress(addr)
	startErr := starter(cfg)
	return awaitFreshDaemon(client, addr, expect, startErr)
}

// acquireRestartLock serializes restarts machine-wide with the same portable
// primitive the caller-bindings writer uses: an atomic fixed-path mkdir under
// the ADR-0007 governance root. Two agents noticing the same stale daemon must
// not both stop and start it, or they thrash the port for each other. Unlike
// the bindings lock this one never degrades to unlocked — an unusable
// governance root is exactly the state in which an unserialized restart does
// the most damage — so every failure to acquire is reported.
func acquireRestartLock() (func(), error) {
	lockDir := filepath.Join(governanceRoot(), restartLockName)
	if err := os.MkdirAll(filepath.Dir(lockDir), 0o755); err != nil {
		return nil, fmt.Errorf("preparing %s: %w", lockDir, err)
	}
	deadline := time.Now().Add(restartLockWait)
	for {
		err := os.Mkdir(lockDir, 0o755)
		if err == nil {
			return func() { _ = os.Remove(lockDir) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("acquiring %s: %w", lockDir, err)
		}
		reapStaleLockDir(lockDir, restartLockStaleAge)
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%s held for over %s; another restart is in progress — wait for it, or remove that directory if no restart is running", lockDir, restartLockWait)
		}
		time.Sleep(restartPollInterval)
	}
}

// reapStaleLockDir removes a lock directory whose age exceeds maxAge — a
// crashed holder's leftover, since live holders keep these locks for a bounded
// operation. Best-effort: losing this race means another waiter reaped it.
func reapStaleLockDir(lockDir string, maxAge time.Duration) {
	info, err := os.Stat(lockDir)
	if err != nil || time.Since(info.ModTime()) < maxAge {
		return
	}
	_ = os.Remove(lockDir)
}

// shutdownRoute is one way to reach POST /shutdown: a client together with the
// address that client was built for.
type shutdownRoute struct {
	client *http.Client
	addr   string
}

// shutdownVerdict classifies one POST /shutdown outcome.
type shutdownVerdict int

const (
	// shutdownAccepted covers a 2xx and, deliberately, a dropped connection:
	// a daemon closing its listener mid-response is indistinguishable from
	// one here, and the drain plus fresh-identity probe are the real verdict.
	shutdownAccepted shutdownVerdict = iota
	// shutdownWrongRoute means something answered but this client cannot
	// speak to it on this scheme — the ADR-0010 migration state.
	shutdownWrongRoute
	// shutdownRefused means the daemon answered and declined.
	shutdownRefused
)

// shutdownRejectedError is a non-2xx answer to POST /shutdown: a live daemon
// that declined, as opposed to a transport failure. The body is carried because
// a TLS server states the scheme mismatch there.
type shutdownRejectedError struct {
	status int
	body   string
}

func (e shutdownRejectedError) Error() string {
	if e.body == "" {
		return fmt.Sprintf("POST /shutdown returned HTTP %d", e.status)
	}
	return fmt.Sprintf("POST /shutdown returned HTTP %d: %s", e.status, e.body)
}

// requestDaemonShutdown asks whatever owns addr to stop. The ADR-0010 plain-HTTP
// default is tried first; a scheme or TLS disagreement means a legacy mTLS
// daemon may still own the address, so the S4 fallback routes are tried before
// giving up. A live listener that no route can stop is a refusal, and the caller
// must not auto-start into it.
func requestDaemonShutdown(client *http.Client, addr string) error {
	err := postShutdown(client, addr)
	if classifyShutdown(err) == shutdownWrongRoute {
		err = tryShutdownRoutes(shutdownRoutes(client, addr), err)
	}
	if classifyShutdown(err) == shutdownAccepted {
		return nil
	}
	return shutdownRefusedError(addr, err)
}

// tryShutdownRoutes posts to each fallback route in turn, returning nil on the
// first acceptance and otherwise the last failure — which is what explains why
// this client cannot stop the listener.
func tryShutdownRoutes(routes []shutdownRoute, lastErr error) error {
	for _, route := range routes {
		err := postShutdown(route.client, route.addr)
		if classifyShutdown(err) == shutdownAccepted {
			return nil
		}
		lastErr = err
	}
	return lastErr
}

// shutdownRoutes are the fallback routes for a shutdown the default scheme could
// not deliver, in the order S4 established for probing: the caller's own client
// on the alternate scheme first, then the machine's managed dev-certificate
// client, which is the only material a legacy loopback mTLS daemon accepts.
// alternateSchemeClients loads that material with publish=false — a restart must
// never mint a certificate generation as a side effect.
func shutdownRoutes(base *http.Client, addr string) []shutdownRoute {
	alternate := alternateSchemeAddr(addr)
	if alternate == "" {
		return nil
	}
	secure := alternate
	if !isLocalHTTPS(secure) {
		secure = addr
	}
	routes := []shutdownRoute{{client: base, addr: alternate}}
	for _, candidate := range alternateSchemeClients(base, secure, resolveDevCertDir()) {
		if candidate != base {
			routes = append(routes, shutdownRoute{client: candidate, addr: secure})
		}
	}
	return routes
}

func classifyShutdown(err error) shutdownVerdict {
	var rejected shutdownRejectedError
	switch {
	case err == nil:
		return shutdownAccepted
	case isSchemeMismatch(err) || isTLSError(err):
		return shutdownWrongRoute
	case errors.As(err, &rejected):
		return shutdownRefused
	default:
		return shutdownAccepted
	}
}

// postShutdown performs one POST /shutdown. The body is read before the status
// is judged so a rejection carries the server's own explanation.
func postShutdown(client *http.Client, addr string) error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownRequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(addr, "/")+"/shutdown", nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<10))
	if response.StatusCode/100 == 2 {
		return nil
	}
	return shutdownRejectedError{status: response.StatusCode, body: strings.TrimSpace(string(body))}
}

// shutdownRefusedError hands the human the escape hatch: the address still has
// an owner this client cannot stop, so the next move is to identify that process
// rather than to start a second daemon behind it.
func shutdownRefusedError(addr string, cause error) error {
	return fmt.Errorf(
		"the daemon at %s would not shut down (%v); identify the process that owns the port with `lsof -nP -iTCP:%s` and stop it, then re-run",
		addr, cause, daemonPort(addr),
	)
}

// drainDaemonAddress waits for the address to stop accepting TCP connections —
// the observable end of the old listener. It is best effort: a listener that
// outlives the wait surfaces as a failed bind or a stale post-start identity,
// which is where the verdict belongs.
func drainDaemonAddress(addr string) {
	hostPort := daemonHostPort(addr)
	if hostPort == "" {
		return
	}
	deadline := time.Now().Add(restartDrainWait)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", hostPort, restartDialTimeout)
		if err != nil {
			return
		}
		_ = connection.Close()
		time.Sleep(restartPollInterval)
	}
}

// awaitFreshDaemon judges the restart the only way a client honestly can: by
// observing a daemon that answers with a current identity. The starter's own
// error is context, not a verdict — a concurrent validate may have won the bind
// race and started the very daemon now answering (ADR-0010).
func awaitFreshDaemon(client *http.Client, addr string, expect daemonExpectations, startErr error) error {
	deadline := time.Now().Add(restartFreshWait)
	var reasons []string
	var probeErr error
	for {
		identity, err := probeDaemonIdentity(client, addr)
		probeErr = err
		if err == nil {
			reasons = daemonStaleness(identity, expect)
			if len(reasons) == 0 {
				return nil
			}
		}
		if !time.Now().Before(deadline) {
			return staleRestartError(addr, reasons, probeErr, startErr)
		}
		time.Sleep(restartPollInterval)
	}
}

// staleRestartError reports what the post-start probe actually saw, carrying the
// starter's error only as context beneath it.
func staleRestartError(addr string, reasons []string, probeErr, startErr error) error {
	detail := fmt.Sprintf("it does not answer GET /health: %v", probeErr)
	if probeErr == nil {
		detail = "it is still stale: " + strings.Join(reasons, "; ")
	}
	if startErr != nil {
		return fmt.Errorf("no current daemon at %s after restart — %s; auto-start reported: %w", addr, detail, startErr)
	}
	return fmt.Errorf("no current daemon at %s after restart — %s", addr, detail)
}

// daemonHostPort is the dialable host:port behind a daemon URL, defaulting the
// port from the scheme so a drain never dials port 0.
func daemonHostPort(addr string) string {
	parsed, err := url.Parse(addr)
	if err != nil || parsed.Host == "" {
		return ""
	}
	if parsed.Port() != "" {
		return parsed.Host
	}
	if parsed.Scheme == "https" {
		return net.JoinHostPort(parsed.Hostname(), "443")
	}
	return net.JoinHostPort(parsed.Hostname(), "80")
}

// daemonPort is the port to name in the escape hatch, or a placeholder when addr
// states none for the human to substitute.
func daemonPort(addr string) string {
	parsed, err := url.Parse(addr)
	if err != nil || parsed.Port() == "" {
		return "<port>"
	}
	return parsed.Port()
}
