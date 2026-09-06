package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/server"
)

// S3 red-test contract (calm-poc-rzss): --listen-mode selects the daemon's
// listen mode ahead of any certificate resolution, prefers the flag over
// AGENT_FITNESS_FUNCTIONS_LISTEN_MODE, defaults to mTLS, and rejects unknown
// values with a usage error.

func TestResolveServerListenModePrecedence(t *testing.T) {
	cases := []struct {
		name string
		flag string
		env  string
		want string
	}{
		{name: "default is mtls", want: server.ListenModeMTLS},
		{name: "flag selects local-http", flag: server.ListenModeLocalHTTP, want: server.ListenModeLocalHTTP},
		{name: "env selects local-http", env: server.ListenModeLocalHTTP, want: server.ListenModeLocalHTTP},
		{name: "flag beats env", flag: server.ListenModeMTLS, env: server.ListenModeLocalHTTP, want: server.ListenModeMTLS},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("AGENT_FITNESS_FUNCTIONS_LISTEN_MODE", testCase.env)
			mode, err := resolveServerListenMode(testCase.flag)
			if err != nil {
				t.Fatalf("resolveServerListenMode(%q) error: %v", testCase.flag, err)
			}
			if mode != testCase.want {
				t.Fatalf("resolveServerListenMode(%q) = %q, want %q", testCase.flag, mode, testCase.want)
			}
		})
	}
}

func TestResolveServerListenModeRejectsUnknownValueAsUsageError(t *testing.T) {
	t.Setenv("AGENT_FITNESS_FUNCTIONS_LISTEN_MODE", "")
	_, err := resolveServerListenMode("bogus")
	var usage usageError
	if !errors.As(err, &usage) {
		t.Fatalf("resolveServerListenMode(bogus) = %v, want a usageError", err)
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error %q should name the rejected value", err.Error())
	}
}

// Local mode must never consult certificate flags, env selectors, or the
// working directory: resolving the start mode for local-http with a failing
// working-directory lookup must still succeed with no TLS material.
func TestResolveServerStartModeLocalHTTPSkipsCertificateResolution(t *testing.T) {
	t.Setenv("AGENT_FITNESS_FUNCTIONS_LISTEN_MODE", "")
	t.Setenv("AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR", "")
	failingGetwd := func() (string, error) { return "", errors.New("working directory must not be consulted") }

	mode, err := resolveServerStartMode(server.ListenModeLocalHTTP, "", "", "", failingGetwd)
	if err != nil {
		t.Fatalf("resolveServerStartMode(local-http) error: %v", err)
	}
	if mode.ListenMode != server.ListenModeLocalHTTP {
		t.Errorf("ListenMode = %q, want %q", mode.ListenMode, server.ListenModeLocalHTTP)
	}
	if mode.ManagedRoot != "" || mode.TLS.Enabled() {
		t.Errorf("local-http start mode carries TLS material: %+v", mode)
	}
}
