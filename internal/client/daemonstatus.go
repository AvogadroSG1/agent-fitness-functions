package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
)

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
	response, err := client.Get(healthURL(addr))
	if err != nil {
		return daemonIdentity{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return daemonIdentity{}, httpStatusError{status: response.StatusCode}
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

func healthURL(addr string) string {
	base := addr
	for len(base) > 0 && base[len(base)-1] == '/' {
		base = base[:len(base)-1]
	}
	return base + "/health"
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
// longer describes what is running.
func buildStaleness(identity daemonIdentity, expect daemonExpectations) []string {
	reasons := make([]string, 0, 2)
	if reason := valueMismatchReason("build revision", identity.BuildRevision, expect.BuildRevision); reason != "" {
		reasons = append(reasons, reason)
	}
	if identity.BuildModified && !expect.BuildModified {
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
