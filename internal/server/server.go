package server

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/devcerts"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
)

const (
	maxValidationRequestBytes = 5 << 20
	defaultConfigsDir         = "/app/configs"
)

// StateResponse is the JSON response returned by GET /state for one repository.
type StateResponse struct {
	Repo       string              `json:"repo"`
	Violations []fitness.Violation `json:"violations"`
}

// PreflightResponse is the JSON response returned by GET /preflight for one
// repository. Unlike /check, this endpoint never fails on an unauthorized caller or
// an unconfigured repo: it reports those facts so a client-side doctor can surface
// them as distinct, actionable check results instead of a single blocked commit.
type PreflightResponse struct {
	AuthenticatedCN  string          `json:"authenticated_cn"`
	RepoConfigured   bool            `json:"repo_configured"`
	RepoConfigValid  bool            `json:"repo_config_valid"`
	CallerAuthorized bool            `json:"caller_authorized"`
	EnforcementMode  EnforcementMode `json:"enforcement_mode"`
}

// ConfigsResponse is the JSON response returned by GET /configs.
type ConfigsResponse struct {
	Version  string                         `json:"version"`
	LoadedAt time.Time                      `json:"loaded_at"`
	Repos    map[string]ConfigsRepoResponse `json:"repos"`
}

// ConfigsRepoResponse describes one repo entry in GET /configs.
type ConfigsRepoResponse struct {
	Status           string          `json:"status"`
	EnforcementMode  EnforcementMode `json:"enforcement-mode,omitempty"`
	FitnessFunctions map[string]bool `json:"fitness-functions,omitempty"`
	Error            string          `json:"error,omitempty"`
	LastValidAt      *time.Time      `json:"last_valid_at,omitempty"`
}

type ServeOptions struct {
	Addr            string
	ConfigDir       string
	Ready           io.Writer
	NewStore        func(context.Context, string) (*ConfigStore, error)
	HandlerOptions  HandlerOptions
	TLS             ServerTLSConfig
	ManagedRoot     string
	RuntimeDir      string
	BlockOnWarmup   bool
	AnalyzerTimeout time.Duration
	tlsConfig       *tls.Config
	runtime         *managedRuntime
}

// NewHandler builds the agent-fitness-functions HTTP daemon routes.
func NewHandler(configStore *ConfigStore, shutdown func()) http.Handler {
	return NewHandlerWithOptions(Checker{ConfigStore: configStore}, shutdown, HandlerOptions{})
}

// NewHandlerWithChecker builds the agent-fitness-functions HTTP daemon routes with injected check dependencies.
func NewHandlerWithChecker(checker Checker, shutdown func()) http.Handler {
	return NewHandlerWithOptions(checker, shutdown, HandlerOptions{})
}

func NewHandlerWithOptions(checker Checker, shutdown func(), options HandlerOptions) http.Handler {
	if checker.State == nil {
		checker.State = NewState()
	}
	if checker.ConcurrencyPermits == 0 && options.MaxConcurrentAnalysesPerRepo > 0 {
		checker.ConcurrencyPermits = options.MaxConcurrentAnalysesPerRepo
	}
	deferredCtx, cancelDeferred := context.WithCancel(context.Background())
	if checker.DeferredContext == nil {
		checker.DeferredContext = deferredCtx
	} else {
		cancelDeferred()
		cancelDeferred = func() {}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/check", withAuthenticatedCaller(checkHandler(checker, options), options))
	mux.HandleFunc("/state", withAuthenticatedCaller(stateHandler(checker, options), options))
	mux.HandleFunc("/preflight", withAuthenticatedCaller(preflightHandler(checker, options), options))
	mux.HandleFunc("/configs", withAuthenticatedCaller(configsHandler(checker, options), options))
	mux.HandleFunc("/shutdown", withAuthenticatedCaller(shutdownHandler(checker, options, cancelDeferred, shutdown), options))
	return mux
}

func checkHandler(checker Checker, options HandlerOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		caller := resolveCheckCaller(r, options)
		if options.RateLimiter != nil && !options.RateLimiter.Allow(caller) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		request, ok := decodeValidationRequest(w, r)
		if !ok {
			return
		}
		if options.RequireAuthentication {
			authorizedRepo, err := authorizeRepoAccess(checker.ConfigStore, caller, request.Repo)
			if err != nil {
				writeAuthorizationError(w, err)
				return
			}
			request.Repo = authorizedRepo
		}
		releaseSlot, slotAcquired := acquireConcurrencySlot(w, checker.State, request.Repo, options.MaxConcurrentAnalysesPerRepo)
		if !slotAcquired {
			return
		}
		defer releaseSlot()
		response, err := (&checker).Check(r.Context(), request)
		if err != nil {
			writeCheckError(w, err)
			return
		}
		writeJSON(w, response)
	}
}

// acquireConcurrencySlot checks the per-repo concurrency cap. When n==0, it
// always succeeds with a no-op release. On failure it writes 503 and returns
// (nil, false).
func acquireConcurrencySlot(w http.ResponseWriter, state *State, repo string, n int) (func(), bool) {
	if n <= 0 {
		return func() {}, true
	}
	release, ok := state.TryLockRepoN(repo, n)
	if !ok {
		http.Error(w, "repository analysis capacity exceeded", http.StatusServiceUnavailable)
		return nil, false
	}
	return release, true
}

func resolveCheckCaller(r *http.Request, options HandlerOptions) string {
	caller, _ := authenticatedCaller(r)
	if caller == "" && !options.RequireAuthentication {
		caller = remoteHost(r)
	}
	return caller
}

// decodeValidationRequest reads the body into a buffer (enforcing the size cap first)
// then unmarshals JSON. Reading the full buffer before decoding ensures that an
// oversized body returns 413 even when the JSON is invalid from the first byte.
func decodeValidationRequest(w http.ResponseWriter, r *http.Request) (fitness.ValidationRequest, bool) {
	limitedBody := http.MaxBytesReader(w, r.Body, maxValidationRequestBytes)
	defer func() { _ = limitedBody.Close() }()
	raw, err := io.ReadAll(limitedBody)
	if err != nil {
		if isMaxBytesError(err) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid check request", http.StatusBadRequest)
		}
		return fitness.ValidationRequest{}, false
	}
	var request fitness.ValidationRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		http.Error(w, "invalid check request", http.StatusBadRequest)
		return fitness.ValidationRequest{}, false
	}
	return request, true
}

func writeAuthorizationError(w http.ResponseWriter, err error) {
	if isInputError(err) {
		writeCheckError(w, err)
		return
	}
	writeForbidden(w, err.Error())
}

func stateHandler(checker Checker, options HandlerOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		caller := ""
		if options.RequireAuthentication {
			var ok bool
			caller, ok = authenticatedCaller(r)
			if !ok {
				writeUnauthorized(w)
				return
			}
		}
		repo := r.URL.Query().Get("repo")
		if repo == "" {
			http.Error(w, "state requires repo", http.StatusBadRequest)
			return
		}
		targetRepo, ok := resolveAuthorizedRepo(w, checker.ConfigStore, caller, repo, options.RequireAuthentication)
		if !ok {
			return
		}
		if _, canonicalRepo, err := loadConfig(checker.ConfigStore, targetRepo); err != nil {
			writeCheckError(w, err)
			return
		} else {
			writeJSON(w, StateResponse{Repo: canonicalRepo, Violations: checker.State.Violations(canonicalRepo)})
		}
	}
}

func resolveAuthorizedRepo(w http.ResponseWriter, store *ConfigStore, caller, repo string, requireAuth bool) (string, bool) {
	if !requireAuth {
		return repo, true
	}
	authorizedRepo, err := authorizeRepoAccess(store, caller, repo)
	if err != nil {
		writeAuthorizationError(w, err)
		return "", false
	}
	return authorizedRepo, true
}

// preflightHandler answers "am I ready?" for the ?repo= parameter. Any authenticated
// caller may call it (not admin-gated like /configs). It deliberately does not 403 an
// unauthorized caller nor 404 an unconfigured repo — those are reported as JSON facts.
func preflightHandler(checker Checker, options HandlerOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		caller := ""
		if options.RequireAuthentication {
			var ok bool
			caller, ok = authenticatedCaller(r)
			if !ok {
				writeUnauthorized(w)
				return
			}
		}
		repo := r.URL.Query().Get("repo")
		if repo == "" {
			http.Error(w, "preflight requires repo", http.StatusBadRequest)
			return
		}
		repoName, err := validateRepoName(repo)
		if err != nil {
			writeCheckError(w, err)
			return
		}
		writeJSON(w, buildPreflightResponse(checker.ConfigStore, caller, repoName))
	}
}

// buildPreflightResponse gathers the four readiness facts for repo directly from the
// config store, so an unconfigured repo is reported honestly rather than masked.
func buildPreflightResponse(store *ConfigStore, caller, repo string) PreflightResponse {
	response := PreflightResponse{
		AuthenticatedCN:  caller,
		CallerAuthorized: store.CallerAllowed(caller, repo),
	}
	entry, ok := store.Lookup(repo)
	if !ok {
		return response
	}
	response.RepoConfigured = true
	response.RepoConfigValid = entry.Valid
	if entry.Valid {
		response.EnforcementMode = entry.Config.EnforcementMode
	}
	return response
}

func configsHandler(checker Checker, options HandlerOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if options.RequireAuthentication {
			caller, ok := authenticatedCaller(r)
			if !ok {
				writeUnauthorized(w)
				return
			}
			if checker.ConfigStore == nil {
				writeCheckError(w, infrastructureError("config store is not configured", nil))
				return
			}
			if !checker.ConfigStore.CallerIsAdmin(caller) {
				writeForbidden(w, fmt.Sprintf("caller %q is not authorized for /configs", caller))
				return
			}
		}
		if checker.ConfigStore == nil {
			writeCheckError(w, infrastructureError("config store is not configured", nil))
			return
		}
		loadedAt, snapshot := checker.ConfigStore.SnapshotState()
		writeJSON(w, buildConfigsResponse(loadedAt, snapshot))
	}
}

func shutdownHandler(checker Checker, options HandlerOptions, cancelDeferred func(), shutdownFn func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if options.RequireAuthentication {
			caller, ok := authenticatedCaller(r)
			if !ok {
				writeUnauthorized(w)
				return
			}
			if checker.ConfigStore == nil {
				writeCheckError(w, infrastructureError("config store is not configured", nil))
				return
			}
			if !checker.ConfigStore.CallerIsAdmin(caller) {
				writeForbidden(w, fmt.Sprintf("caller %q is not authorized for /shutdown", caller))
				return
			}
		}
		writeJSON(w, map[string]string{"status": "shutting_down"})
		cancelDeferred()
		if shutdownFn != nil {
			shutdownFn()
		}
	}
}

// Serve starts the daemon on addr and blocks until ctx is canceled or the server fails.
func Serve(ctx context.Context, addr string) error {
	return ServeWithOptions(ctx, ServeOptions{
		Addr:      addr,
		ConfigDir: os.Getenv("AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR"),
		Ready:     os.Stdout,
		NewStore:  NewConfigStore,
	})
}

func ServeWithOptions(ctx context.Context, options ServeOptions) error {
	applyServeDefaults(&options)
	return serveWithOptions(ctx, options)
}

func applyServeDefaults(options *ServeOptions) {
	if options.Addr == "" {
		options.Addr = "localhost:7890"
	}
	if options.ConfigDir == "" {
		options.ConfigDir = os.Getenv("AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR")
	}
	if options.Ready == nil {
		options.Ready = os.Stdout
	}
	if options.NewStore == nil {
		options.NewStore = NewConfigStore
	}
}

func serveWithDependencies(ctx context.Context, addr, configDir string, ready io.Writer, newStore func(context.Context, string) (*ConfigStore, error)) error {
	return serveWithOptions(ctx, ServeOptions{Addr: addr, ConfigDir: configDir, Ready: ready, NewStore: newStore})
}

func serveWithOptions(ctx context.Context, options ServeOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	applyServeInternalDefaults(&options)
	if err := validateServeOptions(options); err != nil {
		return err
	}
	prepared, err := prepareServeTLS(options)
	if err != nil {
		return err
	}
	options.tlsConfig = prepared.config
	options.runtime = prepared.runtime
	store, err := options.NewStore(ctx, options.ConfigDir)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	return runServer(ctx, store, options)
}

func applyServeInternalDefaults(options *ServeOptions) {
	if options.ConfigDir == "" {
		options.ConfigDir = defaultConfigsDir
	}
	if options.NewStore == nil {
		options.NewStore = NewConfigStore
	}
}

func validateServeOptions(options ServeOptions) error {
	if options.ManagedRoot != "" && options.TLS.Enabled() {
		return errors.New("managed development certificates cannot be combined with explicit server TLS inputs")
	}
	if err := options.TLS.Validate(); err != nil {
		return err
	}
	if options.HandlerOptions.TrustedProxyHeaders {
		if !options.TLS.Enabled() && options.ManagedRoot == "" {
			return errors.New("trusted proxy mode requires TLS")
		}
		if len(options.HandlerOptions.TrustedProxyClientCNs) == 0 {
			return errors.New("trusted proxy mode requires at least one trusted proxy client CN")
		}
	}
	return nil
}

type preparedServerTLS struct {
	config  *tls.Config
	runtime *managedRuntime
}

func prepareServeTLS(options ServeOptions) (preparedServerTLS, error) {
	if options.ManagedRoot == "" {
		config, err := loadServerTLSConfig(options.TLS)
		return preparedServerTLS{config: config}, err
	}
	version, err := devcerts.ResolveManagedVersion(options.ManagedRoot)
	if err != nil {
		return preparedServerTLS{}, fmt.Errorf("resolve managed server certificate version: %w", err)
	}
	material, err := devcerts.LoadManagedServer(version)
	if err != nil {
		return preparedServerTLS{}, fmt.Errorf("load managed server TLS: %w", err)
	}
	var runtime *managedRuntime
	if options.RuntimeDir != "" {
		runtime = &managedRuntime{version: version.RelativePath(), ca: material.CA}
	}
	return preparedServerTLS{config: &tls.Config{
		Certificates: []tls.Certificate{material.Certificate},
		ClientCAs:    material.ClientCAs,
		ClientAuth:   tls.VerifyClientCertIfGiven,
		MinVersion:   tls.VersionTLS12,
	}, runtime: runtime}, nil
}

func runServer(ctx context.Context, store *ConfigStore, options ServeOptions) error {
	shutdownRequested := make(chan struct{}, 1)
	handlerOptions := options.HandlerOptions
	handlerOptions.RequireAuthentication = true

	listener, err := net.Listen("tcp", options.Addr)
	if err != nil {
		return err
	}
	if options.runtime != nil {
		if err := publishManagedRuntime(options.RuntimeDir, *options.runtime, defaultRuntimeOperations); err != nil {
			return errors.Join(err, listener.Close())
		}
	}
	checker := Checker{
		ConfigStore:        store,
		BlockOnWarmup:      options.BlockOnWarmup,
		AnalyzerTimeout:    options.AnalyzerTimeout,
		ConcurrencyPermits: handlerOptions.MaxConcurrentAnalysesPerRepo,
	}
	server := &http.Server{
		Addr: options.Addr,
		Handler: NewHandlerWithOptions(checker, func() {
			select {
			case shutdownRequested <- struct{}{}:
			default:
			}
		}, handlerOptions),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
	}
	if options.tlsConfig != nil {
		server.TLSConfig = options.tlsConfig
		listener = tlsListener(listener, server.TLSConfig)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(listener) }()
	if options.Ready != nil {
		_, _ = fmt.Fprintln(options.Ready, "ready")
	}
	return awaitShutdown(ctx, server, shutdownRequested, errCh)
}

func awaitShutdown(ctx context.Context, server *http.Server, shutdownRequested <-chan struct{}, errCh <-chan error) error {
	select {
	case <-ctx.Done():
		return shutdown(server, ctx.Err())
	case <-shutdownRequested:
		return shutdown(server, nil)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func isInputError(err error) bool {
	var checkErr *CheckError
	return errors.As(err, &checkErr) && checkErr.Kind == ErrorKindInput
}

func isMaxBytesError(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func tlsListener(listener net.Listener, config *tls.Config) net.Listener {
	if config == nil {
		return listener
	}
	return tls.NewListener(listener, config)
}

func shutdown(server *http.Server, returnErr error) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return returnErr
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		http.Error(w, "encoding response failed", http.StatusInternalServerError)
	}
}

func writeCheckError(w http.ResponseWriter, err error) {
	var checkErr *CheckError
	if errors.As(err, &checkErr) {
		switch checkErr.Kind {
		case ErrorKindInput:
			http.Error(w, checkErr.Message, http.StatusBadRequest)
			return
		case ErrorKindInfrastructure:
			http.Error(w, checkErr.Message, http.StatusServiceUnavailable)
			return
		case ErrorKindNotFound:
			http.Error(w, checkErr.Message, http.StatusNotFound)
			return
		case ErrorKindTimeout:
			http.Error(w, checkErr.Message, http.StatusGatewayTimeout)
			return
		}
	}
	http.Error(w, "check failed", http.StatusInternalServerError)
}

func buildConfigsResponse(loadedAt time.Time, snapshot map[string]ConfigEntry) ConfigsResponse {
	response := ConfigsResponse{
		LoadedAt: loadedAt,
		Repos:    make(map[string]ConfigsRepoResponse, len(snapshot)),
	}
	for repo, entry := range snapshot {
		repoResponse := ConfigsRepoResponse{Status: "invalid", Error: entry.Error}
		if entry.Valid {
			repoResponse.Status = "valid"
			repoResponse.EnforcementMode = entry.Config.EnforcementMode
			repoResponse.FitnessFunctions = cloneFitnessFunctions(entry.Config.FitnessFunctions)
		} else if !entry.LastValidAt.IsZero() {
			lastValidAt := entry.LastValidAt
			repoResponse.LastValidAt = &lastValidAt
		}
		response.Repos[repo] = repoResponse
	}
	response.Version = configsResponseVersion(response.LoadedAt, response.Repos)
	return response
}

func configsResponseVersion(loadedAt time.Time, repos map[string]ConfigsRepoResponse) string {
	payload, err := json.Marshal(struct {
		LoadedAt time.Time                      `json:"loaded_at"`
		Repos    map[string]ConfigsRepoResponse `json:"repos"`
	}{LoadedAt: loadedAt, Repos: repos})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func cloneFitnessFunctions(functions map[string]bool) map[string]bool {
	if functions == nil {
		return nil
	}
	cloned := make(map[string]bool, len(functions))
	for name, enabled := range functions {
		cloned[name] = enabled
	}
	return cloned
}
