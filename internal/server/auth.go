package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const callerRepoBindingsFileName = "caller-repos.json"

type HandlerOptions struct {
	RequireAuthentication        bool
	TrustedProxyHeaders          bool
	TrustedProxyClientCNs        []string
	RateLimiter                  RateLimiter
	MaxConcurrentAnalysesPerRepo int
}

type ServerTLSConfig struct {
	CertPath string
	KeyPath  string
	CAPath   string
}

type CallerRepoPolicy struct {
	callers map[string]map[string]struct{}
	admins  map[string]struct{}
}

type callerRepoDocument struct {
	Callers map[string][]string `json:"callers"`
	Admins  []string            `json:"admins"`
}

type callerContextKey struct{}

var errUnauthenticated = errors.New("unauthenticated caller")

func emptyCallerRepoPolicy() CallerRepoPolicy {
	return CallerRepoPolicy{
		callers: map[string]map[string]struct{}{},
		admins:  map[string]struct{}{},
	}
}

func (p CallerRepoPolicy) Allows(caller, repo string) bool {
	if caller == "" || repo == "" {
		return false
	}
	repos, ok := p.callers[caller]
	if !ok {
		return false
	}
	_, ok = repos[repo]
	return ok
}

func (p CallerRepoPolicy) IsAdmin(caller string) bool {
	if caller == "" {
		return false
	}
	_, ok := p.admins[caller]
	return ok
}

func cloneCallerRepoPolicy(policy CallerRepoPolicy) CallerRepoPolicy {
	clone := emptyCallerRepoPolicy()
	for caller, repos := range policy.callers {
		clone.callers[caller] = make(map[string]struct{}, len(repos))
		for repo := range repos {
			clone.callers[caller][repo] = struct{}{}
		}
	}
	for caller := range policy.admins {
		clone.admins[caller] = struct{}{}
	}
	return clone
}

func callerRepoBindingsPath(configDir string) string {
	cleanDir := filepath.Clean(configDir)
	if filepath.Base(cleanDir) == "configs" {
		return filepath.Join(filepath.Dir(cleanDir), callerRepoBindingsFileName)
	}
	return filepath.Join(cleanDir, callerRepoBindingsFileName)
}

// ValidateCallerRepoBindings reports whether content is a caller-repos.json document
// the server can load: valid JSON with well-formed caller identities and repo names.
// Tests and tooling use it to check an authorization file without depending on the
// unexported parser or pinning its exact contents.
func ValidateCallerRepoBindings(content []byte) error {
	_, err := parseCallerRepoPolicy(content)
	return err
}

func parseCallerRepoPolicy(content []byte) (CallerRepoPolicy, error) {
	var document callerRepoDocument
	if err := json.Unmarshal(content, &document); err == nil && (document.Callers != nil || document.Admins != nil) {
		return buildCallerRepoPolicy(document)
	}

	var legacy map[string][]string
	if err := json.Unmarshal(content, &legacy); err != nil {
		return CallerRepoPolicy{}, inputError("parsing caller-repos.json", err)
	}
	return buildCallerRepoPolicy(callerRepoDocument{Callers: legacy})
}

func buildCallerRepoPolicy(document callerRepoDocument) (CallerRepoPolicy, error) {
	policy := emptyCallerRepoPolicy()
	for caller, repos := range document.Callers {
		normalizedCaller, err := normalizeCallerName(caller)
		if err != nil {
			return CallerRepoPolicy{}, err
		}
		if _, ok := policy.callers[normalizedCaller]; !ok {
			policy.callers[normalizedCaller] = map[string]struct{}{}
		}
		for _, repo := range repos {
			repoName, err := validateRepoName(repo)
			if err != nil {
				return CallerRepoPolicy{}, err
			}
			policy.callers[normalizedCaller][repoName] = struct{}{}
		}
	}
	for _, caller := range document.Admins {
		normalizedCaller, err := normalizeCallerName(caller)
		if err != nil {
			return CallerRepoPolicy{}, err
		}
		policy.admins[normalizedCaller] = struct{}{}
	}
	return policy, nil
}

func normalizeCallerName(caller string) (string, error) {
	normalized := strings.TrimSpace(caller)
	if normalized == "" || strings.ContainsRune(normalized, 0) {
		return "", inputError("invalid caller identity", nil)
	}
	return normalized, nil
}

func withAuthenticatedCaller(next http.HandlerFunc, options HandlerOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !options.RequireAuthentication {
			next(w, r)
			return
		}
		caller, err := extractCallerName(r, options)
		if err != nil {
			writeUnauthorized(w)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), callerContextKey{}, caller)))
	}
}

func authenticatedCaller(r *http.Request) (string, bool) {
	caller, ok := r.Context().Value(callerContextKey{}).(string)
	return caller, ok && caller != ""
}

func extractCallerName(r *http.Request, options HandlerOptions) (string, error) {
	if options.TrustedProxyHeaders {
		if !requestHasVerifiedPeerIdentity(r) {
			return "", errUnauthenticated
		}
		proxyPeer, err := normalizeCallerName(r.TLS.PeerCertificates[0].Subject.CommonName)
		if err != nil || !trustedProxyPeer(proxyPeer, options.TrustedProxyClientCNs) {
			return "", errUnauthenticated
		}
		caller, err := normalizeCallerName(r.Header.Get("X-Client-CN"))
		if err != nil {
			return "", errUnauthenticated
		}
		return caller, nil
	}
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return "", errUnauthenticated
	}
	caller, err := normalizeCallerName(r.TLS.PeerCertificates[0].Subject.CommonName)
	if err != nil {
		return "", errUnauthenticated
	}
	return caller, nil
}

func requestHasVerifiedPeerIdentity(r *http.Request) bool {
	return r != nil && r.TLS != nil && len(r.TLS.PeerCertificates) > 0 && len(r.TLS.VerifiedChains) > 0
}

func trustedProxyPeer(peer string, allowed []string) bool {
	for _, candidate := range allowed {
		normalized, err := normalizeCallerName(candidate)
		if err != nil {
			continue
		}
		if normalized == peer {
			return true
		}
	}
	return false
}

func authorizeRepoAccess(store *ConfigStore, caller, repo string) (string, error) {
	repoName, err := canonicalRepoName(repo)
	if err != nil {
		return "", err
	}
	if store == nil || !store.CallerAllowed(caller, repoName) {
		return "", fmt.Errorf("caller %q is not authorized for repository %q", caller, repoName)
	}
	return repoName, nil
}

func writeUnauthorized(w http.ResponseWriter) {
	http.Error(w, "authentication required", http.StatusUnauthorized)
}

func writeForbidden(w http.ResponseWriter, message string) {
	http.Error(w, message, http.StatusForbidden)
}

func (c ServerTLSConfig) Enabled() bool {
	return c.CertPath != "" || c.KeyPath != "" || c.CAPath != ""
}

func (c ServerTLSConfig) Validate() error {
	if !c.Enabled() {
		return nil
	}
	if c.CertPath == "" || c.KeyPath == "" || c.CAPath == "" {
		return errors.New("tls requires --tls-cert, --tls-key, and --tls-ca " +
			"(or AGENT_FITNESS_FUNCTIONS_TLS_CERT, AGENT_FITNESS_FUNCTIONS_TLS_KEY, and AGENT_FITNESS_FUNCTIONS_TLS_CA)")
	}
	return nil
}

func loadServerTLSConfig(config ServerTLSConfig) (*tls.Config, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !config.Enabled() {
		return nil, nil
	}
	certificate, err := tls.LoadX509KeyPair(config.CertPath, config.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("load tls certificate: %w", err)
	}
	caContent, err := os.ReadFile(config.CAPath)
	if err != nil {
		return nil, fmt.Errorf("read tls ca: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caContent) {
		return nil, errors.New("parse tls ca")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{certificate},
		ClientCAs:    pool,
		ClientAuth:   tls.VerifyClientCertIfGiven,
		MinVersion:   tls.VersionTLS12,
	}, nil
}
