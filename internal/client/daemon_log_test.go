package client

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These scenarios pin the calm-poc-l9u compounding gap: StartDaemon used to
// discard the detached daemon's stdout/stderr entirely, so an auto-start
// failure was undiagnosable without a manual repro. The detached child's
// output must land in a per-repo log file (beside the gitignored dev certs),
// and a health-wait failure must name that log.

// TestDaemonLogPathDerivesFromCertDir: the daemon log lives beside the managed
// dev certificates — already gitignored and repo-scoped — and is empty (no
// logging destination) only when no cert dir was resolved.
func TestDaemonLogPathDerivesFromCertDir(t *testing.T) {
	got := daemonLogPath(DaemonStartConfig{CertDir: filepath.Join("/x", "certs")})
	want := filepath.Join("/x", "certs", "daemon.log")
	if got != want {
		t.Fatalf("daemonLogPath = %q, want %q", got, want)
	}
	if got := daemonLogPath(DaemonStartConfig{}); got != "" {
		t.Fatalf("daemonLogPath without cert dir = %q, want empty", got)
	}
}

// TestStartDaemonCapturesDetachedDaemonOutputToLogFile: given a daemon
// executable that writes to both stdout and stderr, when StartDaemon launches
// it detached, then both streams land in the daemon log along with a
// parent-written start header naming the listen address — so an auto-start
// failure is diagnosable after the fact.
func TestStartDaemonCapturesDetachedDaemonOutputToLogFile(t *testing.T) {
	certDir := t.TempDir()
	script := filepath.Join(t.TempDir(), "fake-daemon")
	writeFileTest(t, script, "#!/bin/sh\necho fake-daemon-stdout\necho fake-daemon-stderr >&2\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatalf("chmod fake daemon: %v", err)
	}
	restore := daemonExecutable
	daemonExecutable = func() (string, error) { return script, nil }
	t.Cleanup(func() { daemonExecutable = restore })

	cfg := DaemonStartConfig{Addr: "https://127.0.0.1:7899", CertDir: certDir}
	if err := StartDaemon(cfg); err != nil {
		t.Fatalf("StartDaemon: %v", err)
	}

	logPath := daemonLogPath(cfg)
	deadline := time.Now().Add(5 * time.Second)
	var content string
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(logPath)
		if err == nil {
			content = string(raw)
			if strings.Contains(content, "fake-daemon-stdout") && strings.Contains(content, "fake-daemon-stderr") {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	for _, want := range []string{"fake-daemon-stdout", "fake-daemon-stderr", "127.0.0.1:7899"} {
		if !strings.Contains(content, want) {
			t.Fatalf("daemon log %q = %q, want to contain %q", logPath, content, want)
		}
	}
}

// TestStartDaemonWithoutLogDestinationStillStarts: a missing cert dir (no log
// destination) must not block daemon auto-start — output capture is a
// diagnostic aid, never a new failure mode.
func TestStartDaemonWithoutLogDestinationStillStarts(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-daemon")
	writeFileTest(t, script, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatalf("chmod fake daemon: %v", err)
	}
	restore := daemonExecutable
	daemonExecutable = func() (string, error) { return script, nil }
	t.Cleanup(func() { daemonExecutable = restore })

	if err := StartDaemon(DaemonStartConfig{Addr: "https://127.0.0.1:7899"}); err != nil {
		t.Fatalf("StartDaemon without a log destination: %v", err)
	}
}

// TestDescribeDaemonFailureNamesDaemonLog: a health-wait failure must point at
// the captured daemon log when a managed auto-start owns one, and fall back to the
// unmanaged summary (which names no paths at all) when it does not, so the user knows
// exactly where (not) to look.
func TestDescribeDaemonFailureNamesDaemonLog(t *testing.T) {
	cause := errors.New("daemon at https://127.0.0.1:7890 did not become healthy within 5s")
	withLog := describeDaemonFailure(DaemonStartConfig{
		Addr:        "https://127.0.0.1:7890",
		ManagedRoot: filepath.Join("/x", "certs"),
		CertDir:     filepath.Join("/x", "certs"),
		ConfigsDir:  filepath.Join("/x", "configs"),
	}, cause)
	if !strings.Contains(withLog.Error(), "log="+filepath.Join("/x", "certs", "daemon.log")) {
		t.Fatalf("error = %q, want to name the daemon log path", withLog.Error())
	}
	withoutLog := describeDaemonFailure(DaemonStartConfig{Addr: "https://127.0.0.1:7890"}, cause)
	if strings.Contains(withoutLog.Error(), "log=") {
		t.Fatalf("error = %q, want no log field without a log destination", withoutLog.Error())
	}
}

// TestDescribeDaemonFailureOmitsUnsetManagedFields: a managed auto-start names only
// the paths it actually owns, so a missing configs dir is left out entirely rather
// than reported as an empty or placeholder value.
func TestDescribeDaemonFailureOmitsUnsetManagedFields(t *testing.T) {
	cause := errors.New("daemon at https://127.0.0.1:7890 did not become healthy within 5s")
	message := describeDaemonFailure(DaemonStartConfig{
		Addr:        "https://127.0.0.1:7890",
		ManagedRoot: filepath.Join("/x", "certs"),
		CertDir:     filepath.Join("/x", "certs"),
	}, cause).Error()

	if strings.Contains(message, "configs-dir=") {
		t.Fatalf("error = %q, want the unset configs dir omitted", message)
	}
	for _, want := range []string{
		"dev-cert-dir=" + filepath.Join("/x", "certs"),
		"log=" + filepath.Join("/x", "certs", "daemon.log"),
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("error = %q, want to contain %q", message, want)
		}
	}
}
