package client

import (
	"encoding/json"
	"io"
	"net/http"
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

// daemonStaleness compares an observed identity against expectations,
// returning one reason per mismatched dimension; empty means current.
func daemonStaleness(identity daemonIdentity, expect daemonExpectations) []string {
	return nil // stub pending S5 (calm-poc-5lku)
}

// restartLocalDaemon gracefully replaces the local daemon — restart lock,
// POST /shutdown, drain, starter — judging success solely by re-probing a
// fresh identity (a concurrent validate may win the bind, ADR-0010). It never
// signals processes directly.
func restartLocalDaemon(client *http.Client, addr string, cfg DaemonStartConfig, expect daemonExpectations, starter func(DaemonStartConfig) error) error {
	return nil // stub pending S5 (calm-poc-5lku)
}
