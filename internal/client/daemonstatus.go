package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/buildinfo"
)

// identityProbeTimeout bounds one GET /health, matching probeDaemon's budget.
// The caller's own http.Client timeout cannot be trusted for this: the CLI
// client is configured for a validation round trip (tens of seconds), and one
// hung health probe would then blow the polling budget of every loop built on
// this call.
const identityProbeTimeout = 250 * time.Millisecond

// daemonIdentity is the client-side view of the daemon's GET /health identity
// body (ADR-0010, S2). Legacy marks a daemon that answered 200 with no
// parseable identity — a binary predating the identity contract.
type daemonIdentity struct {
	BuildRevision string `json:"build_revision"`
	BuildModified bool   `json:"build_modified"`
	ListenMode    string `json:"listen_mode"`
	ConfigsDir    string `json:"configs_dir"`
	PID           int    `json:"pid"`
	Legacy        bool   `json:"-"`
}

// localHTTPListenMode is the listen mode an ADR-0010 loopback plain-HTTP daemon
// stamps on GET /health.
const localHTTPListenMode = "local-http"

// daemonExpectations is what the probing client requires of a current daemon.
type daemonExpectations struct {
	BuildRevision string
	BuildModified bool
	ListenMode    string
	ConfigsDir    string
}

// probeDaemonIdentity fetches and parses GET /health. A 200 with an empty or
// unparseable body is a legacy daemon, not an error.
func probeDaemonIdentity(client *http.Client, addr string) (daemonIdentity, error) {
	ctx, cancel := context.WithTimeout(context.Background(), identityProbeTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, daemonURL(addr, "/health"), nil)
	if err != nil {
		return daemonIdentity{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return daemonIdentity{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		// The body is carried because a TLS listener states the scheme mismatch
		// there: it is the difference between a daemon of the previous listen
		// mode and a foreign process owning the address.
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 1<<10))
		return daemonIdentity{}, httpStatusError{status: response.StatusCode, body: strings.TrimSpace(string(detail))}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		return daemonIdentity{}, err
	}
	var identity daemonIdentity
	if len(body) == 0 || json.Unmarshal(body, &identity) != nil {
		return daemonIdentity{Legacy: true}, nil
	}
	return identity, nil
}

// daemonURL joins a daemon base address and an endpoint path. Every caller in
// this package builds daemon URLs through here so a trailing slash on --addr
// can never produce a double-slashed path on one endpoint and not another.
func daemonURL(addr, path string) string {
	return strings.TrimRight(addr, "/") + path
}

// daemonStaleness compares an observed identity against expectations, returning
// one reason per mismatched dimension; an empty result means the daemon is
// current. A legacy daemon short-circuits: it reports no dimensions at all, so
// the single honest reason is that it predates the identity contract.
func daemonStaleness(identity daemonIdentity, expect daemonExpectations) []string {
	if identity.Legacy {
		return []string{"the daemon is a legacy binary that reports no identity on GET /health"}
	}
	reasons := buildStaleness(identity, expect)
	if reason := pathMismatchReason("configs directory", identity.ConfigsDir, expect.ConfigsDir); reason != "" {
		reasons = append(reasons, reason)
	}
	if reason := valueMismatchReason("listen mode", identity.ListenMode, expect.ListenMode); reason != "" {
		reasons = append(reasons, reason)
	}
	return reasons
}

// buildStaleness compares the two build dimensions. A binary built from a dirty
// working tree is stale even when its revision matches, because the revision no
// longer describes what is running — but only a probing binary that knows its
// own revision may say so. An unstamped prober (a `go test` binary, a build
// without VCS information) knows nothing about builds at all, so judging the
// daemon's working tree against its own would restart a perfectly current
// daemon on evidence it does not have.
func buildStaleness(identity daemonIdentity, expect daemonExpectations) []string {
	reasons := make([]string, 0, 2)
	if reason := valueMismatchReason("build revision", identity.BuildRevision, expect.BuildRevision); reason != "" {
		reasons = append(reasons, reason)
	}
	if expect.BuildRevision != "" && identity.BuildModified && !expect.BuildModified {
		reasons = append(reasons, "the daemon binary was built from a modified working tree, so its revision does not describe what is running")
	}
	return reasons
}

// valueMismatchReason reports a mismatch only when both sides state a value. An
// empty expectation means the dimension is not enforced, and an empty
// observation means the daemon does not stamp it (an unstamped build); neither
// is evidence of staleness, so neither may trigger a restart.
func valueMismatchReason(dimension, observed, expected string) string {
	if observed == "" || expected == "" || observed == expected {
		return ""
	}
	return fmt.Sprintf("daemon %s %q does not match the expected %q", dimension, observed, expected)
}

// pathMismatchReason is valueMismatchReason over filesystem paths, compared
// cleaned and absolute so /gov/configs and /gov/./configs name one directory.
func pathMismatchReason(dimension, observed, expected string) string {
	if observed == "" || expected == "" || absoluteCleanPath(observed) == absoluteCleanPath(expected) {
		return ""
	}
	return fmt.Sprintf("daemon %s %q does not match the expected %q", dimension, observed, expected)
}

func absoluteCleanPath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return absolute
}

// currentDaemonExpectations is what a daemon started by this binary, on this
// machine, right now would report: this build's stamp, the configs directory
// this environment resolves, and listenMode when the caller can state one. An
// empty dimension is not enforced (see daemonStaleness), which is what keeps an
// unstamped build — or an address whose listen mode nobody can predict — from
// being judged stale on evidence nobody has.
func currentDaemonExpectations(listenMode string) daemonExpectations {
	revision, modified := buildinfo.Current()
	return daemonExpectations{
		BuildRevision: revision,
		BuildModified: modified,
		ListenMode:    listenMode,
		ConfigsDir:    resolveConfigsDir(),
	}
}

// expectedListenMode is the listen mode a daemon started with cfg would report.
// Only the ADR-0010 managed-local start states one; the legacy loopback https
// start and every unmanaged start leave the dimension unenforced rather than
// guess at a mode this client does not choose.
func expectedListenMode(cfg DaemonStartConfig) string {
	if localHTTPStart(cfg) {
		return localHTTPListenMode
	}
	return ""
}
