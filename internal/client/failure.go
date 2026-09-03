package client

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Infrastructure-failure kinds distinguish a setup problem from a real architecture
// verdict. They are emitted in the machine-readable error object that `client
// validate` prints so the hooks and coding agents can tell "fix your setup" from "fix
// your architecture" instead of treating every failure as a block.
const (
	errorKindServerUnreachable = "server_unreachable"
	errorKindTLSFailure        = "tls_failure"
	errorKindUnauthenticated   = "unauthenticated"
	errorKindUnauthorized      = "unauthorized"
	errorKindNotConfigured     = "not_configured"
	errorKindInvalidRequest    = "invalid_request"
	errorKindServerError       = "server_error"
	errorKindPortConflict      = "port_conflict"
)

// InfraErrorExitCode is the `client validate` process exit code reserved for an
// infrastructure failure. It is deliberately distinct from a block verdict — a block
// is a successful check whose status JSON is "block" and exits 0 — so hooks and agents
// can branch on setup-vs-architecture from the exit code alone.
const InfraErrorExitCode = 3

// infraError classifies a validate failure that is not an architecture verdict. It
// carries a machine-readable kind plus a remediation naming the exact command or file
// that fixes it. It implements error so it flows through RunCheck's return path.
type infraError struct {
	kind        string
	message     string
	remediation string
}

func (e infraError) Error() string { return e.message }

// IsInfraError reports whether err is an infrastructure failure (server unreachable,
// TLS/cert problem, auth rejection, repo not configured, or server error) rather than
// a usage error or a real fitness-function block.
func IsInfraError(err error) bool {
	var target infraError
	return errors.As(err, &target)
}

// httpStatusError is a non-200 response from POST /check, carrying the status and the
// trimmed response body so the classifier can map it to an infra-error kind.
type httpStatusError struct {
	status int
	body   string
}

func (e httpStatusError) Error() string {
	if e.body == "" {
		return fmt.Sprintf("check failed with HTTP %d", e.status)
	}
	return fmt.Sprintf("check failed with HTTP %d: %s", e.status, e.body)
}

// transportError wraps a failure to reach the server at all (dial refused, timeout, or
// a TLS handshake failure) so the classifier can distinguish unreachable from a
// TLS/cert problem.
type transportError struct {
	err error
}

func (e transportError) Error() string { return e.err.Error() }
func (e transportError) Unwrap() error { return e.err }

// daemonConflictError reports a TLS probe failure against a live listener on the shared
// default local daemon port when this client's own managed dev-cert material has
// already loaded cleanly. That combination means the listener trusts a different dev
// CA. Since ADR-0007 moved every repository onto one shared machine governance root,
// this is most likely a stale daemon left over from before this machine migrated to
// the shared root (or from before a certificate rotation) rather than another
// repository's daemon — each repo no longer keeps its own dev CA.
type daemonConflictError struct {
	addr  string
	cause error
}

func (e daemonConflictError) Error() string {
	return fmt.Sprintf(
		"%s is already serving TLS that this client does not trust; most likely a stale daemon started before this machine migrated to the shared governance root (or before a certificate rotation) still owns this port — another repository's local daemon is possible but no longer expected now that repos share one root — stop it, rerun `client onboard` to re-register against the shared governance daemon, or rerun with a distinct --addr (%v)",
		e.addr, e.cause,
	)
}

func (e daemonConflictError) Unwrap() error { return e.cause }

// infraErrorReport is the machine-readable object `client validate` prints to stdout on
// an infrastructure failure. It extends the /check result JSON shape ({"status":...})
// with the error kind, a human message, and a remediation so hooks and agents branch
// on it rather than on an opaque nonzero exit.
type infraErrorReport struct {
	Status      string `json:"status"`
	ErrorKind   string `json:"error_kind"`
	Message     string `json:"message"`
	Remediation string `json:"remediation"`
}

// handleValidateFailure classifies a validate failure: an infrastructure failure is
// emitted as the machine-readable error object on stdout and returned as an infraError
// (mapped to InfraErrorExitCode); a usage error is returned untouched so it keeps its
// own exit code.
func handleValidateFailure(stdout io.Writer, err error, repo string) error {
	ierr, ok := classifyValidationError(err, repo)
	if !ok {
		return err
	}
	return reportInfraError(stdout, ierr)
}

// classifyValidationError maps a validate failure to an infra-error kind, returning
// ok=false for a usage error (which keeps its distinct exit code) and for a nil error.
func classifyValidationError(err error, repo string) (infraError, bool) {
	if err == nil || IsUsageError(err) {
		return infraError{}, false
	}
	var statusErr httpStatusError
	if errors.As(err, &statusErr) {
		return httpStatusInfraError(statusErr, repo), true
	}
	return connectionInfraError(err), true
}

// httpStatusInfraError maps an HTTP status from the governance server to the matching
// infra-error kind and a remediation naming the real command or file that fixes it.
func httpStatusInfraError(statusErr httpStatusError, repo string) infraError {
	detail := serverDetail(statusErr.body)
	switch statusErr.status {
	case http.StatusUnauthorized:
		return infraError{
			kind:        errorKindUnauthenticated,
			message:     "governance server rejected the client certificate (HTTP 401)" + detail,
			remediation: "run `agent-fitness-functions doctor`; regenerate dev certs with scripts/generate-dev-certs.sh --force, or set AGENT_FITNESS_FUNCTIONS_ON_ERROR=advisory to unblock",
		}
	case http.StatusForbidden:
		return infraError{
			kind:        errorKindUnauthorized,
			message:     "client certificate is not authorized for this repository (HTTP 403)" + detail,
			remediation: "add this client certificate's CN to caller-repos.json on the server, then redeploy the container; or set AGENT_FITNESS_FUNCTIONS_ON_ERROR=advisory to unblock",
		}
	case http.StatusNotFound:
		return infraError{
			kind:        errorKindNotConfigured,
			message:     fmt.Sprintf("repository %q is not configured on the governance server (HTTP 404)", repo) + detail,
			remediation: fmt.Sprintf("run `agent-fitness-functions client onboard`, or create configs/%s/config.json on the server (see docs/runbooks/onboard-new-repository.md); or set AGENT_FITNESS_FUNCTIONS_ON_ERROR=advisory to unblock", repo),
		}
	case http.StatusBadRequest:
		return infraError{
			kind:        errorKindInvalidRequest,
			message:     "governance server rejected the request as invalid (HTTP 400)" + detail,
			remediation: "check --repo and --language; run `agent-fitness-functions doctor --repair`, or set AGENT_FITNESS_FUNCTIONS_ON_ERROR=advisory in your repository config to proceed",
		}
	default:
		return infraError{
			kind:        errorKindServerError,
			message:     fmt.Sprintf("governance server could not produce a verdict (HTTP %d)", statusErr.status) + detail,
			remediation: "check the server logs; retry, run `agent-fitness-functions doctor --repair`, or set AGENT_FITNESS_FUNCTIONS_ON_ERROR=advisory in your repository config to proceed",
		}
	}
}

// connectionInfraError classifies a failure to reach or handshake with the server: a
// TLS/cert problem versus an unreachable server. It also covers daemon auto-start
// failures, which surface here as generic (non-status) errors.
func connectionInfraError(err error) infraError {
	var conflict daemonConflictError
	if errors.As(err, &conflict) {
		return portConflictInfraError(conflict)
	}
	if isTLSError(err) {
		return infraError{
			kind:        errorKindTLSFailure,
			message:     "TLS/certificate failure talking to the governance server: " + err.Error(),
			remediation: "run `agent-fitness-functions doctor`; regenerate dev certs with scripts/generate-dev-certs.sh --force, or set AGENT_FITNESS_FUNCTIONS_ON_ERROR=advisory to unblock",
		}
	}
	return infraError{
		kind:        errorKindServerUnreachable,
		message:     "cannot reach the governance server: " + err.Error(),
		remediation: "run `agent-fitness-functions doctor`; the local daemon auto-starts on `client validate` when dev certs and a repo config exist, or set AGENT_FITNESS_FUNCTIONS_ON_ERROR=advisory to unblock",
	}
}

// portConflictInfraError maps a daemonConflictError to its infra-error kind: the
// contested address is already held by a daemon that does not trust this repository's
// dev CA, so the fix is to stop it or move this client to a distinct port.
func portConflictInfraError(conflict daemonConflictError) infraError {
	return infraError{
		kind: errorKindPortConflict,
		message: fmt.Sprintf(
			"a daemon that does not trust this repository's dev CA is already listening at %s (most likely another repository's local daemon, or a stale one from before certificate rotation): %v",
			conflict.addr, conflict.cause,
		),
		remediation: "identify the conflicting daemon with `lsof -i :<port>` and stop it, or rerun with a distinct --addr; only one repository can use the shared default local daemon port at a time",
	}
}

// isTLSError reports whether err is a TLS handshake or certificate-material problem,
// via typed x509/tls errors first and a text fallback for wrapped cert-load failures.
func isTLSError(err error) bool {
	var certVerify *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	var certInvalid x509.CertificateInvalidError
	var recordHeader tls.RecordHeaderError
	if errors.As(err, &certVerify) || errors.As(err, &unknownAuthority) ||
		errors.As(err, &hostnameErr) || errors.As(err, &certInvalid) ||
		errors.As(err, &recordHeader) {
		return true
	}
	return tlsErrorText(err.Error())
}

func tlsErrorText(message string) bool {
	for _, marker := range []string{"tls:", "x509:", "certificate", "CA bundle"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func serverDetail(body string) string {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return ""
	}
	return ": " + trimmed
}

// reportInfraError writes the machine-readable error object to stdout and returns the
// infraError so main maps it to InfraErrorExitCode. A marshal failure falls back to
// returning the error unadorned.
func reportInfraError(stdout io.Writer, ierr infraError) error {
	report := infraErrorReport{
		Status:      "error",
		ErrorKind:   ierr.kind,
		Message:     ierr.message,
		Remediation: ierr.remediation,
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return ierr
	}
	_, _ = stdout.Write(append(encoded, '\n'))
	return ierr
}
