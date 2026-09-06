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
	shutdownRequestTimeout = 2 * time.Second
	restartDialTimeout     = 250 * time.Millisecond
	restartLockName        = "restart.lock"
)

// lockTiming bounds one on-disk lock's lifecycle: how long a waiter waits, how
// old an unrefreshed lock must be before it counts as a crashed holder's
// residue, how often a live holder refreshes it, and how often a waiter retries.
type lockTiming struct {
	wait      time.Duration
	staleAge  time.Duration
	heartbeat time.Duration
	poll      time.Duration
}

// restartTiming bounds every wait a restart performs. Production values are
// defaultRestartTiming; tests inject shorter ones so the timeout paths can be
// exercised without paying their real-world budgets.
type restartTiming struct {
	lock      lockTiming
	drainWait time.Duration
	freshWait time.Duration
	poll      time.Duration
}

// defaultRestartTiming is the production budget. The lock's heartbeat interval
// is a sixth of its stale age, which is what makes the stale-reap safe: see
// startLockHeartbeat.
func defaultRestartTiming() restartTiming {
	return restartTiming{
		lock: lockTiming{
			wait:      5 * time.Second,
			staleAge:  30 * time.Second,
			heartbeat: 5 * time.Second,
			poll:      25 * time.Millisecond,
		},
		drainWait: 5 * time.Second,
		freshWait: 5 * time.Second,
		poll:      25 * time.Millisecond,
	}
}

// restartTrace records how the phases before the verdict went, so a failed
// restart explains itself instead of only reporting "not current".
type restartTrace struct {
	shutdownNote string
	startErr     error
}

// restartLocalDaemon gracefully replaces the local daemon — restart lock, POST
// /shutdown, drain, starter — judging success solely by re-probing a fresh
// identity (a concurrent validate may win the bind, ADR-0010). It never signals
// processes directly.
func restartLocalDaemon(client *http.Client, addr string, cfg DaemonStartConfig, expect daemonExpectations, starter func(DaemonStartConfig) error) error {
	return restartLocalDaemonWithTiming(client, addr, cfg, expect, starter, defaultRestartTiming())
}

// restartLocalDaemonWithTiming is restartLocalDaemon with its waits stated
// explicitly, the seam tests use to reach the drain and fresh-probe timeouts.
func restartLocalDaemonWithTiming(client *http.Client, addr string, cfg DaemonStartConfig, expect daemonExpectations, starter func(DaemonStartConfig) error, timing restartTiming) error {
	release, err := acquireLockDir(restartLockPath(), timing.lock)
	if err != nil {
		return err
	}
	defer release()
	note, err := requestDaemonShutdown(client, addr)
	if err != nil {
		return err
	}
	drainDaemonAddress(addr, timing)
	trace := restartTrace{shutdownNote: note, startErr: starter(cfg)}
	return awaitFreshDaemon(client, addr, expect, trace, timing)
}

// restartLockPath is the machine-wide restart lock (ADR-0007): one governance
// root, therefore one restart at a time on the machine.
func restartLockPath() string {
	return filepath.Join(governanceRoot(), restartLockName)
}

// acquireLockDir takes an on-disk lock with the same portable primitive the
// caller-bindings writer uses: an atomic fixed-path mkdir (no flock, works on
// macOS and Linux). Two agents noticing the same stale daemon must not both
// stop and start it, or they thrash the port for each other. Unlike the
// bindings lock this one never degrades to unlocked — an unusable governance
// root is exactly the state in which an unserialized restart does the most
// damage — so every failure to acquire is reported.
func acquireLockDir(lockDir string, timing lockTiming) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(lockDir), 0o755); err != nil {
		return nil, fmt.Errorf("preparing %s: %w", lockDir, err)
	}
	deadline := time.Now().Add(timing.wait)
	for {
		err := os.Mkdir(lockDir, 0o755)
		if err == nil {
			return startLockHeartbeat(lockDir, timing.heartbeat), nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("acquiring %s: %w", lockDir, err)
		}
		reapStaleLockDir(lockDir, timing.staleAge)
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%s held for over %s; another restart is in progress — wait for it, or remove that directory if no restart is running", lockDir, timing.wait)
		}
		time.Sleep(timing.poll)
	}
}

// startLockHeartbeat keeps a held lock demonstrably alive by touching its mtime
// every interval, and returns the release.
//
// This is what makes reapStaleLockDir safe for a restart. A restart's critical
// section is not bounded by anything small — up to three shutdown routes, a
// five-second drain, an arbitrarily slow starter, and a five-second fresh-probe
// wait — so "old lock" cannot mean "abandoned lock" on age alone. With the
// production budget the holder refreshes every 5s against a 30s stale age, so a
// live lock is proven alive six times inside every reap window and can never be
// mistaken for residue however long it is held; only a holder that died (and so
// stopped refreshing) ages out. The release stops the heartbeat and waits for it
// to exit before removing the directory, so the goroutine can never touch — and
// so resurrect — a lock that has been released or retaken.
func startLockHeartbeat(lockDir string, interval time.Duration) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-ticker.C:
				_ = os.Chtimes(lockDir, now, now)
			}
		}
	}()
	return func() {
		close(stop)
		<-done
		_ = os.Remove(lockDir)
	}
}

// reapStaleLockDir removes a lock directory that has not been refreshed within
// maxAge — a crashed holder's leftover, since a live holder heartbeats far more
// often than that. Best-effort: losing this race means another waiter reaped it.
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
	// shutdownAccepted is a 2xx: the daemon took the request.
	shutdownAccepted shutdownVerdict = iota
	// shutdownDropped is a lost connection. A daemon closing its listener
	// mid-response is indistinguishable from one here, so the restart
	// continues — but it says so, because a listener that never went away
	// produces the same symptom.
	shutdownDropped
	// shutdownWedged is a request deadline: something owns the port and
	// completed the connection but never answered. Worth naming separately —
	// it is the signature of a hung daemon rather than a departing one.
	shutdownWedged
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

// requestDaemonShutdown asks whatever owns addr to stop, returning a note on how
// the acceptance was reached (empty for a clean 2xx). The ADR-0010 plain-HTTP
// default is tried first; a scheme or TLS disagreement means a legacy mTLS
// daemon may still own the address, so the S4 fallback routes are tried before
// giving up. A live listener that no route can stop is a refusal, and the caller
// must not auto-start into it.
func requestDaemonShutdown(client *http.Client, addr string) (string, error) {
	err := postShutdown(client, addr)
	verdict := classifyShutdown(err)
	if verdict == shutdownWrongRoute {
		err = tryShutdownRoutes(shutdownRoutes(client, addr), err)
		verdict = classifyShutdown(err)
	}
	if !shutdownProceeds(verdict) {
		return "", shutdownRefusedError(addr, err)
	}
	return shutdownNote(verdict, err), nil
}

// tryShutdownRoutes posts to each fallback route in turn, returning the first
// outcome the restart can proceed on and otherwise the last failure — which is
// what explains why this client cannot stop the listener.
func tryShutdownRoutes(routes []shutdownRoute, lastErr error) error {
	for _, route := range routes {
		err := postShutdown(route.client, route.addr)
		if shutdownProceeds(classifyShutdown(err)) {
			return err
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
	case isTimeoutError(err):
		return shutdownWedged
	default:
		return shutdownDropped
	}
}

// shutdownProceeds reports whether the restart may continue to the drain: a
// clean acceptance, or an ambiguous ending that only the drain and the
// post-start identity probe can resolve.
func shutdownProceeds(verdict shutdownVerdict) bool {
	return verdict == shutdownAccepted || verdict == shutdownDropped || verdict == shutdownWedged
}

// shutdownNote describes an acceptance that was not a clean 2xx, so a later
// failure can say why the shutdown was believed in the first place.
func shutdownNote(verdict shutdownVerdict, err error) string {
	switch verdict {
	case shutdownWedged:
		return fmt.Sprintf("POST /shutdown timed out — the old daemon completed the connection but never answered (%v)", err)
	case shutdownDropped:
		return fmt.Sprintf("POST /shutdown was believed only because the connection dropped (%v)", err)
	default:
		return ""
	}
}

// postShutdown performs one POST /shutdown. The body is read before the status
// is judged so a rejection carries the server's own explanation.
func postShutdown(client *http.Client, addr string) error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownRequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, daemonURL(addr, "/shutdown"), nil)
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
func drainDaemonAddress(addr string, timing restartTiming) {
	hostPort := daemonHostPort(addr)
	if hostPort == "" {
		return
	}
	deadline := time.Now().Add(timing.drainWait)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", hostPort, restartDialTimeout)
		if err != nil {
			return
		}
		_ = connection.Close()
		time.Sleep(timing.poll)
	}
}

// awaitFreshDaemon judges the restart the only way a client honestly can: by
// observing a daemon that answers with a current identity. The starter's own
// error is context, not a verdict — a concurrent validate may have won the bind
// race and started the very daemon now answering (ADR-0010).
func awaitFreshDaemon(client *http.Client, addr string, expect daemonExpectations, trace restartTrace, timing restartTiming) error {
	deadline := time.Now().Add(timing.freshWait)
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
			return staleRestartError(addr, reasons, probeErr, trace)
		}
		time.Sleep(timing.poll)
	}
}

// staleRestartError reports what the post-start probe actually saw, carrying how
// the shutdown was believed and what the starter reported beneath it.
func staleRestartError(addr string, reasons []string, probeErr error, trace restartTrace) error {
	detail := fmt.Sprintf("it does not answer GET /health: %v", probeErr)
	if probeErr == nil {
		detail = "it is still stale: " + strings.Join(reasons, "; ")
	}
	if trace.shutdownNote != "" {
		detail += " [" + trace.shutdownNote + "]"
	}
	if trace.startErr != nil {
		return fmt.Errorf("no current daemon at %s after restart — %s; auto-start reported: %w", addr, detail, trace.startErr)
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

// ensureCurrentDaemon guarantees a healthy, up-to-date daemon at addr: probe
// its identity, and when it is stale (or absent) replace or start it via the
// S5 restart engine, deriving expectations from this binary and environment.
func ensureCurrentDaemon(httpClient *http.Client, addr string, cfg DaemonStartConfig, starter func(DaemonStartConfig) error) error {
	return ensureCurrentDaemonReporting(httpClient, addr, cfg, starter, nil)
}

// ensureCurrentDaemonReporting is ensureCurrentDaemon with a seam for narrating
// the replacement: onboard prints why the daemon it found was replaced, while
// the silent validate path passes nil.
func ensureCurrentDaemonReporting(httpClient *http.Client, addr string, cfg DaemonStartConfig, starter func(DaemonStartConfig) error, report func([]string)) error {
	if localDaemonScheme(addr) == "" {
		// A daemon that is not on this machine's loopback is not ours to stop:
		// DaemonStartConfig.Local marks managed certificate provisioning, not
		// locality (daemonStartConfigFromMaterial sets it for any managed mode),
		// so the address is the only trustworthy evidence. Currency is a
		// machine-local concern; a remote endpoint gets the plain ensure it has
		// always had, and never a shutdown or a restart.
		return ensureDaemon(httpClient, addr, cfg, starter)
	}
	expect := currentDaemonExpectations(expectedListenMode(cfg))
	identity, err := probeDaemonIdentity(httpClient, addr)
	if err != nil {
		return ensureAfterUnreadableIdentity(httpClient, addr, cfg, starter, expect, err, report)
	}
	reasons := daemonStaleness(identity, expect)
	if len(reasons) == 0 {
		return nil
	}
	return replaceStaleDaemon(httpClient, addr, cfg, starter, expect, reasons, report)
}

// ensureAfterUnreadableIdentity decides what a probe that yielded no identity
// means, and there are three answers. A transport this client cannot speak is
// the legacy mTLS daemon the machine is migrating off (ADR-0010): stale by
// definition, and the restart engine already knows how to stop it. A live
// listener that answers with a readable non-200 is a foreign process owning the
// address; naming it is the only safe move, because auto-starting behind it
// would race a process nobody here owns. Anything else is nothing listening —
// the ordinary cold start.
func ensureAfterUnreadableIdentity(httpClient *http.Client, addr string, cfg DaemonStartConfig, starter func(DaemonStartConfig) error, expect daemonExpectations, probeErr error, report func([]string)) error {
	if isSchemeMismatch(probeErr) || isTLSError(probeErr) {
		reasons := []string{fmt.Sprintf("the daemon at %s does not answer GET /health on this client's transport (%v), so it predates the current listen mode", addr, probeErr)}
		return replaceStaleDaemon(httpClient, addr, cfg, starter, expect, reasons, report)
	}
	var statusErr httpStatusError
	if errors.As(probeErr, &statusErr) {
		return occupiedAddressError(addr, probeErr)
	}
	trace := restartTrace{startErr: starter(cfg)}
	return awaitFreshDaemon(httpClient, addr, expect, trace, defaultRestartTiming())
}

// replaceStaleDaemon narrates the staleness, restarts, and — when the restart
// fails — reports both what was wrong and how the replacement went, so the
// reasons never disappear into a bare failure.
func replaceStaleDaemon(httpClient *http.Client, addr string, cfg DaemonStartConfig, starter func(DaemonStartConfig) error, expect daemonExpectations, reasons []string, report func([]string)) error {
	if report != nil {
		report(reasons)
	}
	if err := restartLocalDaemon(httpClient, addr, cfg, expect, starter); err != nil {
		return fmt.Errorf("replacing the stale daemon at %s (%s): %w", addr, strings.Join(reasons, "; "), err)
	}
	return nil
}

// occupiedAddressError names the listener this client must not disturb: it owns
// the address and answers GET /health as something other than a governance
// daemon, so the next move is to identify that process rather than to start a
// daemon behind it.
func occupiedAddressError(addr string, cause error) error {
	return fmt.Errorf(
		"the address %s is owned by a process that does not answer GET /health as an agent-fitness-functions daemon (%v); identify it with `lsof -nP -iTCP:%s` and stop it, then re-run",
		addr, cause, daemonPort(addr),
	)
}
